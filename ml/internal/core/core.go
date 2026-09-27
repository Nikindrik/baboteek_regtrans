package core

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	EarthR          = 6_371_000.0
	LocalHistorySec = 3600.0
	SnapshotSec     = 300.0
	StopSpeedKMH    = 2.0
	RiskYellowP     = 0.35
	RiskRedP        = 0.65
	ModelVersion    = "final_top96_seed42_onnx_go_v1"
)

var RollingWindowsSec = []int{60, 180, 300, 600, 900}

var ProductionFeatures = []string{
	"cur_dev_s", "horizon_s", "last_speed_kmh", "cur_dev_lag6", "target_order_in_vehicle",
	"cur_dev_lag12", "hour_cos", "heading_to_target_sin", "last_lat", "heading_to_target_cos",
	"hour_sin", "cur_dev_delta2", "cur_dev_accel", "cur_dev_rollstd12", "last_heading_sin",
	"cur_dev_lag3", "target_lat_sched", "last_lon", "seconds_since_last_fact", "cur_dev_rollstd6",
	"last_heading_cos", "straightline_to_target_m", "target_lon_sched", "speed_slope_kmh_per_min_15m",
	"speed_slope_kmh_per_min_1m", "required_speed_to_target_kmh", "cur_dev_rollstd3",
	"points_in_block", "telemetry_staleness_s", "last_known_fact_delay_s", "cur_dev_delta1",
	"location_valid_share_15m", "cur_dev_lag2", "cur_dev_rollstd2", "speed_slope_kmh_per_min_5m",
	"speed_slope_kmh_per_min_10m", "cur_dev_rollmean12", "last_heading", "speed_slope_kmh_per_min_3m",
	"location_valid_share_10m", "abs_cur_dev_s", "speed_trend_1m_vs_5m", "stopped_share_10m",
	"speed_margin_to_required_kmh", "cur_dev_rollmin12", "displacement_15m_m", "displacement_10m_m",
	"telemetry_obs_15m", "stopped_share_15m", "displacement_5m_m", "minute", "scheduled_prev_gap_s",
	"cur_dev_rollmax12", "location_valid_share_5m", "telemetry_obs_10m", "cur_dev_rollmin6",
	"cur_dev_lag1", "location_valid_share_3m", "stopped_share_5m", "stopped_share_3m", "speed_max_15m",
	"eta_at_last_speed_s", "path_distance_1m_m", "speed_std_1m", "path_distance_15m_m",
	"displacement_1m_m", "cur_dev_rollmean6", "speed_std_5m", "speed_std_15m", "cur_dev_rollmax6",
	"telemetry_obs_5m", "speed_max_10m", "speed_std_3m", "displacement_3m_m", "telemetry_obs_3m",
	"speed_mean_15m", "cur_dev_per_horizon_min", "speed_max_1m", "speed_mean_1m", "path_distance_5m_m",
	"cur_dev_rollmin3", "cur_dev_rollmax3", "eta_minus_horizon_s", "path_distance_10m_m",
	"speed_std_10m", "speed_max_3m", "stopped_share_1m", "path_distance_3m_m", "speed_mean_3m",
	"location_valid_share_1m", "speed_max_5m", "speed_mean_10m", "cur_dev_rollmin2", "hour",
	"cur_dev_rollmean2", "speed_mean_5m",
}

// =========================
// Wire contract (HTTP/JSON)
// =========================

type TelemetryEvent struct {
	UnitID          int64   `json:"unit_id"`
	TrID            int64   `json:"tr_id"`
	EventTimeUnixMS int64   `json:"event_time_unix_ms"`
	Lat             float64 `json:"lat"`
	Lon             float64 `json:"lon"`
	SpeedKMH        float64 `json:"speed_kmh"`
	HeadingDeg      float64 `json:"heading_deg"`
	LocationValid   bool    `json:"location_valid"`
}

type ScheduleState struct {
	Valid                bool    `json:"valid"`
	CurDevS              float64 `json:"cur_dev_s"`
	LastKnownFactDelayS  float64 `json:"last_known_fact_delay_s"`
	SecondsSinceLastFact float64 `json:"seconds_since_last_fact"`
	LastStopID           int64   `json:"last_stop_id"`
	LastStopOrder        int32   `json:"last_stop_order"`
	MatchConfidence      float64 `json:"match_confidence"`
}

type TargetPoint struct {
	Valid             bool    `json:"valid"`
	StopID            int64   `json:"stop_id"`
	TargetTimeUnixMS  int64   `json:"target_time_unix_ms"`
	Order             int32   `json:"order"`
	Lon               float64 `json:"-"`
	Lat               float64 `json:"-"`
	ScheduledPrevGapS float64 `json:"-"`
}

func rawFloatOrNaN(raw json.RawMessage) float64 {
	if len(raw) == 0 || string(raw) == "null" {
		return math.NaN()
	}
	var v float64
	if err := json.Unmarshal(raw, &v); err != nil {
		return math.NaN()
	}
	return v
}
func (t *TargetPoint) UnmarshalJSON(b []byte) error {
	var w struct {
		Valid            bool            `json:"valid"`
		StopID           int64           `json:"stop_id"`
		TargetTimeUnixMS int64           `json:"target_time_unix_ms"`
		Order            int32           `json:"order"`
		Lon              json.RawMessage `json:"lon"`
		Lat              json.RawMessage `json:"lat"`
		Gap              json.RawMessage `json:"scheduled_prev_gap_s"`
	}
	if err := json.Unmarshal(b, &w); err != nil {
		return err
	}
	t.Valid = w.Valid
	t.StopID = w.StopID
	t.TargetTimeUnixMS = w.TargetTimeUnixMS
	t.Order = w.Order
	t.Lon = rawFloatOrNaN(w.Lon)
	t.Lat = rawFloatOrNaN(w.Lat)
	t.ScheduledPrevGapS = rawFloatOrNaN(w.Gap)
	return nil
}

type IngestRequest struct {
	Telemetry TelemetryEvent `json:"telemetry"`
	Schedule  ScheduleState  `json:"schedule"`
}
type IngestResponse struct {
	Accepted bool   `json:"accepted"`
	Status   string `json:"status"`
	TrID     int64  `json:"tr_id"`
}
type PredictRequest struct {
	TrID              int64         `json:"tr_id"`
	RequestTimeUnixMS int64         `json:"request_time_unix_ms"`
	Schedule          ScheduleState `json:"schedule"`
	Target            TargetPoint   `json:"target"`
}
type PredictionResponse struct {
	Status              string  `json:"status"`
	TrID                int64   `json:"tr_id"`
	TargetStopID        int64   `json:"target_stop_id"`
	TargetTimeUnixMS    int64   `json:"target_time_unix_ms"`
	HorizonS            float64 `json:"horizon_s"`
	CurrentDelayS       float64 `json:"current_delay_s"`
	PredictedDeltaS     float64 `json:"predicted_delta_s"`
	PredictedDelayS     float64 `json:"predicted_delay_s"`
	LateProbability     float64 `json:"late_probability"`
	RiskLevel           string  `json:"risk_level"`
	Reason              string  `json:"reason"`
	ReasonConfidence    float64 `json:"reason_confidence"`
	TelemetryStalenessS float64 `json:"telemetry_staleness_s"`
	MLLatencyMS         float64 `json:"ml_latency_ms"`
}
type HealthResponse struct {
	OK              bool   `json:"ok"`
	Status          string `json:"status"`
	ModelVersion    string `json:"model_version"`
	TelemetryEvents int64  `json:"telemetry_events"`
	Predictions     int64  `json:"predictions"`
}

// =========================
// ONNX manifest
// =========================

type ModelInfo struct {
	File               string `json:"file"`
	SHA256             string `json:"sha256"`
	InputName          string `json:"input_name"`
	OutputName         string `json:"output_name"`
	PositiveClassIndex int    `json:"positive_class_index"`
}
type Manifest struct {
	FormatVersion int      `json:"format_version"`
	FeatureCount  int      `json:"feature_count"`
	Features      []string `json:"features"`
	Models        struct {
		Residual ModelInfo `json:"residual"`
		Risk     ModelInfo `json:"risk"`
	} `json:"models"`
}

func LoadManifest(modelDir string) (Manifest, error) {
	var m Manifest
	b, e := os.ReadFile(filepath.Join(modelDir, "model_manifest.json"))
	if e != nil {
		return m, e
	}
	if e = json.Unmarshal(b, &m); e != nil {
		return m, e
	}
	if m.FeatureCount != len(ProductionFeatures) || len(m.Features) != len(ProductionFeatures) {
		return m, fmt.Errorf("feature count mismatch: manifest=%d code=%d", m.FeatureCount, len(ProductionFeatures))
	}
	for i := range ProductionFeatures {
		if m.Features[i] != ProductionFeatures[i] {
			return m, fmt.Errorf("feature %d mismatch: manifest=%s code=%s", i, m.Features[i], ProductionFeatures[i])
		}
	}
	return m, nil
}
func SHA256File(path string) (string, error) {
	f, e := os.Open(path)
	if e != nil {
		return "", e
	}
	defer f.Close()
	h := sha256.New()
	if _, e = io.Copy(h, f); e != nil {
		return "", e
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func VerifyModelFiles(modelDir string, m Manifest) error {
	for _, x := range []ModelInfo{m.Models.Residual, m.Models.Risk} {
		p := filepath.Join(modelDir, x.File)
		got, e := SHA256File(p)
		if e != nil {
			return e
		}
		if x.SHA256 != "" && got != x.SHA256 {
			return fmt.Errorf("SHA256 mismatch %s expected=%s actual=%s", x.File, x.SHA256, got)
		}
	}
	return nil
}

// =========================
// Feature state
// =========================

type HistEvent struct {
	EventTime                time.Time
	UnitID, TrID             int64
	Lat, Lon, Speed, Heading float64
	LocationValid            bool
}
type VehicleHistory struct{ Events map[int64][]HistEvent }

func NewVehicleHistory() *VehicleHistory { return &VehicleHistory{Events: map[int64][]HistEvent{}} }
func (v *VehicleHistory) Update(id int64, e HistEvent) {
	q := append(v.Events[id], e)
	cut := e.EventTime.Add(-time.Duration(LocalHistorySec) * time.Second)
	k := 0
	for k < len(q) && q[k].EventTime.Before(cut) {
		k++
	}
	if k > 0 {
		q = append([]HistEvent(nil), q[k:]...)
	}
	v.Events[id] = q
}
func (v *VehicleHistory) RowsAt(id int64, T time.Time) []HistEvent {
	q := v.Events[id]
	n := len(q)
	for n > 0 && q[n-1].EventTime.After(T) {
		n--
	}
	return q[:n]
}

type pointSnap struct {
	T time.Time
	V float64
}
type PointHistory struct{ Hist map[int64][]pointSnap }

func NewPointHistory() *PointHistory { return &PointHistory{Hist: map[int64][]pointSnap{}} }
func (p *PointHistory) MaybeSnapshot(id int64, T time.Time, cur float64) {
	q := p.Hist[id]
	if len(q) > 0 && T.Sub(q[len(q)-1].T) > 30*time.Minute {
		q = nil
	}
	if len(q) == 0 || T.Sub(q[len(q)-1].T) >= time.Duration(SnapshotSec)*time.Second {
		q = append(q, pointSnap{T, cur})
		cut := T.Add(-time.Hour)
		k := 0
		for k < len(q) && q[k].T.Before(cut) {
			k++
		}
		if k > 0 {
			q = append([]pointSnap(nil), q[k:]...)
		}
	}
	p.Hist[id] = q
}
func (p *PointHistory) Features(id int64, T time.Time, current float64) map[string]float64 {
	q := p.Hist[id]
	seq := make([]float64, 0, len(q)+1)
	for _, s := range q {
		if s.T.Before(T) {
			seq = append(seq, s.V)
		}
	}
	seq = append(seq, current)
	f := map[string]float64{}
	for _, lag := range []int{1, 2, 3, 6, 12} {
		k := len(seq) - 1 - lag
		if k >= 0 {
			f[fmt.Sprintf("cur_dev_lag%d", lag)] = seq[k]
		} else {
			f[fmt.Sprintf("cur_dev_lag%d", lag)] = math.NaN()
		}
	}
	f["cur_dev_delta1"] = current - f["cur_dev_lag1"]
	f["cur_dev_delta2"] = current - f["cur_dev_lag2"]
	f["cur_dev_accel"] = f["cur_dev_delta1"] - (f["cur_dev_lag1"] - f["cur_dev_lag2"])
	for _, w := range []int{2, 3, 6, 12} {
		start := len(seq) - w
		if start < 0 {
			start = 0
		}
		a := seq[start:]
		f[fmt.Sprintf("cur_dev_rollmean%d", w)] = nanMean(a)
		f[fmt.Sprintf("cur_dev_rollstd%d", w)] = nanStdSample(a)
		f[fmt.Sprintf("cur_dev_rollmin%d", w)] = nanMin(a)
		f[fmt.Sprintf("cur_dev_rollmax%d", w)] = nanMax(a)
	}
	f["points_in_block"] = float64(len(seq))
	return f
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
func nanMean(a []float64) float64 {
	s := 0.0
	n := 0
	for _, v := range a {
		if finite(v) {
			s += v
			n++
		}
	}
	if n == 0 {
		return math.NaN()
	}
	return s / float64(n)
}
func nanStdPop(a []float64) float64 {
	m := nanMean(a)
	if !finite(m) {
		return math.NaN()
	}
	s := 0.0
	n := 0
	for _, v := range a {
		if finite(v) {
			d := v - m
			s += d * d
			n++
		}
	}
	if n == 0 {
		return math.NaN()
	}
	return math.Sqrt(s / float64(n))
}
func nanStdSample(a []float64) float64 {
	m := nanMean(a)
	if !finite(m) {
		return math.NaN()
	}
	s := 0.0
	n := 0
	for _, v := range a {
		if finite(v) {
			d := v - m
			s += d * d
			n++
		}
	}
	if n < 2 {
		return math.NaN()
	}
	return math.Sqrt(s / float64(n-1))
}
func nanMin(a []float64) float64 {
	x := math.Inf(1)
	ok := false
	for _, v := range a {
		if finite(v) && v < x {
			x = v
			ok = true
		}
	}
	if !ok {
		return math.NaN()
	}
	return x
}
func nanMax(a []float64) float64 {
	x := math.Inf(-1)
	ok := false
	for _, v := range a {
		if finite(v) && v > x {
			x = v
			ok = true
		}
	}
	if !ok {
		return math.NaN()
	}
	return x
}
func HaversineM(lat1, lon1, lat2, lon2 float64) float64 {
	if !finite(lat1) || !finite(lon1) || !finite(lat2) || !finite(lon2) {
		return math.NaN()
	}
	p1, p2 := lat1*math.Pi/180, lat2*math.Pi/180
	dp := p2 - p1
	dl := (lon2 - lon1) * math.Pi / 180
	a := math.Sin(dp/2)*math.Sin(dp/2) + math.Cos(p1)*math.Cos(p2)*math.Sin(dl/2)*math.Sin(dl/2)
	if a < 0 {
		a = 0
	}
	if a > 1 {
		a = 1
	}
	return 2 * EarthR * math.Asin(math.Sqrt(a))
}
func BearingDeg(lat1, lon1, lat2, lon2 float64) float64 {
	a1, a2 := lat1*math.Pi/180, lat2*math.Pi/180
	dl := (lon2 - lon1) * math.Pi / 180
	y := math.Sin(dl) * math.Cos(a2)
	x := math.Cos(a1)*math.Sin(a2) - math.Sin(a1)*math.Cos(a2)*math.Cos(dl)
	return math.Mod(math.Atan2(y, x)*180/math.Pi+360, 360)
}
func linearSlopePerMin(times, values []float64) float64 {
	xs := []float64{}
	ys := []float64{}
	for i := range times {
		if finite(times[i]) && finite(values[i]) {
			xs = append(xs, times[i])
			ys = append(ys, values[i])
		}
	}
	if len(xs) < 2 {
		return math.NaN()
	}
	last := xs[len(xs)-1]
	for i := range xs {
		xs[i] -= last
	}
	xm, ym := nanMean(xs), nanMean(ys)
	den, num := 0.0, 0.0
	for i := range xs {
		dx := xs[i] - xm
		den += dx * dx
		num += dx * (ys[i] - ym)
	}
	if den <= 1e-9 {
		return math.NaN()
	}
	return num / den * 60
}

func (v *VehicleHistory) Features(id int64, T time.Time) map[string]float64 {
	rows := v.RowsAt(id, T)
	if len(rows) == 0 {
		return map[string]float64{}
	}
	f := map[string]float64{}
	last := rows[len(rows)-1]
	f["telemetry_staleness_s"] = math.Max(0, T.Sub(last.EventTime).Seconds())
	for _, n := range []string{"last_speed_kmh", "last_lat", "last_lon", "last_heading", "last_heading_sin", "last_heading_cos"} {
		f[n] = math.NaN()
	}
	for i := len(rows) - 1; i >= 0; i-- {
		e := rows[i]
		if e.LocationValid && finite(e.Lat) && finite(e.Lon) {
			f["last_speed_kmh"] = e.Speed
			f["last_lat"] = e.Lat
			f["last_lon"] = e.Lon
			f["last_heading"] = e.Heading
			if finite(e.Heading) {
				f["last_heading_sin"] = math.Sin(e.Heading * math.Pi / 180)
				f["last_heading_cos"] = math.Cos(e.Heading * math.Pi / 180)
			}
			break
		}
	}
	for _, w := range RollingWindowsSec {
		cut := T.Add(-time.Duration(w) * time.Second)
		start := 0
		for start < len(rows) && rows[start].EventTime.Before(cut) {
			start++
		}
		sub := rows[start:]
		tag := fmt.Sprintf("%dm", w/60)
		speeds := []float64{}
		validCount := 0
		times := make([]float64, len(sub))
		slopeVals := make([]float64, len(sub))
		validMotion := []HistEvent{}
		for i, e := range sub {
			times[i] = e.EventTime.Sub(last.EventTime).Seconds()
			if e.LocationValid {
				validCount++
			}
			if e.LocationValid && finite(e.Speed) {
				speeds = append(speeds, e.Speed)
				slopeVals[i] = e.Speed
			} else {
				slopeVals[i] = math.NaN()
			}
			if e.LocationValid && finite(e.Lat) && finite(e.Lon) {
				validMotion = append(validMotion, e)
			}
		}
		if len(speeds) > 0 {
			f["speed_mean_"+tag] = nanMean(speeds)
			f["speed_std_"+tag] = nanStdPop(speeds)
			f["speed_max_"+tag] = nanMax(speeds)
			st := 0
			for _, s := range speeds {
				if s <= StopSpeedKMH {
					st++
				}
			}
			f["stopped_share_"+tag] = float64(st) / float64(len(speeds))
		} else {
			f["speed_mean_"+tag] = math.NaN()
			f["speed_std_"+tag] = math.NaN()
			f["speed_max_"+tag] = math.NaN()
			f["stopped_share_"+tag] = math.NaN()
		}
		if len(sub) > 0 {
			f["location_valid_share_"+tag] = float64(validCount) / float64(len(sub))
		} else {
			f["location_valid_share_"+tag] = math.NaN()
		}
		f["telemetry_obs_"+tag] = float64(len(sub))
		if len(sub) >= 2 {
			f["speed_slope_kmh_per_min_"+tag] = linearSlopePerMin(times, slopeVals)
			if len(validMotion) >= 2 {
				a, b := validMotion[0], validMotion[len(validMotion)-1]
				f["displacement_"+tag+"_m"] = HaversineM(a.Lat, a.Lon, b.Lat, b.Lon)
				path := 0.0
				for i := 1; i < len(validMotion); i++ {
					path += HaversineM(validMotion[i-1].Lat, validMotion[i-1].Lon, validMotion[i].Lat, validMotion[i].Lon)
				}
				f["path_distance_"+tag+"_m"] = path
			}
		}
	}
	f["speed_trend_1m_vs_5m"] = f["speed_mean_1m"] - f["speed_mean_5m"]
	return f
}

func addDerived(f map[string]float64) {
	cur, horizon, dist, speed := f["cur_dev_s"], f["horizon_s"], f["straightline_to_target_m"], f["last_speed_kmh"]
	if finite(cur) {
		f["abs_cur_dev_s"] = math.Abs(cur)
	} else {
		f["abs_cur_dev_s"] = math.NaN()
	}
	if finite(cur) && finite(horizon) {
		f["cur_dev_per_horizon_min"] = cur / math.Max(horizon/60, 1e-3)
	} else {
		f["cur_dev_per_horizon_min"] = math.NaN()
	}
	req := math.NaN()
	if finite(dist) && finite(horizon) {
		req = dist / math.Max(horizon, 1) * 3.6
	}
	f["required_speed_to_target_kmh"] = req
	if finite(speed) && finite(req) {
		f["speed_margin_to_required_kmh"] = speed - req
	} else {
		f["speed_margin_to_required_kmh"] = math.NaN()
	}
	if finite(dist) && finite(speed) {
		eta := dist / math.Max(speed/3.6, .5)
		f["eta_at_last_speed_s"] = eta
		if finite(horizon) {
			f["eta_minus_horizon_s"] = eta - horizon
		} else {
			f["eta_minus_horizon_s"] = math.NaN()
		}
	} else {
		f["eta_at_last_speed_s"] = math.NaN()
		f["eta_minus_horizon_s"] = math.NaN()
	}
	vals := []float64{f["last_lat"], f["last_lon"], f["target_lat_sched"], f["target_lon_sched"], f["last_heading"]}
	ok := true
	for _, v := range vals {
		if !finite(v) {
			ok = false
		}
	}
	if ok {
		b := BearingDeg(vals[0], vals[1], vals[2], vals[3])
		diff := math.Mod(b-vals[4]+540, 360) - 180
		rad := diff * math.Pi / 180
		f["heading_to_target_cos"] = math.Cos(rad)
		f["heading_to_target_sin"] = math.Sin(rad)
	} else {
		f["heading_to_target_cos"] = math.NaN()
		f["heading_to_target_sin"] = math.NaN()
	}
}

func riskLevel(p, delay float64) string {
	if p >= RiskRedP || delay >= 180 {
		return "red"
	}
	if p >= RiskYellowP || delay > 120 {
		return "yellow"
	}
	return "green"
}
func inferReason(f map[string]float64, pred, cur float64) (string, float64) {
	if s := f["telemetry_staleness_s"]; finite(s) && s > 60 {
		return "недостаточно свежая телеметрия", .95
	}
	speed, st := f["last_speed_kmh"], f["stopped_share_3m"]
	if finite(speed) && finite(st) && speed <= 2 && st >= .60 {
		return "длительный простой / посадка", .85
	}
	s1, s5 := f["speed_mean_1m"], f["speed_mean_5m"]
	if finite(s1) && finite(s5) && s1 < 10 && s1 < s5-4 {
		return "резкое замедление движения", .72
	}
	d1, acc := f["cur_dev_delta1"], f["cur_dev_accel"]
	if (finite(d1) && d1 > 20) || (finite(acc) && acc > 15) {
		return "нарастающее отставание от графика", .75
	}
	if pred-cur > 30 {
		return "ожидаемое накопление задержки", .60
	}
	if pred < cur-30 {
		return "ожидаемое сокращение текущего отклонения", .55
	}
	return "устойчивое текущее отклонение", .45
}

// =========================
// Model service
// =========================

type Runner interface {
	Run([]float32) ([]float32, error)
	Close() error
}

type Service struct {
	mu                                           sync.Mutex
	Reg, Risk                                    Runner
	RiskPositiveIndex                            int
	Vehicle                                      *VehicleHistory
	Points                                       *PointHistory
	Latest                                       map[int64]HistEvent
	LatestTime                                   map[int64]time.Time
	TelemetryEvents, RejectedEvents, Predictions int64
}

func NewService(reg, risk Runner, riskPositive int) *Service {
	return &Service{Reg: reg, Risk: risk, RiskPositiveIndex: riskPositive, Vehicle: NewVehicleHistory(), Points: NewPointHistory(), Latest: map[int64]HistEvent{}, LatestTime: map[int64]time.Time{}}
}
func (s *Service) Close() {
	if s.Reg != nil {
		_ = s.Reg.Close()
	}
	if s.Risk != nil {
		_ = s.Risk.Close()
	}
}
func (s *Service) Ingest(req IngestRequest) IngestResponse {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := req.Telemetry
	if t.TrID <= 0 {
		return IngestResponse{false, "invalid_tr_id", t.TrID}
	}
	if t.EventTimeUnixMS <= 0 {
		return IngestResponse{false, "invalid_event_time", t.TrID}
	}
	T := time.UnixMilli(t.EventTimeUnixMS).UTC()
	lat, lon, speed, heading := t.Lat, t.Lon, t.SpeedKMH, t.HeadingDeg
	gps := t.LocationValid && finite(lat) && finite(lon) && lat >= -90 && lat <= 90 && lon >= -180 && lon <= 180
	if !gps {
		lat, lon, speed, heading = math.NaN(), math.NaN(), math.NaN(), math.NaN()
	} else {
		if !finite(speed) || speed < 0 || speed > 250 {
			speed = math.NaN()
		}
		if finite(heading) {
			heading = math.Mod(heading, 360)
			if heading < 0 {
				heading += 360
			}
		} else {
			heading = math.NaN()
		}
	}
	if last, ok := s.LatestTime[t.TrID]; ok {
		if T.Equal(last) {
			return IngestResponse{true, "duplicate_event", t.TrID}
		}
		if T.Before(last) {
			s.RejectedEvents++
			return IngestResponse{false, "stale_event", t.TrID}
		}
	}
	e := HistEvent{T, t.UnitID, t.TrID, lat, lon, speed, heading, gps}
	s.Latest[t.TrID] = e
	s.LatestTime[t.TrID] = T
	s.Vehicle.Update(t.TrID, e)
	s.TelemetryEvents++
	return IngestResponse{true, "accepted", t.TrID}
}

func (s *Service) Predict(req PredictRequest) PredictionResponse {
	s.mu.Lock()
	defer s.mu.Unlock()
	start := time.Now()
	id := req.TrID
	T := time.UnixMilli(req.RequestTimeUnixMS).UTC()
	if !req.Schedule.Valid {
		return PredictionResponse{Status: "missing_schedule_state", TrID: id}
	}
	if !req.Target.Valid {
		return PredictionResponse{Status: "missing_target", TrID: id}
	}
	latest, ok := s.Latest[id]
	if !ok {
		return PredictionResponse{Status: "no_telemetry", TrID: id}
	}
	if latest.EventTime.After(T) && len(s.Vehicle.RowsAt(id, T)) == 0 {
		return PredictionResponse{Status: "no_telemetry_at_request_time", TrID: id}
	}
	cur := req.Schedule.CurDevS
	if !finite(cur) {
		return PredictionResponse{Status: "invalid_schedule_state", TrID: id}
	}
	targetTime := time.UnixMilli(req.Target.TargetTimeUnixMS).UTC()
	horizon := targetTime.Sub(T).Seconds()
	if !(horizon > 600 && horizon <= 900) {
		return PredictionResponse{Status: "target_outside_10_15m", TrID: id}
	}
	h := float64(T.Hour()) + float64(T.Minute())/60
	f := map[string]float64{"cur_dev_s": cur, "horizon_s": horizon, "hour": float64(T.Hour()), "minute": float64(T.Minute()), "hour_sin": math.Sin(2 * math.Pi * h / 24), "hour_cos": math.Cos(2 * math.Pi * h / 24), "target_order_in_vehicle": float64(req.Target.Order), "target_lon_sched": req.Target.Lon, "target_lat_sched": req.Target.Lat, "scheduled_prev_gap_s": req.Target.ScheduledPrevGapS, "last_known_fact_delay_s": req.Schedule.LastKnownFactDelayS, "seconds_since_last_fact": req.Schedule.SecondsSinceLastFact}
	for k, v := range s.Points.Features(id, T, cur) {
		f[k] = v
	}
	for k, v := range s.Vehicle.Features(id, T) {
		f[k] = v
	}
	if finite(f["last_lat"]) && finite(f["last_lon"]) && finite(f["target_lat_sched"]) && finite(f["target_lon_sched"]) {
		f["straightline_to_target_m"] = HaversineM(f["last_lat"], f["last_lon"], f["target_lat_sched"], f["target_lon_sched"])
	} else {
		f["straightline_to_target_m"] = math.NaN()
	}
	addDerived(f)
	x := make([]float32, len(ProductionFeatures))
	for i, n := range ProductionFeatures {
		v, ok := f[n]
		if !ok {
			v = math.NaN()
		}
		x[i] = float32(v)
	}
	regOut, err := s.Reg.Run(x)
	if err != nil {
		return PredictionResponse{Status: "inference_error: " + err.Error(), TrID: id}
	}
	riskOut, err := s.Risk.Run(x)
	if err != nil {
		return PredictionResponse{Status: "inference_error: " + err.Error(), TrID: id}
	}
	if len(regOut) < 1 || len(riskOut) < 1 {
		return PredictionResponse{Status: "invalid_model_output", TrID: id}
	}
	res := float64(regOut[0])
	pi := s.RiskPositiveIndex
	if len(riskOut) == 1 {
		pi = 0
	}
	if pi < 0 || pi >= len(riskOut) {
		return PredictionResponse{Status: "invalid_risk_output", TrID: id}
	}
	prob := float64(riskOut[pi])
	if prob < 0 {
		prob = 0
	}
	if prob > 1 {
		prob = 1
	}
	pred := cur + res
	reason, rc := inferReason(f, pred, cur)
	s.Points.MaybeSnapshot(id, T, cur)
	s.Predictions++
	stale := f["telemetry_staleness_s"]
	return PredictionResponse{Status: "ok", TrID: id, TargetStopID: req.Target.StopID, TargetTimeUnixMS: req.Target.TargetTimeUnixMS, HorizonS: horizon, CurrentDelayS: cur, PredictedDeltaS: res, PredictedDelayS: pred, LateProbability: prob, RiskLevel: riskLevel(prob, pred), Reason: reason, ReasonConfidence: rc, TelemetryStalenessS: stale, MLLatencyMS: float64(time.Since(start).Microseconds()) / 1000}
}
func (s *Service) Health() HealthResponse {
	s.mu.Lock()
	defer s.mu.Unlock()
	return HealthResponse{true, fmt.Sprintf("ready; rejected_events=%d", s.RejectedEvents), ModelVersion, s.TelemetryEvents, s.Predictions}
}

var ErrONNXUnavailable = errors.New("ONNX runtime unavailable")
