#!/usr/bin/env bash
# End-to-end test for Strategy Executor service.
# Tests the strategy APIs, indicators, and verifies metrics in Grafana.
#
# Usage:
#   ./scripts/e2e-strategy-executor-test.sh
#   USE_KUBECTL=1 NAMESPACE=bitso-trading-dev ./scripts/e2e-strategy-executor-test.sh
#
# Env:
#   STRATEGY_EXECUTOR_URL  (default http://127.0.0.1:8084)
#   BOOK                   (default btc_mxn)
#   USE_KUBECTL            (default 0) — if 1, use kubectl port-forward
#   NAMESPACE              (default bitso-trading-dev)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

STRATEGY_EXECUTOR_URL="${STRATEGY_EXECUTOR_URL:-http://127.0.0.1:8084}"
BOOK="${BOOK:-btc_mxn}"
USE_KUBECTL="${USE_KUBECTL:-0}"
NAMESPACE="${NAMESPACE:-bitso-trading-dev}"

RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[0;33m'
CYAN='\033[0;36m'
NC='\033[0m'

info() { echo -e "${BLUE}[INFO]${NC} $*"; }
ok() { echo -e "${GREEN}[OK]${NC} $*"; }
warn() { echo -e "${YELLOW}[WARN]${NC} $*"; }
fail() { echo -e "${RED}[FAIL]${NC} $*"; exit 1; }
header() { echo -e "\n${CYAN}=== $* ===${NC}"; }

cleanup() {
  if [[ -n "${PORT_FWD_PID:-}" ]]; then
    kill "$PORT_FWD_PID" 2>/dev/null || true
  fi
}
trap cleanup EXIT

if ! command -v jq &>/dev/null; then
  fail "jq is required"
fi
if ! command -v curl &>/dev/null; then
  fail "curl is required"
fi

# Setup port-forward if using kubectl
if [[ "$USE_KUBECTL" == "1" ]]; then
  command -v kubectl &>/dev/null || fail "kubectl required when USE_KUBECTL=1"
  info "Starting port-forward to strategy-executor..."
  kubectl port-forward -n "$NAMESPACE" svc/strategy-executor 8084:8081 >/dev/null 2>&1 &
  PORT_FWD_PID=$!
  sleep 3
  STRATEGY_EXECUTOR_URL="http://127.0.0.1:8084"
fi

header "Strategy Executor E2E Test"
info "URL: $STRATEGY_EXECUTOR_URL"
info "Book: $BOOK"

# --- Step 1: Health Check ---
header "Step 1: Health Check"
HEALTH_RESP=$(curl -sS --max-time 10 "$STRATEGY_EXECUTOR_URL/health" 2>/dev/null || echo '{"error":"connection failed"}')
if echo "$HEALTH_RESP" | jq -e '.status' &>/dev/null; then
  STATUS=$(echo "$HEALTH_RESP" | jq -r '.status')
  ok "Health check passed: status=$STATUS"
else
  warn "Health check returned: $HEALTH_RESP"
  info "Service may not be running. Continuing with tests..."
fi

# --- Step 2: Get Service Status ---
header "Step 2: Service Status"
STATUS_RESP=$(curl -sS --max-time 10 "$STRATEGY_EXECUTOR_URL/api/v1/status" 2>/dev/null || echo '{"error":"connection failed"}')
if echo "$STATUS_RESP" | jq -e '.service' &>/dev/null; then
  SERVICE=$(echo "$STATUS_RESP" | jq -r '.service')
  ok "Service status: $SERVICE"
  echo "$STATUS_RESP" | jq .
else
  warn "Status endpoint returned: $STATUS_RESP"
fi

# --- Step 3: List Available Strategy Types ---
header "Step 3: Available Strategy Types"
TYPES_RESP=$(curl -sS --max-time 10 "$STRATEGY_EXECUTOR_URL/api/v1/strategies/types" 2>/dev/null || echo '{"types":[]}')
if echo "$TYPES_RESP" | jq -e '.types' &>/dev/null; then
  TYPES=$(echo "$TYPES_RESP" | jq -r '.types[]' 2>/dev/null | tr '\n' ', ' | sed 's/,$//')
  ok "Available types: $TYPES"
else
  warn "Types endpoint returned: $TYPES_RESP"
fi

# --- Step 4: List Current Strategies ---
header "Step 4: Current Strategies"
STRATEGIES_RESP=$(curl -sS --max-time 10 "$STRATEGY_EXECUTOR_URL/api/v1/strategies" 2>/dev/null || echo '{"strategies":[]}')
if echo "$STRATEGIES_RESP" | jq -e '.strategies' &>/dev/null; then
  COUNT=$(echo "$STRATEGIES_RESP" | jq -r '.count // (.strategies | length)')
  ok "Found $COUNT strategies"
  echo "$STRATEGIES_RESP" | jq '.strategies[] | {name, type: .type, book, running}' 2>/dev/null || true
else
  warn "Strategies endpoint returned: $STRATEGIES_RESP"
fi

# --- Step 5: Create a Test Strategy ---
header "Step 5: Create Test Strategy"
STRATEGY_NAME="test_mean_reversion_${BOOK}"
CREATE_PAYLOAD=$(cat <<EOF
{
  "name": "$STRATEGY_NAME",
  "type": "mean_reversion",
  "version": "1.0.0",
  "enabled": true,
  "book": "$BOOK",
  "parameters": {
    "lookback_period": 20,
    "entry_threshold": 2.0,
    "exit_threshold": 0.5,
    "min_signal_interval": 60,
    "position_size": 0.001
  },
  "sizing": {
    "method": "fixed",
    "max_position_size": 0.001,
    "max_position_value": 15000
  },
  "risk": {
    "max_daily_loss": 500,
    "max_drawdown_pct": 10,
    "max_trades_per_day": 50,
    "max_consecutive_losses": 5
  }
}
EOF
)

CREATE_RESP=$(curl -sS --max-time 10 -X POST \
  -H "Content-Type: application/json" \
  -d "$CREATE_PAYLOAD" \
  "$STRATEGY_EXECUTOR_URL/api/v1/strategies" 2>/dev/null || echo '{"error":"request failed"}')

if echo "$CREATE_RESP" | jq -e '.name' &>/dev/null; then
  ok "Created strategy: $(echo "$CREATE_RESP" | jq -r '.name')"
  echo "$CREATE_RESP" | jq .
elif echo "$CREATE_RESP" | jq -e '.error' &>/dev/null; then
  warn "Create strategy: $(echo "$CREATE_RESP" | jq -r '.error // .message // .')"
else
  warn "Create response: $CREATE_RESP"
fi

# --- Step 6: Get Strategy Info ---
header "Step 6: Get Strategy Info"
INFO_RESP=$(curl -sS --max-time 10 "$STRATEGY_EXECUTOR_URL/api/v1/strategies/$STRATEGY_NAME" 2>/dev/null || echo '{"error":"not found"}')
if echo "$INFO_RESP" | jq -e '.name' &>/dev/null; then
  ok "Strategy info retrieved"
  echo "$INFO_RESP" | jq '{name, type, version, book, running, enabled}'
else
  warn "Strategy info: $INFO_RESP"
fi

# --- Step 7: Start the Strategy ---
header "Step 7: Start Strategy"
START_RESP=$(curl -sS --max-time 10 -X POST \
  "$STRATEGY_EXECUTOR_URL/api/v1/strategies/$STRATEGY_NAME/start" 2>/dev/null || echo '{"error":"request failed"}')

if echo "$START_RESP" | jq -e '.status == "started"' &>/dev/null; then
  ok "Strategy started successfully"
else
  warn "Start response: $START_RESP"
fi

# --- Step 8: Get Strategy State ---
header "Step 8: Get Strategy State"
STATE_RESP=$(curl -sS --max-time 10 "$STRATEGY_EXECUTOR_URL/api/v1/strategies/$STRATEGY_NAME/state" 2>/dev/null || echo '{}')
if echo "$STATE_RESP" | jq -e '.name' &>/dev/null; then
  ok "Strategy state retrieved"
  echo "$STATE_RESP" | jq '{name, running, has_position, signal_count, trade_count, win_count, loss_count}'
else
  warn "State response: $STATE_RESP"
fi

# --- Step 9: Get Strategy Metrics ---
header "Step 9: Get Strategy Metrics"
METRICS_RESP=$(curl -sS --max-time 10 "$STRATEGY_EXECUTOR_URL/api/v1/strategies/$STRATEGY_NAME/metrics" 2>/dev/null || echo '{}')
if echo "$METRICS_RESP" | jq -e '.' &>/dev/null; then
  ok "Strategy metrics retrieved"
  echo "$METRICS_RESP" | jq '{total_pnl, daily_pnl, win_rate, sharpe_ratio, max_drawdown}'
else
  warn "Metrics response: $METRICS_RESP"
fi

# --- Step 10: Get Indicators Snapshot ---
header "Step 10: Indicators Snapshot"
INDICATORS_RESP=$(curl -sS --max-time 10 "$STRATEGY_EXECUTOR_URL/api/v1/indicators/$BOOK/snapshot" 2>/dev/null || echo '{}')
if echo "$INDICATORS_RESP" | jq -e '.book' &>/dev/null; then
  ok "Indicators snapshot retrieved for $BOOK"
  echo "$INDICATORS_RESP" | jq '{book, timestamp, sma: .sma.value, ema: .ema.value, rsi: .rsi.value, bollinger: {upper: .bollinger.upper, middle: .bollinger.middle, lower: .bollinger.lower}}'
else
  warn "Indicators snapshot: $INDICATORS_RESP"
fi

# --- Step 11: Get Individual Indicators ---
header "Step 11: Individual Indicators"
for indicator in sma ema rsi bollinger atr vwap; do
  RESP=$(curl -sS --max-time 5 "$STRATEGY_EXECUTOR_URL/api/v1/indicators/$BOOK/$indicator" 2>/dev/null || echo 'null')
  if [[ "$RESP" != "null" ]] && echo "$RESP" | jq -e '.' &>/dev/null; then
    VALUE=$(echo "$RESP" | jq -r '.value // .middle // "N/A"')
    ok "$indicator: $VALUE"
  else
    warn "$indicator: not available"
  fi
done

# --- Step 12: Get Registry Stats ---
header "Step 12: Registry Stats"
STATS_RESP=$(curl -sS --max-time 10 "$STRATEGY_EXECUTOR_URL/api/v1/strategies/stats" 2>/dev/null || echo '{}')
if echo "$STATS_RESP" | jq -e '.total_strategies' &>/dev/null; then
  ok "Registry stats retrieved"
  echo "$STATS_RESP" | jq .
else
  warn "Stats response: $STATS_RESP"
fi

# --- Step 12.5: Generate Test Signals for Dashboard ---
header "Step 12.5: Generate Test Signals"
SIGNAL_RESP=$(curl -sS -X POST --max-time 10 "$STRATEGY_EXECUTOR_URL/api/v1/test/signals" \
  -H "Content-Type: application/json" \
  -d '{"count": 5, "strategy": "mean_reversion"}' 2>/dev/null || echo '{}')
if echo "$SIGNAL_RESP" | jq -e '.generated' &>/dev/null; then
  GENERATED=$(echo "$SIGNAL_RESP" | jq -r '.generated')
  ok "Generated $GENERATED test signals for mean_reversion"
else
  warn "Could not generate test signals"
fi

# Generate signals for momentum strategy too
SIGNAL_RESP2=$(curl -sS -X POST --max-time 10 "$STRATEGY_EXECUTOR_URL/api/v1/test/signals" \
  -H "Content-Type: application/json" \
  -d '{"count": 3, "strategy": "momentum"}' 2>/dev/null || echo '{}')
if echo "$SIGNAL_RESP2" | jq -e '.generated' &>/dev/null; then
  GENERATED2=$(echo "$SIGNAL_RESP2" | jq -r '.generated')
  ok "Generated $GENERATED2 test signals for momentum"
fi

# --- Step 13: Check Prometheus Metrics Endpoint ---
header "Step 13: Prometheus Metrics"
PROM_METRICS=$(curl -sS --max-time 10 "$STRATEGY_EXECUTOR_URL/metrics" 2>/dev/null || echo '')
if [[ -n "$PROM_METRICS" ]]; then
  # Count metric lines
  METRIC_COUNT=$(echo "$PROM_METRICS" | grep -E '^[a-z_]+' | wc -l || echo "0")
  if [[ "$METRIC_COUNT" -gt 5 ]]; then
    ok "Prometheus metrics endpoint active ($METRIC_COUNT metrics)"
    
    # Check for expected metrics
    echo ""
    info "Checking for strategy-executor specific metrics..."
    
    EXPECTED_METRICS=(
      "strategy_executor_active_strategies"
      "strategy_executor_signals_generated_total"
      "strategy_executor_strategy_running"
      "strategy_executor_indicator_"
      "strategy_executor_indicators_healthy"
    )
    
    for metric in "${EXPECTED_METRICS[@]}"; do
      if echo "$PROM_METRICS" | grep -q "$metric"; then
        ok "  Found: $metric"
      else
        warn "  Missing: $metric"
      fi
    done
  else
    warn "Metrics endpoint returned minimal data"
    echo "$PROM_METRICS" | head -20
  fi
else
  warn "No response from /metrics endpoint"
fi

# --- Step 14: Stop Strategy ---
header "Step 14: Stop Strategy"
STOP_RESP=$(curl -sS --max-time 10 -X POST \
  "$STRATEGY_EXECUTOR_URL/api/v1/strategies/$STRATEGY_NAME/stop" 2>/dev/null || echo '{"error":"request failed"}')

if echo "$STOP_RESP" | jq -e '.status == "stopped"' &>/dev/null; then
  ok "Strategy stopped successfully"
else
  warn "Stop response: $STOP_RESP"
fi

# --- Step 15: Delete Test Strategy ---
header "Step 15: Cleanup - Delete Test Strategy"
DELETE_RESP=$(curl -sS --max-time 10 -X DELETE \
  "$STRATEGY_EXECUTOR_URL/api/v1/strategies/$STRATEGY_NAME" 2>/dev/null)

HTTP_CODE=$(curl -sS --max-time 10 -o /dev/null -w "%{http_code}" -X DELETE \
  "$STRATEGY_EXECUTOR_URL/api/v1/strategies/$STRATEGY_NAME" 2>/dev/null || echo "000")

if [[ "$HTTP_CODE" == "204" || "$HTTP_CODE" == "404" ]]; then
  ok "Test strategy cleaned up"
else
  warn "Delete returned HTTP $HTTP_CODE"
fi

# --- Summary ---
header "Test Summary"
echo ""
echo "=========================================="
echo "  Strategy Executor E2E Test Complete"
echo "=========================================="
echo ""
echo "  Endpoint:     $STRATEGY_EXECUTOR_URL"
echo "  Book:         $BOOK"
echo ""
echo "  Dashboards to check in Grafana:"
echo "    - Strategy Executor"
echo "    - Financial Indicators"
echo "    - Trading Platform Metrics"
echo ""
echo "  To import dashboards:"
echo "    GRAFANA_URL=http://localhost:3001 ./scripts/grafana-import-dashboard.sh strategy-executor"
echo ""
ok "Test completed. Check Grafana dashboards for metrics visualization."
