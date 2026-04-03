#!/usr/bin/env bash
# End-to-end P&L verification test for Bitso Stage.
# Places BUY and SELL orders directly via Bitso API, waits for fills, then verifies P&L metrics in order-management.
#
# Usage:
#   ./scripts/e2e-pnl-test.sh
#   USE_KUBECTL=1 NAMESPACE=bitso-trading-dev ./scripts/e2e-pnl-test.sh
#
# Env:
#   BITSO_API_KEY       Bitso Stage API key (or fetched from K8s secret if USE_KUBECTL=1)
#   BITSO_API_SECRET    Bitso Stage API secret (or fetched from K8s secret if USE_KUBECTL=1)
#   BITSO_API_BASE      (default https://stage.bitso.com/api)
#   BOOK                (default btc_mxn)
#   TARGET_NOTIONAL_MXN (default 500) — approximate MXN value per order
#   FILL_TIMEOUT_SEC    (default 120) — max seconds to wait for each order to fill
#   USE_KUBECTL         (default 0) — if 1, fetch secrets from K8s and use kubectl port-forward for metrics
#   NAMESPACE           (default bitso-trading-dev)
#   OM_METRICS_URL      (default http://127.0.0.1:8082/metrics) — order-management /metrics endpoint

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

BITSO_API_BASE="${BITSO_API_BASE:-https://stage.bitso.com/api}"
BOOK="${BOOK:-btc_mxn}"
TARGET_NOTIONAL_MXN="${TARGET_NOTIONAL_MXN:-500}"
FILL_TIMEOUT_SEC="${FILL_TIMEOUT_SEC:-120}"
USE_KUBECTL="${USE_KUBECTL:-0}"
NAMESPACE="${NAMESPACE:-bitso-trading-dev}"
OM_METRICS_URL="${OM_METRICS_URL:-http://127.0.0.1:8082/metrics}"
MIN_MAJOR="${MIN_MAJOR:-0.001}"
MAX_MAJOR="${MAX_MAJOR:-0.1}"

RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[0;33m'
NC='\033[0m'
info() { echo -e "${BLUE}[INFO]${NC} $*"; }
ok() { echo -e "${GREEN}[OK]${NC} $*"; }
warn() { echo -e "${YELLOW}[WARN]${NC} $*"; }
fail() { echo -e "${RED}[FAIL]${NC} $*"; exit 1; }

cleanup() {
  if [[ -n "${PORT_FWD_PID:-}" ]]; then
    kill "$PORT_FWD_PID" 2>/dev/null || true
  fi
}
trap cleanup EXIT

if ! command -v jq &>/dev/null; then
  fail "jq is required"
fi

if [[ "$USE_KUBECTL" == "1" ]]; then
  command -v kubectl &>/dev/null || fail "kubectl required when USE_KUBECTL=1"
  if [[ -z "${BITSO_API_KEY:-}" ]]; then
    BITSO_API_KEY="$(kubectl get secret trading-secrets -n "$NAMESPACE" -o jsonpath='{.data.bitso-api-key}' 2>/dev/null | base64 -d)" || fail "Cannot fetch bitso-api-key from K8s secret"
  fi
  if [[ -z "${BITSO_API_SECRET:-}" ]]; then
    BITSO_API_SECRET="$(kubectl get secret trading-secrets -n "$NAMESPACE" -o jsonpath='{.data.bitso-api-secret}' 2>/dev/null | base64 -d)" || fail "Cannot fetch bitso-api-secret from K8s secret"
  fi
  info "Starting port-forward to order-management..."
  kubectl port-forward -n "$NAMESPACE" svc/order-management 8082:8082 >/dev/null 2>&1 &
  PORT_FWD_PID=$!
  sleep 2
  OM_METRICS_URL="http://127.0.0.1:8082/metrics"
fi

[[ -n "${BITSO_API_KEY:-}" ]] || fail "BITSO_API_KEY not set"
[[ -n "${BITSO_API_SECRET:-}" ]] || fail "BITSO_API_SECRET not set"

bitso_auth_header() {
  local method="$1" path="$2" payload="${3:-}"
  local nonce signature
  nonce="$(date +%s%N)"
  local message="${nonce}${method}${path}${payload}"
  signature="$(echo -n "$message" | openssl dgst -sha256 -hmac "$BITSO_API_SECRET" | awk '{print $2}')"
  echo "Bitso ${BITSO_API_KEY}:${nonce}:${signature}"
}

bitso_get() {
  local path="$1"
  local auth
  auth="$(bitso_auth_header GET "$path")"
  curl -sS --max-time 30 -H "Authorization: $auth" "${BITSO_API_BASE}${path}"
}

bitso_post() {
  local path="$1" payload="$2"
  local auth
  auth="$(bitso_auth_header POST "$path" "$payload")"
  curl -sS --max-time 30 -X POST -H "Authorization: $auth" -H "Content-Type: application/json" -d "$payload" "${BITSO_API_BASE}${path}"
}

get_ticker() {
  curl -sS --max-time 15 "${BITSO_API_BASE}/v3/ticker?book=${BOOK}"
}

get_order() {
  local oid="$1"
  bitso_get "/v3/orders/${oid}"
}

wait_for_fill() {
  local oid="$1" side="$2" timeout="$FILL_TIMEOUT_SEC"
  local elapsed=0 interval=5
  info "Waiting for $side order $oid to fill (timeout: ${timeout}s)..."
  while (( elapsed < timeout )); do
    local resp status
    resp="$(get_order "$oid" 2>/dev/null || echo '{}')"
    status="$(echo "$resp" | jq -r '.payload.status // "unknown"')"
    case "$status" in
      completed)
        ok "$side order $oid filled!"
        echo "$resp"
        return 0
        ;;
      cancelled)
        fail "$side order $oid was cancelled"
        ;;
      unknown|"")
        local trades_resp
        trades_resp="$(bitso_get "/v3/order_trades/${oid}" 2>/dev/null || echo '{}')"
        local num_trades
        num_trades="$(echo "$trades_resp" | jq '.payload | length // 0')"
        if (( num_trades > 0 )); then
          ok "$side order $oid filled (detected via /order_trades)!"
          echo "$trades_resp"
          return 0
        fi
        ;;
    esac
    sleep "$interval"
    (( elapsed += interval ))
  done
  warn "$side order $oid did not fill within ${timeout}s"
  return 1
}

place_limit_order() {
  local side="$1" price="$2" amount="$3"
  local payload
  payload="$(jq -nc --arg b "$BOOK" --arg s "$side" --arg t "limit" --arg p "$price" --arg a "$amount" \
    '{book:$b, side:$s, type:$t, price:$p, major:$a}')"
  info "Placing $side limit order: price=$price, amount=$amount"
  local resp
  resp="$(bitso_post "/v3/orders" "$payload")"
  local success oid
  success="$(echo "$resp" | jq -r '.success')"
  oid="$(echo "$resp" | jq -r '.payload.oid // empty')"
  if [[ "$success" != "true" || -z "$oid" ]]; then
    fail "Failed to place $side order: $resp"
  fi
  ok "Placed $side order: $oid"
  echo "$oid"
}

info "=== E2E P&L Test ==="
info "Book: $BOOK | Target notional: ~$TARGET_NOTIONAL_MXN MXN"

TICKER_JSON="$(get_ticker)" || fail "Failed to fetch ticker"
echo "$TICKER_JSON" | jq -e '.success == true' >/dev/null || fail "Ticker error: $TICKER_JSON"
LAST="$(echo "$TICKER_JSON" | jq -r '.payload.last')"
BID="$(echo "$TICKER_JSON" | jq -r '.payload.bid')"
ASK="$(echo "$TICKER_JSON" | jq -r '.payload.ask')"
info "Ticker: last=$LAST bid=$BID ask=$ASK"

RAW_AMT="$(awk -v t="$TARGET_NOTIONAL_MXN" -v l="$LAST" 'BEGIN { printf "%.8f", (t / l) }')"
AMT="$(awk -v a="$RAW_AMT" -v mn="$MIN_MAJOR" -v mx="$MAX_MAJOR" 'BEGIN {
  if (a < mn) a = mn
  if (a > mx) a = mx
  printf "%.8f", a
}')"
info "Amount: $AMT $BOOK (clamped to [$MIN_MAJOR, $MAX_MAJOR])"

BUY_PRICE="$(awk -v a="$ASK" 'BEGIN { printf "%.2f", a * 1.001 }')"
info "BUY price: $BUY_PRICE (slightly above ask to ensure fill)"

info "--- Step 1: Place BUY order ---"
BUY_OID="$(place_limit_order buy "$BUY_PRICE" "$AMT")"

info "--- Step 2: Wait for BUY fill ---"
BUY_FILL_RESP="$(wait_for_fill "$BUY_OID" BUY)" || fail "BUY order did not fill"

TICKER_JSON2="$(get_ticker)" || fail "Failed to fetch ticker"
BID2="$(echo "$TICKER_JSON2" | jq -r '.payload.bid')"
SELL_PRICE="$(awk -v b="$BID2" 'BEGIN { printf "%.2f", b * 0.999 }')"
info "SELL price: $SELL_PRICE (slightly below bid to ensure fill)"

info "--- Step 3: Place SELL order ---"
SELL_OID="$(place_limit_order sell "$SELL_PRICE" "$AMT")"

info "--- Step 4: Wait for SELL fill ---"
SELL_FILL_RESP="$(wait_for_fill "$SELL_OID" SELL)" || fail "SELL order did not fill"

EXPECTED_PNL="$(awk -v bp="$BUY_PRICE" -v sp="$SELL_PRICE" -v a="$AMT" 'BEGIN { printf "%.2f", (sp - bp) * a }')"
info "Expected P&L (approx): $EXPECTED_PNL MXN (sell_price - buy_price) * amount"

info "--- Step 5: Wait for order-management sync (10s) ---"
sleep 10

info "--- Step 6: Query order-management /metrics ---"
METRICS="$(curl -sS --max-time 10 "$OM_METRICS_URL" 2>/dev/null || echo '')"
if [[ -z "$METRICS" ]]; then
  warn "Could not fetch order-management metrics from $OM_METRICS_URL"
else
  REALIZED_PNL="$(echo "$METRICS" | grep 'trading_daily_realized_pnl_currency{currency="MXN"}' | awk '{print $2}' || echo '?')"
  TRADES_TODAY="$(echo "$METRICS" | grep "trading_trades_today_total{book=\"$BOOK\"" | awk '{print $2}' || echo '?')"
  ORDERS_FILLED="$(echo "$METRICS" | grep "orders_filled_total{book=\"$BOOK\"" | awk '{print $2}' || echo '?')"
  
  echo ""
  echo "========== Results =========="
  echo "  BUY Order:         $BUY_OID (price: $BUY_PRICE)"
  echo "  SELL Order:        $SELL_OID (price: $SELL_PRICE)"
  echo "  Amount:            $AMT"
  echo "  Expected P&L:      $EXPECTED_PNL MXN"
  echo "  --------------------------"
  echo "  Realized P&L:      $REALIZED_PNL MXN"
  echo "  Trades Today:      $TRADES_TODAY"
  echo "  Orders Filled:     $ORDERS_FILLED"
  echo "=============================="
fi

echo ""
ok "E2E P&L test complete. Check Grafana for updated metrics."
