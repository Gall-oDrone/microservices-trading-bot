#!/usr/bin/env bash
# Show the latest 5 or 10 trades from Redis (market-data cache and historical storage).
# Usage: ./scripts/show-latest-trades-redis.sh [5|10]
# Requires: redis-cli, jq (optional for pretty-print). Default Redis: localhost:6379.

set -e

N="${1:-5}"
REDIS_HOST="${REDIS_HOST:-localhost}"
REDIS_PORT="${REDIS_PORT:-6379}"
REDIS_CMD="${REDIS_CMD:-redis-cli}"
BOOK="${BOOK:-btc_mxn}"

echo "=== Latest $N trades from Redis (book=$BOOK) ==="
echo ""

# 1) Cache (DB 0): recent_trades:btc_mxn is a list of trade IDs (newest first)
echo "--- Cache (DB 0) - recent_trades:$BOOK ---"
IDS=$($REDIS_CMD -h "$REDIS_HOST" -p "$REDIS_PORT" LRANGE "recent_trades:$BOOK" 0 $((N-1)) 2>/dev/null || true)
if [ -z "$IDS" ] || [ "$IDS" = "(empty list or set)" ]; then
  echo "  (no recent trade IDs in cache)"
else
  count=0
  for id in $IDS; do
    [ -z "$id" ] && continue
    VAL=$($REDIS_CMD -h "$REDIS_HOST" -p "$REDIS_PORT" GET "trade:${BOOK}:${id}" 2>/dev/null || true)
    if [ -n "$VAL" ]; then
      count=$((count+1))
      echo "  Trade ID $id:"
      if command -v jq &>/dev/null; then
        echo "$VAL" | jq -c '.' 2>/dev/null || echo "    $VAL"
      else
        echo "    $VAL"
      fi
    fi
  done
  [ "$count" -eq 0 ] && echo "  (no trade payloads found for those IDs)"
fi

echo ""

# 2) Historical storage (DB 1): keys trade:book:timestamp:id
echo "--- Historical storage (DB 1) - trade:$BOOK:* ---"
KEYS=$($REDIS_CMD -h "$REDIS_HOST" -p "$REDIS_PORT" -n 1 KEYS "trade:${BOOK}:*" 2>/dev/null || true)
if [ -z "$KEYS" ] || [ "$KEYS" = "(empty array)" ]; then
  echo "  (no trade keys in historical storage)"
else
  # Get all keys, sort by timestamp (second field) descending, take first N
  SORTED=$(echo "$KEYS" | tr ' ' '\n' | sort -t: -k3 -nr 2>/dev/null | head -n "$N" || echo "$KEYS" | tr ' ' '\n' | head -n "$N")
  count=0
  for key in $SORTED; do
    [ -z "$key" ] && continue
    VAL=$($REDIS_CMD -h "$REDIS_HOST" -p "$REDIS_PORT" -n 1 GET "$key" 2>/dev/null || true)
    if [ -n "$VAL" ]; then
      count=$((count+1))
      echo "  Key $key:"
      if command -v jq &>/dev/null; then
        echo "$VAL" | jq -c '.' 2>/dev/null || echo "    $VAL"
      else
        echo "    $VAL"
      fi
    fi
  done
  [ "$count" -eq 0 ] && echo "  (could not read values)"
fi

echo ""
echo "Done."
