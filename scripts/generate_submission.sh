#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DATASET_DIR="${DATASET_DIR:-$ROOT/dataset}"
OUT_DIR="${SUBMISSION_DIR:-$ROOT/output}"
OUT_FILE="${SUBMISSION_FILE:-submission.csv}"

required_dataset=(
  "$DATASET_DIR/validate/points.csv"
  "$DATASET_DIR/validate/traffic.csv"
  "$DATASET_DIR/validate/schedule_plan.csv"
  "$DATASET_DIR/sample_submission.csv"
)
required_models=(
  "$ROOT/models/model_manifest.json"
  "$ROOT/models/lgbm_residual_final_seed42.onnx"
  "$ROOT/models/lgbm_late_final.onnx"
)

for f in "${required_dataset[@]}" "${required_models[@]}"; do
  if [[ ! -s "$f" ]]; then
    echo "Missing required file: $f" >&2
    exit 1
  fi
done

mkdir -p "$OUT_DIR"
rm -f "$OUT_DIR/$OUT_FILE"

cd "$ROOT"
docker compose build ml

docker compose run --rm --no-deps \
  -v "$DATASET_DIR:/dataset:ro" \
  -v "$OUT_DIR:/output" \
  ml \
  --model-dir /models \
  --generate-submission \
  --submission-only \
  --dataset-dir /dataset \
  --submission-out "/output/$OUT_FILE"

python3 - "$OUT_DIR/$OUT_FILE" <<'PY'
import csv, math, sys
path = sys.argv[1]
with open(path, encoding="utf-8", newline="") as f:
    rows = list(csv.DictReader(f, delimiter=";"))
if not rows:
    raise SystemExit("submission is empty")
if set(rows[0]) != {"sample_id", "prediction"}:
    raise SystemExit(f"unexpected columns: {list(rows[0])}")
ids = [r["sample_id"] for r in rows]
if len(ids) != len(set(ids)):
    raise SystemExit("duplicate sample_id")
for r in rows:
    v = float(r["prediction"])
    if not math.isfinite(v):
        raise SystemExit(f"non-finite prediction for {r['sample_id']}")
print(f"submission OK: rows={len(rows)} unique_ids={len(set(ids))} path={path}")
PY
