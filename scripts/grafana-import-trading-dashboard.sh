#!/usr/bin/env bash
# Import the main trading dashboard into Grafana via API.
# This imports monitoring/grafana/dashboards/domain/trading-metrics.json, which appears
# in Grafana as "Trading Platform Metrics" (intraday P&L, scrape status, etc.).
# Dashboard layout: dashboards/services/ (per-service) and dashboards/domain/ (platform).
# For per-service dashboards (Trading Engine, Order Management, etc.) use:
#   ./scripts/grafana-import-dashboard.sh trading-engine
#   ./scripts/grafana-import-dashboard.sh order-management
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
DASHBOARD_JSON="$REPO_ROOT/monitoring/grafana/dashboards/domain/trading-metrics.json"

# Default to grafana.local when using CloudFront; override with localhost:3000 if using port-forward
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

if [ ! -f "$DASHBOARD_JSON" ]; then
  echo "Error: Dashboard file not found: $DASHBOARD_JSON" >&2
  exit 1
fi

# Grafana API expects { "dashboard": {...}, "overwrite": true }
payload=$(jq '. + {"overwrite": true}' "$DASHBOARD_JSON") || {
  echo "Error: Failed to read or parse dashboard JSON: $DASHBOARD_JSON" >&2
  exit 1
}

echo "Importing trading dashboard (file: trading-metrics.json) to $GRAFANA_URL ..."
resp=""
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
  echo "  2. Terminal 2: GRAFANA_URL=http://localhost:3000 $0" >&2
  echo "" >&2
  echo "Option B - One command (starts port-forward in background, then imports):" >&2
  echo "  (kubectl port-forward -n monitoring svc/kube-prometheus-stack-grafana 3000:80 &) && sleep 2 && GRAFANA_URL=http://localhost:3000 $0" >&2
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
  echo "  - Try: GRAFANA_URL=http://localhost:3000 $0" >&2
  exit 1
fi

if [ "$http_code" = "200" ]; then
  echo "Dashboard imported successfully."
  echo "In Grafana, look for: Trading Platform Metrics (tags: trading, bitso, intraday)"
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
