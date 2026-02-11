#!/bin/bash
# Script to delete a stuck EKS addon
# Usage: ./delete-stuck-addon.sh <addon-name> [cluster-name] [region]

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

ADDON_NAME=${1:-"aws-ebs-csi-driver"}
ENVIRONMENT=${2:-"development"}
AWS_REGION="${AWS_REGION:-us-east-1}"

# Get cluster name from Terraform or use default
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TERRAFORM_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
TERRAFORM_DIR="$TERRAFORM_ROOT/envs/${ENVIRONMENT}"

get_cluster_name() {
    local cluster_name=""
    
    if [ -d "$TERRAFORM_DIR" ]; then
        cd "$TERRAFORM_DIR"
        terraform init >/dev/null 2>&1 || true
        cluster_name=$(terraform output -raw cluster_name 2>/dev/null || echo "")
        cd - >/dev/null 2>&1
    fi
    
    # Use fallback if empty or if Terraform printed a warning (e.g. no outputs in state)
    if [ -z "$cluster_name" ] || [ "$cluster_name" = "" ] || [[ "$cluster_name" == *"Warning"* ]] || [[ "$cluster_name" == *"output"* ]]; then
        cluster_name="mtb-${ENVIRONMENT}"
        print_warning "Could not retrieve cluster name from Terraform, using fallback: $cluster_name" >&2
    fi
    
    echo "$cluster_name"
}

CLUSTER_NAME=$(get_cluster_name)

print_info "Cluster: $CLUSTER_NAME"
print_info "Region: $AWS_REGION"
print_info "Addon: $ADDON_NAME"

# Check if addon exists
print_info "Checking addon status..."
ADDON_STATUS=$(aws eks describe-addon \
    --cluster-name "$CLUSTER_NAME" \
    --addon-name "$ADDON_NAME" \
    --region "$AWS_REGION" \
    --query 'addon.status' \
    --output text 2>/dev/null || echo "NOT_FOUND")

if [ "$ADDON_STATUS" = "NOT_FOUND" ]; then
    print_warning "Addon $ADDON_NAME not found in cluster $CLUSTER_NAME"
    exit 0
fi

print_info "Current addon status: $ADDON_STATUS"

if [ "$ADDON_STATUS" = "ACTIVE" ]; then
    print_warning "Addon is ACTIVE. Are you sure you want to delete it?"
    read -p "Type 'yes' to continue: " -r response
    if [[ ! "$response" == "yes" ]]; then
        print_info "Deletion cancelled"
        exit 0
    fi
elif [ "$ADDON_STATUS" = "CREATING" ] || [ "$ADDON_STATUS" = "DEGRADED" ] || [ "$ADDON_STATUS" = "UPDATE_FAILED" ]; then
    print_warning "Addon is in $ADDON_STATUS state. This may take a while to delete."
fi

# Delete the addon
print_info "Deleting addon $ADDON_NAME from cluster $CLUSTER_NAME..."
aws eks delete-addon \
    --cluster-name "$CLUSTER_NAME" \
    --addon-name "$ADDON_NAME" \
    --region "$AWS_REGION" || {
    print_error "Failed to delete addon. You may need to delete it manually from AWS Console."
    print_info "AWS Console: https://console.aws.amazon.com/eks/home?region=$AWS_REGION#/clusters/$CLUSTER_NAME/addons"
    exit 1
}

print_success "Addon deletion initiated"

# Wait for deletion to complete
print_info "Waiting for addon deletion to complete (this may take a few minutes)..."
MAX_WAIT=600  # 10 minutes
ELAPSED=0
INTERVAL=15

while [ $ELAPSED -lt $MAX_WAIT ]; do
    CURRENT_STATUS=$(aws eks describe-addon \
        --cluster-name "$CLUSTER_NAME" \
        --addon-name "$ADDON_NAME" \
        --region "$AWS_REGION" \
        --query 'addon.status' \
        --output text 2>/dev/null || echo "DELETED")
    
    if [ "$CURRENT_STATUS" = "DELETED" ] || [ "$CURRENT_STATUS" = "NOT_FOUND" ]; then
        print_success "Addon has been deleted successfully"
        exit 0
    fi
    
    print_info "Addon status: $CURRENT_STATUS (waiting for deletion...)"
    sleep $INTERVAL
    ELAPSED=$((ELAPSED + INTERVAL))
    
    if [ $((ELAPSED % 60)) -eq 0 ]; then
        print_info "Still waiting... ($(($ELAPSED / 60)) minutes elapsed)"
    fi
done

print_warning "Timeout waiting for addon deletion. Check AWS Console for status."
print_info "AWS Console: https://console.aws.amazon.com/eks/home?region=$AWS_REGION#/clusters/$CLUSTER_NAME/addons"
exit 1
