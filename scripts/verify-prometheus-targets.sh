#!/usr/bin/env bash
# Verify Prometheus scrape status for trading services (run after deploying fixes).
# Requires: kubectl, jq, Prometheus pod in namespace monitoring.
set -e
POD="${PROMETHEUS_POD:-prometheus-kube-prometheus-stack-prometheus-0}"
NS="${PROMETHEUS_NS:-monitoring}"
echo "Querying targets from Prometheus pod $NS/$POD ..."
kubectl exec -n "$NS" "$POD" -c prometheus -- wget -qO- 'http://127.0.0.1:9090/api/v1/targets' 2>/dev/null | \
  jq -r '.data.activeTargets[] | "\(.labels.job)\t\(.health)\t\(.lastError // "")"' | sort -u
echo ""
echo "If market-data and trading-engine show 'up', Grafana should receive data after refresh."
