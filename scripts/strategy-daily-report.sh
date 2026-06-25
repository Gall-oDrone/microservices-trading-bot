#!/usr/bin/env bash
#
# Daily shadow-trading observation report. Scrapes strategy-executor /metrics and
# summarizes, per strategy/book: signals, realized P&L, win rate, circuit-breaker
# state, and fee-honesty drift (realized vs assumed). Intended to be run daily
# during the Stage shadow window (Bucket C of RECOMMENDED-NEXT-STEPS).
#
# Usage:
#   ./scripts/strategy-daily-report.sh [--book btc_mxn] [--out report.txt]
#
# Environment:
#   STRATEGY_EXECUTOR_URL  base URL (default: http://localhost:8084)
#
set -euo pipefail

BLUE='\033[0;34m'; NC='\033[0m'
info() { echo -e "${BLUE}[INFO]${NC} $1"; }

STRATEGY_EXECUTOR_URL="${STRATEGY_EXECUTOR_URL:-http://localhost:8084}"
BOOK_FILTER=""
OUT=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --book) BOOK_FILTER="$2"; shift 2 ;;
    --out) OUT="$2"; shift 2 ;;
    -h|--help) sed -n '2,12p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "Unknown option: $1"; exit 1 ;;
  esac
done

METRICS=$(curl -sS --max-time 15 "$STRATEGY_EXECUTOR_URL/metrics" 2>/dev/null) || {
  echo "ERROR: could not scrape $STRATEGY_EXECUTOR_URL/metrics" >&2; exit 2; }

filter() {
  if [[ -n "$BOOK_FILTER" ]]; then grep "book=\"$BOOK_FILTER\""; else cat; fi
}

render() {
  echo "================================================================"
  echo "Strategy Daily Report — $(date -u +%Y-%m-%dT%H:%M:%SZ)"
  echo "Source: $STRATEGY_EXECUTOR_URL"
  [[ -n "$BOOK_FILTER" ]] && echo "Book filter: $BOOK_FILTER"
  echo "================================================================"

  echo ""
  echo "## Realized session P&L (quote)"
  echo "$METRICS" | grep -E '_daily_realized_pnl_quote\{' | filter | sed 's/^/  /' || echo "  (none)"

  echo ""
  echo "## Win rate (0-1)"
  echo "$METRICS" | grep -E '^strategy_executor_strategy_win_rate\{' | sed 's/^/  /' || echo "  (none)"

  echo ""
  echo "## Total P&L gauge"
  echo "$METRICS" | grep -E '^strategy_executor_strategy_pnl_total\{' | sed 's/^/  /' || echo "  (none)"

  echo ""
  echo "## Entry signals"
  echo "$METRICS" | grep -E '_entry_signals_total\{' | filter | sed 's/^/  /' || echo "  (none)"

  echo ""
  echo "## Exit signals (by reason)"
  echo "$METRICS" | grep -E '_exit_signals_total\{' | filter | sed 's/^/  /' || echo "  (none)"

  echo ""
  echo "## Circuit breakers (1 = tripped)"
  echo "$METRICS" | grep -E '_circuit_breaker_active\{' | filter | sed 's/^/  /' || echo "  (none)"

  echo ""
  echo "## Fee honesty — realized vs assumed (POINT-9)"
  echo "  Realized fee rate (last):"
  echo "$METRICS" | grep -E '^strategy_executor_realized_fee_rate\{' | filter | sed 's/^/    /' || echo "    (none)"
  echo "  Assumed fee rate:"
  echo "$METRICS" | grep -E '^strategy_executor_assumed_fee_rate\{' | filter | sed 's/^/    /' || echo "    (none)"
  echo "  Drift ratio (realized/assumed, 1.0 = match):"
  echo "$METRICS" | grep -E '^strategy_executor_fee_drift_ratio\{' | filter | sed 's/^/    /' || echo "    (none)"

  echo ""
  echo "## Indicator health"
  echo "$METRICS" | grep -E '^strategy_executor_indicators_healthy ' | sed 's/^/  /' || echo "  (none)"
  echo ""
  echo "Reminder: economic proof = positive NET P&L per regime AFTER realized fees."
}

if [[ -n "$OUT" ]]; then
  render > "$OUT"
  info "Report written to $OUT"
else
  render
fi
