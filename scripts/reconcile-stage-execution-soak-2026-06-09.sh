#!/usr/bin/env bash
# Reconcile stuck Phase 3 orders and reset mean_reversion strategy state after OM partial-fill fix.
#
# Usage:
#   ./scripts/reconcile-stage-execution-soak-2026-06-09.sh check
#   ./scripts/reconcile-stage-execution-soak-2026-06-09.sh restart-market-data
#   ./scripts/reconcile-stage-execution-soak-2026-06-09.sh reset-strategy
#   ./scripts/reconcile-stage-execution-soak-2026-06-09.sh reconcile-all
#
set -euo pipefail

NAMESPACE="${NAMESPACE:-bitso-trading-dev}"
STRATEGY="${STRATEGY:-mean_reversion_btc_mxn}"
SELL_OID="${SELL_OID:-VgdJKh6fKC2c1xrZ}"
BUY_OID="${BUY_OID:-oVs9Fk5oEnzkfDqo}"
BUY_ORD_ID="${BUY_ORD_ID:-ord-1780976024431969053}"
SELL_ORD_ID="${SELL_ORD_ID:-ord-1780976484426703153}"

RED='\033[0;31m'; GREEN='\033[0;32m'; BLUE='\033[0;34m'; YELLOW='\033[1;33m'; NC='\033[0m'
info() { echo -e "${BLUE}[INFO]${NC} $*"; }
ok() { echo -e "${GREEN}[OK]${NC} $*"; }
warn() { echo -e "${YELLOW}[WARN]${NC} $*"; }
err() { echo -e "${RED}[ERR]${NC} $*"; }

redis_order() {
  local id="$1"
  kubectl -n "$NAMESPACE" exec deploy/redis -- redis-cli --raw GET "om:order:${id}" 2>/dev/null
}

cluster_price() {
  kubectl -n "$NAMESPACE" exec deploy/strategy-executor -- \
    wget -qO- http://127.0.0.1:8081/api/v1/indicators/btc_mxn/snapshot 2>/dev/null \
    | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('current_price','?'))" 2>/dev/null || echo "?"
}

bitso_price() {
  curl -sf 'https://bitso.com/api/v3/ticker/?book=btc_mxn' \
    | python3 -c "import sys,json; print(json.load(sys.stdin)['payload']['last'])" 2>/dev/null || echo "?"
}

market_data_freshness() {
  kubectl -n "$NAMESPACE" logs deploy/market-data --tail=20 2>&1 \
    | grep 'Last Trade' | tail -1 || echo "(no Last Trade line)"
}

check_orders() {
  info "Order-management image:"
  kubectl -n "$NAMESPACE" get deploy order-management -o jsonpath='{.spec.template.spec.containers[0].image}{"\n"}'
  info "BUY order (${BUY_OID}):"
  redis_order "$BUY_ORD_ID" | python3 -c "import sys,json; d=json.load(sys.stdin); print(json.dumps({k:d.get(k) for k in ['status','filled_amount','amount','metadata']}, indent=2))" 2>/dev/null || warn "BUY order not in Redis"
  info "SELL order (${SELL_OID}):"
  redis_order "$SELL_ORD_ID" | python3 -c "import sys,json; d=json.load(sys.stdin); print(json.dumps({k:d.get(k) for k in ['status','filled_amount','amount','metadata']}, indent=2))" 2>/dev/null || warn "SELL order not in Redis"
  info "Recent OM 312 errors (last 5 min):"
  kubectl -n "$NAMESPACE" logs deploy/order-management --since=5m 2>&1 | grep -c 'error.code":312' || true
  info "Stale order marks (last 24h):"
  kubectl -n "$NAMESPACE" logs deploy/order-management --since=24h 2>&1 | grep -c 'Order marked as stale' || true
  info "Strategy state:"
  kubectl -n "$NAMESPACE" exec deploy/strategy-executor -- \
    wget -qO- "http://127.0.0.1:8081/api/v1/strategies/${STRATEGY}" 2>/dev/null \
    | python3 -c "
import sys,json
d=json.load(sys.stdin)
s=d.get('state',{})
print(json.dumps({k:s.get(k) for k in ['running','has_position','pending_sell','position_size','trade_count','position_side']}, indent=2))
" 2>/dev/null || warn "strategy API failed"
  info "Price check — cluster: $(cluster_price) | Bitso prod: $(bitso_price)"
  info "market-data freshness:"
  market_data_freshness
}

restart_market_data() {
  info "Restarting market-data (WS reconnect)..."
  kubectl -n "$NAMESPACE" rollout restart deploy/market-data
  kubectl -n "$NAMESPACE" rollout status deploy/market-data --timeout=180s
  info "Waiting 90s for first trades..."
  sleep 90
  info "Post-restart freshness:"
  market_data_freshness
  info "Cluster price: $(cluster_price)"
  ok "market-data restarted"
}

reset_strategy() {
  info "Stopping ${STRATEGY}..."
  kubectl -n "$NAMESPACE" exec deploy/strategy-executor -- \
    wget -qO- --post-data='' "http://127.0.0.1:8081/api/v1/strategies/${STRATEGY}/stop" >/dev/null 2>&1 || true
  sleep 2
  info "Starting ${STRATEGY}..."
  kubectl -n "$NAMESPACE" exec deploy/strategy-executor -- \
    wget -qO- --post-data='' "http://127.0.0.1:8081/api/v1/strategies/${STRATEGY}/start" >/dev/null
  sleep 2
  ok "Strategy restarted"
  check_orders
}

reconcile_all() {
  info "=== Phase 3 reconciliation (pre-check) ==="
  check_orders
  restart_market_data
  info "Waiting 120s for OM sync to process stale SELL OID (max 10 retries @ ~10s)..."
  sleep 120
  info "=== Phase 3 reconciliation (post market-data + OM sync wait) ==="
  check_orders
  reset_strategy
  ok "reconcile-all complete — watch for next band-breakout signal at 0.001 BTC"
}

cmd="${1:-check}"
case "$cmd" in
  check) check_orders ;;
  restart-market-data) restart_market_data ;;
  reset-strategy) reset_strategy ;;
  reconcile-all) reconcile_all ;;
  *) echo "Usage: $0 {check|restart-market-data|reset-strategy|reconcile-all}"; exit 1 ;;
esac
