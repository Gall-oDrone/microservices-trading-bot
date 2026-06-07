#!/usr/bin/env bash
#
# Stage execution soak helper (POST-POINT-10, after classification PASS).
# See docs/strategy-fee-accuracy/STAGE-EXECUTION-SOAK-OPERATOR-GUIDE-2026-06-04.md
#
# Usage:
#   ./scripts/run-stage-execution-soak-2026-06-04.sh check-prereqs
#   ./scripts/run-stage-execution-soak-2026-06-04.sh phase2-start   # router live, engine dry
#   ./scripts/run-stage-execution-soak-2026-06-04.sh phase2-status
#   ./scripts/run-stage-execution-soak-2026-06-04.sh rollback        # DRY_RUN=true on router + engine
#
set -euo pipefail

NAMESPACE="${NAMESPACE:-bitso-trading-dev}"
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
  mkdir -p "$LOG_DIR"
  cat >"$WINDOW_JSON" <<EOF
{
  "phase": "$phase",
  "started_at": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "classification_pass_ref": "docs/strategy-fee-accuracy/STAGE-SOAK-VERIFICATION-2026-06-07.md",
  "router_dry_run": "$(router_dry_run)",
  "engine_dry_run": "$(engine_dry_run)",
  "notes": "Phase 2: strategy-router DRY_RUN=false, trading-engine DRY_RUN=true. No Bitso orders."
}
EOF
  ok "Execution window written to $WINDOW_JSON"
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
    write_window "2"
    ok "Phase 2 started — observe ≥ 24 h; run phase2-status periodically"
    info "See docs/strategy-fee-accuracy/STAGE-EXECUTION-SOAK-OPERATOR-GUIDE-2026-06-04.md § Phase 2"
    ;;
  phase2-status)
    mkdir -p "$LOG_DIR"
    info "Execution soak window:"
    [[ -f "$WINDOW_JSON" ]] && cat "$WINDOW_JSON" || warn "No $WINDOW_JSON — run phase2-start first"
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
    info "Engine dry-run metric:"
    kubectl -n "$NAMESPACE" exec deploy/trading-engine -- \
      wget -qO- http://127.0.0.1:8080/metrics 2>/dev/null | grep '^trading_engine_dry_run' || warn "metric unavailable"
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
    err "Unknown command: $cmd (check-prereqs|phase2-start|phase2-status|rollback)"
    exit 1
    ;;
esac
