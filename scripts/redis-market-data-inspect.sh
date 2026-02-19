#!/usr/bin/env bash
# Show latest Redis keys and sample values for market-data (Bitso WebSocket).
# Uses: REDIS_HOST (default localhost), REDIS_PORT (default 6379).
# If redis-cli is not installed, uses: docker exec REDIS_CONTAINER redis-cli (set REDIS_CONTAINER or auto-detect).
# Market-data uses: DB 0 = cache, DB 1 = historical storage.

set -e
REDIS_HOST="${REDIS_HOST:-localhost}"
REDIS_PORT="${REDIS_PORT:-6379}"
REDIS_CONTAINER="${REDIS_CONTAINER:-}"

if command -v redis-cli &>/dev/null; then
  REDIS_CMD="redis-cli -h ${REDIS_HOST} -p ${REDIS_PORT}"
else
  if [ -z "$REDIS_CONTAINER" ]; then
    REDIS_CONTAINER=$(docker ps --filter 'ancestor=redis:7-alpine' --format '{{.Names}}' 2>/dev/null | head -1)
    [ -z "$REDIS_CONTAINER" ] && REDIS_CONTAINER=$(docker ps --filter 'name=redis' --format '{{.Names}}' 2>/dev/null | head -1)
  fi
  if [ -n "$REDIS_CONTAINER" ]; then
    REDIS_CMD="docker exec $REDIS_CONTAINER redis-cli"
    echo "(using docker exec $REDIS_CONTAINER redis-cli)"
  else
    echo "Error: redis-cli not found and no Redis container (set REDIS_CONTAINER)" >&2
    exit 1
  fi
fi

echo "=== Redis market-data keys (Bitso WebSocket) ==="
echo "Host: $REDIS_HOST:$REDIS_PORT"
echo ""

echo "--- DB 0 (cache) - Key patterns ---"
echo "  trade:{book}:{tradeID}       - single trade (JSON)"
echo "  recent_trades:{book}         - list of recent trade IDs (newest first)"
echo "  orderbook:{book}             - order book snapshot"
echo "  ticker:{book}                - ticker snapshot"
echo "  trade_stats:{book}           - trade statistics"
echo ""

echo "--- DB 0: All keys ---"
KEYS0=$($REDIS_CMD -n 0 KEYS '*' 2>/dev/null) || true
if [ -z "$KEYS0" ]; then
  echo "(none or Redis unreachable - is Redis running at $REDIS_HOST:$REDIS_PORT?)"
else
  echo "$KEYS0"
fi
echo ""

echo "--- DB 0: Recent trade IDs for btc_mxn (first 15) ---"
$REDIS_CMD -n 0 LRANGE recent_trades:btc_mxn 0 14 2>/dev/null || echo "(Redis unreachable)"
echo ""

echo "--- DB 0: Sample trade (first ID from recent_trades) ---"
FIRST_ID=$($REDIS_CMD -n 0 LINDEX recent_trades:btc_mxn 0 2>/dev/null)
if [ -n "$FIRST_ID" ]; then
  $REDIS_CMD -n 0 GET "trade:btc_mxn:${FIRST_ID}" 2>/dev/null | head -c 500
  echo ""
else
  echo "(no recent_trades:btc_mxn or empty)"
fi
echo ""

echo "--- DB 1 (historical) - Key patterns ---"
echo "  trade:{book}:{unix_ts}:{tradeID}  - stored trade (JSON)"
echo "  time_index:trade:{book}:{unix_ts} - time index -> trade ID"
echo ""

echo "--- DB 1: All keys (up to 50) ---"
KEYS1=$($REDIS_CMD -n 1 KEYS 'trade:*' 2>/dev/null)
if [ -z "$KEYS1" ]; then
  echo "(none or Redis unreachable)"
else
  echo "$KEYS1" | head -50
fi
echo ""

echo "--- DB 1: Sample trade value (first trade key) ---"
SAMPLE_KEY=$($REDIS_CMD -n 1 KEYS 'trade:btc_mxn:*' 2>/dev/null | head -1)
if [ -n "$SAMPLE_KEY" ]; then
  $REDIS_CMD -n 1 GET "$SAMPLE_KEY" 2>/dev/null | head -c 600
  echo ""
else
  echo "(no trade keys in DB 1)"
fi
echo ""
echo "Done."
