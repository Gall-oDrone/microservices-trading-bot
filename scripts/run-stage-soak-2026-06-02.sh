#!/usr/bin/env bash
#
# Run or verify POST-POINT-10 Stage soak (bash + Go regime router, DRY_RUN=true).
# See docs/strategy-fee-accuracy/STAGE-SOAK-OPERATOR-GUIDE-2026-06-02.md
#
# Usage:
#   ./scripts/run-stage-soak-2026-06-02.sh check          # prerequisites + one regime compare
#   ./scripts/run-stage-soak-2026-06-02.sh register       # ROUTER_MANAGED canonical names
#   ./scripts/run-stage-soak-2026-06-02.sh start-bash     # port-forward + bash router in background
#   ./scripts/run-stage-soak-2026-06-02.sh stop-bash      # stop bash router + port-forward
#   ./scripts/run-stage-soak-2026-06-02.sh status         # router state, processes, ATR warmup
#   ./scripts/run-stage-soak-2026-06-02.sh report         # pull Go audit + agreement analysis
#   ./scripts/run-stage-soak-2026-06-02.sh sample         # one live bash vs Go regime sample
#
set -euo pipefail

NAMESPACE="${NAMESPACE:-bitso-trading-dev}"
BOOK="${BOOK:-btc_mxn}"
PF_PORT="${STRATEGY_EXECUTOR_LOCAL_PORT:-8084}"
LOG_DIR="${LOG_DIR:-$(cd "$(dirname "$0")/.." && pwd)/tmp/stage-soak}"
BOOK_ROUTE_SUFFIX="${BOOK_ROUTE_SUFFIX:-${BOOK}}"

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; BLUE='\033[0;34m'; CYAN='\033[0;36m'; NC='\033[0m'
info() { echo -e "${BLUE}[INFO]${NC} $*"; }
ok() { echo -e "${GREEN}[OK]${NC} $*"; }
warn() { echo -e "${YELLOW}[WARN]${NC} $*"; }
err() { echo -e "${RED}[ERROR]${NC} $*" 1>&2; }

cmd="${1:-status}"

ensure_pf() {
  if curl -fsS --max-time 2 "http://127.0.0.1:${PF_PORT}/health" >/dev/null 2>&1; then
    return 0
  fi
  mkdir -p "$LOG_DIR"
  # Reuse port-forward from a prior register/start if still healthy
  if [[ -f "$LOG_DIR/port-forward.pid" ]] && kill -0 "$(cat "$LOG_DIR/port-forward.pid")" 2>/dev/null; then
    sleep 1
    curl -fsS --max-time 3 "http://127.0.0.1:${PF_PORT}/health" >/dev/null 2>&1 && return 0
  fi
  kubectl -n "$NAMESPACE" port-forward "svc/strategy-executor" "${PF_PORT}:8081" >>"$LOG_DIR/port-forward.log" 2>&1 &
  echo $! >"$LOG_DIR/port-forward.pid"
  for _ in 1 2 3 4 5; do
    sleep 1
    curl -fsS --max-time 2 "http://127.0.0.1:${PF_PORT}/health" >/dev/null 2>&1 && return 0
  done
  err "port-forward to strategy-executor failed on :${PF_PORT}"
  return 1
}

stop_pf() {
  if [[ -f "$LOG_DIR/port-forward.pid" ]]; then
    kill "$(cat "$LOG_DIR/port-forward.pid")" 2>/dev/null || true
    rm -f "$LOG_DIR/port-forward.pid"
  fi
  pkill -f "port-forward.*${PF_PORT}:8081" 2>/dev/null || true
}

stop_bash_router_only() {
  if [[ -f "$LOG_DIR/bash-router.pid" ]]; then
    kill "$(cat "$LOG_DIR/bash-router.pid")" 2>/dev/null || true
    rm -f "$LOG_DIR/bash-router.pid"
  fi
  pkill -f 'scripts/strategy-regime-router.sh' 2>/dev/null || true
}

stop_bash() {
  stop_bash_router_only
  stop_pf
}

compare_one_regime() {
  ensure_pf || return 1
  local se="http://127.0.0.1:${PF_PORT}"
  local snap go_regime bash_regime
  snap=$(curl -fsS "${se}/api/v1/indicators/${BOOK}/snapshot")
  go_regime=$(kubectl -n "$NAMESPACE" exec deploy/strategy-router -- \
    wget -qO- --post-data='' http://127.0.0.1:8092/api/v1/router/run 2>/dev/null \
    | jq -r '.decisions[0].regime // "unknown"')
  if [[ -z "$go_regime" || "$go_regime" == "unknown" || "$go_regime" == "null" ]]; then
    go_regime=$(kubectl -n "$NAMESPACE" exec deploy/strategy-router -- \
      wget -qO- http://127.0.0.1:8092/api/v1/router/state 2>/dev/null \
      | jq -r '.last_decisions[0].regime // "unknown"')
  fi
  # Classify same snapshot as bash router (see scripts/strategy-regime-router.sh)
  bash_regime=$(echo "$snap" | jq -r --argjson hi 0.85 --argjson lo 0.15 '
    def price: (.current_price // .bollinger.current_price // .bollinger.middle_band // .sma.value // .sma.Value // 0);
    def atr: (.atr.value // .atr.Value // 0);
    def atr_pct: if price > 0 then (atr / price) * 100 else 0 end;
    def pb:
      if (.bollinger.upper_band // .bollinger.Upper // 0) > (.bollinger.lower_band // .bollinger.Lower // 0) then
        (price - (.bollinger.lower_band // .bollinger.Lower)) /
        ((.bollinger.upper_band // .bollinger.Upper) - (.bollinger.lower_band // .bollinger.Lower))
      else 0.5 end;
    def ema_dist_pct:
      if price > 0 then ((price - (.ema.value // .ema.Value // price)) / price) * 100 else 0 end;
    def rsi: (.rsi.value // .rsi.Value // 50);
    if price == 0 then "neutral"
    elif atr_pct > 1.5 then "high_vol"
    elif atr_pct < 0.30 and pb > $lo and pb < $hi then "low_vol_range"
    elif ema_dist_pct > 0.10 and rsi < 70 then "trending_up"
    elif ema_dist_pct < -0.10 and rsi > 30 then "trending_down"
    else "neutral" end
  ')
  echo "go_regime=$go_regime bash_regime=$bash_regime"
  if [[ "$go_regime" == "$bash_regime" ]]; then
    ok "Regimes match for current snapshot"
  else
    warn "Regime mismatch — check threshold env and snapshot JSON (see strategy-regime-router.sh jq paths)"
  fi
  local atr_val
  atr_val=$(echo "$snap" | jq -r '.atr.value // .atr.Value // empty')
  if [[ -z "$atr_val" ]]; then
    warn "ATR missing in snapshot — high_vol/low_vol_range may be skewed until market-data serves OHLCV bars (GET /api/v1/bars)"
  else
    ok "ATR present: $atr_val"
  fi
}

case "$cmd" in
  check)
    info "Checking soak prerequisites in $NAMESPACE..."
    kubectl -n "$NAMESPACE" get deploy market-data strategy-executor strategy-router >/dev/null
    dry=$(kubectl -n "$NAMESPACE" get deploy strategy-router -o jsonpath='{.spec.template.spec.containers[0].env[?(@.name=="DRY_RUN")].value}')
    if [[ "$dry" != "true" ]]; then
      warn "strategy-router DRY_RUN=$dry (expected true during soak)"
    else
      ok "strategy-router DRY_RUN=true"
    fi
    compare_one_regime
    ;;
  register)
    info "Registering canonical router strategies (stopped)..."
    ROOT="$(cd "$(dirname "$0")/.." && pwd)"
    NAMESPACE="$NAMESPACE" BOOK="$BOOK" ROUTER_MANAGED=true ROUTER_CANONICAL_NAMES=true \
      STRATEGY_TYPES=mean_reversion,momentum,limit_profit \
      "$ROOT/scripts/start-organic-trading.sh" || warn "Some strategies may already exist"
    ok "Registration pass complete (names: mean_reversion_${BOOK_ROUTE_SUFFIX}, momentum_${BOOK_ROUTE_SUFFIX}, limit_profit_${BOOK_ROUTE_SUFFIX})"
    ;;
  start-bash)
    mkdir -p "$LOG_DIR"
    stop_bash
    ensure_pf
    nohup env DRY_RUN=true BOOK="$BOOK" ROUTER_INTERVAL_SEC=30 ROUTER_DURATION_SEC=0 \
      ROUTE_LOW_VOL="mean_reversion_${BOOK_ROUTE_SUFFIX}" \
      ROUTE_NEUTRAL="mean_reversion_${BOOK_ROUTE_SUFFIX}" \
      ROUTE_TRENDING_UP="momentum_${BOOK_ROUTE_SUFFIX}" \
      ROUTE_TRENDING_DOWN="momentum_${BOOK_ROUTE_SUFFIX}" \
      STRATEGY_EXECUTOR_URL="http://127.0.0.1:${PF_PORT}" \
      "$(dirname "$0")/strategy-regime-router.sh" >>"$LOG_DIR/bash-router-soak.log" 2>&1 &
    echo $! >"$LOG_DIR/bash-router.pid"
    ok "Bash router logging to $LOG_DIR/bash-router-soak.log (PID $(cat "$LOG_DIR/bash-router.pid"))"
    info "Let run 24–48h; compare with: kubectl exec deploy/strategy-router -- tail /tmp/strategy-regime-router.log"
    ;;
  stop-bash)
    stop_bash
    ok "Stopped bash soak helpers"
    ;;
  stop-bash-router-only)
    stop_bash_router_only
    ok "Stopped bash router (port-forward kept for classification samples)"
    ;;
  status)
    mkdir -p "$LOG_DIR"
    info "Go router:"
    kubectl -n "$NAMESPACE" exec deploy/strategy-router -- \
      wget -qO- http://127.0.0.1:8092/api/v1/router/state 2>/dev/null \
      | jq '{dry_run, routes, last: .last_decisions[0] | {regime, action, current, preferred, atr_pct: .snapshot.ATRPct}}'
    info "Running strategies:"
    kubectl -n "$NAMESPACE" exec deploy/strategy-executor -- \
      wget -qO- http://127.0.0.1:8081/api/v1/strategies 2>/dev/null \
      | jq '[.strategies[]? | select(.running==true) | .name]'
    info "Soak helpers:"
    pgrep -af 'strategy-regime-router|port-forward.*'"${PF_PORT}"':8081' || echo "(none)"
    [[ -f "$LOG_DIR/bash-router-soak.log" ]] && tail -3 "$LOG_DIR/bash-router-soak.log" || true
    ;;
  report)
    "$(dirname "$0")/analyze-stage-soak-agreement.sh" report
    ;;
  sample)
    "$(dirname "$0")/analyze-stage-soak-agreement.sh" sample
    ;;
  *)
    err "Unknown command: $cmd (check|register|start-bash|stop-bash|status|report|sample)"
    exit 1
    ;;
esac
