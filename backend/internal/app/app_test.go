package app

import (
	"testing"
	"time"
)

func TestPredictionCadenceCanBeForcedAndReset(t *testing.T) {
	a := &App{lastPredTime: make(map[uint32]time.Time)}
	unitID := uint32(985940)

	if !a.shouldPredict(unitID, false) {
		t.Fatal("first prediction must run")
	}
	if a.shouldPredict(unitID, false) {
		t.Fatal("second prediction inside cadence must be skipped")
	}
	if !a.shouldPredict(unitID, true) {
		t.Fatal("new/changed target must force prediction immediately")
	}

	a.resetPredictionCadence(unitID)
	if !a.shouldPredict(unitID, false) {
		t.Fatal("prediction must run immediately after invalid-target cadence reset")
	}
}
