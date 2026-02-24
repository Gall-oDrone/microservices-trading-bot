#!/usr/bin/env bash
# Run the recommended actions so the Backtesting Grafana dashboard shows non-zero metrics:
# 1) Verify which Prometheus has backtesting data (and which backtesting it scrapes).
# 2) If needed, run a backtest against the backtesting instance that Prometheus scrapes.
# 3) Print Grafana datasource and dashboard checks.
#
# Usage:
#   # Docker Compose (Grafana at localhost:3000, Prometheus at localhost:9090, backtesting at 8084):
#   ./scripts/backtesting-dashboard-recommended-actions.sh
#
#   # Kubernetes: port-forward Prometheus and backtesting first (or use the script's K8s mode):
#   ./scripts/backtesting-dashboard-recommended-actions.sh --k8s
#   # Or with custom URLs after manual port-forwards:
#   PROMETHEUS_URL=http://localhost:9091 BACKTEST_URL=http://localhost:8085 ./scripts/backtesting-dashboard-recommended-actions.sh
#
# Requires: curl, scripts/verify-backtesting-prometheus.sh, scripts/run-one-backtest.sh

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
cd "$REPO_ROOT"

K8S_MODE=""
PROMETHEUS_URL="${PROMETHEUS_URL:-http://localhost:9090}"
BACKTEST_URL="${BACKTEST_URL:-http://localhost:8084}"
PROMETHEUS_LOCAL_PORT="9091"
BACKTEST_LOCAL_PORT="8085"

while [ $# -gt 0 ]; do
  case "$1" in
    --k8s) K8S_MODE=1; shift ;;
    *) shift ;;
  esac
done

echo "=== Backtesting dashboard – recommended actions ==="
echo ""

# --- Kubernetes: start port-forwards in background ---
if [ -n "$K8S_MODE" ] && command -v kubectl &>/dev/null; then
  echo "K8s mode: port-forwarding Prometheus (monitoring) and backtesting (bitso-trading-dev)..."
  kubectl port-forward -n monitoring svc/kube-prometheus-stack-prometheus "${PROMETHEUS_LOCAL_PORT}:9090" &>/dev/null &
  PF_PID_PROM=$!
  kubectl port-forward -n bitso-trading-dev svc/backtesting "${BACKTEST_LOCAL_PORT}:8084" &>/dev/null &
  PF_PID_BT=$!
  trap "kill $PF_PID_PROM $PF_PID_BT 2>/dev/null || true" EXIT
  echo "  Waiting for port-forwards to be ready..."
  for i in 1 2 3 4 5 6 7 8 9 10; do
    if curl -sf "http://localhost:${PROMETHEUS_LOCAL_PORT}/api/v1/status/config" >/dev/null 2>&1 && curl -sf "http://localhost:${BACKTEST_LOCAL_PORT}/health/live" >/dev/null 2>&1; then
      break
    fi
    [ "$i" -eq 10 ] && echo "  Warning: port-forwards may not be ready; continuing anyway."
    sleep 2
  done
  PROMETHEUS_URL="http://localhost:${PROMETHEUS_LOCAL_PORT}"
  BACKTEST_URL="http://localhost:${BACKTEST_LOCAL_PORT}"
  echo "  Prometheus: $PROMETHEUS_URL"
  echo "  Backtesting: $BACKTEST_URL"
  echo ""
fi

# --- Step 1: Verify ---
echo "--- Step 1: Verify Prometheus and backtesting ---"
export PROMETHEUS_URL BACKTEST_URL
"$SCRIPT_DIR/verify-backtesting-prometheus.sh" || true
echo ""

# --- Step 2: Run backtest if verification said to ---
echo "--- Step 2: Run one backtest against the scraped backtesting instance ---"
CREATED=$(curl -s "${BACKTEST_URL}/metrics" 2>/dev/null | grep '^backtesting_backtests_created_total ' | awk '{print $2}' || echo "0")
PROM_HAS_DATA=$(curl -s --data-urlencode "query=sum(backtesting_backtests_created_total)" "${PROMETHEUS_URL}/api/v1/query" 2>/dev/null | python3 -c "
import json,sys
try:
    d=json.load(sys.stdin)
    r=d.get('data',{}).get('result',[])
    print('1' if r and r[0].get('value') and float(r[0]['value'][1]) > 0 else '0')
except: print('0')
" 2>/dev/null || echo "0")

if [ "$PROM_HAS_DATA" = "0" ] || [ -z "$CREATED" ] || [ "$CREATED" = "0" ]; then
  echo "  Running one backtest against $BACKTEST_URL so Prometheus gets metrics..."
  if "$SCRIPT_DIR/run-one-backtest.sh" "$BACKTEST_URL" 2>&1; then
    echo "  Backtest completed. Wait ~15–30s for Prometheus to scrape, then refresh the dashboard."
  else
    echo "  Backtest failed or timed out (e.g. market-data unreachable). Scrape/Health and Uptime may still show; run again when data source is available."
  fi
else
  echo "  Prometheus already has backtesting metrics (created_total > 0). Skip backtest unless you want more runs."
fi
echo ""

# --- Step 3: Grafana datasource and dashboard checks ---
echo "--- Step 3: Grafana datasource and dashboard checks ---"
echo "  • In Grafana: Connections → Data sources → Prometheus"
echo "    - URL must point to the Prometheus that scrapes this backtesting instance:"
if [ -n "$K8S_MODE" ]; then
  echo "      Kubernetes: http://kube-prometheus-stack-prometheus:9090 (or with path prefix if set, e.g. ...:9090/prometheus)"
else
  echo "      Docker Compose: http://prometheus:9090"
fi
echo "    - Save & test"
echo "  • Open the Backtesting dashboard; set time range to e.g. Last 1 hour; refresh (10s)."
echo "  • Panels use datasource uid 'prometheus'; ensure the Prometheus datasource has uid prometheus."
echo ""
echo "=== Done ==="
