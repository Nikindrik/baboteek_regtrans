package schedule

import (
	"bufio"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"baboteek_regtrans/internal/domain"
)

const (
	EarthR              = 6371000.0
	PassRadiusM         = 75.0
	DwellRadiusM        = 45.0
	LookaheadStops      = 4
	MaxPlanTimeErrorMin = 45.0
	InitTimeWindowMin   = 35.0
	MaxSegmentGapS      = 90.0
)

var pointRE = regexp.MustCompile(`(?i)POINT\s*\(\s*([-+0-9.eE]+)\s+([-+0-9.eE]+)\s*\)`)

type Stop struct {
	StopID             int64
	Order              int
	PlanTime           time.Time
	Lon, Lat, PrevGapS float64
}

type VehicleScheduleState struct {
	NextIdx          *int
	LastConfirmedIdx *int
	LastStopID       int64
	LastFactTime     *time.Time
	CurDevS          float64
	MatchConfidence  float64
	PrevValidEvent   *domain.TelemetryPoint
	LastSeenTime     *time.Time
}

type ScheduleMatcher struct {
	mu        sync.RWMutex
	ByTr      map[int64][]Stop
	State     map[int64]*VehicleScheduleState
	location  *time.Location
	timeShift time.Duration
}

func NewScheduleMatcher(loc *time.Location, shiftHours int) *ScheduleMatcher {
	return &ScheduleMatcher{
		ByTr:      make(map[int64][]Stop),
		State:     make(map[int64]*VehicleScheduleState),
		location:  loc,
		timeShift: time.Duration(shiftHours) * time.Hour,
	}
}

func LoadScheduleMatcher(path string, loc *time.Location, shiftHours int) (*ScheduleMatcher, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	r := csv.NewReader(bufio.NewReaderSize(f, 1<<20))
	r.ReuseRecord = false
	hrow, err := r.Read()
	if err != nil {
		return nil, err
	}
	h := headerMap(hrow)

	for _, n := range []string{"tr_id", "tt_action_item_id", "time_begin"} {
		if _, ok := h[n]; !ok {
			return nil, fmt.Errorf("schedule missing column %s", n)
		}
	}

	type raw struct {
		tr, stop int64
		t        time.Time
		lon, lat float64
	}
	rows := make([]raw, 0, 10000)

	for {
		row, e := r.Read()
		if errors.Is(e, io.EOF) {
			break
		}
		if e != nil {
			return nil, e
		}
		tr, e1 := parseInt64(getCol(row, h, "tr_id"))
		sid, e2 := parseInt64(getCol(row, h, "tt_action_item_id"))
		t, e3 := ParseTimeInLocation(getCol(row, h, "time_begin"), loc)
		if e1 != nil || e2 != nil || e3 != nil {
			continue
		}
		lon, lat := math.NaN(), math.NaN()
		if _, ok := h["geom"]; ok {
			lon, lat = parseWKTPoint(getCol(row, h, "geom"))
		}
		rows = append(rows, raw{tr: tr, stop: sid, t: t, lon: lon, lat: lat})
	}

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].tr == rows[j].tr {
			return rows[i].t.Before(rows[j].t)
		}
		return rows[i].tr < rows[j].tr
	})

	m := NewScheduleMatcher(loc, shiftHours)
	var prevTr int64
	var prevT time.Time
	hasPrev := false

	for _, rr := range rows {
		order := len(m.ByTr[rr.tr])
		gap := math.NaN()
		if hasPrev && rr.tr == prevTr {
			gap = rr.t.Sub(prevT).Seconds()
		}
		m.ByTr[rr.tr] = append(m.ByTr[rr.tr], Stop{
			StopID:   rr.stop,
			Order:    order,
			PlanTime: rr.t,
			Lon:      rr.lon,
			Lat:      rr.lat,
			PrevGapS: gap,
		})
		prevTr, prevT, hasPrev = rr.tr, rr.t, true
	}

	return m, nil
}

func (m *ScheduleMatcher) Update(trID int64, ev domain.TelemetryPoint) domain.MatchResult {
	m.mu.Lock()
	defer m.mu.Unlock()

	stops := m.ByTr[trID]
	if len(stops) == 0 {
		return m.toResultLocked(trID, ev.EventTime, ev.EventTime)
	}

	evLocal := ev.EventTime.In(m.location).Add(m.timeShift)
	refDate := stops[0].PlanTime.In(m.location)

	alignedEventTime := time.Date(
		refDate.Year(), refDate.Month(), refDate.Day(),
		evLocal.Hour(), evLocal.Minute(), evLocal.Second(),
		evLocal.Nanosecond(), m.location,
	)

	ev.EventTime = alignedEventTime

	st := m.State[trID]
	if st == nil {
		st = &VehicleScheduleState{CurDevS: math.NaN()}
		m.State[trID] = st
	}

	if st.LastSeenTime != nil && !ev.EventTime.After(*st.LastSeenTime) {
		return m.toResultLocked(trID, *st.LastSeenTime, alignedEventTime)
	}
	tcopy := ev.EventTime
	st.LastSeenTime = &tcopy

	gps := ev.Valid && finite(ev.Lat) && finite(ev.Lon)

	if st.NextIdx == nil {
		st.NextIdx = m.initNextIdx(trID, ev)
	}

	prev := st.PrevValidEvent
	if gps && st.NextIdx != nil {
		type conf struct {
			idx  int
			fact time.Time
			c    float64
		}
		confirmed := []conf{}
		upper := *st.NextIdx + LookaheadStops
		if upper > len(stops) {
			upper = len(stops)
		}

		for idx := *st.NextIdx; idx < upper; idx++ {
			stop := stops[idx]
			dt := math.Abs(ev.EventTime.Sub(stop.PlanTime).Minutes())
			if dt > MaxPlanTimeErrorMin || !finite(stop.Lat) || !finite(stop.Lon) {
				continue
			}

			dcur := HaversineM(ev.Lat, ev.Lon, stop.Lat, stop.Lon)
			if prev != nil {
				segdt := ev.EventTime.Sub(prev.EventTime).Seconds()
				if segdt > 0 && segdt <= MaxSegmentGapS {
					segd, u, raw := SegmentClosestToStop(*prev, ev, stop.Lat, stop.Lon)
					crossed := segd <= PassRadiusM && finite(raw) && raw >= 0 && raw <= 1
					headingOK := true
					if idx+1 < len(stops) && ev.Speed > 5 && finite(ev.Heading) {
						n := stops[idx+1]
						if finite(n.Lat) && finite(n.Lon) {
							headingOK = HeadingDiff(ev.Heading, BearingDeg(stop.Lat, stop.Lon, n.Lat, n.Lon)) <= 100
						}
					}
					if crossed && headingOK {
						fact := prev.EventTime.Add(time.Duration(segdt * u * float64(time.Second)))
						c := math.Max(.45, math.Min(.99, 1-segd/(PassRadiusM*1.5)))
						confirmed = append(confirmed, conf{idx, fact, c})
						continue
					}
				}
			}

			if dcur <= DwellRadiusM && ev.Speed <= 4 {
				c := math.Max(.40, math.Min(.90, 1-dcur/(DwellRadiusM*1.5)))
				confirmed = append(confirmed, conf{idx, ev.EventTime, c})
			}
		}

		if len(confirmed) > 0 {
			best := confirmed[0]
			for _, c := range confirmed[1:] {
				if c.idx > best.idx {
					best = c
				}
			}
			s := stops[best.idx]
			i := best.idx
			st.LastConfirmedIdx = &i
			nx := i + 1
			if nx > len(stops) {
				nx = len(stops)
			}
			st.NextIdx = &nx
			st.LastStopID = s.StopID
			ft := best.fact
			st.LastFactTime = &ft
			st.CurDevS = ft.Sub(s.PlanTime).Seconds()
			st.MatchConfidence = best.c
		}

		ec := ev
		st.PrevValidEvent = &ec
	}

	return m.toResultLocked(trID, ev.EventTime, alignedEventTime)
}

func (m *ScheduleMatcher) toResultLocked(trID int64, T time.Time, alignedTime time.Time) domain.MatchResult {
	st := m.State[trID]
	stops := m.ByTr[trID]

	if len(stops) == 0 {
		return domain.MatchResult{
			TrID:             trID,
			AlignedEventTime: alignedTime,
			TargetStopID:     0,
			TargetStatus:     fmt.Sprintf("TRIP_NOT_FOUND: tr_id=%d has 0 stops in schedule", trID),
		}
	}

	var curDev, lastFact, since float64
	conf := 0.0
	lastStopID := int64(0)
	order := int32(-1)

	// В autoGenerate GPS не следует геометрии реального маршрута. Поэтому до
	// первого физически подтвержденного прохождения остановки bootstrap строим
	// только по времени расписания. Если рейс сейчас вообще не активен
	// (ближайшая остановка дальше InitTimeWindowMin), не придумываем гигантское
	// текущее отклонение: ScheduleState остается с confidence=0.
	if st != nil && st.LastConfirmedIdx == nil {
		if idx, ok := nearestStopByTime(stops, T, InitTimeWindowMin); ok {
			s := stops[idx]
			curDev = T.Sub(s.PlanTime).Seconds()
			lastFact = curDev
			since = 0
			conf = 0.55
			lastStopID = s.StopID
			order = int32(idx)
		}
	} else if st != nil && st.LastFactTime != nil && finite(st.CurDevS) {
		curDev = st.CurDevS
		lastFact = st.CurDevS
		since = T.Sub(*st.LastFactTime).Seconds()
		if since < 0 {
			since = 0
		}
		conf = st.MatchConfidence
		lastStopID = st.LastStopID
		if st.LastConfirmedIdx != nil {
			order = int32(*st.LastConfirmedIdx)
		}
	}

	// СТРОГОЕ ОКНО T+10..15 МИНУТ: (T+10min, T+15min]
	lo := T.Add(10 * time.Minute)
	hi := T.Add(15 * time.Minute)

	start := 0
	if st != nil && st.NextIdx != nil {
		start = *st.NextIdx
		if start >= len(stops) {
			start = len(stops) - 1
		}
	}

	var targetStop Stop
	targetFound := false

	// Target определяется ТОЛЬКО плановым временем. Stateful GPS-cursor нельзя
	// использовать как нижнюю границу поиска: официальный autoGenerate создает
	// синтетические координаты, не лежащие на реальном маршруте, и GPS-bootstrap
	// может перескочить через корректную остановку. Сканирование всего рейса
	// гарантирует точное правило (T+10min, T+15min].
	for _, s := range stops {
		if s.PlanTime.After(lo) && !s.PlanTime.After(hi) {
			targetStop = s
			targetFound = true
			break
		}
	}

	// Если в строгом окне остановки нет — target invalid и прогноз пропускается!
	if !targetFound {
		firstStopT := stops[0].PlanTime.In(m.location).Format("15:04:05")
		lastStopT := stops[len(stops)-1].PlanTime.In(m.location).Format("15:04:05")
		currStopPlan := "unknown"
		if start < len(stops) {
			currStopPlan = stops[start].PlanTime.In(m.location).Format("15:04:05")
		}

		targetStatus := fmt.Sprintf(
			"NO_STOP_IN_10_15M: window [%s..%s], trip [%s..%s], curr_idx=%d (plan: %s)",
			lo.In(m.location).Format("15:04:05"), hi.In(m.location).Format("15:04:05"),
			firstStopT, lastStopT, start, currStopPlan,
		)

		return domain.MatchResult{
			TrID:                 trID,
			AlignedEventTime:     alignedTime,
			LastStopID:           lastStopID,
			LastStopOrder:        order,
			CurDevSeconds:        curDev,
			LastKnownFactDelayS:  lastFact,
			SecondsSinceLastFact: since,
			MatchConfidence:      conf,
			TargetStopID:         0, // Target invalid
			HorizonSeconds:       0,
			TargetStatus:         targetStatus,
			ForecastEligible:     true,
		}
	}

	horizon := targetStop.PlanTime.Sub(T).Seconds()

	return domain.MatchResult{
		TrID:                 trID,
		AlignedEventTime:     alignedTime,
		LastStopID:           lastStopID,
		LastStopOrder:        order,
		CurDevSeconds:        curDev,
		LastKnownFactDelayS:  lastFact,
		SecondsSinceLastFact: since,
		MatchConfidence:      conf,

		TargetStopID:      targetStop.StopID,
		TargetTimeBegin:   targetStop.PlanTime,
		TargetOrder:       int32(targetStop.Order),
		TargetLat:         targetStop.Lat,
		TargetLon:         targetStop.Lon,
		HorizonSeconds:    int64(horizon),
		ScheduledPrevGapS: targetStop.PrevGapS,
		TargetStatus:      "OK",
		ForecastEligible:  true,
	}
}

func nearestStopByTime(stops []Stop, t time.Time, maxWindowMin float64) (int, bool) {
	bestIdx := -1
	bestDT := math.Inf(1)
	for i, s := range stops {
		dt := math.Abs(s.PlanTime.Sub(t).Minutes())
		if dt <= maxWindowMin && dt < bestDT {
			bestIdx = i
			bestDT = dt
		}
	}
	return bestIdx, bestIdx >= 0
}

func (m *ScheduleMatcher) initNextIdx(trID int64, ev domain.TelemetryPoint) *int {
	stops := m.ByTr[trID]
	if len(stops) == 0 {
		return nil
	}
	// Начальный cursor выбираем по времени, а не по GPS. Это критично для
	// официального autoGenerate: его координаты синтетические и не обязаны
	// совпадать с геометрией рейса. Реальный GPS по-прежнему используется ниже
	// для подтверждения фактического прохождения остановок.
	if idx, ok := nearestStopByTime(stops, ev.EventTime, InitTimeWindowMin); ok {
		x := idx
		return &x
	}

	for i, s := range stops {
		if !s.PlanTime.Before(ev.EventTime) {
			x := i
			return &x
		}
	}
	x := len(stops) - 1
	return &x
}

func ParseTimeInLocation(s string, loc *time.Location) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, errors.New("empty time")
	}

	// НОРМАЛИЗАЦИЯ: если час из одной цифры (например "2026-01-06 2:28:00") -> превращаем в "2026-01-06 02:28:00"
	parts := strings.Split(s, " ")
	if len(parts) == 2 {
		timeParts := strings.Split(parts[1], ":")
		if len(timeParts) >= 2 && len(timeParts[0]) == 1 {
			timeParts[0] = "0" + timeParts[0] // 2 -> 02
			parts[1] = strings.Join(timeParts, ":")
			s = strings.Join(parts, " ")
		}
	}

	for _, layout := range timeLayouts {
		if t, err := time.ParseInLocation(layout, s, loc); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported datetime %q", s)
}

func HaversineM(lat1, lon1, lat2, lon2 float64) float64 {
	vals := []float64{lat1, lon1, lat2, lon2}
	for _, v := range vals {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return math.Inf(1)
		}
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

func localXYM(lat, lon, lat0, lon0 float64) (float64, float64) {
	x := (lon - lon0) * math.Pi / 180 * EarthR * math.Cos(((lat+lat0)/2)*math.Pi/180)
	y := (lat - lat0) * math.Pi / 180 * EarthR
	return x, y
}

func SegmentClosestToStop(prev, cur domain.TelemetryPoint, stopLat, stopLon float64) (dist, u, rawU float64) {
	ax, ay := localXYM(prev.Lat, prev.Lon, stopLat, stopLon)
	bx, by := localXYM(cur.Lat, cur.Lon, stopLat, stopLon)
	vx, vy := bx-ax, by-ay
	denom := vx*vx + vy*vy
	if denom <= 1e-9 {
		return math.Hypot(ax, ay), 1, math.NaN()
	}
	rawU = -(ax*vx + ay*vy) / denom
	u = math.Max(0, math.Min(1, rawU))
	cx, cy := ax+u*vx, ay+u*vy
	return math.Hypot(cx, cy), u, rawU
}

func BearingDeg(lat1, lon1, lat2, lon2 float64) float64 {
	a1, a2 := lat1*math.Pi/180, lat2*math.Pi/180
	dl := (lon2 - lon1) * math.Pi / 180
	y := math.Sin(dl) * math.Cos(a2)
	x := math.Cos(a1)*math.Sin(a2) - math.Sin(a1)*math.Cos(a2)*math.Cos(dl)
	return math.Mod(math.Atan2(y, x)*180/math.Pi+360, 360)
}

func HeadingDiff(a, b float64) float64 {
	return math.Abs(math.Mod(a-b+540, 360) - 180)
}

func finite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}

func parseWKTPoint(v string) (lon, lat float64) {
	m := pointRE.FindStringSubmatch(v)
	if len(m) != 3 {
		return math.NaN(), math.NaN()
	}
	lon, _ = strconv.ParseFloat(m[1], 64)
	lat, _ = strconv.ParseFloat(m[2], 64)
	return
}

var timeLayouts = []string{
	time.RFC3339Nano,
	"2006-01-02 15:04:05.999999999",
	"2006-01-02 15:04:05.999999",
	"2006-01-02 15:04:05.999",
	"2006-01-02 15:04:05",
	"2006-01-02T15:04:05.999999999",
	"2006-01-02T15:04:05",
}

func parseInt64(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, errors.New("empty int")
	}
	if v, err := strconv.ParseInt(s, 10, 64); err == nil {
		return v, nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, err
	}
	return int64(f), nil
}

func headerMap(h []string) map[string]int {
	m := make(map[string]int, len(h))
	for i, v := range h {
		m[strings.TrimSpace(v)] = i
	}
	return m
}

func getCol(row []string, h map[string]int, name string) string {
	i, ok := h[name]
	if !ok || i < 0 || i >= len(row) {
		return ""
	}
	return row[i]
}
