#!/usr/bin/env bash
# Import the Trading Platform Metrics (intraday) dashboard into Grafana via API.
# No manual copy-paste: run this while Grafana is reachable.
#
# Prerequisites:
#   - Grafana reachable at GRAFANA_URL
#   - Credentials in GRAFANA_USER/GRAFANA_PASSWORD or default admin/admin
#
# Usage:
#   With CloudFront / grafana.local (browser at http://grafana.local):
#     GRAFANA_URL=http://grafana.local ./scripts/grafana-import-trading-dashboard.sh
#   With port-forward only:
#     ./scripts/grafana-port-forward.sh   # in another terminal
#     GRAFANA_URL=http://localhost:3000 ./scripts/grafana-import-trading-dashboard.sh

set -e
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
DASHBOARD_JSON="$REPO_ROOT/monitoring/grafana/dashboards/trading-metrics.json"

# Default to grafana.local when using CloudFront; override with localhost:3000 if using port-forward
GRAFANA_URL="${GRAFANA_URL:-http://grafana.local}"
GRAFANA_USER="${GRAFANA_USER:-admin}"
GRAFANA_PASSWORD="${GRAFANA_PASSWORD:-admin}"

if [ ! -f "$DASHBOARD_JSON" ]; then
  echo "Error: Dashboard file not found: $DASHBOARD_JSON"
  exit 1
fi

# Grafana API expects { "dashboard": {...}, "overwrite": true }
payload=$(jq '. + {"overwrite": true}' "$DASHBOARD_JSON")

echo "Importing Trading Platform Metrics dashboard to $GRAFANA_URL ..."
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
