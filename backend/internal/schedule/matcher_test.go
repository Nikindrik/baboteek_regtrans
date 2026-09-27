package schedule

import (
	"testing"
	"time"
)

func TestStrictTargetSearchDoesNotDependOnGPSCursor(t *testing.T) {
	loc := time.FixedZone("MSK", 3*60*60)
	m := NewScheduleMatcher(loc, 0)
	trID := int64(1)
	T := time.Date(2026, 1, 6, 8, 37, 0, 0, loc)

	// 08:50 is exactly 13 minutes ahead and therefore a valid strict target.
	m.ByTr[trID] = []Stop{
		{StopID: 101, Order: 0, PlanTime: time.Date(2026, 1, 6, 8, 40, 0, 0, loc)},
		{StopID: 102, Order: 1, PlanTime: time.Date(2026, 1, 6, 8, 50, 0, 0, loc)},
		{StopID: 103, Order: 2, PlanTime: time.Date(2026, 1, 6, 9, 10, 0, 0, loc)},
	}

	// Simulate a bad synthetic-GPS bootstrap that has already jumped beyond 08:50.
	badNext := 2
	m.State[trID] = &VehicleScheduleState{NextIdx: &badNext}

	got := m.toResultLocked(trID, T, T)
	if got.TargetStopID != 102 {
		t.Fatalf("strict target must be chosen by plan time, got stop=%d status=%s", got.TargetStopID, got.TargetStatus)
	}
}

func TestInactiveTripDoesNotInventHugeBootstrapDelay(t *testing.T) {
	loc := time.FixedZone("MSK", 3*60*60)
	m := NewScheduleMatcher(loc, 0)
	trID := int64(2)
	T := time.Date(2026, 1, 6, 8, 37, 0, 0, loc)

	m.ByTr[trID] = []Stop{
		{StopID: 201, Order: 0, PlanTime: time.Date(2026, 1, 6, 20, 25, 0, 0, loc)},
	}
	idx := 0
	m.State[trID] = &VehicleScheduleState{NextIdx: &idx}

	got := m.toResultLocked(trID, T, T)
	if got.MatchConfidence != 0 {
		t.Fatalf("inactive trip must not get bootstrap confidence, got %v", got.MatchConfidence)
	}
	if got.CurDevSeconds != 0 {
		t.Fatalf("inactive trip must not invent huge current delay, got %v", got.CurDevSeconds)
	}
}

func TestContextOnlyVehicleIsNotForecastEligible(t *testing.T) {
	loc := time.FixedZone("MSK", 3*60*60)
	m := NewScheduleMatcher(loc, 0)
	T := time.Date(2026, 1, 6, 9, 0, 0, 0, loc)
	got := m.toResultLocked(116445, T, T)
	if got.ForecastEligible {
		t.Fatal("tr_id without schedule must be context-only")
	}
	if got.TargetStopID != 0 {
		t.Fatalf("context-only vehicle received fake target %d", got.TargetStopID)
	}
}
