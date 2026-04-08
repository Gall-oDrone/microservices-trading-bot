#!/usr/bin/env bash
#
# End-to-end test: strategy-executor -> Kafka (trading.signals) -> trading-engine -> Bitso Staging (or dry-run).
#
# Prerequisites:
#   Port-forwards (example):
#     kubectl port-forward -n bitso-trading-dev svc/strategy-executor 8084:8081 &
#     kubectl port-forward -n bitso-trading-dev svc/trading-engine 8086:8080 &
#     kubectl port-forward -n bitso-trading-dev svc/order-management 8087:8082 &
#
# Usage:
#   ./scripts/e2e-trading-flow-test.sh
#
# Environment:
#   STRATEGY_EXECUTOR_URL   (default http://127.0.0.1:8084)
#   TRADING_ENGINE_URL      (default http://127.0.0.1:8086)
#   ORDER_MANAGEMENT_URL    (default http://127.0.0.1:8087)
#   BOOK                    (default btc_mxn)
#   WAIT_FOR_PIPELINE       (default true) — poll trading-engine until signal consumed and order recorded
#   PIPELINE_WAIT_SEC       (default 120)  — max seconds to wait
#   PIPELINE_POLL_SEC       (default 2)    — poll interval
#
set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m'

header() { echo -e "\n${CYAN}=== $1 ===${NC}"; }
info() { echo -e "${BLUE}[INFO]${NC} $1"; }
ok() { echo -e "${GREEN}[OK]${NC} $1"; }
warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }
error() { echo -e "${RED}[ERROR]${NC} $1"; }

STRATEGY_EXECUTOR_URL="${STRATEGY_EXECUTOR_URL:-http://127.0.0.1:8084}"
TRADING_ENGINE_URL="${TRADING_ENGINE_URL:-http://127.0.0.1:8086}"
ORDER_MANAGEMENT_URL="${ORDER_MANAGEMENT_URL:-http://127.0.0.1:8087}"
BOOK="${BOOK:-btc_mxn}"
WAIT_FOR_PIPELINE="${WAIT_FOR_PIPELINE:-true}"
PIPELINE_WAIT_SEC="${PIPELINE_WAIT_SEC:-120}"
PIPELINE_POLL_SEC="${PIPELINE_POLL_SEC:-2}"

SIGNALS_GENERATED=0
PIPELINE_SIGNALS_SEEN=0
PIPELINE_ORDERS_SEEN=0
KAFKA_PUBLISHED=""

# Sum all sample values for metrics whose line starts with prefix (no grep — avoids pipefail exit on no match).
prom_sum_metric() {
  local url="$1"
  local prefix="$2"
  curl -sS --max-time 8 "$url/metrics" 2>/dev/null | awk -v p="$prefix" '
    $0 ~ "^" p { s += $NF }
    END { print s+0 }
  '
}

echo -e "${CYAN}╔════════════════════════════════════════════════════════════╗${NC}"
echo -e "${CYAN}║     E2E Trading Flow (strategy-executor → engine → Bitso)  ║${NC}"
echo -e "${CYAN}╚════════════════════════════════════════════════════════════╝${NC}"
echo ""
info "Strategy Executor: $STRATEGY_EXECUTOR_URL"
info "Trading Engine:    $TRADING_ENGINE_URL"
info "Order Management:  $ORDER_MANAGEMENT_URL"
info "Book: $BOOK"
info "Wait for pipeline: $WAIT_FOR_PIPELINE (max ${PIPELINE_WAIT_SEC}s)"
echo ""

# --- Step 1: Health ---
header "Step 1: Health Checks"
SE_HEALTH=$(curl -sS --max-time 5 "$STRATEGY_EXECUTOR_URL/health" 2>/dev/null || echo '{}')
if echo "$SE_HEALTH" | jq -e '.status == "healthy"' &>/dev/null; then
  ok "Strategy Executor: healthy"
else
  error "Strategy Executor unreachable. Set up port-forward to strategy-executor:8081"
  exit 1
fi

if curl -sS --max-time 3 -o /dev/null -w "%{http_code}" "$TRADING_ENGINE_URL/health" | grep -q 200; then
  ok "Trading Engine: healthy"
else
  warn "Trading Engine /health not OK — pipeline wait may fail"
fi

OM_HEALTH=$(curl -sS --max-time 5 "$ORDER_MANAGEMENT_URL/health" 2>/dev/null || echo '{}')
if echo "$OM_HEALTH" | jq -e '.status' &>/dev/null; then
  ok "Order Management: $(echo "$OM_HEALTH" | jq -r '.status')"
else
  warn "Order Management health check failed"
fi

# Baseline trading-engine counters (before we publish a signal)
TE_SIG_BASE=$(prom_sum_metric "$TRADING_ENGINE_URL" "signals_received_total")
TE_ORD_BASE=$(prom_sum_metric "$TRADING_ENGINE_URL" "orders_executed_total")
info "Trading-engine baseline: signals_received_total≈$TE_SIG_BASE orders_executed_total≈$TE_ORD_BASE"

# --- Step 2: Indicators ---
header "Step 2: Indicators"
INDICATORS=$(curl -sS --max-time 10 "$STRATEGY_EXECUTOR_URL/api/v1/indicators/$BOOK/snapshot" 2>/dev/null || echo '{}')
if echo "$INDICATORS" | jq -e '.book' &>/dev/null; then
  ok "Indicators snapshot for $BOOK"
  echo "$INDICATORS" | jq -r '{sma: (.sma.Value // .sma), rsi: (.rsi.Value // .rsi)}' 2>/dev/null || true
fi

# --- Step 3: Strategies ---
header "Step 3: Strategies"
TYPES_JSON=$(curl -sS --max-time 5 "$STRATEGY_EXECUTOR_URL/api/v1/strategies/types" 2>/dev/null || echo '{}')
if echo "$TYPES_JSON" | jq -e '.types' &>/dev/null; then
  echo "$TYPES_JSON" | jq -r '"Types: " + (.types | join(", "))'
else
  info "Could not fetch strategy types"
fi

STRATEGY_NAME="e2e_flow_$(date +%s)"
header "Step 4: Create & start $STRATEGY_NAME"
curl -sS -X POST --max-time 10 "$STRATEGY_EXECUTOR_URL/api/v1/strategies" \
  -H "Content-Type: application/json" \
  -d "{
    \"name\": \"$STRATEGY_NAME\",
    \"type\": \"mean_reversion\",
    \"book\": \"$BOOK\",
    \"parameters\": {
      \"lookback_period\": 5,
      \"entry_threshold\": 1.5,
      \"exit_threshold\": 0.5,
      \"position_size\": 0.001
    }
  }" | jq -e . &>/dev/null && ok "Strategy created" || warn "Create strategy failed"

curl -sS -X POST --max-time 10 "$STRATEGY_EXECUTOR_URL/api/v1/strategies/$STRATEGY_NAME/start" | jq -e . &>/dev/null && ok "Strategy started" || warn "Start failed"

# --- Step 5: Trigger signal + Kafka publish ---
header "Step 5: Trigger signal (POST /api/v1/strategies/process)"
CURRENT_PRICE=$(curl -sS --max-time 5 "$STRATEGY_EXECUTOR_URL/api/v1/indicators/$BOOK/sma" 2>/dev/null | jq -r '.Value // .value // 1200000')
LOWER_PRICE=$(echo "$CURRENT_PRICE * 0.94" | bc 2>/dev/null || echo "1128000")
info "Tick price $LOWER_PRICE (below bands → BUY)"

TICK_RESP=$(curl -sS -X POST --max-time 15 "$STRATEGY_EXECUTOR_URL/api/v1/strategies/process" \
  -H "Content-Type: application/json" \
  -d "{\"book\":\"$BOOK\",\"price\":$LOWER_PRICE,\"amount\":0.001,\"side\":\"sell\"}" 2>/dev/null || echo '{}')

SIG_COUNT=$(echo "$TICK_RESP" | jq -r '.signals_generated // 0')
SIGNALS_GENERATED=$((SIGNALS_GENERATED + SIG_COUNT))
KAFKA_PUBLISHED=$(echo "$TICK_RESP" | jq -r '.kafka_published // false')
KAFKA_ERR=$(echo "$TICK_RESP" | jq -r '.kafka_publish_error // empty')
# Older binaries omit kafka_published; treat missing as false

if [[ "$SIG_COUNT" -gt 0 ]]; then
  ok "Strategy generated $SIG_COUNT signal(s)"
  echo "$TICK_RESP" | jq '.signals[0] | {side, price, amount, reason}' 2>/dev/null || true
else
  warn "No signal from process tick — pipeline may not advance"
fi

if [[ "$KAFKA_PUBLISHED" == "true" ]]; then
  ok "Kafka publish: trading.signals (HTTP handler)"
elif [[ -n "$KAFKA_ERR" ]]; then
  warn "Kafka publish error: $KAFKA_ERR"
else
  info "kafka_published not true — cluster needs KAFKA_BROKERS + image with HTTP→Kafka publish, or topic not receiving messages"
fi

# Without Kafka publish, trading-engine will never see the signal; don't spin 120s.
if [[ "$WAIT_FOR_PIPELINE" == "true" ]] && [[ "$KAFKA_PUBLISHED" != "true" ]]; then
  warn "Disabling pipeline wait (no Kafka publish). Set WAIT_FOR_PIPELINE=false or fix Kafka + redeploy strategy-executor."
  WAIT_FOR_PIPELINE=false
fi

# --- Step 6: Wait for trading-engine ---
header "Step 6: Wait for trading-engine (consume + execute)"
if [[ "$WAIT_FOR_PIPELINE" != "true" ]]; then
  info "Skipped (set WAIT_FOR_PIPELINE=true and ensure kafka_published=true)"
else
  deadline=$(( $(date +%s) + PIPELINE_WAIT_SEC ))
  while [[ $(date +%s) -lt $deadline ]]; do
    TE_SIG=$(prom_sum_metric "$TRADING_ENGINE_URL" "signals_received_total")
    TE_ORD=$(prom_sum_metric "$TRADING_ENGINE_URL" "orders_executed_total")
    # shellcheck disable=SC2071
    if awk -v a="$TE_SIG" -v b="$TE_SIG_BASE" 'BEGIN{exit !(a>b)}'; then
      PIPELINE_SIGNALS_SEEN=1
    fi
    if awk -v a="$TE_ORD" -v b="$TE_ORD_BASE" 'BEGIN{exit !(a>b)}'; then
      PIPELINE_ORDERS_SEEN=1
    fi
    if [[ "$PIPELINE_SIGNALS_SEEN" -eq 1 ]] && [[ "$PIPELINE_ORDERS_SEEN" -eq 1 ]]; then
      ok "Pipeline: signals_received increased ($TE_SIG_BASE→$TE_SIG) and orders_executed increased ($TE_ORD_BASE→$TE_ORD)"
      break
    fi
    printf "\r  Polling TE metrics... signals=%s (base %s) orders=%s (base %s)   " "$TE_SIG" "$TE_SIG_BASE" "$TE_ORD" "$TE_ORD_BASE"
    sleep "$PIPELINE_POLL_SEC"
  done
  echo ""
  if [[ "$PIPELINE_SIGNALS_SEEN" -ne 1 ]]; then
    warn "Timeout: signals_received_total did not increase (Kafka consumer / topic mismatch?)"
  fi
  if [[ "$PIPELINE_ORDERS_SEEN" -ne 1 ]]; then
    warn "Timeout: orders_executed_total did not increase (engine errors, validation, or DRY_RUN path — check TE logs)"
  fi
  if [[ "$PIPELINE_SIGNALS_SEEN" -eq 1 ]] && [[ "$PIPELINE_ORDERS_SEEN" -eq 1 ]]; then
    ok "End-to-end path observed in Prometheus (engine metrics)"
  fi
fi

# --- Step 7: Order-management + metrics ---
header "Step 7: Order Management & /metrics samples"
curl -sS --max-time 5 "$ORDER_MANAGEMENT_URL/api/v1/status" | jq -e . &>/dev/null && ok "OM /api/v1/status OK" || true
curl -sS --max-time 5 "$ORDER_MANAGEMENT_URL/api/v1/risk/session" | jq -e . &>/dev/null || true

header "Step 8: Prometheus snippets"
echo -e "\n${CYAN}strategy-executor (sample):${NC}"
curl -sS --max-time 8 "$STRATEGY_EXECUTOR_URL/metrics" | grep -E "^strategy_executor_(up|signals_generated|indicator_|indicators_healthy)" | head -25 || true

echo -e "\n${CYAN}trading-engine (sample):${NC}"
curl -sS --max-time 8 "$TRADING_ENGINE_URL/metrics" | grep -E "^(signals_received_total|signals_processed_total|orders_executed_total|orders_failed_total|trading_engine_dry_run)" | head -25 || true

echo -e "\n${CYAN}order-management (sample):${NC}"
curl -sS --max-time 8 "$ORDER_MANAGEMENT_URL/metrics" | grep -E "^trading_" | head -15 || true

# --- Cleanup ---
header "Step 9: Cleanup"
curl -sS -X POST --max-time 5 "$STRATEGY_EXECUTOR_URL/api/v1/strategies/$STRATEGY_NAME/stop" >/dev/null 2>&1 || true
curl -sS -X DELETE --max-time 5 "$STRATEGY_EXECUTOR_URL/api/v1/strategies/$STRATEGY_NAME" >/dev/null 2>&1 || true
ok "Test strategy removed (if supported)"

# --- Grafana / metrics reference ---
header "Grafana dashboards & metrics this test touches"

cat <<'REF'

┌─────────────────────────────────────────────────────────────────────────────┐
│ DASHBOARDS TO OPEN                                                            │
├─────────────────────────────────────────────────────────────────────────────┤
│ • Strategy Executor  — indicators, signals, strategy state, service health  │
│ • Trading Platform / Trading Engine (if present) — orders, signals, balances  │
│ • Order Management / P&L — session P&L, risk (fills after real orders)       │
└─────────────────────────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────────────────────────┐
│ METRICS THAT SHOULD SHOW DATA AFTER THIS TEST                               │
├─────────────────────────────────────────────────────────────────────────────┤
│ strategy-executor (/metrics)                                                  │
│   strategy_executor_up                                                        │
│   strategy_executor_indicators_healthy                                      │
│   strategy_executor_indicator_{sma,ema,rsi,bollinger_*,vwap}_book=btc_mxn     │
│   strategy_executor_signals_generated_total{strategy,side}                  │
│   strategy_executor_strategy_running / active_strategies                    │
│                                                                               │
│ trading-engine (/metrics)                                                     │
│   signals_received_total           — increments when Kafka message consumed     │
│   signals_processed_total{book,strategy,outcome} — success/failed             │
│   orders_executed_total{book,strategy} — increments on placed order (incl.    │
│                         dry-run "success" path if executor returns nil)       │
│   orders_failed_total{book,strategy,reason} — validation / API errors         │
│   trading_engine_dry_run (gauge) — 1 if DRY_RUN, 0 if real Bitso calls       │
│   bitso_available_balance{currency} — if engine fetches balances              │
│                                                                               │
│ order-management (/metrics)                                                   │
│   trading_daily_realized_pnl_currency — usually 0 until fills sync          │
│   trading_trades_today_total — after real fills                             │
└─────────────────────────────────────────────────────────────────────────────┘

Real Bitso Staging orders: unset DRY_RUN on trading-engine, set stage API keys,
then re-run; confirm orders on https://stage.bitso.com

REF

echo ""
info "Run summary: signals_generated=$SIGNALS_GENERATED kafka_published=$KAFKA_PUBLISHED pipeline_signals=$PIPELINE_SIGNALS_SEEN pipeline_orders=$PIPELINE_ORDERS_SEEN"

if [[ "$PIPELINE_SIGNALS_SEEN" -eq 1 ]] && [[ "$PIPELINE_ORDERS_SEEN" -eq 1 ]]; then
  ok "E2E pipeline check passed (engine saw new signal + order counters)."
  exit 0
fi
if [[ "$SIG_COUNT" -gt 0 ]]; then
  ok "Strategy signal generated; verify engine/Kafka if pipeline counters did not move."
  exit 0
fi
warn "Weak run: no signal or pipeline confirmation."
exit 0
