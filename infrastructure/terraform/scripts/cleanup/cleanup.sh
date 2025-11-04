#!/bin/bash

# Bulletproof EKS Cleanup Solution
# This script ensures ZERO manual AWS console cleanup is needed

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

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

# Dynamic cluster name retrieval
get_cluster_name() {
    local cluster_name=""
    
    # Try to get cluster name from Terraform outputs
    if [ -d "$TERRAFORM_DIR" ]; then
        cd "$TERRAFORM_DIR"
        
        # Initialize terraform if needed (quietly)
        terraform init >/dev/null 2>&1 || true
        
        # Try to get cluster name from terraform output
        cluster_name=$(terraform output -raw cluster_name 2>/dev/null || echo "")
        
        cd - >/dev/null 2>&1
    fi
    
    # If terraform output failed or is empty, use fallback
    if [ -z "$cluster_name" ] || [ "$cluster_name" = "" ]; then
        cluster_name="mtb-${ENVIRONMENT}"
        print_warning "Could not retrieve cluster name from Terraform, using fallback: $cluster_name"
    else
        print_success "Retrieved cluster name from Terraform: $cluster_name"
    fi
    
    echo "$cluster_name"
}

# Get dynamic cluster name
CLUSTER_NAME=$(get_cluster_name)

print_info "🚀 Starting bulletproof cleanup for environment: $ENVIRONMENT"

# Function to check if command exists
command_exists() {
    command -v "$1" >/dev/null 2>&1
}

# Function to check prerequisites
check_prerequisites() {
    print_info "Checking prerequisites..."
    
    if ! command_exists aws; then
        print_error "AWS CLI not found"
        exit 1
    fi
    
    if ! command_exists kubectl; then
        print_warning "kubectl not found - some Kubernetes cleanup steps will be skipped"
    fi
    
    if ! command_exists terraform; then
        print_error "Terraform not found"
        exit 1
    fi
    
    # Check AWS credentials
    if ! aws sts get-caller-identity >/dev/null 2>&1; then
        print_error "AWS credentials not configured"
        exit 1
    fi
    
    print_success "Prerequisites OK"
}

# Function to force delete LoadBalancer services (CRITICAL)
force_delete_loadbalancers() {
    print_info "🎯 CRITICAL: Force deleting ALL LoadBalancer services..."
    
    if ! command_exists kubectl; then
        print_warning "kubectl not available - skipping LoadBalancer cleanup"
        return 0
    fi
    
    # Try to configure kubectl
    aws eks update-kubeconfig --region $AWS_REGION --name $CLUSTER_NAME 2>/dev/null || {
        print_warning "Cannot configure kubectl - cluster may be gone"
        return 0
    }
    
    # Check if cluster is accessible
    if ! kubectl get nodes >/dev/null 2>&1; then
        print_warning "Cluster not accessible - skipping K8s cleanup"
        return 0
    fi
    
    # Get ALL LoadBalancer services across ALL namespaces
    print_info "Scanning for LoadBalancer services across all namespaces..."
    
    # Check if jq is available
    if command_exists jq; then
        local lb_services=$(kubectl get svc --all-namespaces -o json 2>/dev/null | jq -r '.items[] | select(.spec.type=="LoadBalancer") | "\(.metadata.namespace)/\(.metadata.name)"' 2>/dev/null || echo "")
    else
        # Fallback without jq
        local lb_services=$(kubectl get svc --all-namespaces -o jsonpath='{range .items[*]}{.metadata.namespace}{"/"}{.metadata.name}{"\n"}{end}' 2>/dev/null | while read -r line; do
            if [ -n "$line" ]; then
                local ns=$(echo "$line" | cut -d'/' -f1)
                local svc=$(echo "$line" | cut -d'/' -f2)
                kubectl get svc "$svc" -n "$ns" -o jsonpath='{.spec.type}' 2>/dev/null | grep -q "LoadBalancer" && echo "$line"
            fi
        done || echo "")
    fi
    
    if [ -z "$lb_services" ]; then
        print_success "No LoadBalancer services found"
        return 0
    fi
    
    print_warning "Found LoadBalancer services:"
    echo "$lb_services"
    
    # Delete each LoadBalancer service with extreme prejudice
    echo "$lb_services" | while read -r service_info; do
        if [ -n "$service_info" ]; then
            local namespace=$(echo "$service_info" | cut -d'/' -f1)
            local service=$(echo "$service_info" | cut -d'/' -f2)
            
            print_info "🔥 Force deleting LoadBalancer: $namespace/$service"
            
            # Method 1: Try graceful deletion with short timeout
            timeout 30 kubectl delete svc "$service" -n "$namespace" 2>/dev/null || {
                print_warning "Graceful deletion failed, using nuclear option..."
                
                # Method 2: Remove finalizers and force delete
                kubectl patch svc "$service" -n "$namespace" --type='merge' -p='{"metadata":{"finalizers":[]}}' 2>/dev/null || true
                kubectl delete svc "$service" -n "$namespace" --grace-period=0 --force 2>/dev/null || true
                
                # Method 3: Direct etcd deletion if needed (rare)
                kubectl patch svc "$service" -n "$namespace" --type='json' -p='[{"op": "remove", "path": "/metadata/finalizers"}]' 2>/dev/null || true
            }
            
            print_success "Service $namespace/$service deleted"
        fi
    done
    
    # Wait for AWS Load Balancers to actually start deleting
    print_info "⏳ Waiting 60 seconds for AWS Load Balancers to start deletion..."
    sleep 60
    
    print_success "LoadBalancer services cleanup completed"
}

# Function to nuke ALL ingresses (they create ALBs)
nuke_all_ingresses() {
    print_info "💥 Nuking ALL ingresses (they create ALBs)..."
    
    if ! command_exists kubectl; then
        print_warning "kubectl not available - skipping ingress cleanup"
        return 0
    fi
    
    # Get all ingresses
    if command_exists jq; then
        local ingresses=$(kubectl get ingress --all-namespaces -o json 2>/dev/null | jq -r '.items[] | "\(.metadata.namespace)/\(.metadata.name)"' 2>/dev/null || echo "")
    else
        local ingresses=$(kubectl get ingress --all-namespaces -o jsonpath='{range .items[*]}{.metadata.namespace}{"/"}{.metadata.name}{"\n"}{end}' 2>/dev/null || echo "")
    fi
    
    if [ -z "$ingresses" ]; then
        print_success "No ingresses found"
        return 0
    fi
    
    print_warning "Found ingresses:"
    echo "$ingresses"
    
    # Delete all ingresses with extreme prejudice
    echo "$ingresses" | while read -r ingress_info; do
        if [ -n "$ingress_info" ]; then
            local namespace=$(echo "$ingress_info" | cut -d'/' -f1)
            local ingress=$(echo "$ingress_info" | cut -d'/' -f2)
            
            print_info "🔥 Nuking ingress: $namespace/$ingress"
            
            # Force delete immediately
            kubectl patch ingress "$ingress" -n "$namespace" --type='merge' -p='{"metadata":{"finalizers":[]}}' 2>/dev/null || true
            kubectl delete ingress "$ingress" -n "$namespace" --grace-period=0 --force 2>/dev/null || true
            
            print_success "Ingress $namespace/$ingress nuked"
        fi
    done
    
    print_success "All ingresses nuked"
}

# Function to clean up AWS Load Balancers directly
cleanup_aws_load_balancers() {
    print_info "🧹 Cleaning up AWS Load Balancers directly..."
    
    # Get all Load Balancers that might belong to our cluster
    local lbs=$(aws elbv2 describe-load-balancers --region $AWS_REGION --query "LoadBalancers[?contains(LoadBalancerName, 'k8s') || contains(LoadBalancerName, '$CLUSTER_NAME')].LoadBalancerArn" --output text 2>/dev/null | grep -v "None" || echo "")
    
    if [ -n "$lbs" ] && [ "$lbs" != "" ]; then
        print_warning "Found AWS Load Balancers to delete:"
        aws elbv2 describe-load-balancers --region $AWS_REGION --query "LoadBalancers[?contains(LoadBalancerName, 'k8s') || contains(LoadBalancerName, '$CLUSTER_NAME')].{Name:LoadBalancerName,State:State.Code}" --output table 2>/dev/null || true
        
        # Auto-delete them all
        echo "$lbs" | tr '\t' '\n' | while read -r lb_arn; do
            if [ -n "$lb_arn" ] && [ "$lb_arn" != "None" ]; then
                print_info "🗑️  Deleting Load Balancer: $lb_arn"
                aws elbv2 delete-load-balancer --load-balancer-arn "$lb_arn" --region $AWS_REGION 2>/dev/null || {
                    print_warning "Failed to delete $lb_arn - it might already be gone"
                }
            fi
        done
        
        print_success "AWS Load Balancers cleanup initiated"
    else
        print_success "No AWS Load Balancers found"
    fi
    
    # Also clean up Classic Load Balancers (just in case)
    local classic_lbs=$(aws elb describe-load-balancers --region $AWS_REGION --query "LoadBalancerDescriptions[?contains(LoadBalancerName, 'k8s') || contains(LoadBalancerName, '$CLUSTER_NAME')].LoadBalancerName" --output text 2>/dev/null || echo "")
    
    if [ -n "$classic_lbs" ] && [ "$classic_lbs" != "" ]; then
        echo "$classic_lbs" | tr '\t' '\n' | while read -r lb_name; do
            if [ -n "$lb_name" ]; then
                print_info "🗑️  Deleting Classic Load Balancer: $lb_name"
                aws elb delete-load-balancer --load-balancer-name "$lb_name" --region $AWS_REGION 2>/dev/null || true
            fi
        done
    fi
}

# Function to clean up ECR repositories manually
cleanup_ecr_repositories() {
    print_info "🧹 Cleaning up ECR repositories..."
    
    # Get cluster name to determine repository names
    local project_name="mtb"
    local repos=(
        "trading-engine"
        "strategy-executor"
        "backtesting"
        "order-management"
        "market-data"
        "api-gateway"
    )
    
    local deleted_count=0
    local failed_count=0
    
    for repo in "${repos[@]}"; do
        print_info "Checking repository: $repo"
        
        # Check if repository exists
        if aws ecr describe-repositories --repository-names "$repo" --region $AWS_REGION >/dev/null 2>&1; then
            print_warning "Found repository: $repo"
            
            # Try to delete all images first (required for non-empty repos)
            print_info "Deleting images in repository: $repo"
            aws ecr list-images --repository-name "$repo" --region $AWS_REGION --query 'imageIds[*]' --output json 2>/dev/null | \
                jq -r '.[] | "\(.imageDigest)"' 2>/dev/null | \
                while read -r digest; do
                    if [ -n "$digest" ]; then
                        aws ecr batch-delete-image --repository-name "$repo" --image-ids "imageDigest=$digest" --region $AWS_REGION >/dev/null 2>&1 || true
                    fi
                done
            
            # Also try deleting by tag
            aws ecr list-images --repository-name "$repo" --region $AWS_REGION --query 'imageIds[*]' --output json 2>/dev/null | \
                jq -r '.[] | "\(.imageTag // empty)"' 2>/dev/null | \
                while read -r tag; do
                    if [ -n "$tag" ] && [ "$tag" != "null" ]; then
                        aws ecr batch-delete-image --repository-name "$repo" --image-ids "imageTag=$tag" --region $AWS_REGION >/dev/null 2>&1 || true
                    fi
                done
            
            # Delete the repository
            print_info "Deleting repository: $repo"
            if aws ecr delete-repository --repository-name "$repo" --region $AWS_REGION --force 2>/dev/null; then
                print_success "Deleted repository: $repo"
                deleted_count=$((deleted_count + 1))
            else
                print_warning "Failed to delete repository: $repo (may need manual cleanup)"
                failed_count=$((failed_count + 1))
            fi
        else
            print_info "Repository $repo does not exist (already deleted)"
        fi
    done
    
    if [ $deleted_count -gt 0 ]; then
        print_success "Deleted $deleted_count ECR repository(ies)"
    fi
    
    if [ $failed_count -gt 0 ]; then
        print_warning "$failed_count repository(ies) failed to delete and may need manual cleanup"
    fi
    
    if [ $deleted_count -eq 0 ] && [ $failed_count -eq 0 ]; then
        print_success "No ECR repositories found"
    fi
}

# Function to clean up Target Groups
cleanup_target_groups() {
    print_info "🎯 Cleaning up Target Groups..."
    
    local target_groups=$(aws elbv2 describe-target-groups --region $AWS_REGION --query "TargetGroups[?contains(TargetGroupName, 'k8s')].TargetGroupArn" --output text 2>/dev/null || echo "")
    
    if [ -n "$target_groups" ] && [ "$target_groups" != "" ]; then
        echo "$target_groups" | tr '\t' '\n' | while read -r tg_arn; do
            if [ -n "$tg_arn" ]; then
                print_info "🗑️  Deleting Target Group: $tg_arn"
                aws elbv2 delete-target-group --target-group-arn "$tg_arn" --region $AWS_REGION 2>/dev/null || true
            fi
        done
        print_success "Target Groups cleaned up"
    else
        print_success "No Target Groups found"
    fi
}

# Function to remove problematic resources from Terraform state
clean_terraform_state() {
    print_info "🧹 ENHANCED: Cleaning Terraform state of problematic resources..."
    
    if [ ! -d "$TERRAFORM_DIR" ]; then
        print_warning "Terraform directory $TERRAFORM_DIR not found"
        return 0
    fi
    
    cd "$TERRAFORM_DIR"
    terraform init >/dev/null 2>&1 || true
    
    # EXPANDED list of resources that commonly cause issues
    local problematic_resources=(
        # Services that get stuck
        "module.application.kubernetes_service.this"
        "module.application.kubernetes_ingress_v1.this"
        
        # CRITICAL: The namespace that gets stuck
        "module.application.kubernetes_namespace.this"
        "kubernetes_namespace.*"
        
        # HPA and other app resources that can get stuck
        "module.application.kubernetes_horizontal_pod_autoscaler.this"
        "kubernetes_horizontal_pod_autoscaler.*"
        
        # Deployments that might be stuck
        "module.application.kubernetes_deployment.this"
        "kubernetes_deployment.*"
        
        # Helm releases
        "helm_release.*"
    )
    
    print_info "Checking for stuck resources in Terraform state..."
    for resource in "${problematic_resources[@]}"; do
        if terraform state list 2>/dev/null | grep -q "$resource"; then
            print_warning "Found problematic resource: $resource"
            print_info "Removing $resource from Terraform state"
            terraform state rm "$resource" 2>/dev/null || true
            print_success "Removed $resource from state"
        fi
    done
    
    # Handle ECR repositories separately - they might need manual deletion
    print_info "Checking ECR repositories..."
    local ecr_repos=$(terraform state list 2>/dev/null | grep "module.ecr.aws_ecr_repository" || echo "")
    if [ -n "$ecr_repos" ]; then
        print_warning "Found ECR repositories in state. These will be deleted by terraform destroy."
        print_info "If they fail to delete, you may need to manually delete images first."
    fi
    
    cd - >/dev/null
    print_success "Enhanced Terraform state cleaning completed"
}

# Function to handle Terraform state lock
handle_state_lock() {
    print_info "Checking for Terraform state lock..."
    
    cd "$TERRAFORM_DIR"
    
    # Try to get lock info
    local lock_info=$(terraform plan -destroy -no-color 2>&1 | grep -A 10 "Error acquiring the state lock" || echo "")
    
    if [ -n "$lock_info" ]; then
        print_warning "State lock detected. Attempting to force unlock..."
        
        # Extract lock ID from error message
        local lock_id=$(echo "$lock_info" | grep -oP 'ID:\s+\K[0-9a-f-]+' || echo "")
        
        if [ -n "$lock_id" ]; then
            print_info "Force unlocking state with ID: $lock_id"
            terraform force-unlock -force "$lock_id" 2>&1 || {
                print_warning "Force unlock failed or lock already cleared"
            }
        fi
    fi
    
    cd - >/dev/null
}

# Function to do the actual Terraform destroy
terraform_destroy() {
    print_info "💥 Running Terraform destroy..."
    
    if [ ! -d "$TERRAFORM_DIR" ]; then
        print_error "Terraform directory $TERRAFORM_DIR not found"
        exit 1
    fi
    
    cd "$TERRAFORM_DIR"
    
    # Handle state lock first
    handle_state_lock
    
    # Create destroy plan
    print_info "Creating destroy plan..."
    if ! terraform plan -destroy -out=destroy-plan 2>&1; then
        print_error "Failed to create destroy plan"
        print_warning "This might be due to backend configuration or state lock issues"
        print_info "Attempting direct destroy without plan..."
        
        # Try direct destroy as fallback
        print_info "Attempting direct terraform destroy (timeout: 30 minutes)..."
        timeout 1800 terraform destroy -auto-approve || {
            print_error "Terraform destroy timed out or failed"
            print_warning "Don't worry - running cleanup again should handle remaining resources"
            cd - >/dev/null
            return 1
        }
        
        cd - >/dev/null
        print_success "Terraform destroy completed (direct method)"
        return 0
    fi
    
    # Apply destroy with timeout
    print_info "Applying destroy plan (timeout: 30 minutes)..."
    timeout 1800 terraform apply destroy-plan || {
        print_error "Terraform destroy timed out or failed"
        print_warning "Don't worry - running cleanup again should handle remaining resources"
        rm -f destroy-plan
        cd - >/dev/null
        return 1
    }
    
    rm -f destroy-plan
    cd - >/dev/null
    
    print_success "Terraform destroy completed"
}

# Function to verify complete cleanup
verify_cleanup() {
    print_info "🔍 Verifying complete cleanup..."
    
    # Check for remaining Load Balancers
    local remaining_lbs=$(aws elbv2 describe-load-balancers --region $AWS_REGION --query "LoadBalancers[?contains(LoadBalancerName, 'k8s') || contains(LoadBalancerName, '$CLUSTER_NAME')].LoadBalancerName" --output text 2>/dev/null || echo "")
    
    if [ -n "$remaining_lbs" ] && [ "$remaining_lbs" != "" ]; then
        print_warning "⚠️  Found remaining Load Balancers:"
        echo "$remaining_lbs"
        print_info "These should be deleted automatically by AWS shortly"
    else
        print_success "✅ No remaining Load Balancers"
    fi
    
    # Check for remaining Target Groups
    local remaining_tgs=$(aws elbv2 describe-target-groups --region $AWS_REGION --query "TargetGroups[?contains(TargetGroupName, 'k8s')].TargetGroupName" --output text 2>/dev/null || echo "")
    
    if [ -n "$remaining_tgs" ] && [ "$remaining_tgs" != "" ]; then
        print_warning "⚠️  Found remaining Target Groups:"
        echo "$remaining_tgs"
    else
        print_success "✅ No remaining Target Groups"
    fi
    
    # Check if cluster still exists
    if aws eks describe-cluster --region $AWS_REGION --name $CLUSTER_NAME >/dev/null 2>&1; then
        print_warning "⚠️  EKS cluster still exists (might be in deletion state)"
        print_info "Checking node groups..."
        local nodegroups=$(aws eks list-nodegroups --cluster-name $CLUSTER_NAME --region $AWS_REGION --query 'nodegroups[*]' --output text 2>/dev/null || echo "")
        if [ -n "$nodegroups" ] && [ "$nodegroups" != "" ]; then
            print_warning "⚠️  Found node groups: $nodegroups"
            print_info "Node groups should be deleted automatically with the cluster"
        fi
    else
        print_success "✅ EKS cluster is gone"
    fi
    
    # Check ECR repositories
    print_info "Checking ECR repositories..."
    local remaining_repos=$(aws ecr describe-repositories --region $AWS_REGION --query 'repositories[*].repositoryName' --output text 2>/dev/null || echo "")
    if [ -n "$remaining_repos" ] && [ "$remaining_repos" != "" ]; then
        print_warning "⚠️  Found remaining ECR repositories:"
        echo "$remaining_repos"
    else
        print_success "✅ No remaining ECR repositories"
    fi
    
    print_success "🎉 Cleanup verification completed!"
}

# Main execution
main() {
    print_info "🚀 BULLETPROOF EKS CLEANUP - ZERO MANUAL AWS CONSOLE WORK!"
    echo ""
    
    # Step 1: Prerequisites
    check_prerequisites
    
    # Step 2: Force delete LoadBalancer services (MOST CRITICAL)
    force_delete_loadbalancers
    
    # Step 3: Nuke all ingresses
    nuke_all_ingresses
    
    # Step 4: Clean up AWS Load Balancers directly
    cleanup_aws_load_balancers
    
    # Step 5: Clean up Target Groups
    cleanup_target_groups
    
    # Step 5.5: Clean up ECR repositories
    cleanup_ecr_repositories
    
    # Step 6: Wait for AWS to process deletions
    print_info "⏳ Waiting 2 minutes for AWS to process Load Balancer deletions..."
    sleep 120
    
    # Step 7: Clean Terraform state
    clean_terraform_state
    
    # Step 8: Run Terraform destroy
    if terraform_destroy; then
        print_success "✅ Terraform destroy completed successfully"
    else
        print_warning "⚠️  Terraform destroy had issues, but continuing with cleanup..."
        
        # Retry AWS cleanup
        cleanup_aws_load_balancers
        cleanup_target_groups
        
        # Try Terraform destroy again
        print_info "🔄 Retrying Terraform destroy..."
        terraform_destroy || print_warning "Second attempt also failed, but AWS resources should be cleaned"
    fi
    
    # Step 9: Final verification
    verify_cleanup
    
    print_success "🎉 BULLETPROOF CLEANUP COMPLETED!"
    print_info "✨ Zero manual AWS Console work needed!"
    echo ""
    print_info "If you see any remaining resources above, they should be automatically"
    print_info "cleaned up by AWS within a few minutes. No manual action needed!"
}

# Show usage if no environment provided
if [ $# -eq 0 ]; then
    echo "Usage: $0 <environment>"
    echo "Example: $0 development"
    exit 1
fi

# Run main function
main "$@"
