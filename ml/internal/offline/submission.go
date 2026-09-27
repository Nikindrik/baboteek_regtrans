package offline

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"mosgortrans-ml-go/internal/core"
)

type Result struct {
	Rows       int
	OutputPath string
}

type pointRow struct {
	SampleID     string
	TrID         int64
	T            time.Time
	TargetStopID int64
	TargetTime   time.Time
	CurDevS      float64
}

type stopRow struct {
	StopID int64
	TrID   int64
	Time   time.Time
	Lon    float64
	Lat    float64
	Order  int32
	GapS   float64
}

type telemetryRow struct {
	Event core.TelemetryEvent
	Time  time.Time
}

var pointRE = regexp.MustCompile(`(?i)^\s*POINT\s*\(\s*([-+0-9.eE]+)\s+([-+0-9.eE]+)\s*\)\s*$`)

// GenerateSubmission reproduces the offline validate path using the exact same
// stateful feature builder and ONNX inference used by the live gRPC service.
// Only telemetry with event_time <= T is ingested for each prediction point.
func GenerateSubmission(svc *core.Service, datasetDir, outputPath string) (Result, error) {
	validateDir := filepath.Join(datasetDir, "validate")

	points, err := loadPoints(filepath.Join(validateDir, "points.csv"))
	if err != nil {
		return Result{}, err
	}
	traffic, err := loadTraffic(filepath.Join(validateDir, "traffic.csv"))
	if err != nil {
		return Result{}, err
	}
	stops, stopByID, err := loadSchedule(filepath.Join(validateDir, "schedule_plan.csv"))
	if err != nil {
		return Result{}, err
	}
	order, err := loadSubmissionOrder(filepath.Join(datasetDir, "sample_submission.csv"))
	if err != nil {
		return Result{}, err
	}

	if len(points) != len(order) {
		return Result{}, fmt.Errorf("validate/template row mismatch: points=%d template=%d", len(points), len(order))
	}

	pointByID := make(map[string]pointRow, len(points))
	for _, p := range points {
		if _, exists := pointByID[p.SampleID]; exists {
			return Result{}, fmt.Errorf("duplicate sample_id in validate/points.csv: %s", p.SampleID)
		}
		pointByID[p.SampleID] = p
	}
	for _, id := range order {
		if _, ok := pointByID[id]; !ok {
			return Result{}, fmt.Errorf("sample_id from sample_submission.csv not found in validate/points.csv: %s", id)
		}
	}

	// Predict chronologically so PointHistory gets the same causal 5-minute
	// snapshots that the production service builds online.
	sort.Slice(points, func(i, j int) bool {
		if points[i].T.Equal(points[j].T) {
			if points[i].TrID == points[j].TrID {
				return points[i].SampleID < points[j].SampleID
			}
			return points[i].TrID < points[j].TrID
		}
		return points[i].T.Before(points[j].T)
	})

	trafficPos := make(map[int64]int, len(traffic))
	predictions := make(map[string]float64, len(points))

	for _, p := range points {
		rows := traffic[p.TrID]
		pos := trafficPos[p.TrID]
		for pos < len(rows) && !rows[pos].Time.After(p.T) {
			resp := svc.Ingest(core.IngestRequest{Telemetry: rows[pos].Event})
			if !resp.Accepted && resp.Status != "stale_event" {
				return Result{}, fmt.Errorf("ingest %s tr_id=%d: %s", p.SampleID, p.TrID, resp.Status)
			}
			pos++
		}
		trafficPos[p.TrID] = pos

		target, ok := stopByID[p.TrID][p.TargetStopID]
		if !ok {
			return Result{}, fmt.Errorf("target stop not found in schedule: sample_id=%s tr_id=%d stop_id=%d", p.SampleID, p.TrID, p.TargetStopID)
		}
		if math.Abs(target.Time.Sub(p.TargetTime).Seconds()) > 1 {
			return Result{}, fmt.Errorf("target time mismatch for %s: points=%s schedule=%s", p.SampleID, p.TargetTime.Format(time.RFC3339), target.Time.Format(time.RFC3339))
		}

		scheduleState := inferScheduleState(stops[p.TrID], p.T, p.CurDevS)
		resp := svc.Predict(core.PredictRequest{
			TrID:              p.TrID,
			RequestTimeUnixMS: p.T.UnixMilli(),
			Schedule:          scheduleState,
			Target: core.TargetPoint{
				Valid:             true,
				StopID:            target.StopID,
				TargetTimeUnixMS:  target.Time.UnixMilli(),
				Order:             target.Order,
				Lon:               target.Lon,
				Lat:               target.Lat,
				ScheduledPrevGapS: target.GapS,
			},
		})
		if resp.Status != "ok" {
			return Result{}, fmt.Errorf("predict %s tr_id=%d: %s", p.SampleID, p.TrID, resp.Status)
		}
		if math.IsNaN(resp.PredictedDelayS) || math.IsInf(resp.PredictedDelayS, 0) {
			return Result{}, fmt.Errorf("non-finite prediction for %s", p.SampleID)
		}
		predictions[p.SampleID] = resp.PredictedDelayS
	}

	if len(predictions) != len(order) {
		return Result{}, fmt.Errorf("prediction count mismatch: got=%d expected=%d", len(predictions), len(order))
	}
	if err := writeSubmission(outputPath, order, predictions); err != nil {
		return Result{}, err
	}
	return Result{Rows: len(order), OutputPath: outputPath}, nil
}

func loadPoints(path string) ([]pointRow, error) {
	r, closeFn, err := openCSV(path, ',')
	if err != nil {
		return nil, err
	}
	defer closeFn()

	header, err := r.Read()
	if err != nil {
		return nil, fmt.Errorf("read %s header: %w", path, err)
	}
	idx, err := requireColumns(header, "sample_id", "tr_id", "T", "target_stop_id", "target_time_begin", "cur_dev_s")
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	var out []pointRow
	for line := 2; ; line++ {
		rec, err := r.Read()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("%s line %d: %w", path, line, err)
		}
		trID, err := parseInt64(rec[idx["tr_id"]])
		if err != nil {
			return nil, fmt.Errorf("%s line %d tr_id: %w", path, line, err)
		}
		stopID, err := parseInt64(rec[idx["target_stop_id"]])
		if err != nil {
			return nil, fmt.Errorf("%s line %d target_stop_id: %w", path, line, err)
		}
		T, err := parseDatasetTime(rec[idx["T"]])
		if err != nil {
			return nil, fmt.Errorf("%s line %d T: %w", path, line, err)
		}
		targetT, err := parseDatasetTime(rec[idx["target_time_begin"]])
		if err != nil {
			return nil, fmt.Errorf("%s line %d target_time_begin: %w", path, line, err)
		}
		cur, err := strconv.ParseFloat(strings.TrimSpace(rec[idx["cur_dev_s"]]), 64)
		if err != nil {
			return nil, fmt.Errorf("%s line %d cur_dev_s: %w", path, line, err)
		}
		out = append(out, pointRow{SampleID: rec[idx["sample_id"]], TrID: trID, T: T, TargetStopID: stopID, TargetTime: targetT, CurDevS: cur})
	}
	return out, nil
}

func loadTraffic(path string) (map[int64][]telemetryRow, error) {
	r, closeFn, err := openCSV(path, ',')
	if err != nil {
		return nil, err
	}
	defer closeFn()

	header, err := r.Read()
	if err != nil {
		return nil, fmt.Errorf("read %s header: %w", path, err)
	}
	idx, err := requireColumns(header, "tr_id", "unit_id", "event_time", "location_valid", "lon", "lat", "speed", "heading")
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}

	out := map[int64][]telemetryRow{}
	for line := 2; ; line++ {
		rec, err := r.Read()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("%s line %d: %w", path, line, err)
		}
		trID, err := parseInt64(rec[idx["tr_id"]])
		if err != nil {
			return nil, fmt.Errorf("%s line %d tr_id: %w", path, line, err)
		}
		unitID, err := parseInt64(rec[idx["unit_id"]])
		if err != nil {
			return nil, fmt.Errorf("%s line %d unit_id: %w", path, line, err)
		}
		T, err := parseDatasetTime(rec[idx["event_time"]])
		if err != nil {
			return nil, fmt.Errorf("%s line %d event_time: %w", path, line, err)
		}
		valid, err := strconv.ParseBool(strings.TrimSpace(rec[idx["location_valid"]]))
		if err != nil {
			return nil, fmt.Errorf("%s line %d location_valid: %w", path, line, err)
		}
		lat := parseOptionalFloat(rec[idx["lat"]])
		lon := parseOptionalFloat(rec[idx["lon"]])
		speed := parseOptionalFloat(rec[idx["speed"]])
		heading := parseOptionalFloat(rec[idx["heading"]])
		e := core.TelemetryEvent{
			UnitID:          unitID,
			TrID:            trID,
			EventTimeUnixMS: T.UnixMilli(),
			Lat:             lat,
			Lon:             lon,
			SpeedKMH:        speed,
			HeadingDeg:      heading,
			LocationValid:   valid,
		}
		out[trID] = append(out[trID], telemetryRow{Event: e, Time: T})
	}
	for id := range out {
		sort.SliceStable(out[id], func(i, j int) bool { return out[id][i].Time.Before(out[id][j].Time) })
	}
	return out, nil
}

func loadSchedule(path string) (map[int64][]stopRow, map[int64]map[int64]stopRow, error) {
	r, closeFn, err := openCSV(path, ',')
	if err != nil {
		return nil, nil, err
	}
	defer closeFn()

	header, err := r.Read()
	if err != nil {
		return nil, nil, fmt.Errorf("read %s header: %w", path, err)
	}
	idx, err := requireColumns(header, "tt_action_item_id", "tr_id", "time_begin", "geom")
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", path, err)
	}

	byTr := map[int64][]stopRow{}
	for line := 2; ; line++ {
		rec, err := r.Read()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, nil, fmt.Errorf("%s line %d: %w", path, line, err)
		}
		stopID, err := parseInt64(rec[idx["tt_action_item_id"]])
		if err != nil {
			return nil, nil, fmt.Errorf("%s line %d stop id: %w", path, line, err)
		}
		trID, err := parseInt64(rec[idx["tr_id"]])
		if err != nil {
			return nil, nil, fmt.Errorf("%s line %d tr_id: %w", path, line, err)
		}
		T, err := parseDatasetTime(rec[idx["time_begin"]])
		if err != nil {
			return nil, nil, fmt.Errorf("%s line %d time_begin: %w", path, line, err)
		}
		lon, lat := parsePoint(rec[idx["geom"]])
		byTr[trID] = append(byTr[trID], stopRow{StopID: stopID, TrID: trID, Time: T, Lon: lon, Lat: lat, GapS: math.NaN()})
	}

	byID := map[int64]map[int64]stopRow{}
	for trID, rows := range byTr {
		sort.SliceStable(rows, func(i, j int) bool {
			if rows[i].Time.Equal(rows[j].Time) {
				return rows[i].StopID < rows[j].StopID
			}
			return rows[i].Time.Before(rows[j].Time)
		})
		for i := range rows {
			rows[i].Order = int32(i)
			if i > 0 {
				rows[i].GapS = rows[i].Time.Sub(rows[i-1].Time).Seconds()
			}
		}
		byTr[trID] = rows
		m := make(map[int64]stopRow, len(rows))
		for _, s := range rows {
			if _, exists := m[s.StopID]; exists {
				return nil, nil, fmt.Errorf("duplicate stop_id=%d for tr_id=%d in schedule", s.StopID, trID)
			}
			m[s.StopID] = s
		}
		byID[trID] = m
	}
	return byTr, byID, nil
}

func inferScheduleState(stops []stopRow, T time.Time, curDevS float64) core.ScheduleState {
	state := core.ScheduleState{
		Valid:               true,
		CurDevS:             curDevS,
		LastKnownFactDelayS: curDevS,
		MatchConfidence:     1.0,
	}
	best := -1
	bestFact := time.Time{}
	for i, s := range stops {
		fact := s.Time.Add(time.Duration(curDevS * float64(time.Second)))
		if !fact.After(T) && (best < 0 || fact.After(bestFact)) {
			best = i
			bestFact = fact
		}
	}
	if best >= 0 {
		state.LastStopID = stops[best].StopID
		state.LastStopOrder = stops[best].Order
		state.SecondsSinceLastFact = math.Max(0, T.Sub(bestFact).Seconds())
	} else {
		// cur_dev_s is explicitly supplied by the official prediction point. If
		// the plan does not expose a preceding stop, keep the state valid but
		// mark the auxiliary recency feature as missing rather than inventing it.
		state.SecondsSinceLastFact = math.NaN()
	}
	return state
}

func loadSubmissionOrder(path string) ([]string, error) {
	r, closeFn, err := openCSV(path, ';')
	if err != nil {
		return nil, err
	}
	defer closeFn()
	header, err := r.Read()
	if err != nil {
		return nil, fmt.Errorf("read %s header: %w", path, err)
	}
	idx, err := requireColumns(header, "sample_id", "prediction")
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	seen := map[string]bool{}
	var out []string
	for line := 2; ; line++ {
		rec, err := r.Read()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("%s line %d: %w", path, line, err)
		}
		id := strings.TrimSpace(rec[idx["sample_id"]])
		if seen[id] {
			return nil, fmt.Errorf("duplicate sample_id in sample_submission.csv: %s", id)
		}
		seen[id] = true
		out = append(out, id)
	}
	return out, nil
}

func writeSubmission(path string, order []string, pred map[string]float64) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil && filepath.Dir(path) != "." {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	w.Comma = ';'
	if err := w.Write([]string{"sample_id", "prediction"}); err != nil {
		return err
	}
	for _, id := range order {
		v, ok := pred[id]
		if !ok {
			return fmt.Errorf("missing prediction for %s", id)
		}
		if err := w.Write([]string{id, strconv.FormatFloat(v, 'f', 6, 64)}); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}

func openCSV(path string, comma rune) (*csv.Reader, func(), error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, fmt.Errorf("open %s: %w", path, err)
	}
	r := csv.NewReader(f)
	r.Comma = comma
	r.ReuseRecord = false
	return r, func() { _ = f.Close() }, nil
}

func requireColumns(header []string, names ...string) (map[string]int, error) {
	idx := make(map[string]int, len(header))
	for i, h := range header {
		idx[strings.TrimSpace(strings.TrimPrefix(h, "\ufeff"))] = i
	}
	for _, n := range names {
		if _, ok := idx[n]; !ok {
			return nil, fmt.Errorf("missing required column %q", n)
		}
	}
	return idx, nil
}

func parseDatasetTime(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	for _, layout := range []string{
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
		time.RFC3339Nano,
	} {
		if t, err := time.ParseInLocation(layout, s, time.UTC); err == nil {
			return t.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported time %q", s)
}

func parseInt64(s string) (int64, error) {
	return strconv.ParseInt(strings.TrimSpace(s), 10, 64)
}

func parseOptionalFloat(s string) float64 {
	s = strings.TrimSpace(s)
	if s == "" || strings.EqualFold(s, "nan") || strings.EqualFold(s, "null") {
		return math.NaN()
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return math.NaN()
	}
	return v
}

func parsePoint(s string) (lon, lat float64) {
	m := pointRE.FindStringSubmatch(strings.TrimSpace(s))
	if len(m) != 3 {
		return math.NaN(), math.NaN()
	}
	lon, err1 := strconv.ParseFloat(m[1], 64)
	lat, err2 := strconv.ParseFloat(m[2], 64)
	if err1 != nil || err2 != nil {
		return math.NaN(), math.NaN()
	}
	return lon, lat
}
