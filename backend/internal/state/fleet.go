package state

import (
	"math"
	"sort"
	"sync"
	"time"

	"baboteek_regtrans/internal/domain"
	pb "baboteek_regtrans/pkg/api/v1"
)

const RingBufferSize = 10

type PointRingBuffer struct {
	points []domain.TelemetryPoint
	head   int
	count  int
	size   int
}

func NewPointRingBuffer(size int) *PointRingBuffer {
	return &PointRingBuffer{
		points: make([]domain.TelemetryPoint, size),
		size:   size,
	}
}

func (rb *PointRingBuffer) Push(pt domain.TelemetryPoint) {
	rb.points[rb.head] = pt
	rb.head = (rb.head + 1) % rb.size
	if rb.count < rb.size {
		rb.count++
	}
}

func (rb *PointRingBuffer) GetAll() []domain.TelemetryPoint {
	res := make([]domain.TelemetryPoint, rb.count)
	if rb.count == 0 {
		return res
	}

	start := (rb.head - rb.count + rb.size) % rb.size
	for i := 0; i < rb.count; i++ {
		res[i] = rb.points[(start+i)%rb.size]
	}
	return res
}

type VehicleInternal struct {
	mu         sync.RWMutex
	data       domain.VehicleState
	ringBuffer *PointRingBuffer
}

// FleetStore реализует domain.FleetRepository
type FleetStore struct {
	mu           sync.RWMutex
	vehicles     map[uint32]*VehicleInternal
	maxIncidents int
	incidents    []domain.IncidentCard
}

func NewFleetStore() *FleetStore {
	return &FleetStore{
		vehicles:     make(map[uint32]*VehicleInternal),
		maxIncidents: 100,
		incidents:    make([]domain.IncidentCard, 0, 100),
	}
}

func predictionTargetValid(stopID int64, targetTime time.Time) bool {
	return stopID > 0 && !targetTime.IsZero()
}

func samePredictionTarget(v domain.VehicleState, match *domain.MatchResult) bool {
	if !predictionTargetValid(match.TargetStopID, match.TargetTimeBegin) {
		return false
	}
	return v.TargetStopID == match.TargetStopID && v.TargetTimeBegin.Equal(match.TargetTimeBegin)
}

func invalidatePrediction(v *domain.VehicleState) {
	v.PredictionValid = false
	v.PredictionSource = ""
	v.MLLatencyMS = 0
	v.PredictedDelaySeconds = 0
	v.LateProbability = 0
	v.RiskLevel = "green"
	v.Reason = ""
	v.ReasonConfidence = 0
}

// deriveTelemetryDiagnostics keeps the Backend-side derived features intentionally
// simple and transparent: mean speed over the recent breadcrumb window and the
// duration of the current contiguous stop (<=2 km/h). The ML service still owns
// its richer rolling feature set.
func deriveTelemetryDiagnostics(points []domain.TelemetryPoint) (avgSpeedKMH, dwellSeconds float64) {
	var sum float64
	var n int
	for _, p := range points {
		if p.Valid && !math.IsNaN(p.Speed) && !math.IsInf(p.Speed, 0) && p.Speed >= 0 {
			sum += p.Speed
			n++
		}
	}
	if n > 0 {
		avgSpeedKMH = sum / float64(n)
	}

	if len(points) < 2 || points[len(points)-1].Speed > 2 {
		return avgSpeedKMH, 0
	}
	end := points[len(points)-1].EventTime
	start := end
	for i := len(points) - 1; i >= 0; i-- {
		p := points[i]
		if !p.Valid || p.Speed > 2 || p.EventTime.IsZero() {
			break
		}
		start = p.EventTime
	}
	if end.After(start) {
		dwellSeconds = end.Sub(start).Seconds()
	}
	return avgSpeedKMH, dwellSeconds
}

func (fs *FleetStore) UpdateFromTelemetry(pt domain.TelemetryPoint, match *domain.MatchResult) *domain.VehicleState {
	fs.mu.Lock()
	v, exists := fs.vehicles[pt.UnitID]
	if !exists {
		v = &VehicleInternal{
			ringBuffer: NewPointRingBuffer(RingBufferSize),
			data: domain.VehicleState{
				UnitID:          pt.UnitID,
				RiskLevel:       "green",
				PredictionValid: false,
			},
		}
		fs.vehicles[pt.UnitID] = v
	}
	fs.mu.Unlock()

	v.mu.Lock()
	defer v.mu.Unlock()

	v.ringBuffer.Push(pt)

	// Prediction fields belong to one exact strict-horizon target. If there is
	// no target now, or the target changed since the last ML result, the old
	// prediction must not survive into the new live state.
	targetValid := predictionTargetValid(match.TargetStopID, match.TargetTimeBegin)
	targetChanged := !samePredictionTarget(v.data, match)
	if !targetValid || targetChanged {
		invalidatePrediction(&v.data)
	}

	v.data.UnitID = pt.UnitID
	v.data.TrID = match.TrID
	v.data.UpdatedAt = time.Now()
	v.data.IsOnline = true
	v.data.LastPoint = pt
	v.data.Track = v.ringBuffer.GetAll()
	v.data.SegmentAvgSpeedKMH, v.data.DwellTimeSeconds = deriveTelemetryDiagnostics(v.data.Track)
	v.data.ForecastEligible = match.ForecastEligible
	v.data.TargetStopID = match.TargetStopID
	v.data.TargetTimeBegin = match.TargetTimeBegin
	v.data.CurrentDelaySeconds = match.CurDevSeconds

	snapshot := v.data
	return &snapshot
}

func (fs *FleetStore) UpdateFromML(unitID uint32, pred *pb.PredictionResponse) *domain.VehicleState {
	fs.mu.RLock()
	targetV := fs.vehicles[unitID]
	fs.mu.RUnlock()
	if targetV == nil {
		return nil
	}

	targetV.mu.Lock()

	if pred == nil || (pred.Status != "ok" && pred.Status != "fallback_active") {
		snapshot := targetV.data
		targetV.mu.Unlock()
		return &snapshot
	}

	// Do not let a delayed ML response for target A overwrite a newer target B.
	// The live VehicleState is authoritative about which strict-horizon target
	// is current for this unit.
	predTargetValid := pred.TargetStopId > 0 && pred.TargetTimeUnixMs > 0
	currentTargetValid := predictionTargetValid(targetV.data.TargetStopID, targetV.data.TargetTimeBegin)
	predTargetTime := time.Time{}
	if pred.TargetTimeUnixMs > 0 {
		predTargetTime = time.UnixMilli(pred.TargetTimeUnixMs)
	}
	if !predTargetValid || !currentTargetValid || pred.TrId != targetV.data.TrID || targetV.data.TargetStopID != pred.TargetStopId || !targetV.data.TargetTimeBegin.Equal(predTargetTime) {
		snapshot := targetV.data
		targetV.mu.Unlock()
		return &snapshot
	}

	targetV.data.PredictionValid = true
	if pred.Status == "fallback_active" {
		targetV.data.PredictionSource = "fallback"
	} else {
		targetV.data.PredictionSource = "ml"
	}
	targetV.data.MLLatencyMS = pred.MlLatencyMs
	targetV.data.PredictedDelaySeconds = pred.PredictedDelayS
	targetV.data.LateProbability = pred.LateProbability
	targetV.data.RiskLevel = pred.RiskLevel
	targetV.data.Reason = pred.Reason
	targetV.data.ReasonConfidence = pred.ReasonConfidence
	snapshot := targetV.data
	targetV.mu.Unlock()

	// Если риск красный или желтый — регистрируем карточку инцидента
	if pred.RiskLevel == "red" || pred.RiskLevel == "yellow" {
		fs.registerIncident(domain.IncidentCard{
			UnitID:          snapshot.UnitID,
			TrID:            snapshot.TrID,
			RiskLevel:       pred.RiskLevel,
			CurrentDelayS:   snapshot.CurrentDelaySeconds,
			PredictedDelayS: pred.PredictedDelayS,
			Reason:          pred.Reason,
			TargetStopID:    snapshot.TargetStopID,
			TargetTimeBegin: snapshot.TargetTimeBegin,
			DetectedAt:      time.Now(),
		})
	}

	return &snapshot
}

func (fs *FleetStore) registerIncident(inc domain.IncidentCard) {
	fs.mu.Lock()
	defer fs.mu.Unlock()

	for i := len(fs.incidents) - 1; i >= 0; i-- {
		if fs.incidents[i].UnitID == inc.UnitID {
			if time.Since(fs.incidents[i].DetectedAt) < 60*time.Second {
				fs.incidents[i] = inc
				return
			}
			break
		}
	}

	if len(fs.incidents) >= fs.maxIncidents {
		fs.incidents = fs.incidents[1:]
	}
	fs.incidents = append(fs.incidents, inc)
}

func (fs *FleetStore) GetVehicleSnapshot(unitID uint32) (domain.VehicleState, bool) {
	fs.mu.RLock()
	v, ok := fs.vehicles[unitID]
	fs.mu.RUnlock()
	if !ok {
		return domain.VehicleState{}, false
	}

	v.mu.RLock()
	snapshot := v.data
	v.mu.RUnlock()
	return snapshot, true
}

func (fs *FleetStore) GetAllVehiclesSnapshot() []domain.VehicleState {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	list := make([]domain.VehicleState, 0, len(fs.vehicles))
	for _, v := range fs.vehicles {
		v.mu.RLock()
		list = append(list, v.data)
		v.mu.RUnlock()
	}
	return list
}

func (fs *FleetStore) GetRecentIncidents() []domain.IncidentCard {
	// The endpoint is documented as active incidents, so derive it from current
	// VehicleState instead of returning historical cards whose prediction may
	// already be invalid. Keep the latest detected_at only as metadata.
	fs.mu.RLock()
	vehicles := make([]*VehicleInternal, 0, len(fs.vehicles))
	for _, v := range fs.vehicles {
		vehicles = append(vehicles, v)
	}
	incidents := make([]domain.IncidentCard, len(fs.incidents))
	copy(incidents, fs.incidents)
	fs.mu.RUnlock()

	detectedAt := make(map[uint32]time.Time, len(incidents))
	for _, inc := range incidents {
		if current, ok := detectedAt[inc.UnitID]; !ok || inc.DetectedAt.After(current) {
			detectedAt[inc.UnitID] = inc.DetectedAt
		}
	}

	res := make([]domain.IncidentCard, 0)
	for _, v := range vehicles {
		v.mu.RLock()
		snapshot := v.data
		v.mu.RUnlock()

		if !snapshot.IsOnline || !snapshot.PredictionValid || (snapshot.RiskLevel != "red" && snapshot.RiskLevel != "yellow") {
			continue
		}

		detected := detectedAt[snapshot.UnitID]
		if detected.IsZero() {
			detected = snapshot.UpdatedAt
		}
		res = append(res, domain.IncidentCard{
			UnitID:          snapshot.UnitID,
			TrID:            snapshot.TrID,
			RiskLevel:       snapshot.RiskLevel,
			CurrentDelayS:   snapshot.CurrentDelaySeconds,
			PredictedDelayS: snapshot.PredictedDelaySeconds,
			Reason:          snapshot.Reason,
			TargetStopID:    snapshot.TargetStopID,
			TargetTimeBegin: snapshot.TargetTimeBegin,
			DetectedAt:      detected,
		})
	}

	sort.Slice(res, func(i, j int) bool {
		if res[i].RiskLevel != res[j].RiskLevel {
			return res[i].RiskLevel == "red"
		}
		return res[i].DetectedAt.After(res[j].DetectedAt)
	})
	return res
}

func (fs *FleetStore) CheckStaleVehicles(threshold time.Duration) {
	fs.mu.RLock()
	defer fs.mu.RUnlock()

	now := time.Now()
	for _, v := range fs.vehicles {
		v.mu.Lock()
		if now.Sub(v.data.UpdatedAt) > threshold {
			v.data.IsOnline = false
		}
		v.mu.Unlock()
	}
}
