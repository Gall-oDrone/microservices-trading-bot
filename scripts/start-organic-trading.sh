#!/usr/bin/env bash
#
# Start organic (market-driven) strategy execution — no synthetic /process ticks.
# Full context: docs/ORGANIC-TRADING-STARTUP.md
#
# What this script DOES:
#   - Verifies kubectl + namespace + core pods exist
#   - Checks strategy-executor logs for Redis (Connected vs in-memory fallback)
#   - Port-forwards strategy-executor, POSTs create + start for mean_reversion
#
# What you MUST verify outside this script (manual / cluster-specific):
#   - kubectl context is the intended EKS/cluster
#   - market-data is receiving live trades for BOOK (logs, Grafana)
#   - Kafka topic trading.signals exists; trading-engine consumes (see docs for replica caveats)
#   - trading-engine: DRY_RUN unset, Stage URL + secrets, OM pre-trade validation reachable
#   - POSITION_SIZE meets order-management / Bitso minimums
#
# Usage:
#   ./scripts/start-organic-trading.sh
#   NAMESPACE=my-ns BOOK=btc_mxn ./scripts/start-organic-trading.sh
#
set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m'

info() { echo -e "${BLUE}[INFO]${NC} $1"; }
ok() { echo -e "${GREEN}[OK]${NC} $1"; }
warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }
err() { echo -e "${RED}[ERROR]${NC} $1"; }
section() { echo -e "\n${CYAN}=== $1 ===${NC}"; }

# --- Configuration (override via env) ---
NAMESPACE="${NAMESPACE:-bitso-trading-dev}"
BOOK="${BOOK:-btc_mxn}"
STRATEGY_NAME="${STRATEGY_NAME:-organic_mean_reversion_$(date +%s)}"
STRATEGY_EXECUTOR_LOCAL_PORT="${STRATEGY_EXECUTOR_LOCAL_PORT:-8084}"
STRATEGY_EXECUTOR_SVC_PORT="${STRATEGY_EXECUTOR_SVC_PORT:-8081}"

# Mean reversion params — tune for more/fewer organic signals (see FINANCIAL-STRATEGY-IMPLEMENTATION-GUIDE.md)
POSITION_SIZE="${POSITION_SIZE:-0.001}"
LOOKBACK_PERIOD="${LOOKBACK_PERIOD:-20}"
ENTRY_THRESHOLD="${ENTRY_THRESHOLD:-2.0}"
EXIT_THRESHOLD="${EXIT_THRESHOLD:-0.5}"

REQUIRED_LABELS=( "service=market-data" "service=strategy-executor" "service=trading-engine" "service=order-management" "service=redis" "service=kafka" )

section "0. Tooling"
if ! command -v kubectl &>/dev/null; then
  err "kubectl not found. Install kubectl and configure cluster access."
  exit 1
fi
if ! command -v curl &>/dev/null || ! command -v jq &>/dev/null; then
  err "curl and jq are required."
  exit 1
fi
ok "kubectl, curl, jq present"
info "kubectl context: $(kubectl config current-context 2>/dev/null || echo 'unknown')"

section "1. Namespace and pods (verify workloads Running)"
if ! kubectl get namespace "$NAMESPACE" &>/dev/null; then
  err "Namespace $NAMESPACE does not exist."
  exit 1
fi
ok "Namespace $NAMESPACE exists"

MISSING=0
for spec in "${REQUIRED_LABELS[@]}"; do
  key="${spec%%=*}"
  val="${spec#*=}"
  n=$(kubectl get pods -n "$NAMESPACE" -l "$key=$val" --no-headers 2>/dev/null | grep -c Running || true)
  if [[ "$n" -eq 0 ]]; then
    warn "No Running pod for -l $key=$val (check deployments)"
    MISSING=1
  else
    ok "Running pod(s) for -l $key=$val: $n"
  fi
done
if [[ "$MISSING" -ne 0 ]]; then
  warn "Some expected services have no Running pods — fix cluster before relying on organic flow."
fi

section "2. Redis for strategy-executor (verify Connected to Redis)"
# TODO(user): If you see only "in-memory store", fix REDIS_HOST/password/network; indicators reset on restart without Redis.
# Startup lines may be far back — scan more lines than recent indicator noise.
REDIS_LOG=$(kubectl logs -n "$NAMESPACE" deploy/strategy-executor --tail=5000 2>/dev/null || true)
if echo "$REDIS_LOG" | grep -q "Connected to Redis"; then
  ok "Log line found: Connected to Redis"
elif echo "$REDIS_LOG" | grep -q "using in-memory store"; then
  warn "Redis fallback in use (in-memory indicator store). Check redis pod, password secret, network."
else
  warn "Could not confirm Redis from last 5000 log lines (JSON/logger format may differ). Check: kubectl logs -n $NAMESPACE deploy/strategy-executor --tail=5000 | grep -i redis"
fi

section "3. Port-forward strategy-executor (required for local API calls)"
# Optional: if port is busy, user must free it or set STRATEGY_EXECUTOR_LOCAL_PORT
if command -v ss &>/dev/null && ss -ltnp 2>/dev/null | grep -q ":${STRATEGY_EXECUTOR_LOCAL_PORT}\\b"; then
  warn "Port $STRATEGY_EXECUTOR_LOCAL_PORT may be in use; set STRATEGY_EXECUTOR_LOCAL_PORT or stop the other process."
fi
kubectl -n "$NAMESPACE" port-forward "svc/strategy-executor" "${STRATEGY_EXECUTOR_LOCAL_PORT}:${STRATEGY_EXECUTOR_SVC_PORT}" >>/tmp/start-organic-trading-pf.log 2>&1 &
PF_PID=$!
sleep 5
if ! kill -0 "$PF_PID" 2>/dev/null; then
  err "port-forward failed. See /tmp/start-organic-trading-pf.log"
  exit 1
fi
ok "port-forward PID $PF_PID → http://127.0.0.1:${STRATEGY_EXECUTOR_LOCAL_PORT}"
trap 'kill $PF_PID 2>/dev/null || true' EXIT

SE_URL="http://127.0.0.1:${STRATEGY_EXECUTOR_LOCAL_PORT}"

section "4. Health + create + start strategy (organic path — no /process)"
if ! curl -sS --max-time 5 "$SE_URL/health" | jq -e '.status == "healthy"' &>/dev/null; then
  err "strategy-executor /health not healthy at $SE_URL"
  exit 1
fi
ok "strategy-executor healthy"

info "Creating strategy: $STRATEGY_NAME (mean_reversion, book=$BOOK)"
CREATE_BODY=$(jq -nc \
  --arg name "$STRATEGY_NAME" \
  --arg book "$BOOK" \
  --argjson lp "$LOOKBACK_PERIOD" \
  --argjson et "$ENTRY_THRESHOLD" \
  --argjson xt "$EXIT_THRESHOLD" \
  --argjson ps "$POSITION_SIZE" \
  '{name:$name, type:"mean_reversion", book:$book, parameters:{lookback_period:$lp, entry_threshold:$et, exit_threshold:$xt, position_size:$ps}}')

HTTP_CODE=$(curl -sS -o /tmp/se-create.json -w "%{http_code}" --max-time 15 -X POST "$SE_URL/api/v1/strategies" \
  -H "Content-Type: application/json" -d "$CREATE_BODY")
if [[ "$HTTP_CODE" != "200" && "$HTTP_CODE" != "201" ]]; then
  err "Create strategy failed HTTP $HTTP_CODE — response:"
  cat /tmp/se-create.json 2>/dev/null || true
  exit 1
fi
ok "Strategy created (HTTP $HTTP_CODE)"

HTTP_CODE=$(curl -sS -o /tmp/se-start.json -w "%{http_code}" --max-time 15 -X POST "$SE_URL/api/v1/strategies/$STRATEGY_NAME/start")
if [[ "$HTTP_CODE" != "200" && "$HTTP_CODE" != "201" ]]; then
  err "Start strategy failed HTTP $HTTP_CODE — response:"
  cat /tmp/se-start.json 2>/dev/null || true
  exit 1
fi
ok "Strategy start requested (HTTP $HTTP_CODE)"

info "Strategy detail:"
curl -sS --max-time 10 "$SE_URL/api/v1/strategies/$STRATEGY_NAME" | jq '{name, type, book, running, parameters}' 2>/dev/null || curl -sS "$SE_URL/api/v1/strategies/$STRATEGY_NAME"

section "5. Done — organic loop is active if strategy shows running"
info "Do NOT call POST /api/v1/strategies/process unless you intentionally want a synthetic tick (E2E)."
echo ""
echo "Verify manually:"
echo "  kubectl -n $NAMESPACE logs -f deploy/strategy-executor   # watch for published signals / ProcessTick"
echo "  kubectl -n $NAMESPACE logs -f deploy/trading-engine     # consume trading.signals, validate, Bitso"
echo "  Grafana: strategy-executor + trading-engine dashboards"
echo ""
echo "Strategy name: $STRATEGY_NAME"
echo "To stop port-forward when finished: kill $PF_PID"
ok "start-organic-trading.sh finished."
