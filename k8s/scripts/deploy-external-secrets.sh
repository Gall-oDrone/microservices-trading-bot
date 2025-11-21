#!/bin/bash
# Deploy ExternalSecret to Kubernetes
# This script deploys the ExternalSecret manifest and verifies secrets are synced

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

print_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

print_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Configuration
NAMESPACE="${NAMESPACE:-bitso-trading-dev}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BASE_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
SERVICE_ACCOUNT_FILE="${BASE_DIR}/base/service-account-external-secrets.yaml"
MANIFEST_FILE="${BASE_DIR}/base/external-secret.yaml"

print_info "🔐 Deploying ExternalSecret to Kubernetes..."
print_info "Namespace: $NAMESPACE"
print_info "ServiceAccount: $SERVICE_ACCOUNT_FILE"
print_info "Manifest: $MANIFEST_FILE"

# Check prerequisites
if ! command -v kubectl &> /dev/null; then
    print_error "kubectl is not installed. Please install kubectl first."
    exit 1
fi

# Check kubectl connectivity
if ! kubectl cluster-info &>/dev/null; then
    print_error "Cannot connect to Kubernetes cluster. Please check your kubeconfig."
    exit 1
fi

print_success "kubectl is installed and configured"

# Step 1: Create namespace if it doesn't exist
print_info "📦 Checking namespace: $NAMESPACE"
if kubectl get namespace "$NAMESPACE" &>/dev/null; then
    print_info "Namespace '$NAMESPACE' already exists"
else
    print_info "Creating namespace: $NAMESPACE"
    kubectl create namespace "$NAMESPACE"
    print_success "✅ Created namespace: $NAMESPACE"
fi

# Step 2: Apply ServiceAccount manifest (required for SecretStore)
print_info "👤 Applying ServiceAccount manifest..."
if [ ! -f "$SERVICE_ACCOUNT_FILE" ]; then
    print_error "ServiceAccount file not found: $SERVICE_ACCOUNT_FILE"
    exit 1
fi

if kubectl apply -f "$SERVICE_ACCOUNT_FILE" -n "$NAMESPACE"; then
    print_success "✅ Applied ServiceAccount manifest"
else
    print_error "Failed to apply ServiceAccount manifest"
    exit 1
fi

# Step 2.1: Verify IAM role ARN is configured
print_info "🔍 Verifying IAM role ARN configuration..."
CURRENT_ROLE_ARN=$(kubectl get serviceaccount external-secrets -n "$NAMESPACE" -o jsonpath='{.metadata.annotations.eks\.amazonaws\.com/role-arn}' 2>/dev/null || echo "")
if [ -z "$CURRENT_ROLE_ARN" ] || [ "$CURRENT_ROLE_ARN" = "PLACEHOLDER_ROLE_ARN" ] || [ "$CURRENT_ROLE_ARN" = "ROLE_ARN_PLACEHOLDER" ]; then
    print_warning "⚠️  ServiceAccount has placeholder IAM role ARN: $CURRENT_ROLE_ARN"
    print_info "Attempting to detect and set the correct IAM role ARN..."
    
    # Try to find the development external-secrets IAM role
    DETECTED_ROLE_ARN=$(aws iam get-role --role-name mtb-development-external-secrets --query 'Role.Arn' --output text 2>/dev/null || echo "")
    
    if [ -n "$DETECTED_ROLE_ARN" ] && [ "$DETECTED_ROLE_ARN" != "None" ]; then
        print_info "Found IAM role: $DETECTED_ROLE_ARN"
        print_info "Patching ServiceAccount with detected IAM role ARN..."
        kubectl annotate serviceaccount external-secrets -n "$NAMESPACE" \
            eks.amazonaws.com/role-arn="$DETECTED_ROLE_ARN" \
            --overwrite
        print_success "✅ Updated ServiceAccount with IAM role ARN: $DETECTED_ROLE_ARN"
    else
        print_warning "Could not automatically detect IAM role ARN"
        print_info "Please manually update the ServiceAccount annotation:"
        print_info "  kubectl annotate serviceaccount external-secrets -n $NAMESPACE eks.amazonaws.com/role-arn=<YOUR_ROLE_ARN> --overwrite"
    fi
else
    print_success "✅ ServiceAccount has IAM role ARN configured: $CURRENT_ROLE_ARN"
fi

# Step 3: Apply ExternalSecret manifest
print_info "📋 Applying ExternalSecret manifest..."
if [ ! -f "$MANIFEST_FILE" ]; then
    print_error "Manifest file not found: $MANIFEST_FILE"
    exit 1
fi

if kubectl apply -f "$MANIFEST_FILE" -n "$NAMESPACE"; then
    print_success "✅ Applied ExternalSecret manifest"
else
    print_error "Failed to apply ExternalSecret manifest"
    exit 1
fi

# Step 4: Wait for SecretStore to be ready
print_info "⏳ Waiting for SecretStore to be validated..."
SECRETSTORE_READY=false
MAX_WAIT=120
WAIT_TIME=0

while [ $WAIT_TIME -lt $MAX_WAIT ]; do
    if kubectl get secretstore aws-secrets-manager -n "$NAMESPACE" -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}' 2>/dev/null | grep -q "True"; then
        SECRETSTORE_READY=true
        break
    fi
    sleep 5
    WAIT_TIME=$((WAIT_TIME + 5))
    echo -n "."
done
echo ""

if [ "$SECRETSTORE_READY" = true ]; then
    print_success "✅ SecretStore is ready"
else
    print_warning "SecretStore validation is taking longer than expected"
    print_info "Checking SecretStore status..."
    kubectl describe secretstore aws-secrets-manager -n "$NAMESPACE" | tail -10
fi

# Step 5: Wait for ExternalSecret to sync
print_info "⏳ Waiting for ExternalSecret to sync secrets..."
EXTERNAL_SECRET_READY=false
WAIT_TIME=0

while [ $WAIT_TIME -lt $MAX_WAIT ]; do
    STATUS=$(kubectl get externalsecret trading-secrets -n "$NAMESPACE" -o jsonpath='{.status.conditions[?(@.type=="Ready")].status}' 2>/dev/null || echo "False")
    if [ "$STATUS" = "True" ]; then
        EXTERNAL_SECRET_READY=true
        break
    fi
    sleep 5
    WAIT_TIME=$((WAIT_TIME + 5))
    echo -n "."
done
echo ""

if [ "$EXTERNAL_SECRET_READY" = true ]; then
    print_success "✅ ExternalSecret synced successfully"
else
    print_warning "ExternalSecret sync is taking longer than expected"
    print_info "Checking ExternalSecret status..."
    kubectl describe externalsecret trading-secrets -n "$NAMESPACE" | tail -10
fi

# Step 6: Verify secrets are synced
print_info "📋 Verifying secrets..."
echo ""

# Check ExternalSecret status
print_info "ExternalSecret status:"
kubectl get externalsecret trading-secrets -n "$NAMESPACE" || {
    print_error "Failed to get ExternalSecret"
    exit 1
}

echo ""

# Check if Kubernetes secret was created
print_info "Kubernetes secret status:"
if kubectl get secret trading-secrets -n "$NAMESPACE" &>/dev/null; then
    print_success "✅ Kubernetes secret 'trading-secrets' exists"
    kubectl get secret trading-secrets -n "$NAMESPACE"
    echo ""
    
    # Show secret keys (without values)
    print_info "Secret keys:"
    kubectl get secret trading-secrets -n "$NAMESPACE" -o jsonpath='{.data}' | jq -r 'keys[]' 2>/dev/null || \
    kubectl get secret trading-secrets -n "$NAMESPACE" -o jsonpath='{.data}' | grep -o '"[^"]*":' | tr -d '":'
    echo ""
    
    # Optionally show secret in yaml format (base64 encoded)
    print_info "Secret details (base64 encoded):"
    kubectl get secret trading-secrets -n "$NAMESPACE" -o yaml | grep -A 10 "^data:"
else
    print_error "❌ Kubernetes secret 'trading-secrets' not found"
    print_info "ExternalSecret status details:"
    kubectl describe externalsecret trading-secrets -n "$NAMESPACE" | tail -20
    exit 1
fi

echo ""
print_success "🎉 ExternalSecret deployment complete!"
print_info ""
print_info "📝 Summary:"
print_info "  - Namespace: $NAMESPACE"
FINAL_ROLE_ARN=$(kubectl get serviceaccount external-secrets -n "$NAMESPACE" -o jsonpath='{.metadata.annotations.eks\.amazonaws\.com/role-arn}' 2>/dev/null || echo "Not configured")
print_info "  - ServiceAccount: external-secrets (IAM Role: $FINAL_ROLE_ARN)"
print_info "  - SecretStore: aws-secrets-manager (Ready: $SECRETSTORE_READY)"
print_info "  - ExternalSecret: trading-secrets (Ready: $EXTERNAL_SECRET_READY)"
print_info "  - Kubernetes Secret: trading-secrets"
echo ""
