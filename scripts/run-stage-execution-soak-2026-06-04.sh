#!/usr/bin/env bash
#
# Stage execution soak helper (POST-POINT-10, after classification PASS).
# See docs/strategy-fee-accuracy/STAGE-EXECUTION-SOAK-OPERATOR-GUIDE-2026-06-04.md
#
# Usage:
#   ./scripts/run-stage-execution-soak-2026-06-04.sh check-prereqs
#   ./scripts/run-stage-execution-soak-2026-06-04.sh phase2-start   # router live, engine dry
#   ./scripts/run-stage-execution-soak-2026-06-04.sh phase2-status
#   ./scripts/run-stage-execution-soak-2026-06-04.sh phase3-start   # router dry, engine live, one strategy
#   ./scripts/run-stage-execution-soak-2026-06-04.sh phase3-status
#   ./scripts/run-stage-execution-soak-2026-06-04.sh phase4-start   # router + engine live, router-managed
#   ./scripts/run-stage-execution-soak-2026-06-04.sh phase4-status
#   ./scripts/run-stage-execution-soak-2026-06-04.sh rollback        # DRY_RUN=true on router + engine
#
set -euo pipefail

NAMESPACE="${NAMESPACE:-bitso-trading-dev}"
BOOK="${BOOK:-btc_mxn}"
STRATEGY="${STRATEGY:-mean_reversion_${BOOK}}"
PHASE2_MIN_HOURS="${PHASE2_MIN_HOURS:-24}"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
LOG_DIR="${LOG_DIR:-$ROOT/tmp/stage-execution-soak}"
CLASSIFICATION_REPORT="${CLASSIFICATION_REPORT:-$ROOT/tmp/stage-soak/soak-verification-report.json}"
WINDOW_JSON="${WINDOW_JSON:-$LOG_DIR/execution-window.json}"

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; BLUE='\033[0;34m'; NC='\033[0m'
info() { echo -e "${BLUE}[INFO]${NC} $*"; }
ok() { echo -e "${GREEN}[OK]${NC} $*"; }
warn() { echo -e "${YELLOW}[WARN]${NC} $*"; }
err() { echo -e "${RED}[ERROR]${NC} $*" 1>&2; }

cmd="${1:-phase2-status}"

check_classification_pass() {
  local samples="$ROOT/tmp/stage-soak/agreement-samples.jsonl"
  if [[ -f "$samples" ]]; then
    local summary
    summary=$(python3 - "$samples" <<'PY'
import json, sys
from pathlib import Path
rows = [json.loads(l) for l in Path(sys.argv[1]).read_text().splitlines() if l.strip()]
if not rows:
    print("0 0 0")
    sys.exit(0)
m = sum(1 for r in rows if r.get("match"))
n = len(rows)
print(n, m, round(m / n, 6))
PY
)
    local n matches rate
    read -r n matches rate <<<"$summary"
    if python3 -c "import sys; sys.exit(0 if float('${rate:-0}') >= 0.99 and int('${n:-0}') > 0 else 1)"; then
      ok "Classification short soak PASS (${matches}/${n} samples, rate=${rate})"
      return 0
    fi
  fi
  if [[ -f "$CLASSIFICATION_REPORT" ]]; then
    warn "Using report file only — re-run ./scripts/run-stage-soak-2026-06-02.sh report for latest"
    ok "Classification report present at $CLASSIFICATION_REPORT"
    return 0
  fi
  err "Classification soak not PASS — see STAGE-SOAK-VERIFICATION-2026-06-07.md"
  return 1
}

router_dry_run() {
  kubectl -n "$NAMESPACE" get deploy strategy-router -o jsonpath='{.spec.template.spec.containers[0].env[?(@.name=="DRY_RUN")].value}' 2>/dev/null || echo ""
}

engine_dry_run() {
  kubectl -n "$NAMESPACE" get deploy trading-engine -o jsonpath='{.spec.template.spec.containers[0].env[?(@.name=="DRY_RUN")].value}' 2>/dev/null || echo ""
}

write_window() {
  local phase="$1"
  local notes="${2:-}"
  mkdir -p "$LOG_DIR"
  cat >"$WINDOW_JSON" <<EOF
{
  "phase": "$phase",
  "started_at": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "classification_pass_ref": "docs/strategy-fee-accuracy/STAGE-SOAK-VERIFICATION-2026-06-07.md",
  "router_dry_run": "$(router_dry_run)",
  "engine_dry_run": "$(engine_dry_run)",
  "strategy": "$STRATEGY",
  "notes": "$notes"
}
EOF
  ok "Execution window written to $WINDOW_JSON"
}

check_phase2_time_gate() {
  if [[ ! -f "$WINDOW_JSON" ]]; then
    err "No Phase 2 window at $WINDOW_JSON — run phase2-start first"
    return 1
  fi
  local phase started
  phase=$(jq -r '.phase // ""' "$WINDOW_JSON")
  started=$(jq -r '.started_at // ""' "$WINDOW_JSON")
  if [[ "$phase" != "2" ]]; then
    warn "Current window phase=$phase (expected 2 completed before Phase 3)"
  fi
  python3 - "$started" "$PHASE2_MIN_HOURS" <<'PY'
import sys
from datetime import datetime, timezone, timedelta
started = datetime.fromisoformat(sys.argv[1].replace("Z", "+00:00"))
min_h = float(sys.argv[2])
elapsed = (datetime.now(timezone.utc) - started).total_seconds() / 3600
print(f"phase2_elapsed_hours={elapsed:.1f}")
print(f"phase2_time_gate_met={elapsed >= min_h}")
sys.exit(0 if elapsed >= min_h else 1)
PY
}

stop_all_strategies() {
  info "Stopping all running strategies..."
  kubectl -n "$NAMESPACE" exec deploy/strategy-executor -- \
    wget -qO- http://127.0.0.1:8081/api/v1/strategies 2>/dev/null \
    | jq -r '.strategies[]? | select(.running==true) | .name' \
    | while read -r n; do
        [[ -z "$n" ]] && continue
        info "  stop $n"
        kubectl -n "$NAMESPACE" exec deploy/strategy-executor -- \
          wget -qO- --post-data='' "http://127.0.0.1:8081/api/v1/strategies/${n}/stop" >/dev/null 2>&1 || true
      done
  ok "All running strategies stopped"
}

ensure_strategy_running() {
  local running
  running=$(kubectl -n "$NAMESPACE" exec deploy/strategy-executor -- \
    wget -qO- "http://127.0.0.1:8081/api/v1/strategies/${STRATEGY}" 2>/dev/null \
    | jq -r '.running // false')
  if [[ "$running" == "true" ]]; then
    ok "$STRATEGY already running"
    return 0
  fi
  info "Starting $STRATEGY manually..."
  kubectl -n "$NAMESPACE" exec deploy/strategy-executor -- \
    wget -qO- --post-data='' "http://127.0.0.1:8081/api/v1/strategies/${STRATEGY}/start" >/dev/null
  ok "Started $STRATEGY"
}

print_status() {
  info "DRY_RUN switches:"
  echo "  strategy-router DRY_RUN=$(router_dry_run)"
  echo "  trading-engine DRY_RUN=$(engine_dry_run)"
  info "Go router state:"
  kubectl -n "$NAMESPACE" exec deploy/strategy-router -- \
    wget -qO- http://127.0.0.1:8092/api/v1/router/state 2>/dev/null \
    | jq '{dry_run, routes, last: .last_decisions[0] | {regime, action, current, preferred, reason}}'
  info "Running strategies:"
  kubectl -n "$NAMESPACE" exec deploy/strategy-executor -- \
    wget -qO- http://127.0.0.1:8081/api/v1/strategies 2>/dev/null \
    | jq '[.strategies[]? | select(.running==true) | .name]'
  info "Strategy $STRATEGY parameters:"
  kubectl -n "$NAMESPACE" exec deploy/strategy-executor -- \
    wget -qO- "http://127.0.0.1:8081/api/v1/strategies/${STRATEGY}" 2>/dev/null \
    | jq '{name, running, parameters: .parameters | {position_size, dry_run}}' || warn "strategy detail unavailable"
  info "Engine dry-run metric:"
  kubectl -n "$NAMESPACE" exec deploy/trading-engine -- \
    wget -qO- http://127.0.0.1:8080/metrics 2>/dev/null | grep '^trading_engine_dry_run' || warn "metric unavailable"
  info "Engine Stage env (redacted):"
  kubectl -n "$NAMESPACE" exec deploy/trading-engine -- env 2>/dev/null \
    | grep -E '^(BITSO_API_BASE_URL|ORDER_MANAGEMENT_URL|DRY_RUN|STAGE_BITSO_API_KEY)=' \
    | sed 's/STAGE_BITSO_API_KEY=.*/STAGE_BITSO_API_KEY=<set>/' || true
}

case "$cmd" in
  check-prereqs)
    info "Checking execution soak prerequisites..."
    check_classification_pass
    kubectl -n "$NAMESPACE" get deploy strategy-router strategy-executor trading-engine market-data >/dev/null
    ok "Core deployments present"
    info "strategy-router DRY_RUN=$(router_dry_run) (Phase 2 target: false)"
    info "trading-engine DRY_RUN=$(engine_dry_run) (Phase 2 target: true)"
    ;;
  phase2-start)
    check_classification_pass
    info "Stopping classification soak helpers..."
    "$ROOT/scripts/run-stage-soak-2026-06-02.sh" stop-bash || true
    "$ROOT/scripts/stage-soak-sample-loop.sh" stop 2>/dev/null || true
    ok "Classification bash leg and sample loop stopped"
    info "Enabling router lifecycle (DRY_RUN=false on strategy-router)..."
    kubectl -n "$NAMESPACE" set env deployment/strategy-router DRY_RUN=false
    kubectl -n "$NAMESPACE" rollout status deploy/strategy-router --timeout=180s
    info "Ensuring trading-engine stays dry (DRY_RUN=true)..."
    kubectl -n "$NAMESPACE" set env deployment/trading-engine DRY_RUN=true
    kubectl -n "$NAMESPACE" rollout status deploy/trading-engine --timeout=180s
    write_window "2" "Phase 2: strategy-router DRY_RUN=false, trading-engine DRY_RUN=true. No Bitso orders."
    ok "Phase 2 started — observe ≥ 24 h; run phase2-status periodically"
    info "See docs/strategy-fee-accuracy/STAGE-EXECUTION-SOAK-OPERATOR-GUIDE-2026-06-04.md § Phase 2"
    ;;
  phase2-status)
    mkdir -p "$LOG_DIR"
    info "Execution soak window:"
    [[ -f "$WINDOW_JSON" ]] && cat "$WINDOW_JSON" || warn "No $WINDOW_JSON — run phase2-start first"
    if [[ -f "$WINDOW_JSON" ]]; then
      python3 - "$WINDOW_JSON" <<'PY'
import json, sys
from datetime import datetime, timezone
w = json.load(open(sys.argv[1]))
started = w.get("started_at", "")
if started:
    t0 = datetime.fromisoformat(started.replace("Z", "+00:00"))
    elapsed = (datetime.now(timezone.utc) - t0).total_seconds() / 3600
    print(f"Phase {w.get('phase','?')} elapsed: {elapsed:.1f}h (24h gate: {'PASS' if elapsed >= 24 else 'pending'})")
PY
    fi
    print_status
    ;;
  phase3-start)
    check_classification_pass
    check_phase2_time_gate
    info "Pulling Go audit before router mode change..."
    "$ROOT/scripts/analyze-stage-soak-agreement.sh" pull-go || warn "pull-go failed (non-fatal)"
    info "Pausing autonomous routing (DRY_RUN=true on strategy-router)..."
    kubectl -n "$NAMESPACE" set env deployment/strategy-router DRY_RUN=true
    kubectl -n "$NAMESPACE" rollout status deploy/strategy-router --timeout=180s
    ensure_strategy_running
    info "Enabling Stage order path (unset trading-engine DRY_RUN)..."
    kubectl -n "$NAMESPACE" set env deployment/trading-engine DRY_RUN-
    kubectl -n "$NAMESPACE" rollout status deploy/trading-engine --timeout=180s
    dry_metric=$(kubectl -n "$NAMESPACE" exec deploy/trading-engine -- \
      wget -qO- http://127.0.0.1:8080/metrics 2>/dev/null | awk '/^trading_engine_dry_run /{print $2}')
    if [[ "$dry_metric" != "0" ]]; then
      err "trading_engine_dry_run=$dry_metric (expected 0 for Phase 3)"
      exit 1
    fi
    ok "trading_engine_dry_run=0 — engine may place Bitso Stage orders"
    write_window "3" "Phase 3: strategy-router DRY_RUN=true, trading-engine live, one strategy for Stage round-trip."
    ok "Phase 3 started — watch engine/OM logs for first round-trip; run phase3-status"
    info "See docs/strategy-fee-accuracy/STAGE-EXECUTION-SOAK-OPERATOR-GUIDE-2026-06-04.md § Phase 3"
    print_status
    ;;
  phase3-status)
    mkdir -p "$LOG_DIR"
    info "Execution soak window:"
    [[ -f "$WINDOW_JSON" ]] && cat "$WINDOW_JSON" || warn "No $WINDOW_JSON — run phase3-start first"
    print_status
    info "Recent trading-engine order metrics:"
    kubectl -n "$NAMESPACE" exec deploy/trading-engine -- \
      wget -qO- http://127.0.0.1:8080/metrics 2>/dev/null \
      | grep -E '^trading_engine_(dry_run|orders_|signals_)' || warn "metrics unavailable"
    ;;
  phase4-start)
    check_classification_pass
    info "Stopping classification bash router (keeping port-forward for drift samples)..."
    "$ROOT/scripts/run-stage-soak-2026-06-02.sh" stop-bash-router-only || true
    stop_all_strategies
    info "Registering canonical router strategies (stopped, POSITION_SIZE=0.001)..."
    NAMESPACE="$NAMESPACE" BOOK="$BOOK" ROUTER_MANAGED=true ROUTER_CANONICAL_NAMES=true \
      POSITION_SIZE=0.001 STRATEGY_TYPES=mean_reversion,momentum,limit_profit \
      "$ROOT/scripts/start-organic-trading.sh" 2>&1 | tail -8 || warn "register had warnings (strategies may already exist)"
    info "Enabling router-managed execution (DRY_RUN=false on strategy-router)..."
    kubectl -n "$NAMESPACE" set env deployment/strategy-router DRY_RUN=false
    kubectl -n "$NAMESPACE" rollout status deploy/strategy-router --timeout=180s
    info "Ensuring trading-engine stays live (DRY_RUN unset)..."
    kubectl -n "$NAMESPACE" set env deployment/trading-engine DRY_RUN-
    kubectl -n "$NAMESPACE" rollout status deploy/trading-engine --timeout=180s
    dry_metric=$(kubectl -n "$NAMESPACE" exec deploy/trading-engine -- \
      wget -qO- http://127.0.0.1:8080/metrics 2>/dev/null | awk '/^trading_engine_dry_run /{print $2}')
    if [[ "$dry_metric" != "0" ]]; then
      err "trading_engine_dry_run=$dry_metric (expected 0 for Phase 4)"
      exit 1
    fi
    router_dry=$(router_dry_run)
    if [[ "$router_dry" == "true" ]]; then
      err "strategy-router still DRY_RUN=true"
      exit 1
    fi
    ok "Phase 4 live — router manages start/stop; engine may place Bitso Stage orders"
    write_window "4" "Phase 4: strategy-router DRY_RUN=false, trading-engine live, router-managed canonical strategies."
    ok "Phase 4 started — observe ≥ 24–48 h; run phase4-status periodically"
    info "Restarting classification sample loop (30 min interval)..."
    "$ROOT/scripts/stage-soak-sample-loop.sh" start 2>/dev/null || warn "sample loop start failed — run manually"
    info "See docs/strategy-fee-accuracy/STAGE-EXECUTION-SOAK-OPERATOR-GUIDE-2026-06-04.md § Phase 4"
    print_status
    ;;
  phase4-status)
    mkdir -p "$LOG_DIR"
    info "Execution soak window:"
    [[ -f "$WINDOW_JSON" ]] && cat "$WINDOW_JSON" || warn "No $WINDOW_JSON — run phase4-start first"
    if [[ -f "$WINDOW_JSON" ]]; then
      python3 - "$WINDOW_JSON" <<'PY'
import json, sys
from datetime import datetime, timezone
w = json.load(open(sys.argv[1]))
started = w.get("started_at", "")
if started:
    t0 = datetime.fromisoformat(started.replace("Z", "+00:00"))
    elapsed = (datetime.now(timezone.utc) - t0).total_seconds() / 3600
    print(f"Phase {w.get('phase','?')} elapsed: {elapsed:.1f}h (24h gate: {'PASS' if elapsed >= 24 else 'pending'})")
PY
    fi
    print_status
    info "Router metrics:"
    kubectl -n "$NAMESPACE" exec deploy/strategy-router -- \
      wget -qO- http://127.0.0.1:8092/metrics 2>/dev/null \
      | grep -E '^strategy_router_(evaluation|switch|error)' || warn "router metrics unavailable"
    info "Trading-engine order metrics:"
    kubectl -n "$NAMESPACE" exec deploy/trading-engine -- \
      wget -qO- http://127.0.0.1:8080/metrics 2>/dev/null \
      | grep -E '^trading_engine_(dry_run|orders_|signals_)' || warn "metrics unavailable"
    info "Classification sample loop:"
    "$ROOT/scripts/stage-soak-sample-loop.sh" status 2>/dev/null || warn "sample loop status unavailable"
    samples="$ROOT/tmp/stage-soak/agreement-samples.jsonl"
    if [[ -f "$samples" ]]; then
      python3 - "$samples" <<'PY'
import json, sys
from datetime import datetime, timezone
from pathlib import Path
rows = [json.loads(l) for l in Path(sys.argv[1]).read_text().splitlines() if l.strip()]
if not rows:
    sys.exit(0)
last = rows[-1]
ts = datetime.fromisoformat(last["timestamp"].replace("Z", "+00:00"))
age_h = (datetime.now(timezone.utc) - ts).total_seconds() / 3600
m = sum(1 for r in rows if r.get("match"))
print(f"  samples={len(rows)} matches={m} rate={m/len(rows):.4f} last_age_h={age_h:.1f}")
if age_h > 1.5:
    print(f"  WARN: last sample stale ({last['timestamp']}) — run: ./scripts/stage-soak-sample-loop.sh start")
PY
    fi
    ;;
  rollback)
    warn "Rolling back to safe mode (router + engine DRY_RUN=true)..."
    kubectl -n "$NAMESPACE" set env deployment/strategy-router DRY_RUN=true
    kubectl -n "$NAMESPACE" rollout status deploy/strategy-router --timeout=180s
    kubectl -n "$NAMESPACE" set env deployment/trading-engine DRY_RUN=true
    kubectl -n "$NAMESPACE" rollout status deploy/trading-engine --timeout=180s
    ok "Rollback complete — strategies may still be running; stop manually if needed"
    ;;
  *)
    err "Unknown command: $cmd (check-prereqs|phase2-start|phase2-status|phase3-start|phase3-status|phase4-start|phase4-status|rollback)"
    exit 1
    ;;
esac
