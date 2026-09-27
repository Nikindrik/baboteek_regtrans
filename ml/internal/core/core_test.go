package core

import (
	"encoding/json"
	"math"
	"testing"
	"time"
)

type fakeRunner struct{ out []float32 }

func (f *fakeRunner) Run([]float32) ([]float32, error) { return append([]float32(nil), f.out...), nil }
func (f *fakeRunner) Close() error                     { return nil }

func TestInvalidGPSDoesNotCreateHugePath(t *testing.T) {
	v := NewVehicleHistory()
	id := int64(1)
	T := time.Unix(1000, 0).UTC()
	v.Update(id, HistEvent{EventTime: T, TrID: id, Lat: 55.75, Lon: 37.61, Speed: 20, Heading: 0, LocationValid: true})
	v.Update(id, HistEvent{EventTime: T.Add(10 * time.Second), TrID: id, Lat: math.NaN(), Lon: math.NaN(), Speed: math.NaN(), Heading: math.NaN(), LocationValid: false})
	f := v.Features(id, T.Add(10*time.Second))
	if d := f["path_distance_1m_m"]; finite(d) && d > 1000 {
		t.Fatalf("bad path distance %v", d)
	}
	if math.Abs(f["last_lat"]-55.75) > 1e-9 {
		t.Fatalf("last valid position lost")
	}
}
func TestPredict(t *testing.T) {
	svc := NewService(&fakeRunner{[]float32{12}}, &fakeRunner{[]float32{.2, .8}}, 1)
	T := time.Unix(10000, 0).UTC()
	ack := svc.Ingest(IngestRequest{Telemetry: TelemetryEvent{UnitID: 1, TrID: 2, EventTimeUnixMS: T.UnixMilli(), Lat: 55.75, Lon: 37.61, SpeedKMH: 20, HeadingDeg: 90, LocationValid: true}})
	if !ack.Accepted {
		t.Fatal(ack)
	}
	p := svc.Predict(PredictRequest{TrID: 2, RequestTimeUnixMS: T.UnixMilli(), Schedule: ScheduleState{Valid: true, CurDevS: 30, LastKnownFactDelayS: 30}, Target: TargetPoint{Valid: true, StopID: 4, TargetTimeUnixMS: T.Add(12 * time.Minute).UnixMilli(), Order: 5, Lon: 37.62, Lat: 55.76, ScheduledPrevGapS: 60}})
	if p.Status != "ok" || math.Abs(p.PredictedDelayS-42) > 1e-6 || math.Abs(p.LateProbability-.8) > 1e-6 {
		t.Fatalf("bad prediction %+v", p)
	}
}
func TestFeatureCount(t *testing.T) {
	if len(ProductionFeatures) != 96 {
		t.Fatalf("features=%d", len(ProductionFeatures))
	}
}

func TestTargetNullBecomesNaN(t *testing.T) {
	var tp TargetPoint
	if err := json.Unmarshal([]byte(`{"valid":true,"stop_id":1,"target_time_unix_ms":2,"order":3,"lon":null,"lat":55.7,"scheduled_prev_gap_s":null}`), &tp); err != nil {
		t.Fatal(err)
	}
	if !math.IsNaN(tp.Lon) || !math.IsNaN(tp.ScheduledPrevGapS) || math.Abs(tp.Lat-55.7) > 1e-9 {
		t.Fatalf("bad target %+v", tp)
	}
}
