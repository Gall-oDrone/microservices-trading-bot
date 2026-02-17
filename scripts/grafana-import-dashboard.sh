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

# Prerequisites
if ! command -v curl &>/dev/null; then
  echo "Error: curl is required but not installed. Install curl and try again." >&2
  exit 1
fi
if ! command -v jq &>/dev/null; then
  echo "Error: jq is required but not installed. Install jq and try again." >&2
  exit 1
fi

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
  echo "Error: Dashboard file not found: $DASHBOARD_JSON" >&2
  exit 1
fi

payload=$(jq '. + {"overwrite": true}' "$DASHBOARD_JSON") || {
  echo "Error: Failed to read or parse dashboard JSON: $DASHBOARD_JSON" >&2
  exit 1
}

echo "Importing $NAME dashboard to $GRAFANA_URL ..."
resp=""
http_code=""
if ! resp=$(curl -s -w "\n%{http_code}" --connect-timeout 10 -X POST \
  -H "Content-Type: application/json" \
  -u "$GRAFANA_USER:$GRAFANA_PASSWORD" \
  -d "$payload" \
  "$GRAFANA_URL/api/dashboards/db" 2>&1); then
  echo "Error: Failed to connect to Grafana at $GRAFANA_URL" >&2
  echo "  (Connection refused, timeout, or DNS failure.)" >&2
  echo "" >&2
  echo "Running from EC2 or a server? grafana.local only works from your laptop (where you set /etc/hosts)." >&2
  echo "" >&2
  echo "Option A - Two terminals:" >&2
  echo "  1. Terminal 1: kubectl port-forward -n monitoring svc/kube-prometheus-stack-grafana 3000:80" >&2
  echo "  2. Terminal 2: GRAFANA_URL=http://localhost:3000 $0 ${NAME:-trading-metrics}" >&2
  echo "" >&2
  echo "Option B - One command (starts port-forward in background, then imports):" >&2
  echo "  (kubectl port-forward -n monitoring svc/kube-prometheus-stack-grafana 3000:80 &) && sleep 2 && GRAFANA_URL=http://localhost:3000 $0 ${NAME:-trading-metrics}" >&2
  exit 1
fi

http_code=$(echo "$resp" | tail -n1)
body=$(echo "$resp" | sed '$d')

# Curl can succeed but return empty or non-HTTP output (e.g. redirect page)
if [ -z "$http_code" ] || [ "${#http_code}" -lt 3 ]; then
  echo "Error: No valid response from Grafana at $GRAFANA_URL" >&2
  echo "  (Got non-HTTP response. Check URL and that Grafana is serving the API.)" >&2
  echo "" >&2
  echo "Tips:" >&2
  echo "  - Try: GRAFANA_URL=http://localhost:3000 $0 $NAME" >&2
  exit 1
fi

if [ "$http_code" = "200" ]; then
  echo "Dashboard imported successfully."
  path=$(echo "$body" | jq -r '.url // empty' 2>/dev/null)
  [ -n "$path" ] && echo "Open: $GRAFANA_URL$path"
else
  echo "Error: Import failed (HTTP $http_code)" >&2
  [ -n "$body" ] && echo "$body" | jq -r '.message // .error // .' 2>/dev/null | head -5 | sed 's/^/  /' || echo "  $body" | head -3
  echo "" >&2
  case "$http_code" in
    401|403) echo "  Check credentials: GRAFANA_USER and GRAFANA_PASSWORD (default: admin/admin)" >&2 ;;
    404)     echo "  Check GRAFANA_URL (e.g. http://grafana.local or http://localhost:3000)" >&2 ;;
  esac
  echo "  Full URL used: $GRAFANA_URL/api/dashboards/db" >&2
  exit 1
fi
