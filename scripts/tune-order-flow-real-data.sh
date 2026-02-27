#!/usr/bin/env bash
# Tune order_flow strategy using real data from Bitso WebSocket (via market-data).
#
# Prerequisites:
#   1. market-data service running with Bitso WebSocket (e.g. BITSO_WS_URL=wss://ws.stage.bitso.com)
#   2. Let market-data run for at least 2+ hours so Redis has trade history, or use a shorter
#      RECENT_WINDOW (see below) after some minutes of data collection.
#   3. Redis and backtesting service running.
#
# Usage:
#   RECENT=1 ./scripts/tune-order-flow-real-data.sh [BACKTEST_BASE_URL] [MARKET_DATA_URL]
#   RECENT_WINDOW_MINUTES=30 RECENT=1 ./scripts/tune-order-flow-real-data.sh
#
# RECENT=1 uses last 2 hours (or RECENT_WINDOW_MINUTES) for the backtest date range.
# Output: best order_flow parameters and summary metrics.

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BACKTEST_BASE_URL="${1:-http://localhost:8084}"
MARKET_DATA_URL="${2:-http://localhost:8083}"
API="${BACKTEST_BASE_URL}/api/v1"
OPT_API="${API}/optimizations"
RECENT="${RECENT:-0}"
RECENT_WINDOW_MINUTES="${RECENT_WINDOW_MINUTES:-120}"   # 2 hours default when RECENT=1

if [ "$RECENT" != "1" ]; then
  echo "Warning: RECENT=1 is required to use real data (Bitso WebSocket)."
  echo "  Using fixed date range 2024-06-01 to 2024-06-02 (may be synthetic if no data)."
  START_DATE="2024-06-01T00:00:00Z"
  END_DATE="2024-06-02T23:59:59Z"
else
  END_DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date -u +%Y-%m-%dT%H:%M:%S 2>/dev/null)
  if [ -n "${RECENT_WINDOW_MINUTES}" ] && [ "${RECENT_WINDOW_MINUTES}" -gt 0 ]; then
    START_DATE=$(date -u -d "${RECENT_WINDOW_MINUTES} minutes ago" +%Y-%m-%dT%H:%M:%SZ 2>/dev/null) || true
    [ -z "$START_DATE" ] && START_DATE=$(date -u -v-${RECENT_WINDOW_MINUTES}M +%Y-%m-%dT%H:%M:%SZ 2>/dev/null) || true
  fi
  [ -z "$START_DATE" ] && START_DATE=$(date -u -d '2 hours ago' +%Y-%m-%dT%H:%M:%SZ 2>/dev/null) || true
  [ -z "$START_DATE" ] && START_DATE=$(date -u -v-2H +%Y-%m-%dT%H:%M:%SZ 2>/dev/null) || true
  if [ -z "$START_DATE" ] || [ -z "$END_DATE" ]; then
    START_DATE="2024-06-01T00:00:00Z"
    END_DATE="2024-06-02T23:59:59Z"
    echo "Warning: Could not compute RECENT date range; using fixed range."
  fi
  echo "Using real-data window: $START_DATE -> $END_DATE"
fi

# Optional: verify market-data has trades in range
if command -v curl &>/dev/null; then
  TRADES_RESP=$(curl -s -G "${MARKET_DATA_URL}/api/v1/trades" --data-urlencode "book=btc_mxn" --data-urlencode "from=${START_DATE}" --data-urlencode "to=${END_DATE}" --data-urlencode "limit=5" 2>/dev/null) || true
  if echo "$TRADES_RESP" | grep -q '"data"'; then
    COUNT=$(echo "$TRADES_RESP" | jq -r '.data | length' 2>/dev/null) || COUNT=0
    echo "Market-data trades in range (sample): $COUNT"
    if [ -n "$COUNT" ] && [ "$COUNT" -eq 0 ] && [ "$RECENT" = "1" ]; then
      echo "  Hint: Ensure market-data is running with BITSO_WS_URL=wss://ws.stage.bitso.com and has been running for at least the RECENT window."
    fi
  fi
fi

echo "Backtesting API: $BACKTEST_BASE_URL"
echo "Strategy: order_flow (parameter grid: buy_threshold, sell_threshold)"
echo ""

# Create optimization: order_flow with grid over buy_threshold and sell_threshold
# ParameterRange: min, max, step (floats)
BODY=$(jq -n \
  --arg name "order_flow tune real data" \
  --arg start_date "$START_DATE" \
  --arg end_date "$END_DATE" \
  --argjson initial_balance 100000 \
  --arg metric "total_return" \
  --argjson max_workers 4 \
  --argjson top_n 5 \
  --arg slippage_model "percentage" \
  --argjson slippage_value 0.001 \
  '{
    name: $name,
    start_date: $start_date,
    end_date: $end_date,
    book: "btc_mxn",
    initial_balance: $initial_balance,
    strategy: "order_flow",
    parameters: {
      buy_threshold:  { min: 0.15, max: 0.4, step: 0.05 },
      sell_threshold: { min: -0.4, max: -0.15, step: 0.05 }
    },
    metric: $metric,
    max_workers: $max_workers,
    top_n: $top_n,
    slippage_model: $slippage_model,
    slippage_value: $slippage_value
  }' 2>/dev/null)

if [ -z "$BODY" ]; then
  # Fallback without jq: minimal JSON
  BODY="{
    \"name\": \"order_flow tune real data\",
    \"start_date\": \"$START_DATE\",
    \"end_date\": \"$END_DATE\",
    \"book\": \"btc_mxn\",
    \"initial_balance\": 100000,
    \"strategy\": \"order_flow\",
    \"parameters\": {
      \"buy_threshold\": { \"min\": 0.15, \"max\": 0.4, \"step\": 0.05 },
      \"sell_threshold\": { \"min\": -0.4, \"max\": -0.15, \"step\": 0.05 }
    },
    \"metric\": \"total_return\",
    \"max_workers\": 4,
    \"top_n\": 5,
    \"slippage_model\": \"percentage\",
    \"slippage_value\": 0.001
  }"
fi

RESP=$(curl -s -w "\n%{http_code}" -X POST "$OPT_API" \
  -H "Content-Type: application/json" \
  -d "$BODY" 2>&1) || true
HTTP=$(echo "$RESP" | tail -n1)
RESP_BODY=$(echo "$RESP" | sed '$d')

if [ "$HTTP" != "201" ] && [ "$HTTP" != "200" ]; then
  echo "Failed to create optimization (HTTP $HTTP)"
  echo "$RESP_BODY"
  exit 1
fi

OPT_ID=$(echo "$RESP_BODY" | jq -r '.data.id // .id // empty' 2>/dev/null) || OPT_ID=$(echo "$RESP_BODY" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
if [ -z "$OPT_ID" ]; then
  echo "Could not parse optimization ID from response:"
  echo "$RESP_BODY"
  exit 1
fi

echo "Created optimization: $OPT_ID"
echo "Polling for completion (max 600s)..."

ELAPSED=0
MAX_WAIT=600
POLL=10
while [ "$ELAPSED" -lt "$MAX_WAIT" ]; do
  STATUS_RESP=$(curl -s "${API}/optimizations/${OPT_ID}" 2>/dev/null) || true
  STATUS=$(echo "$STATUS_RESP" | jq -r '.data.status // .status // "unknown"' 2>/dev/null)
  PROGRESS=$(echo "$STATUS_RESP" | jq -r '.data.progress // .progress // 0' 2>/dev/null)
  echo "  status=$STATUS progress=$PROGRESS (${ELAPSED}s)"
  if [ "$STATUS" = "completed" ]; then
    break
  fi
  if [ "$STATUS" = "failed" ]; then
    echo "Optimization failed."
    echo "$STATUS_RESP" | jq '.' 2>/dev/null || echo "$STATUS_RESP"
    exit 1
  fi
  sleep "$POLL"
  ELAPSED=$((ELAPSED + POLL))
done

if [ "$STATUS" != "completed" ]; then
  echo "Timeout waiting for optimization to complete."
  exit 1
fi

echo ""
echo "--- Best result (order_flow tuned on real data) ---"
BEST=$(curl -s "${API}/optimizations/${OPT_ID}/best" 2>/dev/null) || true
if echo "$BEST" | grep -q '"parameters"'; then
  echo "$BEST" | jq '.'
  PARAMS=$(echo "$BEST" | jq -r '.data.parameters // .parameters // empty' 2>/dev/null)
  SCORE=$(echo "$BEST" | jq -r '.data.score // .score // empty' 2>/dev/null)
  TOTAL_RETURN=$(echo "$BEST" | jq -r '.data.result.summary.total_return_percent // .result.summary.total_return_percent // empty' 2>/dev/null)
  TRADES=$(echo "$BEST" | jq -r '.data.result.summary.total_trades // .result.summary.total_trades // empty' 2>/dev/null)
  WIN_RATE=$(echo "$BEST" | jq -r '.data.result.summary.win_rate // .result.summary.win_rate // empty' 2>/dev/null)
  echo ""
  echo "Best parameters: $PARAMS"
  echo "Score (total_return): $SCORE | Total return %: $TOTAL_RETURN | Trades: $TRADES | Win rate: $WIN_RATE"
else
  echo "$BEST"
  echo ""
  echo "Full results: GET ${API}/optimizations/${OPT_ID}/results"
fi
