#!/bin/bash

# Cleanup script for Kubernetes application resources
# This script deletes all application resources in the trading namespace
# Run this BEFORE cleanup-addons.sh to prevent stuck resources

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
ENVIRONMENT=${1:-"development"}
AWS_REGION="${AWS_REGION:-us-east-1}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TERRAFORM_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
TERRAFORM_DIR="$TERRAFORM_ROOT/envs/${ENVIRONMENT}"

# Application namespace
APP_NAMESPACE="bitso-trading-dev"

# Get cluster name from Terraform or use fallback
get_cluster_name() {
    local cluster_name=""
    
    if [ -d "$TERRAFORM_DIR" ]; then
        cd "$TERRAFORM_DIR"
        terraform init >/dev/null 2>&1 || true
        cluster_name=$(terraform output -raw cluster_name 2>/dev/null || echo "")
        cd - >/dev/null 2>&1
    fi
    
    if [ -z "$cluster_name" ] || [ "$cluster_name" = "" ]; then
        cluster_name="mtb-${ENVIRONMENT}"
        print_warning "Could not retrieve cluster name from Terraform, using fallback: $cluster_name"
    else
        print_success "Retrieved cluster name from Terraform: $cluster_name"
    fi
    
    echo "$cluster_name"
}

# Check prerequisites
check_prerequisites() {
    print_info "Checking prerequisites..."
    
    if ! command -v kubectl &> /dev/null; then
        print_error "kubectl is not installed. Please install kubectl first."
        exit 1
    fi
    
    if ! command -v aws &> /dev/null; then
        print_error "AWS CLI is not installed. Please install AWS CLI first."
        exit 1
    fi
    
    # Check AWS credentials
    if ! aws sts get-caller-identity &>/dev/null; then
        print_error "AWS credentials not configured. Please run 'aws configure' or set AWS credentials."
        exit 1
    fi
    
    print_success "Prerequisites OK"
}

# Update kubeconfig
configure_kubectl() {
    local cluster_name=$1
    
    print_info "Updating kubeconfig for cluster: $cluster_name"
    
    if ! aws eks update-kubeconfig --region "$AWS_REGION" --name "$cluster_name" 2>/dev/null; then
        print_error "Failed to update kubeconfig. Is the cluster accessible?"
        exit 1
    fi
    
    # Verify cluster connectivity
    if ! kubectl get nodes >/dev/null 2>&1; then
        print_error "Cannot connect to cluster. Please check your credentials and network."
        exit 1
    fi
    
    print_success "Connected to cluster: $cluster_name"
}

# Check if namespace exists
namespace_exists() {
    kubectl get namespace "$1" &> /dev/null
}

# Remove finalizers from a resource
remove_finalizers() {
    local resource_type=$1
    local resource_name=$2
    local namespace=$3
    
    print_info "Removing finalizers from $resource_type/$resource_name"
    kubectl patch "$resource_type" "$resource_name" -n "$namespace" \
        --type='merge' -p='{"metadata":{"finalizers":[]}}' 2>/dev/null || true
}

# Delete ExternalSecrets resources
cleanup_external_secrets() {
    local namespace=$1
    
    print_info "Cleaning up ExternalSecrets in namespace: $namespace"
    
    # Get all ExternalSecrets
    local external_secrets=$(kubectl get externalsecrets -n "$namespace" -o jsonpath='{.items[*].metadata.name}' 2>/dev/null || echo "")
    
    if [ -n "$external_secrets" ]; then
        for es in $external_secrets; do
            print_info "Deleting ExternalSecret: $es"
            # Remove finalizers first to prevent stuck deletion
            remove_finalizers "externalsecret" "$es" "$namespace"
            kubectl delete externalsecret "$es" -n "$namespace" --grace-period=0 --force 2>/dev/null || true
        done
        print_success "ExternalSecrets deleted"
    else
        print_info "No ExternalSecrets found"
    fi
    
    # Get all SecretStores
    local secret_stores=$(kubectl get secretstores -n "$namespace" -o jsonpath='{.items[*].metadata.name}' 2>/dev/null || echo "")
    
    if [ -n "$secret_stores" ]; then
        for ss in $secret_stores; do
            print_info "Deleting SecretStore: $ss"
            remove_finalizers "secretstore" "$ss" "$namespace"
            kubectl delete secretstore "$ss" -n "$namespace" --grace-period=0 --force 2>/dev/null || true
        done
        print_success "SecretStores deleted"
    else
        print_info "No SecretStores found"
    fi
}

# Delete all deployments
cleanup_deployments() {
    local namespace=$1
    
    print_info "Cleaning up Deployments in namespace: $namespace"
    
    local deployments=$(kubectl get deployments -n "$namespace" -o jsonpath='{.items[*].metadata.name}' 2>/dev/null || echo "")
    
    if [ -n "$deployments" ]; then
        for deploy in $deployments; do
            print_info "Deleting Deployment: $deploy"
            kubectl delete deployment "$deploy" -n "$namespace" --grace-period=0 2>/dev/null || true
        done
        print_success "Deployments deleted"
    else
        print_info "No Deployments found"
    fi
}

# Delete all services
cleanup_services() {
    local namespace=$1
    
    print_info "Cleaning up Services in namespace: $namespace"
    
    local services=$(kubectl get svc -n "$namespace" -o jsonpath='{.items[*].metadata.name}' 2>/dev/null || echo "")
    
    if [ -n "$services" ]; then
        for svc in $services; do
            # Skip kubernetes default service
            if [ "$svc" = "kubernetes" ]; then
                continue
            fi
            print_info "Deleting Service: $svc"
            kubectl delete svc "$svc" -n "$namespace" --grace-period=0 2>/dev/null || true
        done
        print_success "Services deleted"
    else
        print_info "No Services found"
    fi
}

# Delete all ConfigMaps
cleanup_configmaps() {
    local namespace=$1
    
    print_info "Cleaning up ConfigMaps in namespace: $namespace"
    
    local configmaps=$(kubectl get configmap -n "$namespace" -o jsonpath='{.items[*].metadata.name}' 2>/dev/null || echo "")
    
    if [ -n "$configmaps" ]; then
        for cm in $configmaps; do
            # Skip kube-root-ca.crt as it's managed by Kubernetes
            if [ "$cm" = "kube-root-ca.crt" ]; then
                continue
            fi
            print_info "Deleting ConfigMap: $cm"
            kubectl delete configmap "$cm" -n "$namespace" --grace-period=0 2>/dev/null || true
        done
        print_success "ConfigMaps deleted"
    else
        print_info "No ConfigMaps found"
    fi
}

# Delete all Secrets
cleanup_secrets() {
    local namespace=$1
    
    print_info "Cleaning up Secrets in namespace: $namespace"
    
    local secrets=$(kubectl get secret -n "$namespace" -o jsonpath='{.items[*].metadata.name}' 2>/dev/null || echo "")
    
    if [ -n "$secrets" ]; then
        for secret in $secrets; do
            # Skip default service account tokens
            if [[ "$secret" == default-token-* ]] || [[ "$secret" == sh.helm.* ]]; then
                continue
            fi
            print_info "Deleting Secret: $secret"
            kubectl delete secret "$secret" -n "$namespace" --grace-period=0 2>/dev/null || true
        done
        print_success "Secrets deleted"
    else
        print_info "No Secrets found"
    fi
}

# Delete all PVCs
cleanup_pvcs() {
    local namespace=$1
    
    print_info "Cleaning up PVCs in namespace: $namespace"
    
    local pvcs=$(kubectl get pvc -n "$namespace" -o jsonpath='{.items[*].metadata.name}' 2>/dev/null || echo "")
    
    if [ -n "$pvcs" ]; then
        for pvc in $pvcs; do
            print_info "Deleting PVC: $pvc"
            remove_finalizers "pvc" "$pvc" "$namespace"
            kubectl delete pvc "$pvc" -n "$namespace" --grace-period=0 --force 2>/dev/null || true
        done
        print_success "PVCs deleted"
    else
        print_info "No PVCs found"
    fi
}

# Delete all pods (force cleanup)
cleanup_pods() {
    local namespace=$1
    
    print_info "Force cleaning up remaining Pods in namespace: $namespace"
    
    kubectl delete pods --all -n "$namespace" --grace-period=0 --force 2>/dev/null || true
    
    print_success "Pods cleanup initiated"
}

# Delete StatefulSets
cleanup_statefulsets() {
    local namespace=$1
    
    print_info "Cleaning up StatefulSets in namespace: $namespace"
    
    local statefulsets=$(kubectl get statefulset -n "$namespace" -o jsonpath='{.items[*].metadata.name}' 2>/dev/null || echo "")
    
    if [ -n "$statefulsets" ]; then
        for sts in $statefulsets; do
            print_info "Deleting StatefulSet: $sts"
            kubectl delete statefulset "$sts" -n "$namespace" --grace-period=0 2>/dev/null || true
        done
        print_success "StatefulSets deleted"
    else
        print_info "No StatefulSets found"
    fi
}

# Delete the namespace
cleanup_namespace() {
    local namespace=$1
    
    print_info "Deleting namespace: $namespace"
    
    # Remove finalizers from namespace if stuck
    kubectl patch namespace "$namespace" --type='merge' \
        -p='{"metadata":{"finalizers":[]}}' 2>/dev/null || true
    
    # Delete namespace
    kubectl delete namespace "$namespace" --grace-period=0 --wait=false 2>/dev/null || true
    
    print_success "Namespace deletion initiated: $namespace"
}

# Wait for namespace to be deleted
wait_for_namespace_deletion() {
    local namespace=$1
    local max_wait=120
    local elapsed=0
    local interval=5
    
    print_info "Waiting for namespace $namespace to be deleted (max ${max_wait}s)..."
    
    while [ $elapsed -lt $max_wait ]; do
        if ! namespace_exists "$namespace"; then
            print_success "Namespace $namespace has been deleted"
            return 0
        fi
        
        sleep $interval
        elapsed=$((elapsed + interval))
        print_info "Still waiting... (${elapsed}s elapsed)"
    done
    
    print_warning "Namespace $namespace still exists after ${max_wait}s"
    print_info "It may be stuck in Terminating state. Check with: kubectl get namespace $namespace -o yaml"
    return 1
}

# Main cleanup function
cleanup_application() {
    local namespace=$1
    
    print_info "🧹 Starting cleanup of application resources in namespace: $namespace"
    
    if ! namespace_exists "$namespace"; then
        print_warning "Namespace $namespace does not exist. Nothing to clean up."
        return 0
    fi
    
    # Show current state
    print_info "Current resources in namespace $namespace:"
    kubectl get all -n "$namespace" 2>/dev/null || true
    echo ""
    
    # Cleanup in order (dependencies first)
    cleanup_external_secrets "$namespace"
    cleanup_deployments "$namespace"
    cleanup_statefulsets "$namespace"
    cleanup_services "$namespace"
    cleanup_configmaps "$namespace"
    cleanup_secrets "$namespace"
    cleanup_pvcs "$namespace"
    cleanup_pods "$namespace"
    
    # Wait a moment for resources to be cleaned up
    print_info "Waiting 10 seconds for resources to be cleaned up..."
    sleep 10
    
    # Show remaining resources
    print_info "Remaining resources in namespace $namespace:"
    kubectl get all -n "$namespace" 2>/dev/null || echo "Namespace may already be deleted"
    
    # Delete the namespace
    cleanup_namespace "$namespace"
    
    # Wait for namespace deletion
    wait_for_namespace_deletion "$namespace"
}

# Verify cleanup
verify_cleanup() {
    local namespace=$1
    
    print_info "🔍 Verifying cleanup..."
    
    if namespace_exists "$namespace"; then
        print_warning "⚠️  Namespace $namespace still exists"
        kubectl get namespace "$namespace" -o yaml 2>/dev/null | grep -A 10 "status:" || true
    else
        print_success "✅ Namespace $namespace has been deleted"
    fi
}

# Main execution
main() {
    print_info "🚀 Kubernetes Application Cleanup Script"
    print_info "Environment: $ENVIRONMENT"
    print_info "Target Namespace: $APP_NAMESPACE"
    echo ""
    
    # Check prerequisites
    check_prerequisites
    
    # Get cluster name
    CLUSTER_NAME=$(get_cluster_name)
    print_info "Cluster: $CLUSTER_NAME"
    print_info "Region: $AWS_REGION"
    echo ""
    
    # Configure kubectl
    configure_kubectl "$CLUSTER_NAME"
    echo ""
    
    # Confirm deletion
    print_warning "⚠️  WARNING: This will delete ALL resources in namespace: $APP_NAMESPACE"
    print_warning "This includes: Deployments, Services, ConfigMaps, Secrets, ExternalSecrets, and the namespace itself"
    echo ""
    read -p "Are you sure you want to continue? Type 'yes' to proceed: " -r response
    
    if [[ ! "$response" == "yes" ]]; then
        print_info "Cleanup cancelled by user"
        exit 0
    fi
    
    echo ""
    
    # Run cleanup
    cleanup_application "$APP_NAMESPACE"
    
    echo ""
    
    # Verify cleanup
    verify_cleanup "$APP_NAMESPACE"
    
    echo ""
    print_success "🎉 Application cleanup completed!"
    print_info "You can now proceed with addon cleanup (addons.sh) and infrastructure cleanup (cleanup.sh)"
}

# Show usage if help requested
if [ "$1" = "-h" ] || [ "$1" = "--help" ]; then
    echo "Usage: $0 [environment]"
    echo ""
    echo "Arguments:"
    echo "  environment    The environment to clean up (default: development)"
    echo ""
    echo "Examples:"
    echo "  $0                  # Clean up development environment"
    echo "  $0 development      # Clean up development environment"
    echo "  $0 staging          # Clean up staging environment"
    echo ""
    echo "This script will delete all application resources in the bitso-trading-dev namespace."
    echo "Run this BEFORE addons.sh to prevent stuck resources."
    exit 0
fi

# Run main function
main "$@"
