package grpcapi

import (
	"context"
	"io"

	"mosgortrans-ml-go/internal/core"
	pb "mosgortrans-ml-go/proto"
)

// Server exposes the frozen ML core through the canonical mosgortrans.v1 gRPC contract.
// Backend owns NDTP decoding/schedule matching. ML owns rolling state/features/inference.
type Server struct {
	pb.UnimplementedMLInferenceServer
	svc *core.Service
}

func New(svc *core.Service) *Server { return &Server{svc: svc} }

func (s *Server) Ingest(_ context.Context, req *pb.IngestRequest) (*pb.IngestResponse, error) {
	if req == nil || req.Telemetry == nil {
		return &pb.IngestResponse{Accepted: false, Status: "missing_telemetry"}, nil
	}
	r := s.svc.Ingest(toCoreIngest(req))
	return &pb.IngestResponse{Accepted: r.Accepted, Status: r.Status, TrId: r.TrID}, nil
}

func (s *Server) IngestStream(stream pb.MLInference_IngestStreamServer) error {
	var received, accepted, rejected int64
	for {
		req, err := stream.Recv()
		if err == io.EOF {
			return stream.SendAndClose(&pb.IngestSummary{Received: received, Accepted: accepted, Rejected: rejected})
		}
		if err != nil {
			return err
		}
		received++
		if req == nil || req.Telemetry == nil {
			rejected++
			continue
		}
		r := s.svc.Ingest(toCoreIngest(req))
		if r.Accepted {
			accepted++
		} else {
			rejected++
		}
	}
}

func (s *Server) Predict(_ context.Context, req *pb.PredictRequest) (*pb.PredictionResponse, error) {
	if req == nil {
		return &pb.PredictionResponse{Status: "missing_request"}, nil
	}
	r := s.svc.Predict(toCorePredict(req))
	return &pb.PredictionResponse{
		Status:              r.Status,
		TrId:                r.TrID,
		TargetStopId:        r.TargetStopID,
		TargetTimeUnixMs:    r.TargetTimeUnixMS,
		HorizonS:            r.HorizonS,
		CurrentDelayS:       r.CurrentDelayS,
		PredictedDeltaS:     r.PredictedDeltaS,
		PredictedDelayS:     r.PredictedDelayS,
		LateProbability:     r.LateProbability,
		RiskLevel:           r.RiskLevel,
		Reason:              r.Reason,
		ReasonConfidence:    r.ReasonConfidence,
		TelemetryStalenessS: r.TelemetryStalenessS,
		MlLatencyMs:         r.MLLatencyMS,
	}, nil
}

func (s *Server) Health(_ context.Context, _ *pb.HealthRequest) (*pb.HealthResponse, error) {
	h := s.svc.Health()
	return &pb.HealthResponse{
		Ok:              h.OK,
		Status:          h.Status,
		ModelVersion:    h.ModelVersion,
		TelemetryEvents: h.TelemetryEvents,
		Predictions:     h.Predictions,
	}, nil
}

func toCoreIngest(req *pb.IngestRequest) core.IngestRequest {
	t := req.Telemetry
	out := core.IngestRequest{Telemetry: core.TelemetryEvent{
		UnitID:          t.GetUnitId(),
		TrID:            t.GetTrId(),
		EventTimeUnixMS: t.GetEventTimeUnixMs(),
		Lat:             t.GetLat(),
		Lon:             t.GetLon(),
		SpeedKMH:        t.GetSpeedKmh(),
		HeadingDeg:      t.GetHeadingDeg(),
		LocationValid:   t.GetLocationValid(),
	}}
	if sc := req.Schedule; sc != nil {
		out.Schedule = core.ScheduleState{
			Valid:                sc.GetValid(),
			CurDevS:              sc.GetCurDevS(),
			LastKnownFactDelayS:  sc.GetLastKnownFactDelayS(),
			SecondsSinceLastFact: sc.GetSecondsSinceLastFact(),
			LastStopID:           sc.GetLastStopId(),
			LastStopOrder:        sc.GetLastStopOrder(),
			MatchConfidence:      sc.GetMatchConfidence(),
		}
	}
	return out
}

func toCorePredict(req *pb.PredictRequest) core.PredictRequest {
	out := core.PredictRequest{TrID: req.GetTrId(), RequestTimeUnixMS: req.GetRequestTimeUnixMs()}
	if sc := req.Schedule; sc != nil {
		out.Schedule = core.ScheduleState{
			Valid:                sc.GetValid(),
			CurDevS:              sc.GetCurDevS(),
			LastKnownFactDelayS:  sc.GetLastKnownFactDelayS(),
			SecondsSinceLastFact: sc.GetSecondsSinceLastFact(),
			LastStopID:           sc.GetLastStopId(),
			LastStopOrder:        sc.GetLastStopOrder(),
			MatchConfidence:      sc.GetMatchConfidence(),
		}
	}
	if tp := req.Target; tp != nil {
		out.Target = core.TargetPoint{
			Valid:             tp.GetValid(),
			StopID:            tp.GetStopId(),
			TargetTimeUnixMS:  tp.GetTargetTimeUnixMs(),
			Order:             tp.GetOrder(),
			Lon:               tp.GetLon(),
			Lat:               tp.GetLat(),
			ScheduledPrevGapS: tp.GetScheduledPrevGapS(),
		}
	}
	return out
}
