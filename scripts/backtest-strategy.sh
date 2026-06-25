#!/usr/bin/env bash
#
# Run a backtest for a strategy via the strategy-executor /api/v1/backtests API,
# poll until complete, and print the text report.
#
# Usage:
#   ./scripts/backtest-strategy.sh <strategy> <book> [options]
#
# Examples:
#   ./scripts/backtest-strategy.sh mean_reversion btc_mxn
#   ./scripts/backtest-strategy.sh momentum btc_mxn --limit 5000 --slippage-bps 10
#   ./scripts/backtest-strategy.sh mean_reversion btc_mxn \
#       --start 2026-06-01T00:00:00Z --end 2026-06-07T00:00:00Z
#
# Environment:
#   BACKTESTING_URL   strategy-executor base URL (default: http://localhost:8084)
#   INITIAL_BALANCE   starting quote balance (default: 100000)
#
# Supported strategy types: mean_reversion, momentum, limit_profit
#
set -euo pipefail

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; BLUE='\033[0;34m'; NC='\033[0m'
info() { echo -e "${BLUE}[INFO]${NC} $1"; }
ok()   { echo -e "${GREEN}[ OK ]${NC} $1"; }
warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }
fail() { echo -e "${RED}[FAIL]${NC} $1"; }

BACKTESTING_URL="${BACKTESTING_URL:-http://localhost:8084}"
INITIAL_BALANCE="${INITIAL_BALANCE:-100000}"

usage() {
  sed -n '2,20p' "$0" | sed 's/^# \{0,1\}//'
  exit 1
}

[[ $# -lt 2 ]] && usage
STRATEGY="$1"; BOOK="$2"; shift 2

LIMIT=2000
SLIPPAGE_BPS=0
COMMISSION_BPS=0
START_DATE=""
END_DATE=""
PARAMETERS="{}"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --limit) LIMIT="$2"; shift 2 ;;
    --slippage-bps) SLIPPAGE_BPS="$2"; shift 2 ;;
    --commission-bps) COMMISSION_BPS="$2"; shift 2 ;;
    --start) START_DATE="$2"; shift 2 ;;
    --end) END_DATE="$2"; shift 2 ;;
    --parameters) PARAMETERS="$2"; shift 2 ;;
    --initial-balance) INITIAL_BALANCE="$2"; shift 2 ;;
    -h|--help) usage ;;
    *) fail "Unknown option: $1"; usage ;;
  esac
done

command -v jq >/dev/null 2>&1 || { fail "jq is required"; exit 1; }

info "Backtest: $STRATEGY on $BOOK (limit=$LIMIT, url=$BACKTESTING_URL)"

BODY=$(jq -n \
  --arg book "$BOOK" \
  --arg strategy "$STRATEGY" \
  --arg start "$START_DATE" \
  --arg end "$END_DATE" \
  --argjson params "$PARAMETERS" \
  --argjson limit "$LIMIT" \
  --argjson initial "$INITIAL_BALANCE" \
  --argjson slippage "$SLIPPAGE_BPS" \
  --argjson commission "$COMMISSION_BPS" \
  '{book:$book, strategy:$strategy, parameters:$params, limit:$limit,
    initial_balance:$initial, slippage_bps:$slippage, commission_bps:$commission}
   + (if $start != "" then {start_date:$start} else {} end)
   + (if $end   != "" then {end_date:$end}   else {} end)')

RESP=$(curl -sS --max-time 150 -X POST "$BACKTESTING_URL/api/v1/backtests" \
  -H 'Content-Type: application/json' -d "$BODY")

ID=$(echo "$RESP" | jq -r '.id // empty')
if [[ -z "$ID" ]]; then
  fail "Failed to create backtest"
  echo "$RESP" | jq . 2>/dev/null || echo "$RESP"
  exit 1
fi
info "Backtest ID: $ID"

# Poll until complete (the API runs synchronously, so this is usually one pass).
for _ in $(seq 1 60); do
  STATUS=$(curl -sS --max-time 30 "$BACKTESTING_URL/api/v1/backtests/$ID" | jq -r '.status')
  case "$STATUS" in
    completed) ok "Backtest completed"; break ;;
    failed)
      fail "Backtest failed"
      curl -sS "$BACKTESTING_URL/api/v1/backtests/$ID" | jq -r '.error'
      exit 1 ;;
    *) info "Status: $STATUS"; sleep 3 ;;
  esac
done

echo ""
curl -sS "$BACKTESTING_URL/api/v1/backtests/$ID/report?format=text"
echo ""
info "Validate gates: ./scripts/validate-backtest.sh $ID"
info "Promote:        ./scripts/promote-strategy-to-stage.sh $ID"
echo "$ID"
