package state

import (
	"testing"
	"time"

	"baboteek_regtrans/internal/domain"
	pb "baboteek_regtrans/pkg/api/v1"
)

func testPoint(unitID uint32, ts time.Time, lat float64) domain.TelemetryPoint {
	return domain.TelemetryPoint{
		UnitID:    unitID,
		EventTime: ts,
		Lat:       lat,
		Lon:       37.61,
		Speed:     25,
		Heading:   180,
		Valid:     true,
	}
}

func testMatch(trID, stopID int64, targetTime time.Time, curDev float64) domain.MatchResult {
	return domain.MatchResult{
		TrID:             trID,
		TargetStopID:     stopID,
		TargetTimeBegin:  targetTime,
		CurDevSeconds:    curDev,
		ForecastEligible: true,
		MatchConfidence:  0.9,
		AlignedEventTime: targetTime.Add(-12 * time.Minute),
	}
}

func testPrediction(trID, stopID int64, targetTime time.Time, delay, probability float64, risk string) *pb.PredictionResponse {
	return &pb.PredictionResponse{
		Status:           "ok",
		TrId:             trID,
		TargetStopId:     stopID,
		TargetTimeUnixMs: targetTime.UnixMilli(),
		PredictedDelayS:  delay,
		LateProbability:  probability,
		RiskLevel:        risk,
		Reason:           "test reason",
		ReasonConfidence: 0.8,
	}
}

func TestPredictionLifecycleValidToInvalidClearsOnlyForecast(t *testing.T) {
	fs := NewFleetStore()
	unitID := uint32(985940)
	trID := int64(131672)
	t0 := time.Date(2026, 1, 6, 15, 0, 0, 0, time.UTC)
	targetA := t0.Add(12 * time.Minute)

	matchA := testMatch(trID, 53698328593, targetA, 240)
	fs.UpdateFromTelemetry(testPoint(unitID, t0, 55.75), &matchA)
	got := fs.UpdateFromML(unitID, testPrediction(trID, matchA.TargetStopID, targetA, 850, 0.91, "red"))
	if got == nil || !got.PredictionValid || got.RiskLevel != "red" {
		t.Fatalf("expected valid red prediction, got %+v", got)
	}
	if incidents := fs.GetRecentIncidents(); len(incidents) != 1 {
		t.Fatalf("expected one active incident, got %d", len(incidents))
	}

	invalid := testMatch(trID, 0, time.Time{}, 1244)
	pt2 := testPoint(unitID, t0.Add(5*time.Second), 55.751)
	got = fs.UpdateFromTelemetry(pt2, &invalid)

	if got.PredictionValid {
		t.Fatal("prediction must be invalid when strict target disappears")
	}
	if got.PredictedDelaySeconds != 0 || got.LateProbability != 0 || got.Reason != "" || got.ReasonConfidence != 0 {
		t.Fatalf("stale forecast fields survived invalidation: %+v", got)
	}
	if got.RiskLevel != "green" {
		t.Fatalf("invalid prediction must not keep incident risk, got %q", got.RiskLevel)
	}
	if got.TargetStopID != 0 || !got.TargetTimeBegin.IsZero() {
		t.Fatalf("invalid target must remain empty: stop=%d time=%v", got.TargetStopID, got.TargetTimeBegin)
	}
	if got.CurrentDelaySeconds != 1244 {
		t.Fatalf("current delay must survive forecast invalidation, got %v", got.CurrentDelaySeconds)
	}
	if got.LastPoint.Lat != pt2.Lat || !got.IsOnline {
		t.Fatalf("telemetry state must survive forecast invalidation: %+v", got.LastPoint)
	}
	if incidents := fs.GetRecentIncidents(); len(incidents) != 0 {
		t.Fatalf("invalidated prediction must disappear from active incidents, got %d", len(incidents))
	}
}

func TestPredictionLifecycleTargetChangeRejectsStaleMLAndAcceptsNewTarget(t *testing.T) {
	fs := NewFleetStore()
	unitID := uint32(985940)
	trID := int64(131672)
	t0 := time.Date(2026, 1, 6, 16, 0, 0, 0, time.UTC)
	targetA := t0.Add(11 * time.Minute)
	targetB := t0.Add(14 * time.Minute)

	matchA := testMatch(trID, 1001, targetA, 120)
	fs.UpdateFromTelemetry(testPoint(unitID, t0, 55.75), &matchA)
	fs.UpdateFromML(unitID, testPrediction(trID, 1001, targetA, 300, 0.75, "red"))

	matchB := testMatch(trID, 1002, targetB, 130)
	got := fs.UpdateFromTelemetry(testPoint(unitID, t0.Add(5*time.Second), 55.751), &matchB)
	if got.PredictionValid {
		t.Fatal("prediction for target A must be invalidated when target B becomes current")
	}
	if got.TargetStopID != 1002 || !got.TargetTimeBegin.Equal(targetB) {
		t.Fatalf("target B not stored: %+v", got)
	}

	got = fs.UpdateFromML(unitID, testPrediction(trID, 1001, targetA, 999, 0.99, "red"))
	if got.PredictionValid || got.TargetStopID != 1002 || got.PredictedDelaySeconds != 0 {
		t.Fatalf("stale ML response for target A overwrote target B: %+v", got)
	}

	got = fs.UpdateFromML(unitID, testPrediction(trID, 1002, targetB, 180, 0.45, "yellow"))
	if !got.PredictionValid || got.TargetStopID != 1002 || got.PredictedDelaySeconds != 180 || got.RiskLevel != "yellow" {
		t.Fatalf("new target prediction was not accepted: %+v", got)
	}
}

func TestPredictionLifecycleSameTargetKeepsPredictionBetweenCadenceTicks(t *testing.T) {
	fs := NewFleetStore()
	unitID := uint32(893159)
	trID := int64(133957)
	t0 := time.Date(2026, 1, 6, 17, 0, 0, 0, time.UTC)
	target := t0.Add(12 * time.Minute)
	match := testMatch(trID, 2001, target, 60)

	fs.UpdateFromTelemetry(testPoint(unitID, t0, 55.70), &match)
	fs.UpdateFromML(unitID, testPrediction(trID, 2001, target, 150, 0.40, "yellow"))

	match.CurDevSeconds = 65
	got := fs.UpdateFromTelemetry(testPoint(unitID, t0.Add(5*time.Second), 55.701), &match)
	if !got.PredictionValid || got.PredictedDelaySeconds != 150 || got.LateProbability != 0.40 || got.RiskLevel != "yellow" {
		t.Fatalf("same target should keep last valid prediction until next cadence tick: %+v", got)
	}
	if got.CurrentDelaySeconds != 65 {
		t.Fatalf("current delay should still refresh, got %v", got.CurrentDelaySeconds)
	}
}

func TestContextVehicleIsExplicitAndHasNoForecast(t *testing.T) {
	fs := NewFleetStore()
	unitID := uint32(663271)
	t0 := time.Date(2026, 1, 6, 9, 0, 0, 0, time.UTC)
	match := testMatch(116445, 0, time.Time{}, 0)
	match.ForecastEligible = false

	got := fs.UpdateFromTelemetry(testPoint(unitID, t0, 55.75), &match)
	if got.ForecastEligible {
		t.Fatal("context-only vehicle must not be forecast eligible")
	}
	if got.PredictionValid || got.TargetStopID != 0 {
		t.Fatalf("context-only vehicle must not get a fake forecast: %+v", got)
	}
}

func TestFallbackPredictionIsMarkedAsFallback(t *testing.T) {
	fs := NewFleetStore()
	unitID := uint32(985940)
	trID := int64(131672)
	t0 := time.Date(2026, 1, 6, 16, 0, 0, 0, time.UTC)
	target := t0.Add(12 * time.Minute)
	match := testMatch(trID, 1001, target, 120)
	match.ForecastEligible = true
	fs.UpdateFromTelemetry(testPoint(unitID, t0, 55.75), &match)
	pred := testPrediction(trID, 1001, target, 180, 0.5, "yellow")
	pred.Status = "fallback_active"

	got := fs.UpdateFromML(unitID, pred)
	if !got.PredictionValid || got.PredictionSource != "fallback" {
		t.Fatalf("fallback must be explicit in API state: %+v", got)
	}
}

func TestTelemetryDiagnostics(t *testing.T) {
	points := []domain.TelemetryPoint{
		{EventTime: time.Unix(0, 0), Speed: 20, Valid: true},
		{EventTime: time.Unix(5, 0), Speed: 0, Valid: true},
		{EventTime: time.Unix(10, 0), Speed: 0, Valid: true},
	}
	avg, dwell := deriveTelemetryDiagnostics(points)
	if avg < 6.66 || avg > 6.67 {
		t.Fatalf("unexpected avg speed: %v", avg)
	}
	if dwell != 5 {
		t.Fatalf("unexpected dwell: %v", dwell)
	}
}
