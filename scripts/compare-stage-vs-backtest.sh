#!/usr/bin/env bash
#
# Compare live Stage behavior to the backtest baseline for a strategy. This is
# the reconciliation step from the FINANCIAL guide: a strategy that looked good
# in backtest must behave consistently on Stage (after realized fees) before it
# earns live size.
#
# It compares the backtest result (sharpe/win-rate/profit-factor/pnl direction)
# to live Stage gauges scraped from strategy-executor /metrics.
#
# Usage:
#   ./scripts/compare-stage-vs-backtest.sh <backtest_id> [--book btc_mxn] [--strategy <name>]
#
# Environment:
#   BACKTESTING_URL        backtest API base URL (default: http://localhost:8084)
#   STRATEGY_EXECUTOR_URL  live metrics base URL (default: http://localhost:8084)
#
set -euo pipefail

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; BLUE='\033[0;34m'; NC='\033[0m'
info() { echo -e "${BLUE}[INFO]${NC} $1"; }
ok()   { echo -e "${GREEN}[ OK ]${NC} $1"; }
warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }

BACKTESTING_URL="${BACKTESTING_URL:-http://localhost:8084}"
STRATEGY_EXECUTOR_URL="${STRATEGY_EXECUTOR_URL:-http://localhost:8084}"

BACKTEST_ID=""
BOOK=""
STRATEGY_NAME=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --book) BOOK="$2"; shift 2 ;;
    --strategy) STRATEGY_NAME="$2"; shift 2 ;;
    -h|--help) sed -n '2,17p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) BACKTEST_ID="$1"; shift ;;
  esac
done
[[ -z "$BACKTEST_ID" ]] && { echo "backtest_id required"; exit 1; }
command -v jq >/dev/null 2>&1 || { echo "jq is required"; exit 1; }

RESULTS=$(curl -sS --max-time 30 "$BACKTESTING_URL/api/v1/backtests/$BACKTEST_ID/results")
if echo "$RESULTS" | jq -e '.error' &>/dev/null; then
  echo "Failed to fetch backtest results"; echo "$RESULTS" | jq .; exit 1
fi
BT_WIN=$(echo "$RESULTS" | jq -r '.win_rate // 0')
BT_PF=$(echo "$RESULTS" | jq -r '.profit_factor // 0')
BT_PNL=$(echo "$RESULTS" | jq -r '.total_pnl // 0')
BT_SHARPE=$(echo "$RESULTS" | jq -r '.sharpe_ratio // 0')
[[ -z "$BOOK" ]] && BOOK=$(echo "$RESULTS" | jq -r '.book // "btc_mxn"')

METRICS=$(curl -sS --max-time 15 "$STRATEGY_EXECUTOR_URL/metrics" 2>/dev/null || true)
grepf() { echo "$METRICS" | grep -E "$1" | { if [[ -n "$STRATEGY_NAME" ]]; then grep "strategy=\"$STRATEGY_NAME\""; else cat; fi; } | grep "book=\"$BOOK\"" || true; }

LIVE_WIN=$(echo "$METRICS" | grep -E '^strategy_executor_strategy_win_rate\{' | { [[ -n "$STRATEGY_NAME" ]] && grep "strategy=\"$STRATEGY_NAME\"" || cat; } | awk '{print $NF}' | head -1)
LIVE_PNL=$(grepf '_daily_realized_pnl_quote\{' | awk '{print $NF}' | head -1)

echo ""
info "Backtest $BACKTEST_ID vs Stage (book=$BOOK${STRATEGY_NAME:+, strategy=$STRATEGY_NAME})"
echo "----------------------------------------------------------------"
printf "  %-22s %-16s %-16s\n" "Metric" "Backtest" "Stage(live)"
printf "  %-22s %-16s %-16s\n" "win_rate(%)" "$BT_WIN" "${LIVE_WIN:-n/a}"
printf "  %-22s %-16s %-16s\n" "profit_factor" "$BT_PF" "n/a"
printf "  %-22s %-16s %-16s\n" "sharpe" "$BT_SHARPE" "n/a"
printf "  %-22s %-16s %-16s\n" "session_pnl(quote)" "$BT_PNL" "${LIVE_PNL:-n/a}"
echo "----------------------------------------------------------------"
echo ""

if [[ -z "${LIVE_PNL:-}" && -z "${LIVE_WIN:-}" ]]; then
  warn "No live Stage metrics found yet. Let the shadow run accumulate trades, then re-run."
  warn "Note: Stage win_rate is a fraction (0-1); backtest win_rate is a percentage."
  exit 0
fi

# Directional sanity: live session P&L should not contradict a positive backtest.
if [[ -n "${LIVE_PNL:-}" ]]; then
  if awk "BEGIN{exit !($BT_PNL > 0 && $LIVE_PNL < 0)}"; then
    warn "Backtest P&L positive but Stage session P&L negative — investigate fee drift / regime mix before promoting."
  else
    ok "Stage P&L direction is consistent with backtest baseline."
  fi
fi
echo ""
info "Fee drift during this Stage window:"
echo "$METRICS" | grep -E '^strategy_executor_fee_drift_ratio\{' | grep "book=\"$BOOK\"" | sed 's/^/  /' || echo "  (none reported)"
