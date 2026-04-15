#!/usr/bin/env bash
# reconcile-pnl.sh — Reconcile strategy P&L against Bitso ledger
#
# Compares net_quote_pnl from strategy-executor signals against actual
# ledger entries from Bitso API to detect drift.
#
# Usage:
#   ./scripts/reconcile-pnl.sh [--book btc_mxn] [--strategy limit_profit_btc_mxn] [--since 2024-01-01]
#
# Environment:
#   BITSO_API_KEY, BITSO_API_SECRET — required for ledger access
#   STRATEGY_EXECUTOR_URL — defaults to http://localhost:8082
#   ORDER_MANAGEMENT_URL — defaults to http://localhost:8086

set -euo pipefail

BOOK="${BOOK:-btc_mxn}"
STRATEGY="${STRATEGY:-}"
SINCE="${SINCE:-$(date -d '1 day ago' +%Y-%m-%d)}"
STRATEGY_EXECUTOR_URL="${STRATEGY_EXECUTOR_URL:-http://localhost:8082}"
ORDER_MANAGEMENT_URL="${ORDER_MANAGEMENT_URL:-http://localhost:8086}"

while [[ $# -gt 0 ]]; do
  case $1 in
    --book) BOOK="$2"; shift 2 ;;
    --strategy) STRATEGY="$2"; shift 2 ;;
    --since) SINCE="$2"; shift 2 ;;
    *) echo "Unknown option: $1"; exit 1 ;;
  esac
done

if [[ -z "${BITSO_API_KEY:-}" || -z "${BITSO_API_SECRET:-}" ]]; then
  echo "ERROR: BITSO_API_KEY and BITSO_API_SECRET must be set"
  exit 1
fi

echo "=============================================="
echo "P&L Reconciliation Report"
echo "=============================================="
echo "Book: $BOOK"
echo "Strategy: ${STRATEGY:-all}"
echo "Since: $SINCE"
echo "Date: $(date -u +%Y-%m-%dT%H:%M:%SZ)"
echo "=============================================="
echo ""

# Function to generate Bitso auth header
bitso_auth() {
  local method="$1"
  local path="$2"
  local nonce=$(date +%s%N | cut -b1-13)
  local message="${nonce}${method}${path}"
  local signature=$(echo -n "$message" | openssl dgst -sha256 -hmac "$BITSO_API_SECRET" | sed 's/^.* //')
  echo "Bitso ${BITSO_API_KEY}:${nonce}:${signature}"
}

# 1. Get strategy-reported P&L from metrics
echo "=== Strategy-Executor Reported P&L ==="
strategy_pnl=$(curl -s "${STRATEGY_EXECUTOR_URL}/metrics" 2>/dev/null | grep "limit_profit_daily_realized_pnl_quote" | grep -v "^#" || echo "")

if [[ -n "$strategy_pnl" ]]; then
  echo "$strategy_pnl" | while read line; do
    value=$(echo "$line" | awk '{print $2}')
    labels=$(echo "$line" | grep -oP '{[^}]+}' || echo "{}")
    echo "  Metric: $labels = $value MXN"
  done
else
  echo "  No limit_profit P&L metrics found"
fi
echo ""

# 2. Get completed trades from order-management
echo "=== Order-Management Trades (last 24h) ==="
trades_response=$(curl -s "${ORDER_MANAGEMENT_URL}/api/v1/trades?book=${BOOK}&limit=100" 2>/dev/null || echo "{}")

if [[ "$trades_response" != "{}" && "$trades_response" != "" ]]; then
  # Parse trades and calculate P&L
  trade_count=$(echo "$trades_response" | jq -r '.trades | length' 2>/dev/null || echo "0")
  echo "  Trades found: $trade_count"
  
  if [[ "$trade_count" -gt "0" ]]; then
    total_pnl=$(echo "$trades_response" | jq -r '[.trades[].net_pnl // 0] | add' 2>/dev/null || echo "0")
    echo "  Total reported P&L: $total_pnl MXN"
  fi
else
  echo "  Could not fetch trades from order-management"
fi
echo ""

# 3. Get Bitso ledger entries (fees paid)
echo "=== Bitso Ledger Entries ==="
ledger_path="/v3/ledger?book=${BOOK}&limit=100"
auth_header=$(bitso_auth "GET" "$ledger_path")

ledger_response=$(curl -s "https://api.bitso.com${ledger_path}" \
  -H "Authorization: $auth_header" 2>/dev/null || echo '{"success":false}')

if echo "$ledger_response" | jq -e '.success == true' > /dev/null 2>&1; then
  # Calculate fees from ledger
  total_fees=$(echo "$ledger_response" | jq -r '[.payload[] | select(.operation == "fee") | .balance_update | tonumber] | add // 0')
  trade_entries=$(echo "$ledger_response" | jq -r '[.payload[] | select(.operation == "trade")] | length')
  
  echo "  Trade entries: $trade_entries"
  echo "  Total fees paid: $total_fees MXN"
  
  # Show recent trades
  echo ""
  echo "  Recent trades:"
  echo "$ledger_response" | jq -r '.payload[] | select(.operation == "trade") | "    \(.created_at): \(.balance_update) \(.currency)"' | head -10
else
  echo "  Could not fetch ledger from Bitso API"
  echo "  Response: $(echo "$ledger_response" | jq -r '.error.message // "unknown error"')"
fi
echo ""

# 4. Get current balances
echo "=== Current Balances ==="
balance_path="/v3/balance"
auth_header=$(bitso_auth "GET" "$balance_path")

balance_response=$(curl -s "https://api.bitso.com${balance_path}" \
  -H "Authorization: $auth_header" 2>/dev/null || echo '{"success":false}')

if echo "$balance_response" | jq -e '.success == true' > /dev/null 2>&1; then
  echo "$balance_response" | jq -r '.payload.balances[] | select(.currency == "mxn" or .currency == "btc") | "  \(.currency): available=\(.available) locked=\(.locked) total=\(.total)"'
else
  echo "  Could not fetch balances from Bitso API"
fi
echo ""

# 5. Reconciliation summary
echo "=============================================="
echo "Reconciliation Summary"
echo "=============================================="
echo ""
echo "Manual verification steps:"
echo "1. Compare strategy-reported P&L with ledger trade entries"
echo "2. Verify fee calculations match actual fees in ledger"
echo "3. Check for any missed fills (orders in Bitso not in strategy state)"
echo "4. Verify position sizes match between strategy state and actual holdings"
echo ""
echo "If discrepancy found:"
echo "  - Check strategy-executor logs for fill events"
echo "  - Check order-management logs for order lifecycle"
echo "  - Compare signal event_id with order signal_id"
echo "=============================================="
