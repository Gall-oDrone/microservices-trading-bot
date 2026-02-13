#!/usr/bin/env bash
# Run scrape verification for all trading services. Exits 0 only if all pass.
#
# Usage:
#   PROMETHEUS_URL=http://localhost:9090 ./scripts/verify-prometheus-scrape-all-services.sh
#
# Prerequisites: port-forward Prometheus if needed:
#   kubectl port-forward -n monitoring svc/kube-prometheus-stack-prometheus 9090:9090

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SERVICES="trading-engine order-management api-gateway market-data"
FAILED=0

for svc in $SERVICES; do
  if "$SCRIPT_DIR/verify-prometheus-scrape-service.sh" "$svc"; then
    :
  else
    FAILED=1
  fi
done

if [ "$FAILED" -eq 0 ]; then
  echo "All services are being scraped by Prometheus."
  exit 0
fi

echo "One or more services have no scrape data. Check ServiceMonitors and /metrics endpoints."
exit 1
