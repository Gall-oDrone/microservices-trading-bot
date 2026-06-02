#!/usr/bin/env bash
#
# Strategy Regime Router — picks the active strategy based on live market indicators.
#
# Full design + rationale: docs/strategy-fee-accuracy/STRATEGY-REGIME-ROUTER.md
#
# What this script does on a loop:
#   1. GET /api/v1/indicators/{book}/snapshot from strategy-executor
#   2. Compute the current "regime" (low_vol_range, trending_up, trending_down,
#      high_vol, neutral) from ATR, EMA slope, RSI, Bollinger %B.
#   3. Resolve regime → preferred strategy (see ROUTING_TABLE below).
#   4. If the preferred strategy differs from the currently running one (single-
#      active-strategy assumption), stop the current one and start the preferred.
#
# Hard guardrails (the router will refuse to switch when any of these is true):
#   - The currently running strategy has a non-zero `has_position` (we do not
#     hand a live position off to another strategy mid-flight).
#   - The currently running strategy was started less than COOLDOWN_SECS ago.
#   - The preferred strategy is not registered.
#
# Usage:
#   ./scripts/strategy-regime-router.sh                  # run forever
#   ROUTER_DURATION_SEC=600 ./scripts/strategy-regime-router.sh   # run for 10 min
#   DRY_RUN=true ./scripts/strategy-regime-router.sh     # only print decisions
#
# Required tools: kubectl (for in-cluster port-forward) OR a reachable
# STRATEGY_EXECUTOR_URL, plus curl + jq.

set -euo pipefail

# --- Configuration (override via env) ---
NAMESPACE="${NAMESPACE:-bitso-trading-dev}"
BOOK="${BOOK:-btc_mxn}"
STRATEGY_EXECUTOR_URL="${STRATEGY_EXECUTOR_URL:-}"
STRATEGY_EXECUTOR_LOCAL_PORT="${STRATEGY_EXECUTOR_LOCAL_PORT:-8084}"
STRATEGY_EXECUTOR_SVC_PORT="${STRATEGY_EXECUTOR_SVC_PORT:-8081}"

# Polling and lifecycle
ROUTER_INTERVAL_SEC="${ROUTER_INTERVAL_SEC:-30}"
ROUTER_DURATION_SEC="${ROUTER_DURATION_SEC:-0}"   # 0 = forever
COOLDOWN_SECS="${COOLDOWN_SECS:-120}"             # min seconds between strategy switches
DRY_RUN="${DRY_RUN:-false}"

# Regime thresholds (tune to your book; see docs/strategy-fee-accuracy/STRATEGY-REGIME-ROUTER.md)
ATR_HIGH_VOL_PCT="${ATR_HIGH_VOL_PCT:-1.5}"   # ATR/price * 100 > this  → high_vol
ATR_LOW_VOL_PCT="${ATR_LOW_VOL_PCT:-0.30}"    # ATR/price * 100 < this  → low_vol candidate
RSI_OVERBOUGHT="${RSI_OVERBOUGHT:-70}"
RSI_OVERSOLD="${RSI_OVERSOLD:-30}"
BB_PB_UPPER="${BB_PB_UPPER:-0.85}"            # Bollinger %B > this → near upper band (potential reversal)
BB_PB_LOWER="${BB_PB_LOWER:-0.15}"            # Bollinger %B < this → near lower band

# Routing table: regime → strategy name (must be already registered in strategy-executor)
# Defaults follow the fee-aware recommendation in docs/strategy-fee-accuracy/POINT-9-FEES-AND-POINT-10-REGIMES.md:
#   - low-volatility / range-bound → mean_reversion (wider entry/exit, better fee/profit ratio)
#   - trending                     → momentum (RSI + EMA, larger expected moves)
#   - high-volatility chop         → none (pause)
#   - limit_profit                 → only on books with maker rebates AND tight spreads
ROUTE_LOW_VOL="${ROUTE_LOW_VOL:-mean_reversion_${BOOK}}"
ROUTE_TRENDING_UP="${ROUTE_TRENDING_UP:-momentum_${BOOK}}"
ROUTE_TRENDING_DOWN="${ROUTE_TRENDING_DOWN:-momentum_${BOOK}}"
ROUTE_HIGH_VOL="${ROUTE_HIGH_VOL:-none}"
ROUTE_NEUTRAL="${ROUTE_NEUTRAL:-mean_reversion_${BOOK}}"

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; BLUE='\033[0;34m'; CYAN='\033[0;36m'; NC='\033[0m'
info()    { echo -e "${BLUE}[INFO]${NC} $*"; }
ok()      { echo -e "${GREEN}[OK]${NC} $*"; }
warn()    { echo -e "${YELLOW}[WARN]${NC} $*"; }
err()     { echo -e "${RED}[ERROR]${NC} $*" 1>&2; }
section() { echo -e "\n${CYAN}=== $* ===${NC}"; }

require() {
  local tool="$1"
  command -v "$tool" >/dev/null 2>&1 || { err "Missing required tool: $tool"; exit 1; }
}
require curl
require jq

PF_PID=""
cleanup() {
  if [[ -n "$PF_PID" ]] && kill -0 "$PF_PID" 2>/dev/null; then
    kill "$PF_PID" 2>/dev/null || true
  fi
}
trap cleanup EXIT INT TERM

ensure_strategy_executor_url() {
  if [[ -n "$STRATEGY_EXECUTOR_URL" ]]; then
    return
  fi
  require kubectl
  info "Starting port-forward to strategy-executor (-n $NAMESPACE) :$STRATEGY_EXECUTOR_LOCAL_PORT → $STRATEGY_EXECUTOR_SVC_PORT"
  kubectl -n "$NAMESPACE" port-forward svc/strategy-executor "$STRATEGY_EXECUTOR_LOCAL_PORT:$STRATEGY_EXECUTOR_SVC_PORT" >/dev/null 2>&1 &
  PF_PID=$!
  STRATEGY_EXECUTOR_URL="http://127.0.0.1:${STRATEGY_EXECUTOR_LOCAL_PORT}"
  sleep 2
}

get_snapshot() {
  curl -fsS "${STRATEGY_EXECUTOR_URL}/api/v1/indicators/${BOOK}/snapshot" 2>/dev/null || return 1
}

list_strategies() {
  curl -fsS "${STRATEGY_EXECUTOR_URL}/api/v1/strategies" 2>/dev/null || return 1
}

# get_running_strategy_name prints the first name with running==true (assumes single-active-strategy mode).
get_running_strategy_name() {
  list_strategies | jq -r '.strategies[] | select(.running == true) | .name' 2>/dev/null | head -n1
}

# get_strategy_state returns the JSON state document for a strategy.
get_strategy_state() {
  curl -fsS "${STRATEGY_EXECUTOR_URL}/api/v1/strategies/$1/state" 2>/dev/null || return 1
}

stop_strategy()  {
  if [[ "$DRY_RUN" == "true" ]]; then warn "[dry-run] would STOP $1"; return; fi
  curl -fsS -X POST "${STRATEGY_EXECUTOR_URL}/api/v1/strategies/$1/stop" >/dev/null && ok "Stopped $1"
}
start_strategy() {
  if [[ "$DRY_RUN" == "true" ]]; then warn "[dry-run] would START $1"; return; fi
  curl -fsS -X POST "${STRATEGY_EXECUTOR_URL}/api/v1/strategies/$1/start" >/dev/null && ok "Started $1"
}

# classify_regime echoes one of: low_vol_range | trending_up | trending_down | high_vol | neutral
classify_regime() {
  local snapshot="$1"

  # Extract values; defaults to "0" when an indicator hasn't warmed up yet.
  local atr ema_value rsi_value bb_lower bb_upper bb_middle price
  # strategy-executor snapshot uses snake_case in docs; wire JSON may use PascalCase (no json tags on IndicatorValue).
  atr=$(echo "$snapshot"      | jq -r '.atr.value // .atr.Value // 0')
  ema_value=$(echo "$snapshot" | jq -r '.ema.value // .ema.Value // 0')
  rsi_value=$(echo "$snapshot" | jq -r '.rsi.value // .rsi.Value // 0')
  bb_lower=$(echo "$snapshot" | jq -r '.bollinger.lower_band // .bollinger.Lower // 0')
  bb_upper=$(echo "$snapshot" | jq -r '.bollinger.upper_band // .bollinger.Upper // 0')
  bb_middle=$(echo "$snapshot"| jq -r '.bollinger.middle_band // .bollinger.Middle // 0')
  price=$(echo "$snapshot"    | jq -r '.bollinger.current_price // .sma.value // .sma.Value // .ema.value // .ema.Value // 0')

  if [[ "$price" == "0" || "$price" == "null" ]]; then
    echo "neutral"
    return
  fi

  # ATR as % of price
  local atr_pct
  atr_pct=$(awk -v a="$atr" -v p="$price" 'BEGIN{ if(p>0){printf "%.4f", (a/p)*100} else {print 0} }')

  # Bollinger %B = (price - lower)/(upper - lower)
  local pb
  pb=$(awk -v u="$bb_upper" -v l="$bb_lower" -v p="$price" 'BEGIN{ if(u>l){printf "%.4f", (p-l)/(u-l)} else {print 0.5} }')

  # EMA slope proxy: price vs EMA distance as % of price
  local ema_dist_pct
  ema_dist_pct=$(awk -v e="$ema_value" -v p="$price" 'BEGIN{ if(p>0){printf "%.4f", ((p-e)/p)*100} else {print 0} }')

  info "regime inputs: price=$price atr_pct=$atr_pct rsi=$rsi_value pb=$pb ema_dist_pct=$ema_dist_pct"

  # High volatility takes precedence — pause trading.
  if awk -v a="$atr_pct" -v t="$ATR_HIGH_VOL_PCT" 'BEGIN{ exit !(a>t) }'; then
    echo "high_vol"; return
  fi

  # Low-volatility range: tight ATR + price near the middle of the band.
  if awk -v a="$atr_pct" -v t="$ATR_LOW_VOL_PCT" 'BEGIN{ exit !(a<t) }' \
     && awk -v pb="$pb" -v lo="$BB_PB_LOWER" -v hi="$BB_PB_UPPER" 'BEGIN{ exit !(pb>lo && pb<hi) }'; then
    echo "low_vol_range"; return
  fi

  # Trending up: price above EMA, RSI not yet overbought.
  if awk -v d="$ema_dist_pct" 'BEGIN{ exit !(d>0.10) }' \
     && awk -v r="$rsi_value" -v ob="$RSI_OVERBOUGHT" 'BEGIN{ exit !(r<ob) }'; then
    echo "trending_up"; return
  fi

  # Trending down: price below EMA, RSI not yet oversold.
  if awk -v d="$ema_dist_pct" 'BEGIN{ exit !(d<-0.10) }' \
     && awk -v r="$rsi_value" -v os="$RSI_OVERSOLD" 'BEGIN{ exit !(r>os) }'; then
    echo "trending_down"; return
  fi

  echo "neutral"
}

# resolve_route echoes the strategy name preferred for a regime (or empty for "pause").
resolve_route() {
  case "$1" in
    low_vol_range)   echo "$ROUTE_LOW_VOL" ;;
    trending_up)     echo "$ROUTE_TRENDING_UP" ;;
    trending_down)   echo "$ROUTE_TRENDING_DOWN" ;;
    high_vol)        echo "$ROUTE_HIGH_VOL" ;;
    *)               echo "$ROUTE_NEUTRAL" ;;
  esac
}

LAST_SWITCH_AT=0
SWITCH_LOG="/tmp/strategy-regime-router.log"

run_one_cycle() {
  local snapshot regime preferred current pos
  snapshot=$(get_snapshot) || { warn "snapshot fetch failed"; return; }
  regime=$(classify_regime "$snapshot")
  preferred=$(resolve_route "$regime")
  current=$(get_running_strategy_name || echo "")

  info "regime=$regime  preferred=$preferred  current=${current:-<none>}"

  if [[ "$preferred" == "none" || -z "$preferred" ]]; then
    if [[ -n "$current" ]]; then
      warn "Regime $regime → pausing; stopping current strategy ($current)"
      stop_strategy "$current"
      LAST_SWITCH_AT=$(date +%s)
      echo "$(date -u +%FT%TZ) regime=$regime → pause stop=$current" >> "$SWITCH_LOG"
    fi
    return
  fi

  if [[ "$preferred" == "$current" ]]; then
    return  # nothing to do
  fi

  # Guardrail: do not switch when current has an open position.
  if [[ -n "$current" ]]; then
    local state has_pos
    state=$(get_strategy_state "$current" || echo "{}")
    has_pos=$(echo "$state" | jq -r '.has_position // false')
    if [[ "$has_pos" == "true" ]]; then
      warn "Refusing to switch: $current has an open position; waiting for it to close."
      return
    fi
  fi

  # Guardrail: cooldown between switches.
  local now=$(date +%s)
  if (( now - LAST_SWITCH_AT < COOLDOWN_SECS )); then
    warn "Cooldown: $((COOLDOWN_SECS - (now - LAST_SWITCH_AT)))s remaining; staying on ${current:-<none>}"
    return
  fi

  # Verify the preferred strategy is registered.
  if ! list_strategies | jq -e ".strategies[] | select(.name==\"$preferred\")" >/dev/null 2>&1; then
    warn "Preferred strategy '$preferred' is not registered. Skipping. Register it via scripts/start-organic-trading.sh first."
    return
  fi

  if [[ -n "$current" ]]; then
    stop_strategy "$current"
  fi
  start_strategy "$preferred"
  LAST_SWITCH_AT=$(date +%s)
  echo "$(date -u +%FT%TZ) regime=$regime stop=${current:-<none>} start=$preferred" >> "$SWITCH_LOG"
}

main() {
  section "Strategy Regime Router"
  info "BOOK=$BOOK INTERVAL=${ROUTER_INTERVAL_SEC}s COOLDOWN=${COOLDOWN_SECS}s DRY_RUN=$DRY_RUN"
  info "Routes: low_vol_range=$ROUTE_LOW_VOL  trending_up=$ROUTE_TRENDING_UP  trending_down=$ROUTE_TRENDING_DOWN  high_vol=$ROUTE_HIGH_VOL  neutral=$ROUTE_NEUTRAL"
  ensure_strategy_executor_url
  info "strategy-executor URL: $STRATEGY_EXECUTOR_URL"

  local started_at deadline
  started_at=$(date +%s)
  if (( ROUTER_DURATION_SEC > 0 )); then
    deadline=$((started_at + ROUTER_DURATION_SEC))
    info "Will exit after ${ROUTER_DURATION_SEC}s"
  else
    deadline=0
    info "Running indefinitely (CTRL-C to stop)"
  fi

  while true; do
    run_one_cycle || warn "cycle errored; continuing"
    if (( deadline > 0 )) && (( $(date +%s) >= deadline )); then
      info "Reached deadline; exiting"
      return 0
    fi
    sleep "$ROUTER_INTERVAL_SEC"
  done
}

main "$@"
