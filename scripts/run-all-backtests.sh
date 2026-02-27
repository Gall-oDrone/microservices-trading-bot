#!/usr/bin/env bash
# Run backtests for all strategies and output a summary table (markdown) and CSV.
# Usage: ./scripts/run-all-backtests.sh [BASE_URL]
# Example: RECENT=1 ./scripts/run-all-backtests.sh http://localhost:8086
# Requires: curl, jq (optional but recommended for parsing)

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BASE_URL="${1:-http://localhost:8084}"
API="${BASE_URL}/api/v1/backtests"
STRATEGIES="basic trend arbitrage vwap_deviation bollinger order_flow rsi_momentum volatility_breakout"
OUTPUT_DIR="${OUTPUT_DIR:-$SCRIPT_DIR/../.backtest-results}"
RECENT="${RECENT:-0}"

mkdir -p "$OUTPUT_DIR"
TS=$(date -u +%Y%m%dT%H%M%SZ)
SUMMARY_MD="$OUTPUT_DIR/backtest-summary-${TS}.md"
SUMMARY_CSV="$OUTPUT_DIR/backtest-summary-${TS}.csv"

# Date range (RECENT=1 uses last 2 hours for real Bitso WebSocket data)
START_DATE="${START_DATE:-2024-06-01T00:00:00Z}"
END_DATE="${END_DATE:-2024-06-02T23:59:59Z}"
if [ "$RECENT" = "1" ]; then
  END_DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date -u +%Y-%m-%dT%H:%M:%S 2>/dev/null)
  START_DATE=$(date -u -d '2 hours ago' +%Y-%m-%dT%H:%M:%SZ 2>/dev/null) || true
  [ -z "$START_DATE" ] && START_DATE=$(date -u -v-2H +%Y-%m-%dT%H:%M:%SZ 2>/dev/null) || true
fi

echo "Using backtesting API at: $API"
echo "Date range: $START_DATE -> $END_DATE"
echo "RECENT=$RECENT"
echo ""

# CSV header
echo "strategy,status,total_trades,win_rate_pct,total_return_pct,net_pnl,max_drawdown_pct,backtest_id" > "$SUMMARY_CSV"

# Markdown header
{
  echo "# Backtest summary"
  echo ""
  echo "**Date:** $(date -u +%Y-%m-%d\ %H:%M:%S\ UTC)"
  echo "**Date range:** $START_DATE → $END_DATE"
  echo "**RECENT:** $RECENT"
  echo ""
  echo "| Strategy | Status | Trades | Win Rate | Total Return | Net P&L | Max DD% | ID |"
  echo "|----------|--------|--------|----------|--------------|---------|---------|-----|"
} > "$SUMMARY_MD"

for STRATEGY in $STRATEGIES; do
  echo "Running backtest: $STRATEGY ..."
  RESP=$(curl -s -w "\n%{http_code}" -X POST "$API" \
    -H "Content-Type: application/json" \
    -d "{
      \"name\": \"run-all ($STRATEGY)\",
      \"start_date\": \"$START_DATE\",
      \"end_date\": \"$END_DATE\",
      \"book\": \"btc_mxn\",
      \"initial_balance\": 100000.0,
      \"strategy\": \"$STRATEGY\",
      \"slippage_model\": \"percentage\",
      \"slippage_value\": 0.001,
      \"data_source\": \"market-data\",
      \"data_granularity\": \"trades\"
    }" 2>&1) || true

  CREATE_HTTP=$(echo "$RESP" | tail -n1)
  BODY=$(echo "$RESP" | sed '$d')
  ID=""
  if [ "$CREATE_HTTP" = "200" ] || [ "$CREATE_HTTP" = "201" ]; then
    ID=$(echo "$BODY" | jq -r '.data.id // empty' 2>/dev/null) || ID=$(echo "$BODY" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
  fi

  if [ -z "$ID" ]; then
    echo "  Failed to create backtest (HTTP $CREATE_HTTP)"
    echo "| $STRATEGY | FAILED (create) | - | - | - | - | - | - |" >> "$SUMMARY_MD"
    echo "$STRATEGY,failed_create,-,-,-,-,-," >> "$SUMMARY_CSV"
    continue
  fi

  # Poll until completed or failed
  ELAPSED=0
  MAX_WAIT="${MAX_WAIT:-600}"
  POLL_INTERVAL="${POLL_INTERVAL:-5}"
  STATUS="running"
  while [ "$ELAPSED" -lt "$MAX_WAIT" ]; do
    RESULT=$(curl -s "$API/$ID/results" 2>/dev/null) || true
    if echo "$RESULT" | grep -q '"status":"completed"'; then
      STATUS="completed"
      break
    fi
    if echo "$RESULT" | grep -q '"status":"failed"'; then
      STATUS="failed"
      ERR=$(echo "$RESULT" | jq -r '.data.error // "unknown"' 2>/dev/null) || ERR="unknown"
      break
    fi
    sleep "$POLL_INTERVAL"
    ELAPSED=$((ELAPSED + POLL_INTERVAL))
  done

  if [ "$STATUS" = "failed" ]; then
    echo "  Failed: ${ERR:-unknown}"
    echo "| $STRATEGY | FAILED | - | - | - | - | - | $ID |" >> "$SUMMARY_MD"
    echo "$STRATEGY,failed,-,-,-,-,-,$ID" >> "$SUMMARY_CSV"
    continue
  fi

  if [ "$STATUS" != "completed" ]; then
    echo "  Timeout"
    echo "| $STRATEGY | TIMEOUT | - | - | - | - | - | $ID |" >> "$SUMMARY_MD"
    echo "$STRATEGY,timeout,-,-,-,-,-,$ID" >> "$SUMMARY_CSV"
    continue
  fi

  # Parse from results JSON (summary: total_trades, win_rate 0-1, total_return_percent, net_profit_loss, max_drawdown_percent)
  TOTAL_TRADES=$(echo "$RESULT" | jq -r '.data.summary.total_trades // 0' 2>/dev/null) || TOTAL_TRADES="0"
  WIN_RATE_01=$(echo "$RESULT" | jq -r '.data.summary.win_rate // 0' 2>/dev/null) || WIN_RATE_01="0"
  TOTAL_RETURN=$(echo "$RESULT" | jq -r '.data.summary.total_return_percent // 0' 2>/dev/null) || TOTAL_RETURN="0"
  NET_PNL=$(echo "$RESULT" | jq -r '.data.summary.net_profit_loss // .data.summary.total_return // 0' 2>/dev/null) || NET_PNL="0"
  MAX_DD=$(echo "$RESULT" | jq -r '.data.summary.max_drawdown_percent // 0' 2>/dev/null) || MAX_DD="0"
  # win_rate in API is 0-1; display as percent
  WIN_RATE=$(echo "$WIN_RATE_01" | awk '{printf "%.1f", $1*100}')

  # Fallback: parse from text report if jq or structure differs
  if [ -z "$TOTAL_TRADES" ] || [ "$TOTAL_TRADES" = "null" ] || [ "$TOTAL_TRADES" = "" ]; then
    REPORT=$(curl -s "$API/$ID/report?format=text" 2>/dev/null) || true
    TOTAL_TRADES=$(echo "$REPORT" | grep -E "Total Trades:" | sed -n 's/.*Total Trades:[^0-9]*\([0-9]*\).*/\1/p' | tr -d ' ')
    WIN_RATE=$(echo "$REPORT" | grep -E "Win Rate:" | sed -n 's/.*Win Rate:[^0-9.-]*\([0-9.-]*\)%.*/\1/p' | tr -d ' ')
    TOTAL_RETURN=$(echo "$REPORT" | grep -E "Total Return:" | sed -n 's/.*(\([^)]*\)%).*/\1/p' | tr -d ' ')
    NET_PNL=$(echo "$REPORT" | grep -E "Net P&L:" | sed -n 's/.*Net P&L:[^0-9.-]*\([0-9.-]*\).*/\1/p' | tr -d ' ')
    MAX_DD=$(echo "$REPORT" | grep -E "Max Drawdown:" | sed -n 's/.*(\([0-9.]*\)%).*/\1/p' | tr -d ' ')
  fi

  TOTAL_TRADES="${TOTAL_TRADES:-0}"
  WIN_RATE="${WIN_RATE:-0}"
  TOTAL_RETURN="${TOTAL_RETURN:-0}"
  NET_PNL="${NET_PNL:-0}"
  MAX_DD="${MAX_DD:-0}"

  echo "  Trades=$TOTAL_TRADES WinRate=$WIN_RATE% Return=$TOTAL_RETURN%"
  echo "| $STRATEGY | completed | $TOTAL_TRADES | $WIN_RATE% | $TOTAL_RETURN% | $NET_PNL | $MAX_DD% | $ID |" >> "$SUMMARY_MD"
  echo "$STRATEGY,completed,$TOTAL_TRADES,$WIN_RATE,$TOTAL_RETURN,$NET_PNL,$MAX_DD,$ID" >> "$SUMMARY_CSV"
done

echo ""
echo "--- Summary (markdown) ---"
cat "$SUMMARY_MD"
echo ""
echo "--- Summary (CSV) ---"
cat "$SUMMARY_CSV"
echo ""
echo "Written: $SUMMARY_MD and $SUMMARY_CSV"
