package app

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"baboteek_regtrans/config"
	"baboteek_regtrans/internal/domain"
	"baboteek_regtrans/internal/mlclient"
	"baboteek_regtrans/internal/ndtp"
	"baboteek_regtrans/internal/schedule"
	"baboteek_regtrans/internal/state"
	httpTransport "baboteek_regtrans/internal/transport/http"
	"baboteek_regtrans/internal/transport/ws"
	pb "baboteek_regtrans/pkg/api/v1"

	"github.com/rs/zerolog"
)

type App struct {
	cfg          *config.Config
	logger       zerolog.Logger
	ndtpServer   *ndtp.Server
	httpServer   *http.Server
	wsHub        *ws.Hub
	fleetStore   *state.FleetStore
	scheduler    *schedule.ScheduleMatcher
	resolver     *schedule.UnitResolver
	mlClient     *mlclient.Client
	predMu       sync.Mutex
	lastPredTime map[uint32]time.Time
}

func New(cfg *config.Config, log zerolog.Logger) (*App, error) {
	loc, err := time.LoadLocation(cfg.App.Timezone)
	if err != nil {
		return nil, fmt.Errorf("failed to load timezone %s: %w", cfg.App.Timezone, err)
	}

	if cfg.App.TimeShiftHours != 0 {
		log.Warn().
			Int("time_shift_hours", cfg.App.TimeShiftHours).
			Msg("⚠️ NON-ZERO TIME_SHIFT_HOURS CONFIGURED! This virtual offset is intended for DEMO / TESTING ONLY and MUST NOT be used in PRODUCTION!")
	}

	schedMatcher, err := schedule.LoadScheduleMatcher(cfg.Dataset.ScheduleCSVPath, loc, cfg.App.TimeShiftHours)
	if err != nil {
		return nil, fmt.Errorf("failed to load schedule matcher: %w", err)
	}
	log.Info().Int("trips_indexed", len(schedMatcher.ByTr)).Msg("stateful schedule matcher loaded")

	resolver, err := schedule.LoadUnitResolver(cfg.Dataset.UnitMappingCSVPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load unit resolver from %s: %w", cfg.Dataset.UnitMappingCSVPath, err)
	}
	log.Info().Int("units_mapped", len(resolver.ByUnit)).Msg("unit resolver mapping successfully loaded")

	fleetStore := state.NewFleetStore()

	mlCl, err := mlclient.New(cfg.MLClient.Host, cfg.MLClient.Port)
	if err != nil {
		if cfg.MLClient.EnableFallback {
			log.Warn().Err(err).Msg("ML gRPC service offline; transparent heuristic fallback is enabled")
		} else {
			log.Warn().Err(err).Msg("ML gRPC service offline; predictions remain unavailable until reconnect")
		}
	} else {
		log.Info().Str("host", cfg.MLClient.Host).Int("port", cfg.MLClient.Port).Msg("connected to ML gRPC service")
	}

	wsHub := ws.NewHub(fleetStore, log)
	handler := httpTransport.NewHandler(fleetStore, log)
	router := httpTransport.NewRouter(handler, wsHub)
	httpServer := &http.Server{
		Addr:         fmt.Sprintf("0.0.0.0:%d", cfg.HTTP.Port),
		Handler:      router,
		ReadTimeout:  cfg.HTTP.ReadTimeout,
		WriteTimeout: cfg.HTTP.WriteTimeout,
	}

	app := &App{
		cfg:          cfg,
		logger:       log,
		httpServer:   httpServer,
		wsHub:        wsHub,
		fleetStore:   fleetStore,
		scheduler:    schedMatcher,
		resolver:     resolver,
		mlClient:     mlCl,
		lastPredTime: make(map[uint32]time.Time),
	}

	telemetryHandler := func(rec *ndtp.NavRecord) {
		app.processTelemetryPoint(rec)
	}

	app.ndtpServer = ndtp.NewServer(cfg.NDTP.TCPPort, cfg.NDTP.ReadTimeout, telemetryHandler, log)
	return app, nil
}

func (a *App) processTelemetryPoint(rec *ndtp.NavRecord) {
	pt := domain.TelemetryPoint{
		UnitID:    rec.UnitID,
		EventTime: rec.Timestamp,
		Lat:       rec.Latitude,
		Lon:       rec.Longitude,
		Speed:     float64(rec.SpeedAvg),
		Heading:   float64(rec.Course),
		Valid:     rec.Valid,
	}

	trID, ok := a.resolver.Resolve(int64(pt.UnitID), pt.EventTime)
	if !ok {
		a.logger.Debug().Uint32("unit_id", pt.UnitID).Msg("unknown_unit: dropped packet without mapping")
		return
	}

	// Матчинг: выравнивает дату и рассчитывает target
	match := a.scheduler.Update(trID, pt)

	vehicleState := a.fleetStore.UpdateFromTelemetry(pt, &match)

	// Ingest на каждый входящий тик (в шкале января 2026!)
	a.sendIngest(pt, &match)

	// Predict вызывается раз в 15 секунд для неизменившейся цели.
	// Если strict-target появился/сменился, UpdateFromTelemetry инвалидирует старый
	// прогноз, и новый inference запускается немедленно, без ожидания cadence.
	var updatedVehicle *domain.VehicleState = vehicleState
	previousPredictionValid := vehicleState.PredictionValid
	previousRiskLevel := vehicleState.RiskLevel
	targetValid := match.TargetStopID > 0 && !match.TargetTimeBegin.IsZero()
	if targetValid {
		forcePredict := !vehicleState.PredictionValid
		if a.shouldPredict(pt.UnitID, forcePredict) {
			pred := a.predictDelay(pt, &match)
			if pred != nil {
				updatedVehicle = a.fleetStore.UpdateFromML(pt.UnitID, pred)
				if updatedVehicle == nil {
					updatedVehicle = vehicleState
				}
			}
		}
	} else {
		// Когда target исчез, сбрасываем cadence: следующая валидная цель должна
		// получить прогноз сразу же.
		a.resetPredictionCadence(pt.UnitID)
		a.logger.Debug().
			Int64("tr_id", trID).
			Uint32("unit_id", pt.UnitID).
			Str("skip_reason", match.TargetStatus).
			Msg("predict skipped: no valid target in strict (T+10, T+15] window")
	}

	a.wsHub.BroadcastEvent("VEHICLE_UPDATE", updatedVehicle)

	incidentBecameActive := updatedVehicle.PredictionValid &&
		(updatedVehicle.RiskLevel == "red" || updatedVehicle.RiskLevel == "yellow") &&
		(!previousPredictionValid || previousRiskLevel != updatedVehicle.RiskLevel)
	if incidentBecameActive {
		a.wsHub.BroadcastEvent("INCIDENT", domain.IncidentCard{
			UnitID:          updatedVehicle.UnitID,
			TrID:            updatedVehicle.TrID,
			RiskLevel:       updatedVehicle.RiskLevel,
			CurrentDelayS:   updatedVehicle.CurrentDelaySeconds,
			PredictedDelayS: updatedVehicle.PredictedDelaySeconds,
			Reason:          updatedVehicle.Reason,
			TargetStopID:    updatedVehicle.TargetStopID,
			TargetTimeBegin: updatedVehicle.TargetTimeBegin,
			DetectedAt:      time.Now(),
		})
	}
}

func (a *App) shouldPredict(unitID uint32, force bool) bool {
	a.predMu.Lock()
	defer a.predMu.Unlock()

	if a.lastPredTime == nil {
		a.lastPredTime = make(map[uint32]time.Time)
	}

	now := time.Now()
	lastPred, exists := a.lastPredTime[unitID]
	if !force && exists && now.Sub(lastPred) < 15*time.Second {
		return false
	}

	a.lastPredTime[unitID] = now
	return true
}

func (a *App) resetPredictionCadence(unitID uint32) {
	a.predMu.Lock()
	delete(a.lastPredTime, unitID)
	a.predMu.Unlock()
}

func (a *App) sendIngest(pt domain.TelemetryPoint, match *domain.MatchResult) {
	if a.mlClient == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), a.cfg.MLClient.Timeout)
	defer cancel()

	// ВАЖНО: EventTimeUnixMs передаем в ТОЙ ЖЕ выровненной шкале января 2026!
	ack, err := a.mlClient.Ingest(ctx, &pb.IngestRequest{
		Telemetry: &pb.TelemetryEvent{
			UnitId:          int64(pt.UnitID),
			TrId:            match.TrID,
			EventTimeUnixMs: match.AlignedEventTime.UnixMilli(),
			Lat:             pt.Lat,
			Lon:             pt.Lon,
			SpeedKmh:        pt.Speed,
			HeadingDeg:      pt.Heading,
			LocationValid:   pt.Valid,
		},
		Schedule: &pb.ScheduleState{
			Valid:                match.MatchConfidence > 0,
			CurDevS:              match.CurDevSeconds,
			LastKnownFactDelayS:  match.LastKnownFactDelayS,
			SecondsSinceLastFact: match.SecondsSinceLastFact,
			LastStopId:           match.LastStopID,
			LastStopOrder:        match.LastStopOrder,
			MatchConfidence:      match.MatchConfidence,
		},
	})

	if err != nil {
		a.logger.Warn().Err(err).Int64("tr_id", match.TrID).Msg("ML Ingest RPC failed")
	} else if !ack.Accepted && ack.Status != "duplicate_event" && ack.Status != "stale_event" {
		a.logger.Warn().Int64("tr_id", match.TrID).Str("status", ack.Status).Msg("ML Ingest rejected event")
	}
}

func (a *App) predictDelay(pt domain.TelemetryPoint, match *domain.MatchResult) *pb.PredictionResponse {
	schedState := &pb.ScheduleState{
		Valid:                match.MatchConfidence > 0,
		CurDevS:              match.CurDevSeconds,
		LastKnownFactDelayS:  match.LastKnownFactDelayS,
		SecondsSinceLastFact: match.SecondsSinceLastFact,
		LastStopId:           match.LastStopID,
		LastStopOrder:        match.LastStopOrder,
		MatchConfidence:      match.MatchConfidence,
	}

	if a.mlClient != nil {
		ctx, cancel := context.WithTimeout(context.Background(), a.cfg.MLClient.Timeout)
		defer cancel()

		// ВАЖНО: RequestTimeUnixMs передаем в ТОЙ ЖЕ выровненной шкале января 2026!
		predResp, err := a.mlClient.Predict(ctx, &pb.PredictRequest{
			TrId:              match.TrID,
			RequestTimeUnixMs: match.AlignedEventTime.UnixMilli(),
			Schedule:          schedState,
			Target: &pb.TargetPoint{
				Valid:             match.TargetStopID > 0,
				StopId:            match.TargetStopID,
				TargetTimeUnixMs:  match.TargetTimeBegin.UnixMilli(),
				Order:             match.TargetOrder,
				Lon:               match.TargetLon,
				Lat:               match.TargetLat,
				ScheduledPrevGapS: match.ScheduledPrevGapS,
			},
		})

		// ВАЖНО: Проверяем не только err == nil, но и статус ответа от ML!
		if err == nil && predResp != nil && predResp.Status == "ok" {
			a.logger.Info().
				Int64("tr_id", match.TrID).
				Str("source", "ML_SERVICE").
				Str("risk", predResp.RiskLevel).
				Float64("pred_delay_s", predResp.PredictedDelayS).
				Float64("ml_latency_ms", predResp.MlLatencyMs).
				Msg("prediction received from ML engine")
			return predResp
		}

		if predResp != nil && predResp.Status != "ok" {
			a.logger.Warn().
				Int64("tr_id", match.TrID).
				Str("ml_status", predResp.Status).
				Msg("ML Predict returned non-ok status, falling back")
		} else if err != nil {
			a.logger.Warn().Err(err).Int64("tr_id", match.TrID).Msg("ML Predict RPC failed, falling back")
		}
	}

	if !a.cfg.MLClient.EnableFallback {
		return nil
	}

	// Прозрачный эвристический fallback для degraded mode. Он помечается
	// отдельным prediction_source и не должен выдаваться за ответ ML-модели.
	riskLevel := "green"
	predDelay := match.CurDevSeconds * 1.15
	lateProb := 0.1
	reason := "резервная эвристика: движение по графику"

	if match.CurDevSeconds > 180 {
		riskLevel = "red"
		lateProb = 0.85
		reason = "резервная эвристика: критическое отставание от графика"
	} else if match.CurDevSeconds > 60 {
		riskLevel = "yellow"
		lateProb = 0.55
		reason = "резервная эвристика: нарастающее отставание"
	}

	return &pb.PredictionResponse{
		Status:           "fallback_active",
		TrId:             match.TrID,
		TargetStopId:     match.TargetStopID,
		TargetTimeUnixMs: match.TargetTimeBegin.UnixMilli(),
		HorizonS:         float64(match.HorizonSeconds),
		CurrentDelayS:    match.CurDevSeconds,
		PredictedDelayS:  predDelay,
		LateProbability:  lateProb,
		RiskLevel:        riskLevel,
		Reason:           reason,
		ReasonConfidence: 0.8,
	}
}

func (a *App) Run(ctx context.Context) error {
	go a.wsHub.Run()

	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				a.fleetStore.CheckStaleVehicles(15 * time.Second)
			}
		}
	}()

	go func() {
		a.logger.Info().Str("addr", a.httpServer.Addr).Msg("HTTP REST & WebSocket server listening")
		if err := a.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			a.logger.Error().Err(err).Msg("HTTP server error")
		}
	}()

	return a.ndtpServer.Start(ctx)
}

func (a *App) Shutdown(ctx context.Context) {
	_ = a.httpServer.Shutdown(ctx)
	a.ndtpServer.Shutdown()
	if a.mlClient != nil {
		_ = a.mlClient.Close()
	}
	a.logger.Info().Msg("all services cleanly shut down")
}
