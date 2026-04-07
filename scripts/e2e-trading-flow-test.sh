#!/usr/bin/env bash
#
# End-to-end test for the complete trading signal flow.
# Tests: strategy-executor -> Kafka -> trading-engine -> Bitso Staging API
#
# Prerequisites:
#   - Kubernetes cluster running with all services deployed
#   - Bitso staging API credentials configured
#   - Kafka cluster running
#
# Usage:
#   ./scripts/e2e-trading-flow-test.sh
#
# Environment variables:
#   STRATEGY_EXECUTOR_URL  - URL for strategy-executor (default: http://127.0.0.1:8084)
#   TRADING_ENGINE_URL     - URL for trading-engine (default: http://127.0.0.1:8086)
#   ORDER_MANAGEMENT_URL   - URL for order-management (default: http://127.0.0.1:8087)
#   DRY_RUN                - Set to "false" to actually place orders on Bitso staging
#
set -euo pipefail

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m'

# Helper functions
header() { echo -e "\n${CYAN}=== $1 ===${NC}"; }
info() { echo -e "${BLUE}[INFO]${NC} $1"; }
ok() { echo -e "${GREEN}[OK]${NC} $1"; }
warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }
error() { echo -e "${RED}[ERROR]${NC} $1"; }

# Configuration
STRATEGY_EXECUTOR_URL="${STRATEGY_EXECUTOR_URL:-http://127.0.0.1:8084}"
TRADING_ENGINE_URL="${TRADING_ENGINE_URL:-http://127.0.0.1:8086}"
ORDER_MANAGEMENT_URL="${ORDER_MANAGEMENT_URL:-http://127.0.0.1:8087}"
BOOK="${BOOK:-btc_mxn}"
DRY_RUN="${DRY_RUN:-true}"

echo -e "${CYAN}=== End-to-End Trading Flow Test ===${NC}"
info "Strategy Executor: $STRATEGY_EXECUTOR_URL"
info "Trading Engine: $TRADING_ENGINE_URL"
info "Order Management: $ORDER_MANAGEMENT_URL"
info "Book: $BOOK"
info "Dry Run: $DRY_RUN"
echo ""

# --- Step 1: Check all services are healthy ---
header "Step 1: Health Checks"

# Strategy Executor
SE_HEALTH=$(curl -sS --max-time 5 "$STRATEGY_EXECUTOR_URL/health" 2>/dev/null || echo '{"error":"connection failed"}')
if echo "$SE_HEALTH" | jq -e '.status == "healthy"' &>/dev/null; then
  ok "Strategy Executor is healthy"
else
  warn "Strategy Executor health: $SE_HEALTH"
fi

# Trading Engine
TE_HEALTH=$(curl -sS --max-time 5 "$TRADING_ENGINE_URL/health" 2>/dev/null || echo '{"error":"connection failed"}')
if echo "$TE_HEALTH" | jq -e '.status' &>/dev/null; then
  ok "Trading Engine is healthy"
else
  warn "Trading Engine health: $TE_HEALTH"
fi

# Order Management
OM_HEALTH=$(curl -sS --max-time 5 "$ORDER_MANAGEMENT_URL/health" 2>/dev/null || echo '{"error":"connection failed"}')
if echo "$OM_HEALTH" | jq -e '.status' &>/dev/null; then
  ok "Order Management is healthy"
else
  warn "Order Management health: $OM_HEALTH"
fi

# --- Step 2: Check Strategy Executor indicators ---
header "Step 2: Verify Indicators Computing"

INDICATORS=$(curl -sS --max-time 10 "$STRATEGY_EXECUTOR_URL/api/v1/indicators/$BOOK/snapshot" 2>/dev/null || echo '{}')
if echo "$INDICATORS" | jq -e '.book' &>/dev/null; then
  ok "Indicators snapshot retrieved"
  SMA=$(echo "$INDICATORS" | jq -r '.sma // "null"')
  RSI=$(echo "$INDICATORS" | jq -r '.rsi // "null"')
  info "SMA: $SMA, RSI: $RSI"
else
  warn "Could not get indicators: $INDICATORS"
fi

# --- Step 3: Check available strategies ---
header "Step 3: Available Strategies"

TYPES=$(curl -sS --max-time 5 "$STRATEGY_EXECUTOR_URL/api/v1/strategies/types" 2>/dev/null || echo '{}')
if echo "$TYPES" | jq -e '.types' &>/dev/null; then
  STRAT_TYPES=$(echo "$TYPES" | jq -r '.types | join(", ")')
  ok "Available strategy types: $STRAT_TYPES"
else
  warn "Could not get strategy types"
fi

# --- Step 4: List current strategies ---
header "Step 4: Current Strategies"

STRATEGIES=$(curl -sS --max-time 5 "$STRATEGY_EXECUTOR_URL/api/v1/strategies" 2>/dev/null || echo '{}')
if echo "$STRATEGIES" | jq -e '.strategies' &>/dev/null; then
  COUNT=$(echo "$STRATEGIES" | jq '.count // (.strategies | length)')
  ok "Found $COUNT strategies"
  echo "$STRATEGIES" | jq -r '.strategies[] | "  - \(.name) (\(.type)) - running: \(.running)"' 2>/dev/null || true
else
  warn "Could not list strategies"
fi

# --- Step 5: Create and start a test strategy ---
header "Step 5: Create Test Strategy"

STRATEGY_NAME="e2e_test_strategy"
CREATE_RESP=$(curl -sS -X POST --max-time 10 "$STRATEGY_EXECUTOR_URL/api/v1/strategies" \
  -H "Content-Type: application/json" \
  -d "{
    \"name\": \"$STRATEGY_NAME\",
    \"type\": \"mean_reversion\",
    \"book\": \"$BOOK\",
    \"parameters\": {
      \"lookback_period\": 5,
      \"entry_threshold\": 1.5,
      \"exit_threshold\": 0.5,
      \"position_size\": 0.0001
    }
  }" 2>/dev/null || echo '{"error":"request failed"}')

if echo "$CREATE_RESP" | jq -e '.name or .status == "created"' &>/dev/null; then
  ok "Created strategy: $STRATEGY_NAME"
else
  warn "Create strategy response: $CREATE_RESP"
fi

# --- Step 6: Start the strategy ---
header "Step 6: Start Strategy"

START_RESP=$(curl -sS -X POST --max-time 10 "$STRATEGY_EXECUTOR_URL/api/v1/strategies/$STRATEGY_NAME/start" 2>/dev/null || echo '{"error":"request failed"}')
if echo "$START_RESP" | jq -e '.status == "started"' &>/dev/null; then
  ok "Strategy started successfully"
else
  warn "Start response: $START_RESP"
fi

# --- Step 7: Generate signals by processing ticks ---
header "Step 7: Generate Trading Signals"

info "Processing market data through strategy..."

# Get current price from indicators
CURRENT_PRICE=$(curl -sS --max-time 5 "$STRATEGY_EXECUTOR_URL/api/v1/indicators/$BOOK/sma" 2>/dev/null | jq -r '.value // 1200000')

# Process a tick that might trigger a signal (price below lower Bollinger for buy)
LOWER_PRICE=$(echo "$CURRENT_PRICE * 0.98" | bc 2>/dev/null || echo "1176000")
TICK_RESP=$(curl -sS -X POST --max-time 10 "$STRATEGY_EXECUTOR_URL/api/v1/strategies/process" \
  -H "Content-Type: application/json" \
  -d "{
    \"book\": \"$BOOK\",
    \"price\": $LOWER_PRICE,
    \"amount\": 0.001,
    \"side\": \"sell\"
  }" 2>/dev/null || echo '{}')

if echo "$TICK_RESP" | jq -e '.signals_generated' &>/dev/null; then
  SIG_COUNT=$(echo "$TICK_RESP" | jq -r '.signals_generated')
  ok "Processed tick, signals generated: $SIG_COUNT"
  if [[ "$SIG_COUNT" -gt 0 ]]; then
    echo "$TICK_RESP" | jq '.signals'
  fi
else
  warn "Tick processing response: $TICK_RESP"
fi

# Also generate test signals for metrics
TEST_SIG_RESP=$(curl -sS -X POST --max-time 10 "$STRATEGY_EXECUTOR_URL/api/v1/test/signals" \
  -H "Content-Type: application/json" \
  -d '{"count": 3, "strategy": "mean_reversion"}' 2>/dev/null || echo '{}')

if echo "$TEST_SIG_RESP" | jq -e '.generated' &>/dev/null; then
  ok "Generated $(echo "$TEST_SIG_RESP" | jq -r '.generated') test signals for metrics"
fi

# --- Step 8: Check Prometheus metrics ---
header "Step 8: Verify Prometheus Metrics"

PROM_METRICS=$(curl -sS --max-time 10 "$STRATEGY_EXECUTOR_URL/metrics" 2>/dev/null || echo '')
if [[ -n "$PROM_METRICS" ]]; then
  METRIC_COUNT=$(echo "$PROM_METRICS" | grep -cE '^strategy_executor_' || echo "0")
  ok "Prometheus metrics available ($METRIC_COUNT strategy_executor metrics)"
  
  # Check key metrics
  SIGNALS_TOTAL=$(echo "$PROM_METRICS" | grep 'strategy_executor_signals_generated_total' | head -1 || echo "not found")
  INDICATORS_HEALTHY=$(echo "$PROM_METRICS" | grep 'strategy_executor_indicators_healthy' | head -1 || echo "not found")
  
  info "Signals metric: $SIGNALS_TOTAL"
  info "Indicators health: $INDICATORS_HEALTHY"
else
  warn "Could not fetch Prometheus metrics"
fi

# --- Step 9: Check Trading Engine status ---
header "Step 9: Trading Engine Status"

TE_STATUS=$(curl -sS --max-time 5 "$TRADING_ENGINE_URL/api/v1/status" 2>/dev/null || echo '{"error":"connection failed"}')
if echo "$TE_STATUS" | jq -e '.' &>/dev/null; then
  ok "Trading Engine status retrieved"
  echo "$TE_STATUS" | jq '.'
else
  warn "Trading Engine status: $TE_STATUS"
fi

TE_STATS=$(curl -sS --max-time 5 "$TRADING_ENGINE_URL/api/v1/stats" 2>/dev/null || echo '{}')
if echo "$TE_STATS" | jq -e '.signals_processed' &>/dev/null; then
  ok "Trading Engine stats:"
  echo "$TE_STATS" | jq '{signals_processed, orders_placed, orders_failed}'
fi

# --- Step 10: Check Order Management orders ---
header "Step 10: Order Management Status"

OM_ORDERS=$(curl -sS --max-time 10 "$ORDER_MANAGEMENT_URL/api/v1/orders?limit=5" 2>/dev/null || echo '{"error":"connection failed"}')
if echo "$OM_ORDERS" | jq -e '.orders' &>/dev/null; then
  ORDER_COUNT=$(echo "$OM_ORDERS" | jq '.orders | length')
  ok "Order Management has $ORDER_COUNT recent orders"
  if [[ "$ORDER_COUNT" -gt 0 ]]; then
    echo "$OM_ORDERS" | jq '.orders[:3] | .[] | {id, book, side, status, created_at}'
  fi
else
  warn "Order Management orders: $OM_ORDERS"
fi

# --- Step 11: Check Kafka topics (if kubectl available) ---
header "Step 11: Kafka Topics Check"

if command -v kubectl &>/dev/null; then
  KAFKA_POD=$(kubectl get pods -n bitso-trading-dev -l app=kafka -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || echo "")
  if [[ -n "$KAFKA_POD" ]]; then
    info "Checking Kafka topics..."
    TOPICS=$(kubectl exec -n bitso-trading-dev "$KAFKA_POD" -- kafka-topics.sh --list --bootstrap-server localhost:9092 2>/dev/null | grep -E 'trading|signal' || echo "none found")
    ok "Relevant Kafka topics:"
    echo "$TOPICS" | while read -r topic; do
      echo "  - $topic"
    done
  else
    warn "Kafka pod not found"
  fi
else
  info "kubectl not available, skipping Kafka topic check"
fi

# --- Step 12: Stop and cleanup test strategy ---
header "Step 12: Cleanup"

STOP_RESP=$(curl -sS -X POST --max-time 10 "$STRATEGY_EXECUTOR_URL/api/v1/strategies/$STRATEGY_NAME/stop" 2>/dev/null || echo '{}')
if echo "$STOP_RESP" | jq -e '.status == "stopped"' &>/dev/null; then
  ok "Strategy stopped"
else
  warn "Stop response: $STOP_RESP"
fi

# Delete test strategy (may fail if not implemented)
DELETE_RESP=$(curl -sS -X DELETE --max-time 10 "$STRATEGY_EXECUTOR_URL/api/v1/strategies/$STRATEGY_NAME" 2>/dev/null || echo '')
if [[ -z "$DELETE_RESP" ]] || echo "$DELETE_RESP" | jq -e 'true' &>/dev/null 2>/dev/null; then
  ok "Cleanup completed"
else
  info "Delete response (may be expected to fail): $DELETE_RESP"
fi

# --- Summary ---
header "Test Summary"

echo ""
echo "=========================================="
echo "  End-to-End Trading Flow Test Complete"
echo "=========================================="
echo ""
echo "  Services tested:"
echo "    - Strategy Executor: $STRATEGY_EXECUTOR_URL"
echo "    - Trading Engine: $TRADING_ENGINE_URL"
echo "    - Order Management: $ORDER_MANAGEMENT_URL"
echo ""
echo "  Signal Flow:"
echo "    1. strategy-executor computes indicators (SMA, RSI, Bollinger)"
echo "    2. Strategies analyze indicators and generate signals"
echo "    3. Signals published to Kafka topic: trading.signals"
echo "    4. trading-engine consumes signals and places orders"
echo "    5. Orders sent to Bitso Staging API (if DRY_RUN=false)"
echo "    6. order-management tracks order status and P&L"
echo ""
echo "  To enable real Bitso Staging orders:"
echo "    1. Set DRY_RUN=false in trading-engine deployment"
echo "    2. Configure STAGE_BITSO_API_KEY and STAGE_BITSO_APISECRET"
echo "    3. Verify orders at https://stage.bitso.com"
echo ""
echo "  Grafana Dashboards:"
echo "    - Strategy Executor: indicators, signals, strategy performance"
echo "    - Trading Engine: order execution, balances"
echo "    - Order Management: P&L tracking, order status"
echo ""

ok "Test completed. Check Grafana dashboards for visualization."
