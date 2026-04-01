#!/bin/bash
# Post-Deployment Checklist Runner
# Runs all phases from POST-DEPLOYMENT-CHECKLIST.md: verify pods, ESO, Kafka topics,
# health checks, monitoring, security policies, integration checks, ingress.
# Usage: ./post-deployment-checklist.sh [--skip-phase N] [--namespace NS]
# Example: ./post-deployment-checklist.sh --namespace bitso-trading-dev

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

print_info()    { echo -e "${BLUE}[INFO]${NC} $1"; }
print_success() { echo -e "${GREEN}[SUCCESS]${NC} $1"; }
print_warning() { echo -e "${YELLOW}[WARNING]${NC} $1"; }
print_error()   { echo -e "${RED}[ERROR]${NC} $1"; }

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
NAMESPACE="${NAMESPACE:-bitso-trading-dev}"
SKIP_PHASES=""

while [ $# -gt 0 ]; do
  case "$1" in
    --namespace)   NAMESPACE="$2"; shift 2 ;;
    --skip-phase)  SKIP_PHASES="$SKIP_PHASES $2"; shift 2 ;;
    *) shift ;;
  esac
done

skip_phase() {
  local p="$1"
  for s in $SKIP_PHASES; do [ "$s" = "$p" ] && return 0; done
  return 1
}

echo "🚀 Post-Deployment Checklist (namespace: $NAMESPACE)"
print_info "Repo root: $REPO_ROOT"

# ---------------------------------------------------------------------------
# Prerequisites
# ---------------------------------------------------------------------------
print_info "📦 Checking prerequisites..."
if ! command -v kubectl &>/dev/null; then
  print_error "kubectl is not installed."
  exit 1
fi
if ! kubectl cluster-info &>/dev/null; then
  print_error "Cannot connect to Kubernetes cluster. Check kubeconfig."
  exit 1
fi
print_success "kubectl configured and cluster reachable"

# ---------------------------------------------------------------------------
# Phase 1: Service Health Verification
# ---------------------------------------------------------------------------
run_phase_1() {
  print_info "📦 Phase 1: Service Health Verification"
  kubectl get pods -n "$NAMESPACE" -o wide
  kubectl get svc,endpoints -n "$NAMESPACE"
  if kubectl top pods -n "$NAMESPACE" &>/dev/null; then
    kubectl top pods -n "$NAMESPACE"
  else
    print_warning "metrics-server not available; skipping kubectl top"
  fi
  local not_ready
  not_ready=$(kubectl get pods -n "$NAMESPACE" --no-headers 2>/dev/null | grep -v "Running" | grep -v "Completed" | wc -l)
  if [ "${not_ready:-0}" -gt 0 ]; then
    print_warning "Some pods are not Running. Check: kubectl get pods -n $NAMESPACE"
  else
    print_success "Phase 1: All pods and services look healthy"
  fi
}

# ---------------------------------------------------------------------------
# Phase 2: External Secrets Configuration
# ---------------------------------------------------------------------------
run_phase_2() {
  print_info "📦 Phase 2: External Secrets Configuration"
  kubectl get secretstore,externalsecret -n "$NAMESPACE" 2>/dev/null || true
  if kubectl get secret trading-secrets -n "$NAMESPACE" &>/dev/null; then
    print_success "Phase 2: trading-secrets exists and ESO is configured"
  else
    print_warning "Phase 2: trading-secrets not found. Ensure AWS secrets and IRSA are set."
  fi
}

# ---------------------------------------------------------------------------
# Phase 3: Kafka Topics + Initial Service Testing
# ---------------------------------------------------------------------------
run_phase_3() {
  print_info "📦 Phase 3: Kafka Topics and Health Checks"

  local kafka_pod
  kafka_pod=$(kubectl get pods -n "$NAMESPACE" -l app=bitso-trading-platform,service=kafka -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)
  if [ -n "$kafka_pod" ]; then
    local topics="market-data.trades market-data.orderbook market-data.ticker trading.orders"
    for t in $topics; do
      kubectl exec "$kafka_pod" -n "$NAMESPACE" -- /opt/kafka/bin/kafka-topics.sh --create --if-not-exists \
        --bootstrap-server localhost:9092 --topic "$t" --partitions 1 --replication-factor 1 2>/dev/null || true
    done
    # trading.signals: use >= trading-engine Deployment replicas so each consumer in trading-engine-group gets a partition
    local te_replicas
    te_replicas=$(kubectl get deployment trading-engine -n "$NAMESPACE" -o jsonpath='{.spec.replicas}' 2>/dev/null || echo "2")
    if [ -z "$te_replicas" ] || [ "$te_replicas" -lt 1 ]; then te_replicas=2; fi
    local sig_parts="$te_replicas"
    if [ "$sig_parts" -lt 2 ]; then sig_parts=2; fi
    kubectl exec "$kafka_pod" -n "$NAMESPACE" -- /opt/kafka/bin/kafka-topics.sh --create --if-not-exists \
      --bootstrap-server localhost:9092 --topic trading.signals --partitions "$sig_parts" --replication-factor 1 2>/dev/null || true
    kubectl exec "$kafka_pod" -n "$NAMESPACE" -- /opt/kafka/bin/kafka-topics.sh --bootstrap-server localhost:9092 \
      --describe --topic trading.signals &>/dev/null && \
      kubectl exec "$kafka_pod" -n "$NAMESPACE" -- /opt/kafka/bin/kafka-topics.sh --bootstrap-server localhost:9092 \
        --alter --topic trading.signals --partitions "$sig_parts" 2>/dev/null || true
    print_success "Kafka topics created/verified (trading.signals partitions=$sig_parts)"
  else
    print_warning "Kafka pod not found; skipping topic creation"
  fi

  print_info "Health endpoint checks (port-forward + curl)..."
  for spec in "api-gateway:8085:/health" "market-data:8083:/health" "strategy-executor:8081:/health" "order-management:8082:/health" "backtesting:8084:/health"; do
    svc="${spec%%:*}"; rest="${spec#*:}"; port="${rest%%:*}"; path="${rest#*:}"
    ( kubectl port-forward "svc/$svc" "$port:$port" -n "$NAMESPACE" &>/dev/null & ); sleep 3
    code=$(curl -s -o /dev/null -w "%{http_code}" --max-time 5 "http://127.0.0.1:$port$path" 2>/dev/null || echo "000")
    pkill -f "port-forward.*$svc.*$port" 2>/dev/null || true
    if [ "$code" = "200" ]; then
      print_success "  $svc $path -> $code"
    else
      print_warning "  $svc $path -> $code"
    fi
  done
  print_success "Phase 3: Kafka topics and health checks done"
}

# ---------------------------------------------------------------------------
# Phase 4: Monitoring Stack (ServiceMonitors + Prometheus rules)
# ---------------------------------------------------------------------------
run_phase_4() {
  print_info "📦 Phase 4: Monitoring (ServiceMonitors)"
  if [ -f "$REPO_ROOT/k8s/monitoring/servicemonitors.yaml" ]; then
    kubectl apply -f "$REPO_ROOT/k8s/monitoring/servicemonitors.yaml" 2>/dev/null && print_success "ServiceMonitors applied" || print_warning "ServiceMonitors apply failed (monitoring CRD may be missing)"
  fi
  if [ -f "$REPO_ROOT/k8s/monitoring/prometheus-rules.yaml" ]; then
    kubectl apply -f "$REPO_ROOT/k8s/monitoring/prometheus-rules.yaml" 2>/dev/null && print_success "Prometheus rules applied" || print_warning "Prometheus rules apply failed"
  fi
  print_success "Phase 4: Monitoring manifests applied"
}

# ---------------------------------------------------------------------------
# Phase 5: Security Policies (NetworkPolicy + RBAC)
# ---------------------------------------------------------------------------
run_phase_5() {
  print_info "📦 Phase 5: Security Policies"
  if [ -f "$REPO_ROOT/security/network-policies/default-deny.yml" ]; then
    kubectl apply -f "$REPO_ROOT/security/network-policies/default-deny.yml" -n "$NAMESPACE" 2>/dev/null && print_success "Default deny network policy applied"
  fi
  if [ -f "$REPO_ROOT/security/network-policies/service-policies.yml" ]; then
    kubectl apply -f "$REPO_ROOT/security/network-policies/service-policies.yml" -n "$NAMESPACE" 2>/dev/null && print_success "Service network policies applied"
  fi
  if [ -f "$REPO_ROOT/security/rbac/roles.yml" ]; then
    kubectl apply -f "$REPO_ROOT/security/rbac/roles.yml" -n "$NAMESPACE" 2>/dev/null && print_success "RBAC roles applied"
  fi
  kubectl get networkpolicies -n "$NAMESPACE" 2>/dev/null || true
  print_success "Phase 5: Security policies applied"
}

# ---------------------------------------------------------------------------
# Phase 6: Integration Testing (API routes + Kafka sample)
# ---------------------------------------------------------------------------
run_phase_6() {
  print_info "📦 Phase 6: Integration (API Gateway routes)"
  ( kubectl port-forward "svc/api-gateway" 8085:8085 -n "$NAMESPACE" &>/dev/null & ); sleep 2
  for path in "/health" "/api/v1/status"; do
    code=$(curl -s -o /dev/null -w "%{http_code}" "http://127.0.0.1:8085$path" 2>/dev/null || echo "000")
    [ "$code" = "200" ] && print_success "  GET $path -> $code" || print_warning "  GET $path -> $code"
  done
  pkill -f "port-forward.*api-gateway.*8085" 2>/dev/null || true
  print_success "Phase 6: API Gateway integration checked"
}

# ---------------------------------------------------------------------------
# Phase 7: External Access (Ingress)
# ---------------------------------------------------------------------------
run_phase_7() {
  print_info "📦 Phase 7: Ingress (External Access)"
  if [ -f "$REPO_ROOT/k8s/base/ingress.yaml" ]; then
    kubectl apply -f "$REPO_ROOT/k8s/base/ingress.yaml" -n "$NAMESPACE" 2>/dev/null && print_success "Ingress applied" || print_warning "Ingress apply failed (e.g. ALB controller not installed)"
  else
    print_warning "k8s/base/ingress.yaml not found"
  fi
  kubectl get ingress -n "$NAMESPACE" 2>/dev/null || true
  print_success "Phase 7: Ingress step done"
}

# ---------------------------------------------------------------------------
# Phase 8–10: Summary / optional (no automated E2E or load test)
# ---------------------------------------------------------------------------
run_phase_8_10() {
  print_info "📦 Phases 8–10: E2E, Load Test, Production Readiness (manual)"
  print_info "  See POST-DEPLOYMENT-CHECKLIST.md for: E2E testing, k6 load test, final sign-off."
  print_success "Checklist script complete."
}

# ---------------------------------------------------------------------------
# Run all phases
# ---------------------------------------------------------------------------
cd "$REPO_ROOT"

! skip_phase 1 && run_phase_1 || true
! skip_phase 2 && run_phase_2 || true
! skip_phase 3 && run_phase_3 || true
! skip_phase 4 && run_phase_4 || true
! skip_phase 5 && run_phase_5 || true
! skip_phase 6 && run_phase_6 || true
! skip_phase 7 && run_phase_7 || true
! skip_phase 8 && run_phase_8_10 || true

echo ""
print_success "✅ Post-deployment checklist finished."
print_info "  Namespace: $NAMESPACE"
print_info "  Full guide: $REPO_ROOT/POST-DEPLOYMENT-CHECKLIST.md"
