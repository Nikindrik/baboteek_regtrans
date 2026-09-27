# Mosgortrans Backend

Go orchestration service for the hackathon demo:

`NDTP :9201 -> parser -> unit_id/tr_id resolver -> stateful schedule matcher -> gRPC ML -> fleet state -> REST/WebSocket :8080`.

## Important live-state semantics

- Strict target window is `(T+10 min, T+15 min]`.
- Vehicles present in telemetry but absent from `schedule_plan.csv` are **context-only**. They are not an error and do not receive a fake target or prediction.
- `forecast_eligible=false` means the vehicle has no schedule in the evaluation plan.
- `forecast_eligible=true, prediction_valid=false` means the vehicle is forecast-capable but there is no valid current strict-horizon forecast (for example no stop in the 10–15 minute window).
- A forecast is tied to exact `(tr_id, target_stop_id, target_time_begin)` identity. A target change invalidates the old forecast and stale ML replies are rejected.
- `prediction_source=ml` is a real ONNX prediction. `prediction_source=fallback` is an explicitly marked degraded heuristic and is never presented as model output.

## Canonical emulator configs

- `emulator/config.json`: 30 validate-era vehicles: 13 schedule-backed + 17 context-only.
- `emulator/config_stress_56.json`: optional stress config adding 26 synthetic train vehicles.
- `emulator/config.compose.json`: same canonical 30 vehicles, but `targetHost=backend` for Docker Compose networking.

## Local run

Copy `.env.example` to `.env` and adjust paths/clock shift as needed.

```bash
go test ./...
go run ./cmd/server
```

Endpoints:

- `GET /healthz`
- `GET /api/v1/fleet`
- `GET /api/v1/incidents`
- `POST /api/v1/what-if/traffic-light`
- `POST /api/v1/what-if/reserve`
- `GET /ws/fleet`
- Swagger: `http://localhost:8080/swagger/index.html`

## Backend-derived diagnostics

The live state includes `segment_avg_speed_kmh` and `dwell_time_s`, calculated from the recent telemetry breadcrumb window. The ML service still owns the richer rolling 96-feature set.

## Demo clock

`TIME_SHIFT_HOURS` exists only to align the real-time synthetic emulator clock with the historical January-2026 schedule used for the demo. It must not be described as production behavior and must never be used to manufacture a target for a context-only vehicle.
