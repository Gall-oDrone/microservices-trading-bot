#!/usr/bin/env bash
# Debug script: trading.orders.placed topic, consumer group lag, and order-management logs.
# Prerequisites: kubectl, jq (optional), Kafka CLI in-cluster (kafka pod) or bitso-trading-platform kafka.
#
# Usage:
#   NAMESPACE=bitso-trading-dev ./scripts/debug-kafka-orders-placed.sh
#
set -euo pipefail

NAMESPACE="${NAMESPACE:-bitso-trading-dev}"
MONITORING_NS="${MONITORING_NS:-monitoring}"
TOPIC="${KAFKA_TOPIC_ORDERS_PLACED:-trading.orders.placed}"
TOPIC_SIGNALS="${KAFKA_TOPIC_SIGNALS:-trading.signals}"
GROUP="${ORDERS_PLACED_GROUP:-order-management-group-orders-placed}"
GROUP_SIGNALS="${SIGNALS_GROUP:-order-management-group-signals}"

echo "=== Namespace: $NAMESPACE | orders.placed: $TOPIC / $GROUP | signals: $TOPIC_SIGNALS / $GROUP_SIGNALS ==="
echo ""

echo "--- order-management pods ---"
kubectl get pods -n "$NAMESPACE" -l service=order-management -o wide 2>/dev/null || true
echo ""

echo "--- Recent OM logs: Linked Bitso / RecordOrderPlaced / orders.placed errors (last 200 lines) ---"
kubectl logs -n "$NAMESPACE" -l service=order-management --tail=200 2>/dev/null \
  | grep -iE 'Linked Bitso|Recorded order placed|RecordOrderPlaced|orders\.placed|trading\.orders\.placed|KAFKA-CONSUMER' \
  | tail -40 || echo "(no matches)"
echo ""

echo "--- Kafka pod (bitso-trading-dev) ---"
KAFKA_POD="$(kubectl get pods -n "$NAMESPACE" -l app=bitso-trading-platform,service=kafka -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)"
if [[ -z "$KAFKA_POD" ]]; then
  echo "No pod with labels app=bitso-trading-platform,service=kafka in $NAMESPACE"
else
  echo "Using pod: $KAFKA_POD"
  echo ""
  echo "--- kafka-topics --describe $TOPIC ---"
  kubectl exec -n "$NAMESPACE" "$KAFKA_POD" -- /opt/kafka/bin/kafka-topics.sh \
    --bootstrap-server localhost:9092 --describe --topic "$TOPIC" 2>&1 || true
  echo ""
  echo "--- kafka-consumer-groups --describe $GROUP (orders.placed) ---"
  kubectl exec -n "$NAMESPACE" "$KAFKA_POD" -- /opt/kafka/bin/kafka-consumer-groups.sh \
    --bootstrap-server localhost:9092 --describe --group "$GROUP" 2>&1 || true
  echo ""
  echo "--- kafka-consumer-groups --describe $GROUP_SIGNALS (trading.signals) ---"
  kubectl exec -n "$NAMESPACE" "$KAFKA_POD" -- /opt/kafka/bin/kafka-consumer-groups.sh \
    --bootstrap-server localhost:9092 --describe --group "$GROUP_SIGNALS" 2>&1 || true
  echo ""
  echo "--- kafka-console-consumer (dry-run: one message if any, 5s timeout) ---"
  kubectl exec -n "$NAMESPACE" "$KAFKA_POD" -- timeout 8 \
    /opt/kafka/bin/kafka-console-consumer.sh \
    --bootstrap-server localhost:9092 --topic "$TOPIC" --from-beginning --max-messages 1 2>&1 || true
fi
echo ""

echo "--- Prometheus (bitso-trading-dev order-management): sync + trades (needs port-forward) ---"
echo "  kubectl port-forward -n $MONITORING_NS svc/kube-prometheus-stack-prometheus 9090:9090 &"
echo "  curl -sG http://127.0.0.1:9090/api/v1/query --data-urlencode 'query=bitso_sync_last_success_timestamp_seconds{namespace=\"$NAMESPACE\"}' | jq .data.result"
echo "  curl -sG http://127.0.0.1:9090/api/v1/query --data-urlencode 'query=bitso_sync_attempts_total{namespace=\"$NAMESPACE\"}' | jq .data.result"
echo ""

echo "--- trading-engine: order placed publish counter (if scraped) ---"
kubectl get pods -n "$NAMESPACE" -l service=trading-engine -o name 2>/dev/null | head -1 | while read -r p; do
  [[ -z "$p" ]] && continue
  echo "Pod $p /metrics grep order_placed:"
  kubectl exec -n "$NAMESPACE" "${p#pod/}" -- wget -qO- http://127.0.0.1:8080/metrics 2>/dev/null | grep -E 'order_placed|OrderPlaced' | head -5 || echo "(no metrics or wget failed)"
done || true

echo ""
echo "Done."
