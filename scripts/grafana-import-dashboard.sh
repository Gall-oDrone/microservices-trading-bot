#!/usr/bin/env bash
# Import a Grafana dashboard by name. Use for per-service dashboards or the main trading-metrics dashboard.
#
# Prerequisites: Grafana reachable at GRAFANA_URL (default http://grafana.local).
#
# Usage:
#   GRAFANA_URL=http://localhost:3000 ./scripts/grafana-import-dashboard.sh trading-engine
#   ./scripts/grafana-import-dashboard.sh order-management
#   ./scripts/grafana-import-dashboard.sh trading-metrics   # main platform dashboard
#
# Dashboards: trading-engine | order-management | api-gateway | market-data | trading-metrics

set -e
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
DASHBOARDS_DIR="$REPO_ROOT/monitoring/grafana/dashboards"

NAME="${1:-trading-metrics}"
GRAFANA_URL="${GRAFANA_URL:-http://grafana.local}"
GRAFANA_USER="${GRAFANA_USER:-admin}"
GRAFANA_PASSWORD="${GRAFANA_PASSWORD:-admin}"

case "$NAME" in
  trading-engine)    DASHBOARD_JSON="$DASHBOARDS_DIR/trading-engine.json" ;;
  order-management)   DASHBOARD_JSON="$DASHBOARDS_DIR/order-management.json" ;;
  api-gateway)        DASHBOARD_JSON="$DASHBOARDS_DIR/api-gateway.json" ;;
  market-data)        DASHBOARD_JSON="$DASHBOARDS_DIR/market-data.json" ;;
  trading-metrics)    DASHBOARD_JSON="$DASHBOARDS_DIR/trading-metrics.json" ;;
  *)
    echo "Usage: $0 DASHBOARD_NAME"
    echo "  DASHBOARD_NAME: trading-engine | order-management | api-gateway | market-data | trading-metrics"
    exit 2
    ;;
esac

if [ ! -f "$DASHBOARD_JSON" ]; then
  echo "Error: Dashboard file not found: $DASHBOARD_JSON"
  exit 1
fi

payload=$(jq '. + {"overwrite": true}' "$DASHBOARD_JSON")
echo "Importing $NAME dashboard to $GRAFANA_URL ..."
resp=$(curl -s -w "\n%{http_code}" -X POST \
  -H "Content-Type: application/json" \
  -u "$GRAFANA_USER:$GRAFANA_PASSWORD" \
  -d "$payload" \
  "$GRAFANA_URL/api/dashboards/db")

http_code=$(echo "$resp" | tail -n1)
body=$(echo "$resp" | sed '$d')

if [ "$http_code" = "200" ]; then
  echo "Dashboard imported successfully."
  path=$(echo "$body" | jq -r '.url // empty')
  [ -n "$path" ] && echo "Open: $GRAFANA_URL$path"
else
  echo "Import failed (HTTP $http_code): $body"
  exit 1
fi
