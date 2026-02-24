#!/usr/bin/env bash
# Start Redis, market-data (BITSO STAGE WebSocket), backtesting, Prometheus, and Grafana;
# then run one backtest so the Backtesting Grafana dashboard shows activity.
#
# Usage: ./scripts/start-backtest-with-stage.sh [backtest_base_url]
# Example: ./scripts/start-backtest-with-stage.sh
#          RECENT=1 ./scripts/start-backtest-with-stage.sh   # use last 2h for real data
#
# Prerequisites: docker, docker-compose, curl
# After run: open Grafana http://localhost:3000 → Dashboards → Backtesting

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
BACKTEST_BASE_URL="${1:-http://localhost:8084}"

cd "$REPO_ROOT"

echo "=== Starting stack for backtesting with Bitso STAGE market-data ==="
echo "  Redis, market-data (BITSO_WS_URL=stage), backtesting, prometheus, grafana"
echo ""

# Use legacy Docker builder when buildx is older than 0.17.0 (avoids "compose build requires buildx 0.17.0 or later")
export DOCKER_BUILDKIT=0
# Start only the services needed for backtest + Grafana (kafka required by backtesting depends_on)
# Prefer docker-compose when "docker compose" is not available (e.g. older Docker)
if docker compose up -d redis kafka market-data backtesting prometheus grafana 2>/dev/null; then
  :
else
  docker-compose up -d redis kafka market-data backtesting prometheus grafana
fi

echo "Waiting for services to be reachable..."
for i in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15; do
  if curl -sf "${BACKTEST_BASE_URL}/health/live" >/dev/null 2>&1; then
    echo "  Backtesting service is up."
    break
  fi
  if [ "$i" -eq 15 ]; then
    echo "  Timeout waiting for backtesting service at ${BACKTEST_BASE_URL}. Check: docker compose ps"
    exit 1
  fi
  sleep 2
done

# Allow market-data to persist some trades (optional: skip if RECENT=0 and using synthetic)
if [ "${RECENT}" = "1" ]; then
  echo "Waiting 30s for market-data to persist trades from Bitso WebSocket..."
  sleep 30
fi

echo ""
echo "=== Running one backtest (Grafana will show activity) ==="
RECENT="${RECENT:-0}" "$SCRIPT_DIR/run-one-backtest.sh" "$BACKTEST_BASE_URL"

echo ""
echo "=== Done ==="
echo "  Grafana: http://localhost:3000 (admin/admin) → Dashboards → Backtesting"
echo "  Backtesting API: $BACKTEST_BASE_URL"
echo "  Market-data (trades): http://localhost:8083/api/v1/trades?book=btc_mxn&from=...&to=..."
