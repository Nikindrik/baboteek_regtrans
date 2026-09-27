package domain

import (
	"time"
)

// ScheduleStop представляет одну плановую остановку из schedule_plan.csv
type ScheduleStop struct {
	ActionItemID int64     `json:"action_item_id"` // tt_action_item_id
	TrID         int64     `json:"tr_id"`
	TimeBegin    time.Time `json:"time_begin"`
	Lat          float64   `json:"lat"`
	Lon          float64   `json:"lon"`
	Address      string    `json:"address"`
}

// TelemetryPoint — чистая точка навигации
type TelemetryPoint struct {
	UnitID    uint32    `json:"unit_id"`
	EventTime time.Time `json:"event_time"`
	Lat       float64   `json:"lat"`
	Lon       float64   `json:"lon"`
	Speed     float64   `json:"speed"`
	Heading   float64   `json:"heading"`
	Valid     bool      `json:"valid"`
}

// MatchResult — результат сопоставления телеметрии с расписанием
type MatchResult struct {
	TrID                 int64     `json:"tr_id"`
	AlignedEventTime     time.Time `json:"aligned_event_time"`
	LastStopID           int64     `json:"last_stop_id"`
	LastStopOrder        int32     `json:"last_stop_order"`
	CurDevSeconds        float64   `json:"cur_dev_s"`
	LastKnownFactDelayS  float64   `json:"last_known_fact_delay_s"`
	SecondsSinceLastFact float64   `json:"seconds_since_last_fact"`
	MatchConfidence      float64   `json:"match_confidence"`

	TargetStopID      int64     `json:"target_stop_id"`
	TargetTimeBegin   time.Time `json:"target_time_begin"`
	TargetOrder       int32     `json:"target_order"`
	TargetLat         float64   `json:"target_lat"`
	TargetLon         float64   `json:"target_lon"`
	HorizonSeconds    int64     `json:"horizon_s"`
	ScheduledPrevGapS float64   `json:"scheduled_prev_gap_s"`

	TargetStatus     string `json:"target_status"`
	ForecastEligible bool   `json:"forecast_eligible"`
}

// VehicleState — полное оперативное состояние одного транспортного средства
type VehicleState struct {
	UnitID    uint32    `json:"unit_id"`
	TrID      int64     `json:"tr_id"`
	UpdatedAt time.Time `json:"updated_at"`
	IsOnline  bool      `json:"is_online"`

	// Кинематика и навигация
	LastPoint TelemetryPoint   `json:"last_point"`
	Track     []TelemetryPoint `json:"track"` // 10 последних точек для карты

	// Сопоставление с планом
	TargetStopID        int64     `json:"target_stop_id"`
	TargetTimeBegin     time.Time `json:"target_time_begin"`
	CurrentDelaySeconds float64   `json:"current_delay_s"`
	ForecastEligible    bool      `json:"forecast_eligible"` // есть расписание для данного tr_id

	// Простые диагностические признаки, рассчитанные Backend по live-телеметрии.
	SegmentAvgSpeedKMH float64 `json:"segment_avg_speed_kmh"`
	DwellTimeSeconds   float64 `json:"dwell_time_s"`

	// Предсказание ML / degraded fallback.
	PredictionValid       bool    `json:"prediction_valid"`
	PredictionSource      string  `json:"prediction_source"` // "ml", "fallback" или ""
	MLLatencyMS           float64 `json:"ml_latency_ms"`
	PredictedDelaySeconds float64 `json:"predicted_delay_s"`
	LateProbability       float64 `json:"late_probability"`
	RiskLevel             string  `json:"risk_level"` // "green", "yellow", "red"
	Reason                string  `json:"reason"`
	ReasonConfidence      float64 `json:"reason_confidence"`
}

// IncidentCard — карточка сбоя для диспетчера (Критерий 4)
type IncidentCard struct {
	UnitID          uint32    `json:"unit_id"`
	TrID            int64     `json:"tr_id"`
	RiskLevel       string    `json:"risk_level"`
	CurrentDelayS   float64   `json:"current_delay_s"`
	PredictedDelayS float64   `json:"predicted_delay_s"`
	Reason          string    `json:"reason"`
	TargetStopID    int64     `json:"target_stop_id"`
	TargetTimeBegin time.Time `json:"target_time_begin"`
	DetectedAt      time.Time `json:"detected_at"`
}

// TrafficLightWhatIfRequest — параметры сценарной оценки продления зелёной фазы.
type TrafficLightWhatIfRequest struct {
	UnitID           uint32 `json:"unit_id" example:"985940"`
	ExtensionSeconds int    `json:"extension_s" example:"15"`
}

// TrafficLightWhatIfResponse — результат What-if без изменения live-state.
type TrafficLightWhatIfResponse struct {
	Scenario                 string    `json:"scenario"`
	UnitID                   uint32    `json:"unit_id"`
	TrID                     int64     `json:"tr_id"`
	TargetStopID             int64     `json:"target_stop_id"`
	TargetTimeBegin          time.Time `json:"target_time_begin"`
	ExtensionSeconds         int       `json:"extension_s"`
	BaselinePredictedDelayS  float64   `json:"baseline_predicted_delay_s"`
	ScenarioPredictedDelayS  float64   `json:"scenario_predicted_delay_s"`
	EstimatedDelayReductionS float64   `json:"estimated_delay_reduction_s"`
	BaselineLateProbability  float64   `json:"baseline_late_probability"`
	BaselineRiskLevel        string    `json:"baseline_risk_level"`
	LiveStateChanged         bool      `json:"live_state_changed"`
	GeneratedAt              time.Time `json:"generated_at"`
	Assumption               string    `json:"assumption"`
}

// ReserveWhatIfRequest — параметры сценария выпуска резервного ТС.
type ReserveWhatIfRequest struct {
	UnitID             uint32 `json:"unit_id" example:"985940"`
	DispatchETASeconds int    `json:"dispatch_eta_s" example:"300"`
}

// ReserveWhatIfResponse — оценка влияния резерва на разрыв обслуживания.
type ReserveWhatIfResponse struct {
	Scenario                      string    `json:"scenario"`
	UnitID                        uint32    `json:"unit_id"`
	TrID                          int64     `json:"tr_id"`
	TargetStopID                  int64     `json:"target_stop_id"`
	TargetTimeBegin               time.Time `json:"target_time_begin"`
	DispatchETASeconds            int       `json:"dispatch_eta_s"`
	BaselineVehicleDelayS         float64   `json:"baseline_vehicle_delay_s"`
	VehicleDelayAfterActionS      float64   `json:"vehicle_delay_after_action_s"`
	ScenarioServiceDelayS         float64   `json:"scenario_service_delay_s"`
	EstimatedServiceGapReductionS float64   `json:"estimated_service_gap_reduction_s"`
	BaselineLateProbability       float64   `json:"baseline_late_probability"`
	BaselineRiskLevel             string    `json:"baseline_risk_level"`
	LiveStateChanged              bool      `json:"live_state_changed"`
	GeneratedAt                   time.Time `json:"generated_at"`
	Assumption                    string    `json:"assumption"`
}
