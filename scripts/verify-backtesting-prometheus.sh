#!/usr/bin/env bash
# Verify that (1) backtesting service exposes non-zero metrics and (2) the Prometheus
# that Grafana uses has those metrics. Use this to find why the Backtesting dashboard shows 0.
#
# Usage:
#   ./scripts/verify-backtesting-prometheus.sh
#   BACKTEST_URL=http://localhost:8084 PROMETHEUS_URL=http://localhost:9090 ./scripts/verify-backtesting-prometheus.sh
#   # For K8s: port-forward Prometheus then:
#   PROMETHEUS_URL=http://localhost:9090 ./scripts/verify-backtesting-prometheus.sh
#
# Requires: curl

set -e

BACKTEST_URL="${BACKTEST_URL:-http://localhost:8084}"
PROMETHEUS_URL="${PROMETHEUS_URL:-http://localhost:9090}"

echo "=== Backtesting dashboard metrics verification ==="
echo "  Backtesting API: $BACKTEST_URL"
echo "  Prometheus API:  $PROMETHEUS_URL"
echo ""

# 1) Backtesting /metrics
echo "1) Backtesting service /metrics (what the service exposes):"
CREATED=$(curl -s "${BACKTEST_URL}/metrics" 2>/dev/null | grep '^backtesting_backtests_created_total ' | awk '{print $2}' || echo "0")
COMPLETED=$(curl -s "${BACKTEST_URL}/metrics" 2>/dev/null | grep '^backtesting_backtests_completed_total' | awk '{sum+=$2} END {print sum+0}')
if [ -z "$CREATED" ] || [ "$CREATED" = "0" ]; then
  echo "   backtesting_backtests_created_total: (empty or 0) - run a backtest against $BACKTEST_URL"
else
  echo "   backtesting_backtests_created_total: $CREATED"
fi
if [ -z "$COMPLETED" ] || [ "$COMPLETED" = "0" ]; then
  echo "   backtesting_backtests_completed_total (sum): (empty or 0)"
else
  echo "   backtesting_backtests_completed_total (sum): $COMPLETED"
fi
if ! curl -sf "${BACKTEST_URL}/metrics" >/dev/null 2>&1; then
  echo "   ERROR: Cannot reach $BACKTEST_URL/metrics (connection refused or timeout)"
fi
echo ""

# 2) Prometheus targets for backtesting
echo "2) Prometheus backtesting scrape target:"
TARGETS=$(curl -s "${PROMETHEUS_URL}/api/v1/targets" 2>/dev/null | python3 -c "
import json,sys
try:
    d=json.load(sys.stdin)
    for t in d.get('data',{}).get('activeTargets',[]):
        if 'backtest' in t.get('labels',{}).get('job','').lower():
            print('   Job:', t.get('labels',{}).get('job'), '| Instance:', t.get('labels',{}).get('instance'))
            print('   Health:', t.get('health'), '| LastError:', t.get('lastError') or '(none)')
            break
    else:
        print('   No backtesting target found in Prometheus.')
except Exception as e:
    print('   Error:', e)
" 2>/dev/null) || echo "   ERROR: Cannot reach $PROMETHEUS_URL (is Prometheus running?)"
echo "$TARGETS"
echo ""

# 3) Same query as dashboard: sum(backtesting_backtests_created_total)
echo "3) Prometheus query (same as Grafana panel): sum(backtesting_backtests_created_total)"
QUERY=$(curl -s --data-urlencode "query=sum(backtesting_backtests_created_total)" "${PROMETHEUS_URL}/api/v1/query" 2>/dev/null | python3 -c "
import json,sys
try:
    d=json.load(sys.stdin)
    r=d.get('data',{}).get('result',[])
    if r and r[0].get('value'):
        print('   Result:', r[0]['value'][1])
    else:
        print('   Result: (no data)')
except Exception as e:
    print('   Error:', e)
" 2>/dev/null) || echo "   ERROR: Query failed"
echo "$QUERY"
echo ""

# 4) Root cause hint
echo "4) Conclusion:"
if ! curl -sf "${PROMETHEUS_URL}/api/v1/query" --data-urlencode "query=sum(backtesting_backtests_created_total)" >/dev/null 2>&1; then
  echo "   - Prometheus at $PROMETHEUS_URL is not reachable. Grafana cannot get data."
  echo "   - Fix: Use the same Prometheus URL that Grafana uses (Connections → Data sources → Prometheus)."
elif [ -n "$CREATED" ] && [ "$CREATED" != "0" ] && [ -z "$QUERY" ] || echo "$QUERY" | grep -q "no data"; then
  echo "   - Backtesting service HAS non-zero metrics, but Prometheus at $PROMETHEUS_URL has NO data."
  echo "   - Root cause: Grafana is querying a DIFFERENT Prometheus than the one that scrapes this backtesting instance."
  echo "   - Example: You run backtests against Docker backtesting (localhost:8084) but view Grafana in Kubernetes;"
  echo "     K8s Grafana uses K8s Prometheus, which only scrapes K8s backtesting pods (they have 0 backtests)."
  echo "   - Fix: Either (a) view Grafana in the SAME environment (Docker) where you ran backtests, or"
  echo "     (b) run backtests against the BACKTESTING instance that the Prometheus used by Grafana scrapes"
  echo "     (e.g. K8s backtesting URL / port-forward), then re-check the dashboard."
else
  echo "   - Prometheus has backtesting metrics. If Grafana still shows 0, check: dashboard time range,"
  echo "     datasource URL (e.g. path prefix /prometheus), and that the dashboard uses datasource uid 'prometheus'."
fi
