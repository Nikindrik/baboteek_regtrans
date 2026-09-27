#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

required_models=(
  models/lgbm_residual_final_seed42.onnx
  models/lgbm_late_final.onnx
  models/model_manifest.json
)
for f in "${required_models[@]}"; do
  if [[ ! -s "$f" ]]; then
    echo "ERROR: missing required ML artifact: $f" >&2
    echo "Copy the canonical three files into $ROOT/models first." >&2
    exit 2
  fi
done

if ! docker image inspect ndtp-telemetry-emulator:1.0 >/dev/null 2>&1; then
  echo "ERROR: organizer image ndtp-telemetry-emulator:1.0 is not loaded." >&2
  echo "Run: docker load -i /path/to/ndtp-telemetry-emulator.tar" >&2
  exit 3
fi

# Align current Moscow clock to a dense, known-active part of the historical
# 2026-01-06 schedule. This is demo-only and never changes context-only eligibility.
if [[ -z "${DEMO_TIME_SHIFT_HOURS:-}" ]]; then
  current_hour_raw="$(TZ=Europe/Moscow date +%H)"
  current_hour=$((10#$current_hour_raw))
  export DEMO_TIME_SHIFT_HOURS=$((7-current_hour))
fi
echo "Demo clock shift: ${DEMO_TIME_SHIFT_HOURS}h (current MSK hour -> ~07:xx historical schedule)"

echo "[1/4] Building and starting services..."
docker compose up -d --build

echo "[2/4] Waiting for Backend HTTP..."
for i in {1..90}; do
  if curl -fsS http://127.0.0.1:8080/healthz >/dev/null 2>&1; then
    break
  fi
  if [[ "$i" == 90 ]]; then
    echo "ERROR: backend did not become healthy in time" >&2
    docker compose logs --tail=150 backend >&2 || true
    exit 4
  fi
  sleep 1
done

echo "[3/4] Waiting for emulator REST API..."
for i in {1..60}; do
  if curl -fsS http://127.0.0.1:18080/api/config >/dev/null 2>&1; then
    break
  fi
  if [[ "$i" == 60 ]]; then
    echo "ERROR: emulator REST API did not become available" >&2
    docker compose logs --tail=150 emulator >&2 || true
    exit 5
  fi
  sleep 1
done

echo "[4/4] Loading 56-vehicle emulator config..."
curl -fsS -X POST http://127.0.0.1:18080/api/config \
  -H 'Content-Type: application/json' \
  --data-binary @backend/emulator/config.compose.56.json >/dev/null

echo
echo "Demo stack started."
echo "Dashboard: http://localhost:8088/"
echo "Swagger:   http://localhost:8088/swagger/index.html"
echo "Backend:   http://localhost:8080/healthz"
echo "Emulator:  http://localhost:18080/api/config"
echo
echo "Run ./scripts/smoke_test.sh after ~20-30 seconds of telemetry."
