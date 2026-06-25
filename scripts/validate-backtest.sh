#!/usr/bin/env bash
#
# Validate a backtest result against the Gate 1 thresholds from
# docs/FINANCIAL-STRATEGY-IMPLEMENTATION-GUIDE.md (any strategy type).
#
# Usage:
#   ./scripts/validate-backtest.sh <backtest_id> [options]
#
# Options (defaults from Gate 1):
#   --min-sharpe <v>         (default: ${MIN_SHARPE:-1.0})
#   --max-dd <v>             (default: ${MAX_DRAWDOWN_PCT:-10})
#   --min-win-rate <v>       (default: ${MIN_WIN_RATE:-40})
#   --min-profit-factor <v>  (default: ${MIN_PROFIT_FACTOR:-1.1})
#   --min-trades <v>         (default: ${MIN_TRADES:-50})
#
# Environment:
#   BACKTESTING_URL  strategy-executor base URL (default: http://localhost:8084)
#
set -euo pipefail

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; BLUE='\033[0;34m'; NC='\033[0m'
info() { echo -e "${BLUE}[INFO]${NC} $1"; }
ok()   { echo -e "${GREEN}[PASS]${NC} $1"; }
fail() { echo -e "${RED}[FAIL]${NC} $1"; }

BACKTESTING_URL="${BACKTESTING_URL:-http://localhost:8084}"
MIN_SHARPE="${MIN_SHARPE:-1.0}"
MAX_DRAWDOWN_PCT="${MAX_DRAWDOWN_PCT:-10}"
MIN_WIN_RATE="${MIN_WIN_RATE:-40}"
MIN_PROFIT_FACTOR="${MIN_PROFIT_FACTOR:-1.1}"
MIN_TRADES="${MIN_TRADES:-50}"

usage() { sed -n '2,18p' "$0" | sed 's/^# \{0,1\}//'; exit 1; }

BACKTEST_ID=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --min-sharpe) MIN_SHARPE="$2"; shift 2 ;;
    --max-dd) MAX_DRAWDOWN_PCT="$2"; shift 2 ;;
    --min-win-rate) MIN_WIN_RATE="$2"; shift 2 ;;
    --min-profit-factor) MIN_PROFIT_FACTOR="$2"; shift 2 ;;
    --min-trades) MIN_TRADES="$2"; shift 2 ;;
    -h|--help) usage ;;
    -*) fail "Unknown option: $1"; usage ;;
    *) BACKTEST_ID="$1"; shift ;;
  esac
done

[[ -z "$BACKTEST_ID" ]] && { fail "backtest_id required"; usage; }
command -v jq >/dev/null 2>&1 || { fail "jq is required"; exit 1; }
command -v bc >/dev/null 2>&1 || { fail "bc is required"; exit 1; }

RESULTS=$(curl -sS --max-time 30 "$BACKTESTING_URL/api/v1/backtests/$BACKTEST_ID/results" 2>/dev/null)
if [[ -z "$RESULTS" ]] || echo "$RESULTS" | jq -e '.error' &>/dev/null; then
  fail "Failed to fetch results for $BACKTEST_ID"
  echo "$RESULTS" | jq . 2>/dev/null || echo "$RESULTS"
  exit 1
fi

SHARPE=$(echo "$RESULTS" | jq -r '.sharpe_ratio // 0')
DRAWDOWN=$(echo "$RESULTS" | jq -r '.max_drawdown_pct // 0')
WIN_RATE=$(echo "$RESULTS" | jq -r '.win_rate // 0')
PROFIT_FACTOR=$(echo "$RESULTS" | jq -r '.profit_factor // 0')
TOTAL_TRADES=$(echo "$RESULTS" | jq -r '.total_trades // 0')
TOTAL_PNL=$(echo "$RESULTS" | jq -r '.total_pnl // 0')
STRATEGY=$(echo "$RESULTS" | jq -r '.strategy_name // "unknown"')

echo ""
info "Backtest $BACKTEST_ID — $STRATEGY"
echo "  Sharpe Ratio:  $SHARPE (min $MIN_SHARPE)"
echo "  Max Drawdown:  $DRAWDOWN% (max $MAX_DRAWDOWN_PCT%)"
echo "  Win Rate:      $WIN_RATE% (min $MIN_WIN_RATE%)"
echo "  Profit Factor: $PROFIT_FACTOR (min $MIN_PROFIT_FACTOR)"
echo "  Total Trades:  $TOTAL_TRADES (min $MIN_TRADES)"
echo "  Total P&L:     $TOTAL_PNL"
echo ""

FAILURES=0
gate() { # name actual op threshold
  if (( $(echo "$2 $3 $4" | bc -l) )); then
    fail "$1 ($2 $3 $4)"; FAILURES=$((FAILURES+1))
  else
    ok "$1"
  fi
}
gate "Sharpe >= $MIN_SHARPE"        "$SHARPE"        "<" "$MIN_SHARPE"
gate "Drawdown <= $MAX_DRAWDOWN_PCT" "$DRAWDOWN"     ">" "$MAX_DRAWDOWN_PCT"
gate "Win rate >= $MIN_WIN_RATE"     "$WIN_RATE"     "<" "$MIN_WIN_RATE"
gate "Profit factor >= $MIN_PROFIT_FACTOR" "$PROFIT_FACTOR" "<" "$MIN_PROFIT_FACTOR"
if [[ "$TOTAL_TRADES" -lt "$MIN_TRADES" ]]; then
  fail "Trades >= $MIN_TRADES ($TOTAL_TRADES)"; FAILURES=$((FAILURES+1))
else
  ok "Trades >= $MIN_TRADES"
fi

echo ""
if [[ "$FAILURES" -eq 0 ]]; then
  ok "All Gate 1 checks passed."
  exit 0
else
  fail "$FAILURES gate(s) failed — not approved for stage."
  exit 1
fi
