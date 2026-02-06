#!/usr/bin/env bash
# Run one backtest via the backtesting service API: create, poll until done, fetch report.
# Usage: ./scripts/run-one-backtest.sh [BASE_URL]
# Example: ./scripts/run-one-backtest.sh http://localhost:8084
# Requires: curl, jq (optional, for parsing JSON)

set -e

BASE_URL="${1:-http://localhost:8084}"
API="${BASE_URL}/api/v1/backtests"
POLL_INTERVAL="${POLL_INTERVAL:-5}"
MAX_WAIT="${MAX_WAIT:-600}"

echo "Using backtesting API at: $API"
echo ""

# Create backtest (minimal payload; adjust dates/data_source as needed)
RESP=$(curl -s -X POST "$API" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Phase 6 workflow test",
    "start_date": "2024-06-01T00:00:00Z",
    "end_date": "2024-06-02T23:59:59Z",
    "book": "btc_mxn",
    "initial_balance": 100000.0,
    "strategy": "basic",
    "slippage_model": "percentage",
    "slippage_value": 0.001,
    "commission_rate": 0.001,
    "data_source": "market-data",
    "data_granularity": "trades"
  }')

if echo "$RESP" | grep -q '"success":true'; then
  ID=$(echo "$RESP" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
  if [ -z "$ID" ] && command -v jq &>/dev/null; then
    ID=$(echo "$RESP" | jq -r '.data.id // empty')
  fi
fi

if [ -z "$ID" ]; then
  echo "Failed to create backtest. Response:"
  echo "$RESP"
  exit 1
fi

echo "Created backtest: $ID"
echo "Polling for completion (interval ${POLL_INTERVAL}s, max wait ${MAX_WAIT}s)..."

ELAPSED=0
while [ "$ELAPSED" -lt "$MAX_WAIT" ]; do
  STATUS_RESP=$(curl -s "$API/$ID")
  if echo "$STATUS_RESP" | grep -q '"status":"completed"'; then
    echo "Backtest completed."
    break
  fi
  if echo "$STATUS_RESP" | grep -q '"status":"failed"'; then
    echo "Backtest failed."
    echo "$STATUS_RESP"
    exit 1
  fi
  STATUS="unknown"
  if command -v jq &>/dev/null; then
    STATUS=$(echo "$STATUS_RESP" | jq -r '.data.status // "unknown"')
    PROGRESS=$(echo "$STATUS_RESP" | jq -r '.data.progress // 0')
    echo "  status=$STATUS progress=$PROGRESS (${ELAPSED}s)"
  else
    echo "  waiting... (${ELAPSED}s)"
  fi
  sleep "$POLL_INTERVAL"
  ELAPSED=$((ELAPSED + POLL_INTERVAL))
done

if [ "$ELAPSED" -ge "$MAX_WAIT" ]; then
  echo "Timeout waiting for backtest to complete."
  exit 1
fi

echo ""
echo "--- Report (text) ---"
curl -s "$API/$ID/report?format=text"
echo ""
echo "--- Done. Full results: GET $API/$ID/results ---"
