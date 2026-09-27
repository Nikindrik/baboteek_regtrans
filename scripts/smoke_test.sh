#!/usr/bin/env bash
set -euo pipefail

BASE="${BACKEND_URL:-http://127.0.0.1:8080}"
EMU="${EMULATOR_URL:-http://127.0.0.1:18080}"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

pass(){ printf 'PASS  %s\n' "$*"; }
fail(){ printf 'FAIL  %s\n' "$*" >&2; exit 1; }
warn(){ printf 'WARN  %s\n' "$*"; }

curl -fsS "$BASE/healthz" -o "$TMP/health.json" || fail "backend /healthz"
pass "backend health"

curl -fsS "$BASE/swagger/doc.json" -o "$TMP/swagger.json" || fail "Swagger JSON"
python3 -m json.tool "$TMP/swagger.json" >/dev/null || fail "Swagger JSON syntax"
pass "Swagger/OpenAPI"

curl -fsS "$EMU/api/config" -o "$TMP/emu.json" || fail "emulator REST"
EMU_COUNT=$(python3 - "$TMP/emu.json" <<'PY'
import json,sys
x=json.load(open(sys.argv[1])); print(len(x.get('units',[])))
PY
)
[[ "$EMU_COUNT" -eq 56 ]] && pass "emulator fleet = 56" || warn "emulator fleet is $EMU_COUNT (demo expects 56)"

# Give the NDTP stream enough time to populate live state.
for _ in {1..30}; do
  curl -fsS "$BASE/api/v1/fleet" -o "$TMP/fleet.json" || true
  COUNT=$(python3 - "$TMP/fleet.json" 2>/dev/null <<'PY' || echo 0
import json,sys
try: print(len(json.load(open(sys.argv[1]))))
except Exception: print(0)
PY
)
  [[ "$COUNT" -gt 0 ]] && break
  sleep 1
done
[[ "${COUNT:-0}" -gt 0 ]] || fail "fleet stayed empty"

python3 - "$TMP/fleet.json" <<'PY'
import json,sys
fleet=json.load(open(sys.argv[1]))

online=sum(bool(v.get('is_online')) for v in fleet)
context=sum(v.get('forecast_eligible') is False for v in fleet)
eligible=sum(v.get('forecast_eligible') is True for v in fleet)
valid=sum(v.get('prediction_valid') is True for v in fleet)
fallback=sum(v.get('prediction_source')=='fallback' for v in fleet)
ml=sum(v.get('prediction_source')=='ml' for v in fleet)
print(f"Fleet: total={len(fleet)} online={online} context={context} forecast_eligible={eligible} prediction_valid={valid} ml={ml} fallback={fallback}")

errors=[]
for v in fleet:
    uid=v.get('unit_id')
    target=v.get('target_stop_id') or 0
    valid_pred=v.get('prediction_valid') is True
    eligible_v=v.get('forecast_eligible') is not False
    if not eligible_v and (target != 0 or valid_pred):
        errors.append(f"context unit {uid}: fake target/prediction")
    if target == 0:
        if valid_pred:
            errors.append(f"unit {uid}: target=0 but prediction_valid=true")
        if abs(float(v.get('predicted_delay_s') or 0)) > 1e-9:
            errors.append(f"unit {uid}: target=0 but predicted_delay_s != 0")
        if abs(float(v.get('late_probability') or 0)) > 1e-9:
            errors.append(f"unit {uid}: target=0 but late_probability != 0")
        if v.get('prediction_source') not in (None, ''):
            errors.append(f"unit {uid}: target=0 but prediction_source={v.get('prediction_source')!r}")
    if valid_pred and v.get('prediction_source') not in ('ml','fallback'):
        errors.append(f"unit {uid}: valid prediction without explicit source")
if errors:
    print("STATE INVARIANT FAILURES:")
    for e in errors: print(" -",e)
    raise SystemExit(1)
print("State invariants: OK")
PY
pass "context/target/prediction lifecycle invariants"

# Context vehicle: What-if must be rejected when one is currently visible.
CTX_UID=$(python3 - "$TMP/fleet.json" <<'PY'
import json,sys
for v in json.load(open(sys.argv[1])):
    if v.get('forecast_eligible') is False:
        print(v['unit_id']); break
PY
)
if [[ -n "$CTX_UID" ]]; then
  code=$(curl -sS -o "$TMP/ctx_whatif.json" -w '%{http_code}' -X POST "$BASE/api/v1/what-if/traffic-light" \
      -H 'Content-Type: application/json' -d "{\"unit_id\":$CTX_UID,\"extension_s\":15}")
  [[ "$code" == "409" ]] && pass "context-only What-if guardrail = 409" || fail "context-only What-if returned HTTP $code"
else
  warn "no context-only vehicle was visible yet; skipped 409 context test"
fi

# If a valid current prediction exists, both scenario endpoints must be non-mutating.
VALID_UID=$(python3 - "$TMP/fleet.json" <<'PY'
import json,sys
for v in json.load(open(sys.argv[1])):
    if v.get('prediction_valid') is True and (v.get('target_stop_id') or 0)>0:
        print(v['unit_id']); break
PY
)
if [[ -n "$VALID_UID" ]]; then
  curl -fsS -X POST "$BASE/api/v1/what-if/traffic-light" -H 'Content-Type: application/json' \
    -d "{\"unit_id\":$VALID_UID,\"extension_s\":15}" -o "$TMP/tl.json" || fail "traffic-light What-if"
  curl -fsS -X POST "$BASE/api/v1/what-if/reserve" -H 'Content-Type: application/json' \
    -d "{\"unit_id\":$VALID_UID,\"dispatch_eta_s\":300}" -o "$TMP/reserve.json" || fail "reserve What-if"
  python3 - "$TMP/tl.json" "$TMP/reserve.json" <<'PY'
import json,sys
for p in sys.argv[1:]:
    x=json.load(open(p))
    if x.get('live_state_changed') is not False:
        raise SystemExit(f"{p}: live_state_changed must be false")
print("What-if responses non-mutating: OK")
PY
  pass "both What-if endpoints"
else
  warn "no strict-horizon prediction currently available; valid What-if test skipped (this can be legitimate)"
fi

# Active incidents may only correspond to current valid predictions.
curl -fsS "$BASE/api/v1/incidents" -o "$TMP/incidents.json" || fail "active incidents endpoint"
python3 - "$TMP/fleet.json" "$TMP/incidents.json" <<'PY'
import json,sys
fleet={v['unit_id']:v for v in json.load(open(sys.argv[1]))}
incs=json.load(open(sys.argv[2]))
seen=set()
for inc in incs:
    uid=inc['unit_id']
    if uid in seen: raise SystemExit(f"duplicate active incident for {uid}")
    seen.add(uid)
    v=fleet.get(uid)
    if not v or v.get('prediction_valid') is not True or v.get('risk_level') not in ('yellow','red'):
        raise SystemExit(f"stale/invalid active incident for {uid}")
print(f"Active incidents: {len(incs)}; invariant OK")
PY
pass "active incident dedup/lifecycle"

echo "ALL AVAILABLE LIVE SMOKE TESTS PASSED"
