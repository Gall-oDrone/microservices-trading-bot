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
RESP=$(curl -s -w "\n%{http_code}" -X POST "$API" \
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
  }' 2>&1) || true
CREATE_HTTP=$(echo "$RESP" | tail -n1)
RESP_BODY=$(echo "$RESP" | sed '$d')

if [ "$CREATE_HTTP" = "000" ] || [ -z "$CREATE_HTTP" ]; then
  echo "Failed to create backtest: cannot connect to $API (connection refused or unreachable)."
  echo "Ensure the backtesting service is running (e.g. docker compose up -d backtesting and dependencies)."
  exit 1
fi

if echo "$RESP_BODY" | grep -q '"success":true'; then
  ID=$(echo "$RESP_BODY" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
  if [ -z "$ID" ] && command -v jq &>/dev/null; then
    ID=$(echo "$RESP_BODY" | jq -r '.data.id // empty')
  fi
fi

if [ -z "$ID" ]; then
  echo "Failed to create backtest (HTTP $CREATE_HTTP). Response:"
  echo "$RESP_BODY"
  exit 1
fi

echo "Created backtest: $ID"
echo "Polling for completion (interval ${POLL_INTERVAL}s, max wait ${MAX_WAIT}s)..."

ELAPSED=0
while [ "$ELAPSED" -lt "$MAX_WAIT" ]; do
  # Capture both body and HTTP status code
  STATUS_RESP=$(curl -s -w "\n%{http_code}" "$API/$ID")
  HTTP_CODE=$(echo "$STATUS_RESP" | tail -n1)
  STATUS_BODY=$(echo "$STATUS_RESP" | sed '$d')
  if echo "$STATUS_BODY" | grep -q '"status":"completed"'; then
    echo "Backtest completed."
    break
  fi
  if echo "$STATUS_BODY" | grep -q '"status":"failed"'; then
    echo "Backtest failed."
    echo "$STATUS_BODY"
    exit 1
  fi
  STATUS="unknown"
  if command -v jq &>/dev/null; then
    STATUS=$(echo "$STATUS_BODY" | jq -r '.data.status // "unknown"')
    PROGRESS=$(echo "$STATUS_BODY" | jq -r '.data.progress // 0')
    echo "  status=$STATUS progress=$PROGRESS (${ELAPSED}s)"
    if [ "$STATUS" = "unknown" ]; then
      echo "  -> GET returned HTTP $HTTP_CODE. Response: $STATUS_BODY"
      echo "  -> If 404: backtest not found (e.g. service restarted or another instance; use a single backtesting instance or sticky sessions)."
    fi
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
