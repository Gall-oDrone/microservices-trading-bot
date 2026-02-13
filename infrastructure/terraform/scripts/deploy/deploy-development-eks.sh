#!/bin/bash
# Complete deployment script for development EKS environment
set -e

echo "🚀 Starting complete deployment for development EKS environment..."

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

# Get the script directory and navigate to terraform root
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TERRAFORM_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
ENV_DIR="$TERRAFORM_ROOT/envs/development"

print_info "Terraform root: $TERRAFORM_ROOT"
print_info "Environment directory: $ENV_DIR"

# Check prerequisites
print_info "Checking prerequisites..."

# Check if terraform is installed
if ! command -v terraform &> /dev/null; then
    print_error "Terraform is not installed. Please install Terraform first."
    exit 1
fi
print_success "Terraform is installed"

# Check if aws cli is installed
if ! command -v aws &> /dev/null; then
    print_error "AWS CLI is not installed. Please install AWS CLI first."
    exit 1
fi
print_success "AWS CLI is installed"

# Check if kubectl is installed
if ! command -v kubectl &> /dev/null; then
    print_warning "kubectl is not installed. Some verification steps will be skipped."
fi

# Change to environment directory
cd "$ENV_DIR"
print_info "Changed to directory: $(pwd)"

# Function to wait for EKS cluster to be ready
wait_for_cluster() {
    local cluster_name=$1
    local region=$2
    local max_wait=${3:-600}  # Default 10 minutes
    
    print_info "⏳ Waiting for EKS cluster '$cluster_name' to be ready (this may take 5-10 minutes)..."
    
    local elapsed=0
    local interval=30
    
    while [ $elapsed -lt $max_wait ]; do
        STATUS=$(aws eks describe-cluster --name "$cluster_name" --region "$region" --query 'cluster.status' --output text 2>/dev/null || echo "NOT_FOUND")
        
        if [ "$STATUS" = "ACTIVE" ]; then
            print_success "✅ EKS cluster is active!"
            return 0
        elif [ "$STATUS" = "NOT_FOUND" ]; then
            print_warning "Cluster not found yet, waiting..."
        else
            print_info "Cluster status: $STATUS (waiting for ACTIVE...)"
        fi
        
        sleep $interval
        elapsed=$((elapsed + interval))
        
        if [ $((elapsed % 60)) -eq 0 ]; then
            print_info "Still waiting... ($(($elapsed / 60)) minutes elapsed)"
        fi
    done
    
    print_warning "⚠️ Timeout waiting for cluster to be ready"
    return 1
}

# Function to wait for nodes to be ready
wait_for_nodes() {
    local max_wait=${1:-300}  # Default 5 minutes
    local elapsed=0
    local interval=15
    
    print_info "⏳ Waiting for nodes to be ready..."
    
    while [ $elapsed -lt $max_wait ]; do
        READY_NODES=$(kubectl get nodes --no-headers 2>/dev/null | grep -c " Ready " || echo "0")
        TOTAL_NODES=$(kubectl get nodes --no-headers 2>/dev/null | wc -l || echo "0")
        
        if [ "$TOTAL_NODES" -gt 0 ] && [ "$READY_NODES" -eq "$TOTAL_NODES" ]; then
            print_success "✅ All $READY_NODES node(s) are ready!"
            return 0
        fi
        
        if [ "$TOTAL_NODES" -gt 0 ]; then
            print_info "Nodes ready: $READY_NODES/$TOTAL_NODES"
        fi
        
        sleep $interval
        elapsed=$((elapsed + interval))
    done
    
    print_warning "⚠️ Timeout waiting for nodes"
    return 1
}

# Stage 1: Initialize Terraform
print_info "📦 Stage 1: Initializing Terraform..."

# Check for backend configuration
BACKEND_CONFIG_FILE="$ENV_DIR/backend.hcl"
TF_BACKEND_REGION="${AWS_REGION:-us-east-1}"

if [ ! -f "$BACKEND_CONFIG_FILE" ]; then
    print_warning "No backend.hcl file found. Checking for backend configuration via environment variables..."
    
    # Check if backend config is provided via environment variables
    if [ -z "$TF_STATE_BUCKET" ] || [ -z "$TF_LOCK_TABLE" ]; then
        print_warning "Backend configuration not found. Using local backend for development..."
        print_info "To use S3 backend, set TF_STATE_BUCKET and TF_LOCK_TABLE environment variables"
        print_info "or create a backend.hcl file in $ENV_DIR"
        
        # Temporarily modify backend.tf to use local backend
        BACKEND_TF_BACKUP="$ENV_DIR/backend.tf.backup"
        if [ ! -f "$BACKEND_TF_BACKUP" ]; then
            cp "$ENV_DIR/backend.tf" "$BACKEND_TF_BACKUP"
            cat > "$ENV_DIR/backend.tf" << 'EOF'
terraform {
  backend "local" {
    path = "terraform.tfstate"
  }
}
EOF
        fi
        terraform init -upgrade -reconfigure
    else
        print_info "Using S3 backend with environment variables..."
        terraform init -upgrade \
            -backend-config="bucket=$TF_STATE_BUCKET" \
            -backend-config="key=microservices-trading-bot/dev/terraform.tfstate" \
            -backend-config="region=$TF_BACKEND_REGION" \
            -backend-config="dynamodb_table=$TF_LOCK_TABLE" \
            -backend-config="encrypt=true"
    fi
else
    print_info "Using backend configuration from backend.hcl"
    terraform init -upgrade -backend-config="$BACKEND_CONFIG_FILE"
fi

# Stage 2: Deploy core infrastructure (VPC, EKS, ECR)
print_info "📦 Stage 2: Deploying core infrastructure (VPC, EKS, ECR)..."
print_info "This may take 10-15 minutes..."
timeout 1800 terraform apply -target=module.vpc -target=module.eks -target=module.ecr -auto-approve || {
    print_error "Terraform apply timed out or failed after 30 minutes"
    print_warning "This may indicate a resource dependency issue. Check Terraform state and AWS console."
    exit 1
}

# Get outputs from Terraform
print_info "📋 Getting infrastructure details..."
CLUSTER_NAME=$(terraform output -raw cluster_name 2>/dev/null || echo "")
AWS_REGION=$(terraform output -raw aws_region 2>/dev/null || terraform output -raw region 2>/dev/null || echo "")

if [ -z "$AWS_REGION" ]; then
    # Try to get region from variables or provider
    AWS_REGION=$(terraform show -json 2>/dev/null | grep -o '"aws_region"[^}]*' | grep -o '"[^"]*"' | head -1 | tr -d '"' || echo "us-east-1")
fi
if [ -z "$AWS_REGION" ]; then
    AWS_REGION="us-east-1"
    print_warning "Could not get region from outputs, using default: $AWS_REGION"
fi

if [ -z "$CLUSTER_NAME" ]; then
    print_error "Failed to get cluster name from Terraform outputs"
    exit 1
fi

print_success "Cluster Name: $CLUSTER_NAME"
print_success "AWS Region: $AWS_REGION"

# Wait for cluster to be ready
wait_for_cluster "$CLUSTER_NAME" "$AWS_REGION"

# Update kubeconfig
print_info "🔧 Updating kubeconfig..."
aws eks update-kubeconfig --region "$AWS_REGION" --name "$CLUSTER_NAME"

# Verify cluster is accessible
if command -v kubectl &> /dev/null; then
    print_info "✅ Verifying cluster access..."
    if kubectl get nodes 2>/dev/null; then
        print_success "Cluster is accessible"
        
        # Wait for nodes to be ready
        wait_for_nodes
        
        print_info "Cluster details:"
        kubectl cluster-info
    else
        print_warning "Could not access cluster yet, continuing anyway..."
    fi
fi

# Stage 3: Deploy remaining infrastructure (MSK, Redis, IAM, GitHub OIDC, etc.)
print_info "📦 Stage 3: Deploying remaining infrastructure (MSK, Redis, IAM, GitHub OIDC, Helm addons)..."
print_info "This may take 10-20 minutes..."
timeout 1800 terraform apply -auto-approve || {
    print_error "Terraform apply timed out or failed after 30 minutes"
    print_warning "Some resources may still be deploying. Check Terraform state and AWS console."
    exit 1
}

# Wait for deployments to stabilize
print_info "⏳ Waiting for deployments to stabilize (60 seconds)..."
sleep 60

# Stage 4: Verify Helm releases
if command -v kubectl &> /dev/null && command -v helm &> /dev/null; then
    print_info "📦 Stage 4: Verifying Helm releases..."
    
    print_info "Checking Helm releases in kube-system namespace..."
    if kubectl get namespace kube-system &>/dev/null; then
        helm list -n kube-system || true
        
        print_info "Checking for metrics-server..."
        if kubectl get deployment metrics-server -n kube-system &>/dev/null; then
            print_success "✅ metrics-server is deployed"
            timeout 180 kubectl rollout status deployment/metrics-server -n kube-system --timeout=2m 2>/dev/null || print_warning "metrics-server rollout status check timed out or failed"
        fi
        
        print_info "Checking for aws-load-balancer-controller..."
        if kubectl get deployment aws-load-balancer-controller -n kube-system &>/dev/null; then
            print_success "✅ aws-load-balancer-controller is deployed"
            timeout 180 kubectl rollout status deployment/aws-load-balancer-controller -n kube-system --timeout=2m 2>/dev/null || print_warning "aws-load-balancer-controller rollout status check timed out or failed"
        fi
        
        print_info "Checking for external-dns..."
        if kubectl get deployment external-dns -n kube-system &>/dev/null; then
            print_success "✅ external-dns is deployed"
            timeout 180 kubectl rollout status deployment/external-dns -n kube-system --timeout=2m 2>/dev/null || print_warning "external-dns rollout status check timed out or failed"
        fi
    fi
    
    print_info "Checking Helm releases in cert-manager namespace..."
    if kubectl get namespace cert-manager &>/dev/null; then
        helm list -n cert-manager || true
    fi
    
    print_info "Checking Helm releases in external-secrets namespace..."
    if kubectl get namespace external-secrets &>/dev/null; then
        helm list -n external-secrets || true
    fi
    
    print_info "Checking Helm releases in monitoring namespace..."
    if kubectl get namespace monitoring &>/dev/null; then
        helm list -n monitoring || true
        if kubectl get deployment prometheus-operator -n monitoring &>/dev/null 2>&1; then
            print_success "✅ Prometheus stack is deployed"
        fi
    fi
else
    print_warning "kubectl or helm not available, skipping Helm verification"
fi

# Stage 5: Final verification
print_info "📦 Stage 5: Final verification..."

if command -v kubectl &> /dev/null; then
    print_info "Cluster nodes:"
    kubectl get nodes || true
    
    print_info "All namespaces:"
    kubectl get namespaces || true
    
    print_info "System pods:"
    kubectl get pods -n kube-system || true
else
    print_warning "kubectl not available, skipping final verification"
fi

# Get final outputs
print_info "📋 Final infrastructure outputs:"
terraform output || true

# Get CI role ARN if available
CI_ROLE_ARN=$(terraform output -raw ci_role_arn 2>/dev/null || echo "")
if [ -n "$CI_ROLE_ARN" ]; then
    print_success "✅ CI/CD GitHub OIDC Role ARN: $CI_ROLE_ARN"
fi

print_success "✅ Deployment complete!"

# Provide helpful commands
echo ""
print_info "📝 Useful commands:"
echo "  - Check cluster status: aws eks describe-cluster --name $CLUSTER_NAME --region $AWS_REGION"
echo "  - Update kubeconfig: aws eks update-kubeconfig --region $AWS_REGION --name $CLUSTER_NAME"
echo "  - Check nodes: kubectl get nodes"
echo "  - Check all pods: kubectl get pods --all-namespaces"
echo "  - Check Helm releases: helm list --all-namespaces"
echo "  - View Prometheus: kubectl port-forward -n monitoring svc/kube-prometheus-stack-prometheus 9090:9090"
echo "  - View Grafana: kubectl port-forward -n monitoring svc/kube-prometheus-stack-grafana 3000:80"
echo "    (If port-forward fails due to CloudFront subpath, use: add Grafana ALB IP to /etc/hosts as grafana.local, then open http://grafana.local)"
echo "    Get current Grafana ALB IP: kubectl get ingress -n monitoring kube-prometheus-stack-grafana -o jsonpath='{.status.loadBalancer.ingress[0].hostname}' | xargs -I{} sh -c 'getent ahosts {} 2>/dev/null | head -1'"
echo ""
print_info "📝 Terraform commands:"
echo "  - View outputs: terraform output"
echo "  - Destroy infrastructure: terraform destroy"
echo "  - Plan changes: terraform plan"
