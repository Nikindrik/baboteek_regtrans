export interface TelemetryPoint {
  unit_id: number;
  event_time: string;
  lat: number;
  lon: number;
  speed: number;
  heading: number;
  valid: boolean;
}

export interface VehicleState {
  unit_id: number;
  tr_id: number;
  updated_at: string;
  is_online: boolean;
  last_point: TelemetryPoint;
  track: TelemetryPoint[];

  target_stop_id: number;
  target_time_begin: string;

  current_delay_s: number;
  forecast_eligible?: boolean;
  segment_avg_speed_kmh?: number;
  dwell_time_s?: number;

  prediction_valid?: boolean;
  prediction_source?: "ml" | "fallback" | string;
  ml_latency_ms?: number;
  predicted_delay_s: number;
  late_probability: number;

  risk_level: "green" | "yellow" | "red";

  reason: string;
  reason_confidence: number;
}

export interface IncidentCard {
  unit_id: number;
  tr_id: number;

  risk_level: "yellow" | "red";

  current_delay_s: number;
  prediction_valid?: boolean;
  predicted_delay_s: number;

  reason: string;

  target_stop_id: number;
  target_time_begin: string;
  detected_at: string;
}

export interface TrafficLightWhatIfResponse {
  scenario: "traffic_light";
  unit_id: number;
  tr_id: number;
  target_stop_id: number;
  target_time_begin: string;
  extension_s: number;
  baseline_predicted_delay_s: number;
  scenario_predicted_delay_s: number;
  estimated_delay_reduction_s: number;
  baseline_late_probability: number;
  baseline_risk_level: string;
  live_state_changed: boolean;
  generated_at: string;
  assumption: string;
}

export interface ReserveWhatIfResponse {
  scenario: "reserve_vehicle";
  unit_id: number;
  tr_id: number;
  target_stop_id: number;
  target_time_begin: string;
  dispatch_eta_s: number;
  baseline_vehicle_delay_s: number;
  vehicle_delay_after_action_s: number;
  scenario_service_delay_s: number;
  estimated_service_gap_reduction_s: number;
  baseline_late_probability: number;
  baseline_risk_level: string;
  live_state_changed: boolean;
  generated_at: string;
  assumption: string;
}

export type WSEvent =
  | {type:"INIT"; payload:{vehicles:VehicleState[]; incidents:IncidentCard[]}}
  | {type:"VEHICLE_UPDATE"; payload:VehicleState}
  | {type:"INCIDENT"; payload:IncidentCard};
