#!/usr/bin/env bash
#
# Smoke-test order-management Kafka order-fill publish + kafka_order_fill_published log.
# Requires OM_DEV_ORDER_FILL_PUBLISH_TEST_ENABLED=true (set in k8s/overlays/development for dev).
#
# Usage:
#   ./scripts/dev-publish-order-fill-test.sh
#   NAMESPACE=my-ns LOCAL_PORT=18082 ./scripts/dev-publish-order-fill-test.sh
#
set -euo pipefail

NAMESPACE="${NAMESPACE:-bitso-trading-dev}"
LOCAL_PORT="${LOCAL_PORT:-18082}"
SVC_PORT="${SVC_PORT:-8082}"

section() { echo ""; echo "=== $1 ==="; }

section "Port-forward order-management"
kubectl -n "$NAMESPACE" port-forward "svc/order-management" "${LOCAL_PORT}:${SVC_PORT}" >>/tmp/dev-om-pf.log 2>&1 &
PF_PID=$!
trap 'kill "$PF_PID" 2>/dev/null || true' EXIT
sleep 3
if ! kill -0 "$PF_PID" 2>/dev/null; then
  echo "port-forward failed; see /tmp/dev-om-pf.log"
  exit 1
fi

EID="${EVENT_ID:-dev-smoke-$(date +%s)}"
OID="${ORDER_ID:-dev-smoke-order-$(date +%s)}"

BODY=$(jq -nc \
  --arg eid "$EID" \
  --arg oid "$OID" \
  --arg book "${BOOK:-btc_mxn}" \
  --arg side "${SIDE:-buy}" \
  --arg strat "${STRATEGY:-limit_profit}" \
  --argjson price "${AVERAGE_PRICE:-1000000}" \
  --argjson amt "${FILLED_AMOUNT:-0.001}" \
  '{event_id:$eid, order_id:$oid, book:$book, side:$side, strategy:$strat, average_price:$price, filled_amount:$amt, timestamp_ms: (now * 1000 | floor)}')

section "POST /internal/v1/dev/publish-order-fill-test"
curl -sS --max-time 30 -X POST "http://127.0.0.1:${LOCAL_PORT}/internal/v1/dev/publish-order-fill-test" \
  -H "Content-Type: application/json" \
  -d "$BODY" | jq .

section "order-management logs (kafka_order_fill_published)"
sleep 2
kubectl logs -n "$NAMESPACE" deploy/order-management --tail=80 --since=2m 2>/dev/null | grep -F kafka_order_fill_published || {
  echo "(no matching line yet — check KAFKA_ORDER_FILLS_PUBLISH_ENABLED and topic connectivity)"
  exit 1
}

echo ""
echo "OK: dev publish + log line found."
