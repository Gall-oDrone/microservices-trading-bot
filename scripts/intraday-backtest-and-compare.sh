#!/usr/bin/env bash
# Intraday Priority 2: Run one backtest then print comparison checklist for Grafana.
# Usage: ./scripts/intraday-backtest-and-compare.sh [BACKTEST_BASE_URL]
# Example: ./scripts/intraday-backtest-and-compare.sh http://localhost:8084
# Requires: run-one-backtest.sh in same directory; curl. Optional: jq.

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BACKTEST_URL="${1:-http://localhost:8084}"

echo "Intraday Priority 2: Backtest run + Grafana comparison"
echo "Backtesting service: $BACKTEST_URL"
echo ""

# Run the existing backtest script (capture report; don't exit on failure so we always print comparison)
REPORT=$("$SCRIPT_DIR/run-one-backtest.sh" "$BACKTEST_URL" 2>&1) || true
echo "$REPORT"
echo ""

# Always print comparison checklist
echo "--- Compare to Grafana (Trading Platform Metrics dashboard, Intraday / P&L row) ---"
echo ""
echo "From the backtest report above, note:"
echo "  - Total Return / P&L    -> compare to Grafana: Daily Realized P&L (over same period)"
echo "  - Max Drawdown         -> compare to Grafana: Drawdown %"
echo "  - Win Rate             -> compare to Grafana: Win Rate Today %"
echo "  - Total Trades         -> compare to Grafana: Trades Today"
echo ""
echo "If backtest and live strategy parameters match, these should be in the same ballpark."
echo "See INTRADAY-STRATEGY-IMPLEMENTATION-PLAN.md (Priority 2: Backtest vs Live Comparison)."
