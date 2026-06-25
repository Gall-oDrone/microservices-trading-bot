#!/usr/bin/env bash
#
# Show kill-switch / circuit-breaker status for all strategies by scraping the
# strategy-executor /metrics endpoint. Exits non-zero if any breaker is tripped,
# so it can gate automation.
#
# Usage:
#   ./scripts/kill-switch-status.sh
#
# Environment:
#   STRATEGY_EXECUTOR_URL  base URL (default: http://localhost:8084)
#
set -euo pipefail

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; BLUE='\033[0;34m'; NC='\033[0m'
info() { echo -e "${BLUE}[INFO]${NC} $1"; }
ok()   { echo -e "${GREEN}[ OK ]${NC} $1"; }
warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }
fail() { echo -e "${RED}[TRIP]${NC} $1"; }

STRATEGY_EXECUTOR_URL="${STRATEGY_EXECUTOR_URL:-http://localhost:8084}"

METRICS=$(curl -sS --max-time 15 "$STRATEGY_EXECUTOR_URL/metrics" 2>/dev/null) || {
  fail "Could not scrape $STRATEGY_EXECUTOR_URL/metrics"; exit 2; }

info "Circuit-breaker status ($STRATEGY_EXECUTOR_URL)"
echo ""

TRIPPED=0
# Lines look like: <metric>{strategy="x",book="y"} 1
while IFS= read -r line; do
  [[ -z "$line" ]] && continue
  labels=$(echo "$line" | sed -n 's/.*{\(.*\)}.*/\1/p')
  value=$(echo "$line" | awk '{print $NF}')
  if [[ "$value" == "1" ]]; then
    fail "TRIPPED  $labels"
    TRIPPED=$((TRIPPED+1))
  else
    ok "normal   $labels"
  fi
done < <(echo "$METRICS" | grep -E '^(limit_profit|momentum)_circuit_breaker_active\{' || true)

echo ""
info "Session realized P&L (quote):"
echo "$METRICS" | grep -E '^(limit_profit|momentum)_daily_realized_pnl_quote\{' \
  | sed 's/^/  /' || echo "  (none reported)"

echo ""
if [[ "$TRIPPED" -gt 0 ]]; then
  fail "$TRIPPED circuit breaker(s) tripped."
  exit 1
fi
ok "No circuit breakers tripped."
