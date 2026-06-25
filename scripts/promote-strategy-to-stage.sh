#!/usr/bin/env bash
#
# Phase 4 promotion gate: validate a backtest, then print the exact steps to
# promote the strategy to Stage shadow trading. This script is intentionally
# read-only — it never starts live trading itself. It enforces the lifecycle in
# docs/FINANCIAL-STRATEGY-IMPLEMENTATION-GUIDE.md: backtest -> validate -> stage
# shadow (DRY_RUN / tiny size) -> economic review.
#
# Usage:
#   ./scripts/promote-strategy-to-stage.sh <backtest_id> [--book btc_mxn]
#
# Environment:
#   BACKTESTING_URL  strategy-executor base URL (default: http://localhost:8084)
#
set -euo pipefail

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; BLUE='\033[0;34m'; NC='\033[0m'
info() { echo -e "${BLUE}[INFO]${NC} $1"; }
ok()   { echo -e "${GREEN}[ OK ]${NC} $1"; }
warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }
fail() { echo -e "${RED}[FAIL]${NC} $1"; }

BACKTESTING_URL="${BACKTESTING_URL:-http://localhost:8084}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

BACKTEST_ID=""
BOOK=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --book) BOOK="$2"; shift 2 ;;
    -h|--help) sed -n '2,14p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) BACKTEST_ID="$1"; shift ;;
  esac
done
[[ -z "$BACKTEST_ID" ]] && { fail "backtest_id required"; exit 1; }
command -v jq >/dev/null 2>&1 || { fail "jq is required"; exit 1; }

info "Validating backtest $BACKTEST_ID against Gate 1..."
if ! "$SCRIPT_DIR/validate-backtest.sh" "$BACKTEST_ID"; then
  fail "Gate 1 not passed. Promotion blocked."
  exit 1
fi

RESULTS=$(curl -sS --max-time 30 "$BACKTESTING_URL/api/v1/backtests/$BACKTEST_ID/results")
STRATEGY=$(echo "$RESULTS" | jq -r '.strategy_name // "unknown"')
[[ -z "$BOOK" ]] && BOOK=$(echo "$RESULTS" | jq -r '.book // "btc_mxn"')

echo ""
ok "Approved for Stage shadow trading."
echo ""
info "Next steps (Stage, shadow / dry-run first):"
cat <<EOF
  1. Deploy to Stage in DRY_RUN with tiny size on a single book ($BOOK):
       STRATEGY_TYPE=$STRATEGY DRY_RUN=true ./scripts/start-organic-trading.sh
  2. Confirm the strategy is running and not tripped:
       ./scripts/kill-switch-status.sh
  3. Observe daily net P&L per regime after realized fees (2-4+ weeks):
       ./scripts/strategy-daily-report.sh --book $BOOK
  4. Periodically compare live Stage stats to the backtest baseline:
       ./scripts/compare-stage-vs-backtest.sh $BACKTEST_ID --book $BOOK
  5. Only after a positive, fee-honest economic review: graduate to live size.
EOF
echo ""
warn "Do NOT skip the shadow/economic-review gate. See docs/strategy-fee-accuracy/STAGE-FINANCIAL-APPROACH-2026-06-04.md"
