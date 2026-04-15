#!/usr/bin/env bash
#
# Validate limit_profit strategy backtest results before stage promotion.
# This script checks if backtest metrics pass the defined gates.
#
# Usage:
#   ./scripts/validate-limit-profit-backtest.sh <backtest_id>
#   ./scripts/validate-limit-profit-backtest.sh <backtest_id> --min-sharpe 1.0 --max-dd 10
#
# Environment variables:
#   BACKTESTING_URL - URL of backtesting service (default: http://localhost:8084)
#   MIN_SHARPE - Minimum Sharpe ratio (default: 1.0)
#   MAX_DRAWDOWN_PCT - Maximum drawdown percentage (default: 10)
#   MIN_WIN_RATE - Minimum win rate percentage (default: 40)
#   MIN_PROFIT_FACTOR - Minimum profit factor (default: 1.1)
#   MIN_TRADES - Minimum number of trades (default: 50)
#
set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

info() { echo -e "${BLUE}[INFO]${NC} $1"; }
ok() { echo -e "${GREEN}[PASS]${NC} $1"; }
warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }
fail() { echo -e "${RED}[FAIL]${NC} $1"; }

usage() {
  cat <<EOF
Usage: $0 <backtest_id> [options]

Options:
  --min-sharpe <value>      Minimum Sharpe ratio (default: ${MIN_SHARPE:-1.0})
  --max-dd <value>          Maximum drawdown percentage (default: ${MAX_DRAWDOWN_PCT:-10})
  --min-win-rate <value>    Minimum win rate percentage (default: ${MIN_WIN_RATE:-40})
  --min-profit-factor <value> Minimum profit factor (default: ${MIN_PROFIT_FACTOR:-1.1})
  --min-trades <value>      Minimum number of trades (default: ${MIN_TRADES:-50})
  --help                    Show this help

Environment variables:
  BACKTESTING_URL           URL of backtesting service
EOF
  exit 1
}

# Default gate values
BACKTESTING_URL="${BACKTESTING_URL:-http://localhost:8084}"
MIN_SHARPE="${MIN_SHARPE:-1.0}"
MAX_DRAWDOWN_PCT="${MAX_DRAWDOWN_PCT:-10}"
MIN_WIN_RATE="${MIN_WIN_RATE:-40}"
MIN_PROFIT_FACTOR="${MIN_PROFIT_FACTOR:-1.1}"
MIN_TRADES="${MIN_TRADES:-50}"

# Parse arguments
BACKTEST_ID=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --min-sharpe)
      MIN_SHARPE="$2"
      shift 2
      ;;
    --max-dd)
      MAX_DRAWDOWN_PCT="$2"
      shift 2
      ;;
    --min-win-rate)
      MIN_WIN_RATE="$2"
      shift 2
      ;;
    --min-profit-factor)
      MIN_PROFIT_FACTOR="$2"
      shift 2
      ;;
    --min-trades)
      MIN_TRADES="$2"
      shift 2
      ;;
    --help|-h)
      usage
      ;;
    -*)
      echo "Unknown option: $1"
      usage
      ;;
    *)
      BACKTEST_ID="$1"
      shift
      ;;
  esac
done

if [[ -z "$BACKTEST_ID" ]]; then
  echo "Error: backtest_id is required"
  usage
fi

info "Fetching backtest results for ID: $BACKTEST_ID"
info "Backtesting URL: $BACKTESTING_URL"

# Fetch backtest results
RESULTS=$(curl -sS --max-time 30 "$BACKTESTING_URL/api/v1/backtests/$BACKTEST_ID/results" 2>/dev/null)
if [[ -z "$RESULTS" ]] || echo "$RESULTS" | jq -e '.error' &>/dev/null; then
  fail "Failed to fetch backtest results"
  echo "$RESULTS" | jq . 2>/dev/null || echo "$RESULTS"
  exit 1
fi

# Extract metrics
SHARPE=$(echo "$RESULTS" | jq -r '.sharpe_ratio // 0')
DRAWDOWN=$(echo "$RESULTS" | jq -r '.max_drawdown_pct // 0')
WIN_RATE=$(echo "$RESULTS" | jq -r '.win_rate // 0')
PROFIT_FACTOR=$(echo "$RESULTS" | jq -r '.profit_factor // 0')
TOTAL_TRADES=$(echo "$RESULTS" | jq -r '.total_trades // 0')
TOTAL_PNL=$(echo "$RESULTS" | jq -r '.total_pnl // 0')
STRATEGY=$(echo "$RESULTS" | jq -r '.strategy_name // "unknown"')

echo ""
info "========================================"
info "Backtest Results for: $STRATEGY"
info "========================================"
echo "  Sharpe Ratio:    $SHARPE (min: $MIN_SHARPE)"
echo "  Max Drawdown:    $DRAWDOWN% (max: $MAX_DRAWDOWN_PCT%)"
echo "  Win Rate:        $WIN_RATE% (min: $MIN_WIN_RATE%)"
echo "  Profit Factor:   $PROFIT_FACTOR (min: $MIN_PROFIT_FACTOR)"
echo "  Total Trades:    $TOTAL_TRADES (min: $MIN_TRADES)"
echo "  Total P&L:       $TOTAL_PNL"
echo ""

# Validate gates
FAILURES=0

# Gate 1: Sharpe Ratio
if (( $(echo "$SHARPE < $MIN_SHARPE" | bc -l) )); then
  fail "Gate 1: Sharpe ratio $SHARPE < minimum $MIN_SHARPE"
  FAILURES=$((FAILURES + 1))
else
  ok "Gate 1: Sharpe ratio $SHARPE >= $MIN_SHARPE"
fi

# Gate 2: Max Drawdown
if (( $(echo "$DRAWDOWN > $MAX_DRAWDOWN_PCT" | bc -l) )); then
  fail "Gate 2: Max drawdown $DRAWDOWN% > maximum $MAX_DRAWDOWN_PCT%"
  FAILURES=$((FAILURES + 1))
else
  ok "Gate 2: Max drawdown $DRAWDOWN% <= $MAX_DRAWDOWN_PCT%"
fi

# Gate 3: Win Rate
if (( $(echo "$WIN_RATE < $MIN_WIN_RATE" | bc -l) )); then
  fail "Gate 3: Win rate $WIN_RATE% < minimum $MIN_WIN_RATE%"
  FAILURES=$((FAILURES + 1))
else
  ok "Gate 3: Win rate $WIN_RATE% >= $MIN_WIN_RATE%"
fi

# Gate 4: Profit Factor
if (( $(echo "$PROFIT_FACTOR < $MIN_PROFIT_FACTOR" | bc -l) )); then
  fail "Gate 4: Profit factor $PROFIT_FACTOR < minimum $MIN_PROFIT_FACTOR"
  FAILURES=$((FAILURES + 1))
else
  ok "Gate 4: Profit factor $PROFIT_FACTOR >= $MIN_PROFIT_FACTOR"
fi

# Gate 5: Minimum Trades
if [[ "$TOTAL_TRADES" -lt "$MIN_TRADES" ]]; then
  fail "Gate 5: Total trades $TOTAL_TRADES < minimum $MIN_TRADES"
  FAILURES=$((FAILURES + 1))
else
  ok "Gate 5: Total trades $TOTAL_TRADES >= $MIN_TRADES"
fi

echo ""
info "========================================"

if [[ "$FAILURES" -eq 0 ]]; then
  ok "All gates passed! Strategy is approved for stage deployment."
  echo ""
  info "Next steps:"
  echo "  1. Deploy to stage: STRATEGY_TYPE=limit_profit ./scripts/start-organic-trading.sh"
  echo "  2. Monitor for 2-4 weeks"
  echo "  3. Compare stage results to backtest metrics"
  exit 0
else
  fail "$FAILURES gate(s) failed. Strategy not approved for stage deployment."
  echo ""
  info "Recommendations:"
  echo "  - Review strategy parameters"
  echo "  - Run on different date ranges for out-of-sample testing"
  echo "  - Check for overfitting"
  exit 1
fi
