#!/usr/bin/env bash
# Verify that Prometheus is scraping metrics for a given service.
# Exits 0 if at least one of the service's metrics has data; 1 otherwise.
#
# Usage:
#   PROMETHEUS_URL=http://localhost:9090 ./scripts/verify-prometheus-scrape-service.sh trading-engine
#   ./scripts/verify-prometheus-scrape-service.sh order-management
#
# Requires: curl, jq. Prometheus must be reachable (e.g. kubectl port-forward -n monitoring svc/kube-prometheus-stack-prometheus 9090:9090).

set -e

SERVICE="${1:?Usage: $0 SERVICE (trading-engine|order-management|api-gateway|market-data)}"
PROMETHEUS_URL="${PROMETHEUS_URL:-http://localhost:9090}"
# Strip trailing slash
PROMETHEUS_URL="${PROMETHEUS_URL%/}"

# Returns the number of time series returned by the query (0 if none or API error).
# Uses -L to follow redirects. If the response is not JSON (e.g. HTML when ALB routes to Grafana), prints a hint.
query_result_count() {
  local query="$1"
  local raw
  raw=$(curl -sLG --data-urlencode "query=${query}" "${PROMETHEUS_URL}/api/v1/query") || true
  if echo "$raw" | jq -e '.data.result' >/dev/null 2>&1; then
    echo "$raw" | jq -r '.data.result | length // 0'
    return
  fi
  if echo "$raw" | head -c 50 | grep -q '<!DOCTYPE\|<html'; then
    echo "HINT: Prometheus API returned HTML (wrong ALB routing?). Use port-forward: kubectl port-forward -n monitoring svc/kube-prometheus-stack-prometheus 9090:9090" >&2
  fi
  echo "0"
}

# Per-service: one query that returns series if the service is scraped.
case "$SERVICE" in
  trading-engine)
    n=$(query_result_count 'up{job=~"trading-engine.*"}')
    ;;
  order-management)
    n=$(query_result_count 'up{job=~"order-management.*"}')
    ;;
  api-gateway)
    n=$(query_result_count 'up{job=~"api-gateway.*"}')
    ;;
  market-data)
    n=$(query_result_count 'up{job=~"market-data.*"}')
    ;;
  *)
    echo "Unknown service: $SERVICE. Use: trading-engine, order-management, api-gateway, market-data"
    exit 2
    ;;
esac

if [ "${n:-0}" -gt 0 ]; then
  echo "OK: Prometheus has scraped at least one metric for $SERVICE"
  exit 0
fi

echo "FAIL: No scraped data for $SERVICE. Check ServiceMonitor, /metrics endpoint, and Prometheus targets."
exit 1
