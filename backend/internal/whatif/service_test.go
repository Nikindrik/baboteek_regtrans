package whatif

import (
	"testing"
	"time"

	"baboteek_regtrans/internal/domain"
	pb "baboteek_regtrans/pkg/api/v1"
)

type fakeRepo struct {
	vehicle domain.VehicleState
	ok      bool
}

func (f *fakeRepo) UpdateFromTelemetry(domain.TelemetryPoint, *domain.MatchResult) *domain.VehicleState {
	return nil
}
func (f *fakeRepo) UpdateFromML(uint32, *pb.PredictionResponse) *domain.VehicleState { return nil }
func (f *fakeRepo) GetAllVehiclesSnapshot() []domain.VehicleState                    { return nil }
func (f *fakeRepo) GetVehicleSnapshot(uint32) (domain.VehicleState, bool)            { return f.vehicle, f.ok }
func (f *fakeRepo) GetRecentIncidents() []domain.IncidentCard                        { return nil }
func (f *fakeRepo) CheckStaleVehicles(time.Duration)                                 {}

func testVehicle() domain.VehicleState {
	return domain.VehicleState{
		UnitID:                985940,
		TrID:                  131672,
		IsOnline:              true,
		PredictionValid:       true,
		TargetStopID:          53698328593,
		TargetTimeBegin:       time.Now().Add(12 * time.Minute),
		PredictedDelaySeconds: 600,
		LateProbability:       0.81,
		RiskLevel:             "red",
	}
}

func TestTrafficLightScenarioDoesNotRewriteBaseline(t *testing.T) {
	repo := &fakeRepo{vehicle: testVehicle(), ok: true}
	service := NewService(repo)

	got, err := service.TrafficLight(domain.TrafficLightWhatIfRequest{
		UnitID:           985940,
		ExtensionSeconds: 15,
	})
	if err != nil {
		t.Fatalf("TrafficLight returned error: %v", err)
	}
	if got.BaselinePredictedDelayS != 600 || got.ScenarioPredictedDelayS != 585 {
		t.Fatalf("unexpected scenario: baseline=%v scenario=%v", got.BaselinePredictedDelayS, got.ScenarioPredictedDelayS)
	}
	if got.EstimatedDelayReductionS != 15 {
		t.Fatalf("unexpected reduction: %v", got.EstimatedDelayReductionS)
	}
	if got.LiveStateChanged {
		t.Fatal("what-if must never mutate live state")
	}
	if repo.vehicle.PredictedDelaySeconds != 600 {
		t.Fatal("repository vehicle was mutated")
	}
}

func TestReserveScenarioMeasuresServiceGapNotVehicleDelay(t *testing.T) {
	repo := &fakeRepo{vehicle: testVehicle(), ok: true}
	service := NewService(repo)

	got, err := service.Reserve(domain.ReserveWhatIfRequest{
		UnitID:             985940,
		DispatchETASeconds: 300,
	})
	if err != nil {
		t.Fatalf("Reserve returned error: %v", err)
	}
	if got.VehicleDelayAfterActionS != 600 {
		t.Fatalf("reserve must not rewrite original vehicle delay: %v", got.VehicleDelayAfterActionS)
	}
	if got.ScenarioServiceDelayS != 300 || got.EstimatedServiceGapReductionS != 300 {
		t.Fatalf("unexpected service scenario: delay=%v reduction=%v", got.ScenarioServiceDelayS, got.EstimatedServiceGapReductionS)
	}
	if got.LiveStateChanged {
		t.Fatal("what-if must never mutate live state")
	}
}

func TestScenarioRequiresStrictHorizonTarget(t *testing.T) {
	v := testVehicle()
	v.TargetStopID = 0
	repo := &fakeRepo{vehicle: v, ok: true}
	service := NewService(repo)

	_, err := service.TrafficLight(domain.TrafficLightWhatIfRequest{UnitID: v.UnitID})
	if err != ErrPredictionUnavailable {
		t.Fatalf("expected ErrPredictionUnavailable, got %v", err)
	}
}

func TestScenarioRejectsStalePredictionEvenIfTargetFieldsRemain(t *testing.T) {
	v := testVehicle()
	v.PredictionValid = false
	// This is the exact stale-state shape that previously leaked to the UI:
	// target/prediction fields may still contain values, but validity is authoritative.
	v.PredictedDelaySeconds = 850
	v.LateProbability = 0.91
	v.RiskLevel = "red"
	repo := &fakeRepo{vehicle: v, ok: true}
	service := NewService(repo)

	_, err := service.TrafficLight(domain.TrafficLightWhatIfRequest{UnitID: v.UnitID})
	if err != ErrPredictionUnavailable {
		t.Fatalf("expected ErrPredictionUnavailable for invalid prediction, got %v", err)
	}
}
