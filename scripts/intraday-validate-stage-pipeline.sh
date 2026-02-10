#!/usr/bin/env bash
# Intraday Priority 1: Validate stage pipeline (order-management health + risk/session).
# Usage:
#   Local:  ./scripts/intraday-validate-stage-pipeline.sh
#   K8s:   NAMESPACE=bitso-trading-dev USE_KUBECTL=1 ./scripts/intraday-validate-stage-pipeline.sh
# Env: ORDER_MANAGEMENT_URL (default http://localhost:8080), NAMESPACE (default bitso-trading-dev), USE_KUBECTL (1 = port-forward then curl)

set -e

OM_URL="${ORDER_MANAGEMENT_URL:-http://localhost:8080}"
NAMESPACE="${NAMESPACE:-bitso-trading-dev}"
USE_KUBECTL="${USE_KUBECTL:-0}"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'
ok()  { echo -e "${GREEN}[OK]${NC} $1"; }
warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }
fail() { echo -e "${RED}[FAIL]${NC} $1"; }
info() { echo -e "${BLUE}[INFO]${NC} $1"; }

echo "Intraday Priority 1: Operational validation (stage pipeline)"
echo "Order-management URL: $OM_URL"
echo ""

# Optional: port-forward then override OM_URL to localhost
if [ "$USE_KUBECTL" = "1" ]; then
  if ! command -v kubectl &>/dev/null; then
    fail "USE_KUBECTL=1 but kubectl not found"
    exit 1
  fi
  info "Checking order-management pod in namespace $NAMESPACE..."
  OM_POD=$(kubectl get pods -n "$NAMESPACE" -l app=bitso-trading-platform,service=order-management -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)
  if [ -z "$OM_POD" ]; then
    OM_POD=$(kubectl get pods -n "$NAMESPACE" -l service=order-management -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)
  fi
  if [ -z "$OM_POD" ]; then
    fail "No order-management pod found in $NAMESPACE"
    exit 1
  fi
  ok "Found pod: $OM_POD"
  # Pod may expose 8080 or 8082; try 8082 first (common in k8s manifests)
  OM_PORT=$(kubectl get pod -n "$NAMESPACE" "$OM_POD" -o jsonpath='{.spec.containers[0].ports[0].containerPort}' 2>/dev/null || echo "8082")
  info "Port-forwarding order-management 8080:${OM_PORT} (background)..."
  kubectl port-forward -n "$NAMESPACE" "pod/$OM_POD" 8080:"${OM_PORT}" &>/dev/null &
  PF_PID=$!
  trap "kill $PF_PID 2>/dev/null || true" EXIT
  sleep 3
  OM_URL="http://localhost:8080"
fi

# 1. Health
info "Checking order-management health..."
HEALTH=$(curl -s -o /dev/null -w "%{http_code}" --max-time 5 "$OM_URL/health" 2>/dev/null || echo "000")
if [ "$HEALTH" = "200" ]; then
  ok "GET $OM_URL/health -> 200"
else
  fail "GET $OM_URL/health -> $HEALTH (expected 200)"
  exit 1
fi

# 2. Risk session endpoint (used by trading-engine for MaxDailyLoss / MaxDrawdownPct)
info "Checking GET /api/v1/risk/session..."
RISK_RESP=$(curl -s --max-time 5 "$OM_URL/api/v1/risk/session" 2>/dev/null || echo "")
RISK_CODE=$(curl -s -o /dev/null -w "%{http_code}" --max-time 5 "$OM_URL/api/v1/risk/session" 2>/dev/null || echo "000")
if [ "$RISK_CODE" = "200" ]; then
  ok "GET $OM_URL/api/v1/risk/session -> 200"
  if echo "$RISK_RESP" | grep -q "daily_realized_pnl\|drawdown_percent"; then
    ok "Response contains daily_realized_pnl and/or drawdown_percent"
    echo "  Sample: $(echo "$RISK_RESP" | head -c 200)..."
  else
    warn "Response may not match expected shape (daily_realized_pnl, drawdown_percent)"
  fi
else
  warn "GET $OM_URL/api/v1/risk/session -> $RISK_CODE (trading-engine needs this for session risk limits)"
fi

echo ""
info "Manual checklist (verify in logs/config):"
echo "  1. BITSO_API_BASE_URL unset or https://stage.bitso.com/api (stage)."
echo "  2. Trading-engine: stage keys (STAGE_BITSO_API_KEY, STAGE_BITSO_API_SECRET); ORDER_MANAGEMENT_URL set for limits."
echo "  3. Order-management: consumer for trading.orders.placed (logs: 'Orders-placed consumer configured')."
echo "  4. Order-management: Bitso sync job (logs: 'Bitso sync job configured') when stage keys set."
echo "  5. MaxDailyLoss / MaxDrawdownPct set in trading config if you want limits enforced."
echo ""
ok "Stage pipeline validation done. Fix any [FAIL] or [WARN] before relying on limits or sync."
