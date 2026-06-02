#!/usr/bin/env bash
#
# Start organic (market-driven) strategy execution — no synthetic /process ticks.
# Full context: docs/ORGANIC-TRADING-STARTUP.md
# limit_profit strategy: docs/LIMIT-PROFIT-STRATEGY.md
# momentum strategy:    docs/MOMENTUM-STRATEGY.md
#
# What this script DOES:
#   - Verifies kubectl + namespace + core pods exist
#   - Checks strategy-executor logs for Redis (Connected vs in-memory fallback)
#   - Port-forwards strategy-executor, POSTs create + start for the configured strategy
#     types (single via STRATEGY_TYPE, or multiple via STRATEGY_TYPES=a,b,c)
#
# What you MUST verify outside this script (manual / cluster-specific):
#   - kubectl context is the intended EKS/cluster
#   - market-data is receiving live trades for BOOK (logs, Grafana)
#   - Kafka topic trading.signals exists; trading-engine consumes (see docs for replica caveats)
#   - trading-engine: DRY_RUN unset, Stage URL + secrets, OM pre-trade validation reachable
#   - POSITION_SIZE meets order-management / Bitso minimums
#
# Usage:
#   ./scripts/start-organic-trading.sh
#   NAMESPACE=my-ns BOOK=btc_mxn ./scripts/start-organic-trading.sh
#
set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m'

info() { echo -e "${BLUE}[INFO]${NC} $1"; }
ok() { echo -e "${GREEN}[OK]${NC} $1"; }
warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }
err() { echo -e "${RED}[ERROR]${NC} $1"; }
section() { echo -e "\n${CYAN}=== $1 ===${NC}"; }

# --- Configuration (override via env) ---
NAMESPACE="${NAMESPACE:-bitso-trading-dev}"
BOOK="${BOOK:-btc_mxn}"
# STRATEGY_TYPE: mean_reversion | limit_profit | momentum (must exist in strategy-executor)
# STRATEGY_TYPES: comma-separated list to register more than one in a single run
#   (e.g. STRATEGY_TYPES=mean_reversion,momentum). When set, takes precedence
#   over STRATEGY_TYPE; STRATEGY_NAME is ignored and per-type names are derived.
STRATEGY_TYPE="${STRATEGY_TYPE:-mean_reversion}"
STRATEGY_TYPES="${STRATEGY_TYPES:-}"
STRATEGY_NAME="${STRATEGY_NAME:-organic_${STRATEGY_TYPE}_$(date +%s)}"
STRATEGY_EXECUTOR_LOCAL_PORT="${STRATEGY_EXECUTOR_LOCAL_PORT:-8084}"
STRATEGY_EXECUTOR_SVC_PORT="${STRATEGY_EXECUTOR_SVC_PORT:-8081}"

# Mean reversion params — tune for more/fewer organic signals (see FINANCIAL-STRATEGY-IMPLEMENTATION-GUIDE.md)
POSITION_SIZE="${POSITION_SIZE:-0.001}"
LOOKBACK_PERIOD="${LOOKBACK_PERIOD:-20}"
ENTRY_THRESHOLD="${ENTRY_THRESHOLD:-2.0}"
EXIT_THRESHOLD="${EXIT_THRESHOLD:-0.5}"

# Momentum params (RSI + EMA cross) — see docs/MOMENTUM-STRATEGY.md
# RSI_PERIOD/EMA_PERIOD are surfaced for audit; the indicator service uses its
# own configured periods (cfg.Indicators.RSIPeriod / EMAPeriod) at read time.
RSI_PERIOD="${RSI_PERIOD:-14}"
OVERBOUGHT_LEVEL="${OVERBOUGHT_LEVEL:-70}"
OVERSOLD_LEVEL="${OVERSOLD_LEVEL:-30}"
EMA_PERIOD="${EMA_PERIOD:-20}"
MOMENTUM_MIN_SIGNAL_INTERVAL="${MOMENTUM_MIN_SIGNAL_INTERVAL:-60}"
MOMENTUM_MIN_CONFIDENCE="${MOMENTUM_MIN_CONFIDENCE:-0}"

# limit_profit params — buy at reference + entry_offset; sell when last >= entry + min_profit + fee_addon
# Optional lifecycle: PENDING_BUY_TIMEOUT_SEC, MAX_POSITION_HOLD_SEC, STOP_LOSS_QUOTE — docs/LIMIT-PROFIT-ROBUSTNESS.md
# (see docs/LIMIT-PROFIT-STRATEGY.md, docs/LIMIT-PROFIT-IMPROVEMENTS.md)
ENTRY_OFFSET="${ENTRY_OFFSET:-500}"
ENTRY_OFFSET_BPS="${ENTRY_OFFSET_BPS:-0}"          # if >0, offset = reference * bps / 10000 (takes precedence)
MIN_PROFIT_LP="${MIN_PROFIT_LP:-5000}"
MIN_PROFIT_BPS="${MIN_PROFIT_BPS:-0}"              # if >0, min_profit = entry * bps / 10000 (takes precedence)
FEE_LP="${FEE_LP:-0}"
FEE_BPS_LP="${FEE_BPS_LP:-0}"
BUY_LIQUIDITY="${BUY_LIQUIDITY:-maker}"
SELL_LIQUIDITY="${SELL_LIQUIDITY:-taker}"
EXIT_PRICE_REF="${EXIT_PRICE_REF:-last}"
LP_REFERENCE="${LP_REFERENCE:-last_trade}"
MIN_SIGNAL_INTERVAL="${MIN_SIGNAL_INTERVAL:-60}"
# Lifecycle (0 = disabled) — see docs/LIMIT-PROFIT-ROBUSTNESS.md
PENDING_BUY_TIMEOUT_SEC="${PENDING_BUY_TIMEOUT_SEC:-0}"
PENDING_CANCEL_MAX_RETRIES="${PENDING_CANCEL_MAX_RETRIES:-3}"
MAX_POSITION_HOLD_SEC="${MAX_POSITION_HOLD_SEC:-0}"
STOP_LOSS_QUOTE="${STOP_LOSS_QUOTE:-0}"
# Circuit breaker (0 = disabled)
MAX_DAILY_LOSS_QUOTE="${MAX_DAILY_LOSS_QUOTE:-0}"
DAILY_LOSS_RESET_HOUR_UTC="${DAILY_LOSS_RESET_HOUR_UTC:-0}"
# Trailing stop (0 = disabled)
TRAILING_STOP_QUOTE="${TRAILING_STOP_QUOTE:-0}"
TRAILING_STOP_ACTIVATION_QUOTE="${TRAILING_STOP_ACTIVATION_QUOTE:-0}"
# Max pending orders (default 1)
MAX_PENDING_ORDERS="${MAX_PENDING_ORDERS:-1}"
# ATR-scaled sizing (sizing_mode: fixed or atr_scaled)
SIZING_MODE="${SIZING_MODE:-fixed}"
TARGET_RISK_QUOTE="${TARGET_RISK_QUOTE:-0}"
ATR_MULTIPLIER="${ATR_MULTIPLIER:-1.0}"
ATR_PERIOD="${ATR_PERIOD:-14}"
# Dry run mode (true = emit signals with dry_run=true, trading-engine should skip execution)
DRY_RUN="${DRY_RUN:-false}"
# Router-managed mode: register strategies but do not start — strategy-router picks the active one.
# When true, defaults STRATEGY_TYPES to mean_reversion,limit_profit,momentum unless overridden.
ROUTER_MANAGED="${ROUTER_MANAGED:-false}"
# When ROUTER_MANAGED=true, use stable names (mean_reversion_btc_mxn) instead of organic_* timestamps
# so strategy-router ROUTE_* env vars match. Set ROUTER_CANONICAL_NAMES=false to keep organic_* names.
ROUTER_CANONICAL_NAMES="${ROUTER_CANONICAL_NAMES:-true}"
# Momentum lifecycle (0 = disabled) — parity with limit_profit; see docs/MOMENTUM-STRATEGY.md §7
MOMENTUM_MAX_POSITION_HOLD_SEC="${MOMENTUM_MAX_POSITION_HOLD_SEC:-0}"
MOMENTUM_STOP_LOSS_QUOTE="${MOMENTUM_STOP_LOSS_QUOTE:-0}"
MOMENTUM_MAX_DAILY_LOSS_QUOTE="${MOMENTUM_MAX_DAILY_LOSS_QUOTE:-0}"
MOMENTUM_DAILY_LOSS_RESET_HOUR_UTC="${MOMENTUM_DAILY_LOSS_RESET_HOUR_UTC:-0}"
# Bitso GET /fees for limit_profit exit thresholds (requires BITSO_API_* on strategy-executor)
USE_BITSO_FEES_LP="${USE_BITSO_FEES_LP:-true}"
# Cleanup on exit (true = delete strategy when script exits)
CLEANUP_ON_EXIT="${CLEANUP_ON_EXIT:-false}"

REQUIRED_LABELS=( "service=market-data" "service=strategy-executor" "service=trading-engine" "service=order-management" "service=redis" "service=kafka" )

section "0. Tooling"
if ! command -v kubectl &>/dev/null; then
  err "kubectl not found. Install kubectl and configure cluster access."
  exit 1
fi
if ! command -v curl &>/dev/null || ! command -v jq &>/dev/null; then
  err "curl and jq are required."
  exit 1
fi
ok "kubectl, curl, jq present"
info "kubectl context: $(kubectl config current-context 2>/dev/null || echo 'unknown')"

section "1. Namespace and pods (verify workloads Running)"
if ! kubectl get namespace "$NAMESPACE" &>/dev/null; then
  err "Namespace $NAMESPACE does not exist."
  exit 1
fi
ok "Namespace $NAMESPACE exists"

# strategy-executor: registry + running strategies are in-memory per pod. With replicas>1,
# API create/start lands on one pod while ticks/fills run everywhere — strategies only exist
# on one pod; kubectl logs deploy/... may tail a different pod (zero "Published" lines);
# Kafka order-fill may be handled by a pod that has no matching strategy (stuck pending_buy).
SE_REPLICAS="$(kubectl get deploy strategy-executor -n "$NAMESPACE" -o jsonpath='{.spec.replicas}' 2>/dev/null || echo '')"
if [[ -n "$SE_REPLICAS" && "$SE_REPLICAS" != "1" ]]; then
  warn "strategy-executor replicas=$SE_REPLICAS (expected 1 for organic trading). Scale: kubectl -n $NAMESPACE scale deploy/strategy-executor --replicas=1"
fi

MISSING=0
for spec in "${REQUIRED_LABELS[@]}"; do
  key="${spec%%=*}"
  val="${spec#*=}"
  n=$(kubectl get pods -n "$NAMESPACE" -l "$key=$val" --no-headers 2>/dev/null | grep -c Running || true)
  if [[ "$n" -eq 0 ]]; then
    warn "No Running pod for -l $key=$val (check deployments)"
    MISSING=1
  else
    ok "Running pod(s) for -l $key=$val: $n"
  fi
done
if [[ "$MISSING" -ne 0 ]]; then
  warn "Some expected services have no Running pods — fix cluster before relying on organic flow."
fi

section "2. Redis for strategy-executor (verify Connected to Redis)"
# TODO(user): If you see only "in-memory store", fix REDIS_HOST/password/network; indicators reset on restart without Redis.
# Startup lines may be far back — scan more lines than recent indicator noise.
REDIS_LOG=$(kubectl logs -n "$NAMESPACE" deploy/strategy-executor --tail=5000 2>/dev/null || true)
if echo "$REDIS_LOG" | grep -q "Connected to Redis"; then
  ok "Log line found: Connected to Redis"
elif echo "$REDIS_LOG" | grep -q "using in-memory store"; then
  warn "Redis fallback in use (in-memory indicator store). Check redis pod, password secret, network."
else
  warn "Could not confirm Redis from last 5000 log lines (JSON/logger format may differ). Check: kubectl logs -n $NAMESPACE deploy/strategy-executor --tail=5000 | grep -i redis"
fi

section "3. Port-forward strategy-executor (required for local API calls)"
# Optional: if port is busy, user must free it or set STRATEGY_EXECUTOR_LOCAL_PORT
if command -v ss &>/dev/null && ss -ltnp 2>/dev/null | grep -q ":${STRATEGY_EXECUTOR_LOCAL_PORT}\\b"; then
  warn "Port $STRATEGY_EXECUTOR_LOCAL_PORT may be in use; set STRATEGY_EXECUTOR_LOCAL_PORT or stop the other process."
fi
kubectl -n "$NAMESPACE" port-forward "svc/strategy-executor" "${STRATEGY_EXECUTOR_LOCAL_PORT}:${STRATEGY_EXECUTOR_SVC_PORT}" >>/tmp/start-organic-trading-pf.log 2>&1 &
PF_PID=$!
sleep 5
if ! kill -0 "$PF_PID" 2>/dev/null; then
  err "port-forward failed. See /tmp/start-organic-trading-pf.log"
  exit 1
fi
ok "port-forward PID $PF_PID → http://127.0.0.1:${STRATEGY_EXECUTOR_LOCAL_PORT}"

SE_URL="http://127.0.0.1:${STRATEGY_EXECUTOR_LOCAL_PORT}"

# Cleanup function for trap. CLEANUP_NAMES is appended after each successful
# create_and_start so multi-strategy runs (STRATEGY_TYPES) clean up everything.
declare -a CLEANUP_NAMES=()
cleanup() {
  local exit_code=$?
  if [[ "$CLEANUP_ON_EXIT" == "true" ]]; then
    for sname in "${CLEANUP_NAMES[@]}"; do
      [[ -z "$sname" ]] && continue
      info "Cleaning up: deleting strategy $sname..."
      curl -sS --max-time 10 -X DELETE "$SE_URL/api/v1/strategies/$sname" >/dev/null 2>&1 || true
      ok "Strategy $sname deleted"
    done
  fi
  kill $PF_PID 2>/dev/null || true
  exit $exit_code
}
trap cleanup EXIT INT TERM

section "3.5. Indicator warm-up check"
INDICATOR_RESP=$(curl -sS --max-time 10 "$SE_URL/api/v1/indicators/$BOOK/snapshot" 2>/dev/null || echo '{}')
INDICATOR_COUNT=$(echo "$INDICATOR_RESP" | jq 'if type == "object" then (keys | length) else 0 end' 2>/dev/null || echo "0")
if [[ "$INDICATOR_COUNT" -eq 0 ]] || echo "$INDICATOR_RESP" | jq -e '.error' &>/dev/null; then
  warn "Indicator snapshot for $BOOK is empty or errored. Strategy may not emit signals until indicators warm up."
  warn "  Response: $(echo "$INDICATOR_RESP" | head -c 200)"
  warn "  Ensure market-data is ingesting trades for $BOOK and wait for indicator computation."
else
  ok "Indicator snapshot has $INDICATOR_COUNT indicators for $BOOK"
  # Show key indicators if available
  SMA=$(echo "$INDICATOR_RESP" | jq -r '.sma_20.value // "N/A"' 2>/dev/null)
  RSI=$(echo "$INDICATOR_RESP" | jq -r '.rsi_14.value // "N/A"' 2>/dev/null)
  VWAP=$(echo "$INDICATOR_RESP" | jq -r '.vwap.value // "N/A"' 2>/dev/null)
  info "  SMA(20)=$SMA  RSI(14)=$RSI  VWAP=$VWAP"
fi

section "4. Health + create + start strategy (organic path — no /process)"
if ! curl -sS --max-time 5 "$SE_URL/health" | jq -e '.status == "healthy"' &>/dev/null; then
  err "strategy-executor /health not healthy at $SE_URL"
  exit 1
fi
ok "strategy-executor healthy"

# Convert DRY_RUN / USE_BITSO_FEES_LP strings to booleans for jq
DRY_RUN_BOOL="false"
[[ "$DRY_RUN" == "true" ]] && DRY_RUN_BOOL="true"
USE_BITSO_FEES_BOOL="true"
[[ "$USE_BITSO_FEES_LP" == "false" ]] && USE_BITSO_FEES_BOOL="false"

# build_create_body <type> <name> — emits JSON body for POST /api/v1/strategies
build_create_body() {
  local stype="$1"
  local sname="$2"
  case "$stype" in
    limit_profit)
      jq -nc \
        --arg name "$sname" \
        --arg book "$BOOK" \
        --arg ref "$LP_REFERENCE" \
        --arg buyl "$BUY_LIQUIDITY" \
        --arg selll "$SELL_LIQUIDITY" \
        --arg xref "$EXIT_PRICE_REF" \
        --arg smode "$SIZING_MODE" \
        --argjson eo "$ENTRY_OFFSET" \
        --argjson eobps "$ENTRY_OFFSET_BPS" \
        --argjson mp "$MIN_PROFIT_LP" \
        --argjson mpbps "$MIN_PROFIT_BPS" \
        --argjson fee "$FEE_LP" \
        --argjson fbps "$FEE_BPS_LP" \
        --argjson ps "$POSITION_SIZE" \
        --argjson msi "$MIN_SIGNAL_INTERVAL" \
        --argjson pbto "$PENDING_BUY_TIMEOUT_SEC" \
        --argjson pcmr "$PENDING_CANCEL_MAX_RETRIES" \
        --argjson mhold "$MAX_POSITION_HOLD_SEC" \
        --argjson slq "$STOP_LOSS_QUOTE" \
        --argjson mdlq "$MAX_DAILY_LOSS_QUOTE" \
        --argjson dlrh "$DAILY_LOSS_RESET_HOUR_UTC" \
        --argjson tsq "$TRAILING_STOP_QUOTE" \
        --argjson tsaq "$TRAILING_STOP_ACTIVATION_QUOTE" \
        --argjson mpo "$MAX_PENDING_ORDERS" \
        --argjson trq "$TARGET_RISK_QUOTE" \
        --argjson atrm "$ATR_MULTIPLIER" \
        --argjson atrp "$ATR_PERIOD" \
        --argjson dryrun "$DRY_RUN_BOOL" \
        --argjson usebf "$USE_BITSO_FEES_BOOL" \
        '{name:$name, type:"limit_profit", book:$book, parameters:{reference:$ref, entry_offset:$eo, entry_offset_bps:$eobps, min_profit:$mp, min_profit_bps:$mpbps, fee:$fee, fee_bps:$fbps, use_bitso_fees:$usebf, buy_liquidity:$buyl, sell_liquidity:$selll, exit_price_reference:$xref, position_size:$ps, min_signal_interval:$msi, pending_buy_timeout_seconds:$pbto, pending_cancel_max_retries:$pcmr, max_position_hold_seconds:$mhold, stop_loss_quote:$slq, max_daily_loss_quote:$mdlq, daily_loss_reset_hour_utc:$dlrh, trailing_stop_quote:$tsq, trailing_stop_activation_quote:$tsaq, max_pending_orders:$mpo, sizing_mode:$smode, target_risk_quote:$trq, atr_multiplier:$atrm, atr_period:$atrp, dry_run:$dryrun}}'
      ;;
    momentum)
      jq -nc \
        --arg name "$sname" \
        --arg book "$BOOK" \
        --argjson rsip "$RSI_PERIOD" \
        --argjson ob "$OVERBOUGHT_LEVEL" \
        --argjson os "$OVERSOLD_LEVEL" \
        --argjson emap "$EMA_PERIOD" \
        --argjson msi "$MOMENTUM_MIN_SIGNAL_INTERVAL" \
        --argjson mc "$MOMENTUM_MIN_CONFIDENCE" \
        --argjson ps "$POSITION_SIZE" \
        --argjson mhold "$MOMENTUM_MAX_POSITION_HOLD_SEC" \
        --argjson slq "$MOMENTUM_STOP_LOSS_QUOTE" \
        --argjson mdlq "$MOMENTUM_MAX_DAILY_LOSS_QUOTE" \
        --argjson dlrh "$MOMENTUM_DAILY_LOSS_RESET_HOUR_UTC" \
        --argjson dryrun "$DRY_RUN_BOOL" \
        '{name:$name, type:"momentum", book:$book, parameters:{rsi_period:$rsip, overbought_level:$ob, oversold_level:$os, ema_period:$emap, min_signal_interval:$msi, min_confidence:$mc, position_size:$ps, max_position_hold_seconds:$mhold, stop_loss_quote:$slq, max_daily_loss_quote:$mdlq, daily_loss_reset_hour_utc:$dlrh, dry_run:$dryrun}}'
      ;;
    mean_reversion|*)
      jq -nc \
        --arg name "$sname" \
        --arg book "$BOOK" \
        --argjson lp "$LOOKBACK_PERIOD" \
        --argjson et "$ENTRY_THRESHOLD" \
        --argjson xt "$EXIT_THRESHOLD" \
        --argjson ps "$POSITION_SIZE" \
        '{name:$name, type:"mean_reversion", book:$book, parameters:{lookback_period:$lp, entry_threshold:$et, exit_threshold:$xt, position_size:$ps}}'
      ;;
  esac
}

# create_strategy <type> <name> — POSTs create; returns 0 on success.
create_strategy() {
  local stype="$1"
  local sname="$2"
  info "Creating strategy: $sname (type=$stype, book=$BOOK)"
  local body
  body=$(build_create_body "$stype" "$sname")

  local code
  code=$(curl -sS -o /tmp/se-create.json -w "%{http_code}" --max-time 15 -X POST "$SE_URL/api/v1/strategies" \
    -H "Content-Type: application/json" -d "$body")
  if [[ "$code" != "200" && "$code" != "201" ]]; then
    err "Create strategy '$sname' failed HTTP $code — response:"
    cat /tmp/se-create.json 2>/dev/null || true
    return 1
  fi
  ok "Strategy '$sname' created (HTTP $code)"
  return 0
}

# start_strategy <name> — POSTs start; returns 0 on success.
start_strategy() {
  local sname="$1"
  local code
  code=$(curl -sS -o /tmp/se-start.json -w "%{http_code}" --max-time 15 -X POST "$SE_URL/api/v1/strategies/$sname/start")
  if [[ "$code" != "200" && "$code" != "201" ]]; then
    err "Start strategy '$sname' failed HTTP $code — response:"
    cat /tmp/se-start.json 2>/dev/null || true
    return 1
  fi
  ok "Strategy '$sname' start requested (HTTP $code)"
  return 0
}

# create_and_start <type> <name> — POSTs create + start; returns 0 on success.
create_and_start() {
  local stype="$1"
  local sname="$2"
  create_strategy "$stype" "$sname" || return 1
  if [[ "$ROUTER_MANAGED" == "true" ]]; then
    info "ROUTER_MANAGED=true: '$sname' registered but not started (strategy-router will start the active strategy)."
    curl -sS --max-time 10 "$SE_URL/api/v1/strategies/$sname" | jq '{name, type, book, running, parameters}' 2>/dev/null \
      || curl -sS "$SE_URL/api/v1/strategies/$sname"
    return 0
  fi
  start_strategy "$sname" || return 1
  info "Strategy detail ($sname):"
  curl -sS --max-time 10 "$SE_URL/api/v1/strategies/$sname" | jq '{name, type, book, running, parameters}' 2>/dev/null \
    || curl -sS "$SE_URL/api/v1/strategies/$sname"
  return 0
}

# Resolve which strategy types to register: STRATEGY_TYPES wins when set,
# otherwise fall back to single STRATEGY_TYPE (preserves prior behavior).
# ROUTER_MANAGED defaults to all router-eligible types when STRATEGY_TYPES is unset.
declare -a TYPES_TO_RUN=()
declare -a NAMES_TO_RUN=()
if [[ "$ROUTER_MANAGED" == "true" && -z "$STRATEGY_TYPES" ]]; then
  STRATEGY_TYPES="mean_reversion,limit_profit,momentum"
  info "ROUTER_MANAGED=true: registering $STRATEGY_TYPES (override with STRATEGY_TYPES=...)"
fi
if [[ -n "$STRATEGY_TYPES" ]]; then
  IFS=',' read -ra TYPES_TO_RUN <<< "$STRATEGY_TYPES"
  TS="$(date +%s)"
  for t in "${TYPES_TO_RUN[@]}"; do
    t="$(echo "$t" | xargs)"
    if [[ "$ROUTER_MANAGED" == "true" && "$ROUTER_CANONICAL_NAMES" == "true" ]]; then
      NAMES_TO_RUN+=("${t}_${BOOK}")
    else
      NAMES_TO_RUN+=("organic_${t}_${TS}")
    fi
  done
else
  TYPES_TO_RUN=("$STRATEGY_TYPE")
  NAMES_TO_RUN=("$STRATEGY_NAME")
fi

REGISTERED=()
for i in "${!TYPES_TO_RUN[@]}"; do
  stype="$(echo "${TYPES_TO_RUN[$i]}" | xargs)"
  sname="${NAMES_TO_RUN[$i]}"
  if create_and_start "$stype" "$sname"; then
    REGISTERED+=("$sname")
    CLEANUP_NAMES+=("$sname")
  fi
done

if [[ "${#REGISTERED[@]}" -eq 0 ]]; then
  err "No strategies were registered successfully."
  exit 1
fi

section "5. Done — organic loop is active if strategies show running"
if [[ "$ROUTER_MANAGED" == "true" ]]; then
  warn "ROUTER_MANAGED=true: strategies are registered but stopped — start strategy-router (or POST /api/v1/router/run) to pick the active strategy."
fi
if [[ "$DRY_RUN" == "true" ]]; then
  warn "DRY_RUN=true: signals will have metadata.dry_run=true — trading-engine should skip execution."
fi
if [[ "$CLEANUP_ON_EXIT" == "true" ]]; then
  info "CLEANUP_ON_EXIT=true: strategies will be deleted when this script exits (Ctrl+C or termination)."
fi
info "Do NOT call POST /api/v1/strategies/process unless you intentionally want a synthetic tick (E2E)."
echo ""
echo "Verify manually:"
echo "  kubectl -n $NAMESPACE logs -f deploy/strategy-executor   # watch for published signals / ProcessTick"
echo "  kubectl -n $NAMESPACE logs -f deploy/trading-engine     # consume trading.signals, validate, Bitso"
echo "  Grafana: strategy-executor + trading-engine dashboards"
echo ""
echo "Registered strategies:"
for sname in "${REGISTERED[@]}"; do
  echo "  - $sname"
done
echo "To stop port-forward when finished: kill $PF_PID"
ok "start-organic-trading.sh finished."

# Optional: after strategies are running, snapshot registry + indicators to S3 (paper-trading-reporter).
# Requires AWS_REGION, AWS credentials, and Go. Set EXPORT_PAPER_SNAPSHOT_TO_S3=1 to enable.
if [[ "${EXPORT_PAPER_SNAPSHOT_TO_S3:-}" == "1" ]]; then
  section "6. Optional: export paper snapshot to S3"
  if [[ -z "${AWS_REGION:-}" ]]; then
    warn "EXPORT_PAPER_SNAPSHOT_TO_S3=1 but AWS_REGION is unset; skipping snapshot export."
  elif ! command -v go &>/dev/null; then
    warn "go not found; skipping snapshot export."
  else
    REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
    PT_FLAGS=()
    if [[ "${PAPER_TRADING_ENSURE_BUCKET:-}" == "0" ]]; then
      PT_FLAGS+=( -ensure-bucket=false )
    fi
    # Optional: Bitso GET /fees for limit_profit_pnl_estimates in snapshot (same env as strategy-executor).
    [[ -n "${BITSO_API_BASE_URL:-}" ]] && PT_FLAGS+=( -bitso-api-base-url "$BITSO_API_BASE_URL" )
    [[ -n "${BITSO_API_KEY:-}" ]] && PT_FLAGS+=( -bitso-api-key "$BITSO_API_KEY" )
    [[ -n "${BITSO_API_SECRET:-}" ]] && PT_FLAGS+=( -bitso-api-secret "$BITSO_API_SECRET" )
    # Optional: static fee decimals (skip Bitso fetch when both set), e.g. stage snapshot without mounting secrets here.
    [[ -n "${PAPER_LP_ESTIMATE_BUY_FEE_DECIMAL:-}" ]] && PT_FLAGS+=( -lp-estimate-buy-fee-decimal "$PAPER_LP_ESTIMATE_BUY_FEE_DECIMAL" )
    [[ -n "${PAPER_LP_ESTIMATE_SELL_FEE_DECIMAL:-}" ]] && PT_FLAGS+=( -lp-estimate-sell-fee-decimal "$PAPER_LP_ESTIMATE_SELL_FEE_DECIMAL" )
    if (
      cd "$REPO_ROOT/services/paper-trading-reporter" &&
      go run ./cmd/paper-trading-reporter \
        -strategy-executor-url "$SE_URL" \
        -region "$AWS_REGION" \
        -environment "${PAPER_TRADING_ENV:-paper}" \
        ${PAPER_TRADING_S3_BUCKET:+-bucket "$PAPER_TRADING_S3_BUCKET"} \
        ${PAPER_TRADING_S3_PREFIX:+-s3-prefix "$PAPER_TRADING_S3_PREFIX"} \
        "${PT_FLAGS[@]}"
    ); then
      ok "Paper snapshot uploaded to S3."
    else
      warn "Paper snapshot export failed (non-fatal)."
    fi
  fi
fi
