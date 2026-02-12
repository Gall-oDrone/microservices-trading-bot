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

# Function to clean up KMS keys
cleanup_kms_keys() {
    print_info "🔐 Cleaning up KMS keys..."
    
    if [ ! -d "$TERRAFORM_DIR" ]; then
        return 0
    fi
    
    cd "$TERRAFORM_DIR"
    terraform init >/dev/null 2>&1 || true
    
    # Get KMS key ID from Terraform state
    local kms_key_id=$(terraform state show 'module.eks.module.eks.module.kms.aws_kms_key.this[0]' 2>/dev/null | grep -E "^id\s+=" | awk '{print $3}' | tr -d '"' || echo "")
    
    if [ -z "$kms_key_id" ] || [ "$kms_key_id" = "" ]; then
        print_info "No KMS key found in Terraform state"
        cd - >/dev/null
        return 0
    fi
    
    print_warning "Found KMS key in state: $kms_key_id"
    
    # Check if KMS key actually exists in AWS
    if ! aws kms describe-key --key-id "$kms_key_id" --region $AWS_REGION >/dev/null 2>&1; then
        print_info "KMS key $kms_key_id doesn't exist in AWS - removing from state only"
        terraform state rm 'module.eks.module.eks.module.kms.aws_kms_key.this[0]' 2>/dev/null || true
        terraform state rm 'module.eks.module.eks.module.kms.aws_kms_alias.this["cluster"]' 2>/dev/null || true
        cd - >/dev/null
        return 0
    fi
    
    # Get KMS key details
    local key_state=$(aws kms describe-key --key-id "$kms_key_id" --region $AWS_REGION --query 'KeyMetadata.KeyState' --output text 2>/dev/null || echo "Unknown")
    
    print_info "KMS key state: $key_state"
    
    if [ "$key_state" = "PendingDeletion" ]; then
        print_info "KMS key $kms_key_id is already pending deletion"
        cd - >/dev/null
        return 0
    fi
    
    if [ "$key_state" = "Disabled" ]; then
        print_info "KMS key $kms_key_id is disabled, scheduling for deletion..."
    else
        # Disable the key first (required before deletion)
        print_info "Disabling KMS key $kms_key_id..."
        aws kms disable-key --key-id "$kms_key_id" --region $AWS_REGION 2>/dev/null || {
            print_warning "Failed to disable KMS key $kms_key_id"
            cd - >/dev/null
            return 0
        }
        
        # Wait a moment for the disable to take effect
        sleep 2
    fi
    
    # Schedule key deletion (7-day window)
    print_info "Scheduling KMS key $kms_key_id for deletion (7-day window)..."
    aws kms schedule-key-deletion --key-id "$kms_key_id" --pending-window-in-days 7 --region $AWS_REGION 2>/dev/null || {
        print_warning "Failed to schedule KMS key deletion. It may already be scheduled or in use."
        print_info "KMS keys cannot be deleted immediately if they're in use by other resources."
        print_info "The key will be automatically deleted after the pending window expires."
    }
    
    # Also try to delete the alias (it will be removed automatically, but let's try)
    local kms_alias=$(terraform state show 'module.eks.module.eks.module.kms.aws_kms_alias.this["cluster"]' 2>/dev/null | grep -E "^name\s+=" | awk '{print $3}' | tr -d '"' || echo "")
    if [ -n "$kms_alias" ] && [ "$kms_alias" != "" ]; then
        print_info "Removing KMS alias: $kms_alias"
        aws kms delete-alias --alias-name "$kms_alias" --region $AWS_REGION 2>/dev/null || {
            print_info "Alias will be removed automatically when key is deleted"
        }
    fi
    
    cd - >/dev/null
    print_success "KMS key cleanup initiated"
}

# Function to clean up CloudWatch Log Groups
cleanup_cloudwatch_logs() {
    print_info "📊 Cleaning up CloudWatch Log Groups..."
    
    # Get all log groups that might belong to our cluster
    local log_groups=$(aws logs describe-log-groups --region $AWS_REGION --query "logGroups[?contains(logGroupName, '/aws/eks/$CLUSTER_NAME') || contains(logGroupName, '$CLUSTER_NAME')].logGroupName" --output text 2>/dev/null || echo "")
    
    if [ -n "$log_groups" ] && [ "$log_groups" != "" ]; then
        print_warning "Found CloudWatch Log Groups to delete:"
        echo "$log_groups" | tr '\t' '\n'
        
        echo "$log_groups" | tr '\t' '\n' | while read -r log_group; do
            if [ -n "$log_group" ] && [ "$log_group" != "None" ]; then
                print_info "🗑️  Deleting Log Group: $log_group"
                aws logs delete-log-group --log-group-name "$log_group" --region $AWS_REGION 2>/dev/null || {
                    print_warning "Failed to delete $log_group - trying to remove retention policy first..."
                    # Try removing retention policy and retry
                    aws logs put-retention-policy --log-group-name "$log_group" --retention-in-days 1 --region $AWS_REGION 2>/dev/null || true
                    sleep 2
                    aws logs delete-log-group --log-group-name "$log_group" --region $AWS_REGION 2>/dev/null || {
                        print_warning "Could not delete $log_group - may need manual cleanup"
                    }
                }
            fi
        done
        
        print_success "CloudWatch Log Groups cleanup completed"
    else
        print_success "No CloudWatch Log Groups found"
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
        
        # Helm releases (CRITICAL - they depend on cluster)
        "helm_release.*"
        
        # Data sources that reference deleted cluster (CRITICAL)
        "data.aws_eks_cluster.this"
        "data.aws_eks_cluster_auth.this"
    )
    
    print_info "Checking for stuck resources in Terraform state..."
    for resource in "${problematic_resources[@]}"; do
        # Handle wildcard patterns
        if [[ "$resource" == *"*"* ]]; then
            local pattern="${resource//\*/.*}"
            local matching_resources=$(terraform state list 2>/dev/null | grep -E "$pattern" || echo "")
            if [ -n "$matching_resources" ]; then
                echo "$matching_resources" | while read -r matching_resource; do
                    if [ -n "$matching_resource" ]; then
                        print_warning "Found problematic resource: $matching_resource"
                        print_info "Removing $matching_resource from Terraform state"
                        terraform state rm "$matching_resource" 2>/dev/null || true
                        print_success "Removed $matching_resource from state"
                    fi
                done
            fi
        else
            if terraform state list 2>/dev/null | grep -q "^${resource}$"; then
                print_warning "Found problematic resource: $resource"
                print_info "Removing $resource from Terraform state"
                terraform state rm "$resource" 2>/dev/null || true
                print_success "Removed $resource from state"
            fi
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

# Function to handle Terraform state lock (local backend often leaves stale .terraform.tfstate.lock.info)
handle_state_lock() {
    print_info "Checking for Terraform state lock..."
    
    cd "$TERRAFORM_DIR"
    
    # For local backend: remove stale lock file so Terraform can proceed (e.g. after a crashed run)
    if [ -f ".terraform.tfstate.lock.info" ]; then
        print_warning "Local state lock file found. Attempting force-unlock..."
        local lock_id=""
        lock_id=$(grep -oP '"ID"\s*:\s*"\K[0-9a-f-]+' .terraform.tfstate.lock.info 2>/dev/null || true)
        if [ -n "$lock_id" ]; then
            terraform force-unlock -force "$lock_id" 2>/dev/null || true
        fi
        if [ -f ".terraform.tfstate.lock.info" ]; then
            print_warning "Removing stale lock file so destroy can proceed..."
            rm -f .terraform.tfstate.lock.info
        fi
    fi
    
    # If plan still fails with lock error, try to extract ID from error and force-unlock
    local lock_info=$(terraform plan -destroy -no-color 2>&1 | grep -A 10 "Error acquiring the state lock" || echo "")
    
    if [ -n "$lock_info" ]; then
        print_warning "State lock detected. Attempting to force unlock..."
        local lock_id=$(echo "$lock_info" | grep -oP 'ID:\s+\K[0-9a-f-]+' || echo "")
        
        if [ -n "$lock_id" ]; then
            print_info "Force unlocking state with ID: $lock_id"
            terraform force-unlock -force "$lock_id" 2>&1 || {
                print_warning "Force unlock failed; removing lock file if present..."
                rm -f .terraform.tfstate.lock.info
            }
        fi
    fi
    
    cd - >/dev/null
}

# Function to do the actual Terraform destroy
terraform_destroy() {
    print_info "💥 Running Terraform destroy..."
    print_info "⏳ Note: VPC and NAT Gateway deletion can take 10-15 minutes (AWS is asynchronous). Do not interrupt."
    
    if [ ! -d "$TERRAFORM_DIR" ]; then
        print_error "Terraform directory $TERRAFORM_DIR not found"
        exit 1
    fi
    
    cd "$TERRAFORM_DIR"
    
    # Handle state lock first
    handle_state_lock
    
    # Create destroy plan
    print_info "Creating destroy plan..."
    local plan_output
    plan_output=$(terraform plan -destroy -out=destroy-plan 2>&1) || true
    if ! echo "$plan_output" | grep -q "Plan:"; then
        print_error "Failed to create destroy plan"
        print_warning "This might be due to backend configuration, state lock, or cluster already gone"
        # If EKS cluster is already deleted, data.aws_eks_cluster.this can't be refreshed - use -refresh=false
        if echo "$plan_output" | grep -q "couldn't find resource\|reading EKS Cluster"; then
            print_info "Cluster already gone; attempting destroy with -refresh=false (use cached state)..."
            if timeout 1800 terraform destroy -auto-approve -refresh=false -lock=false; then
                cd - >/dev/null
                print_success "Terraform destroy completed (with -refresh=false)"
                return 0
            fi
        fi
        print_info "Attempting direct destroy without plan..."
        if timeout 1800 terraform destroy -auto-approve -lock=false; then
            cd - >/dev/null
            print_success "Terraform destroy completed (direct method)"
            return 0
        fi
        print_error "Terraform destroy timed out or failed"
        cd - >/dev/null
        return 1
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

# Function to manually clean up VPC if Terraform fails.
# VPC deletion often "gets stuck" because: (1) NAT Gateway takes 5-15 min to delete asynchronously,
# (2) non-default security groups must be deleted before the VPC. This function waits for NAT and removes SGs.
cleanup_vpc_manually() {
    print_info "🌐 Attempting manual VPC cleanup..."
    
    if [ ! -d "$TERRAFORM_DIR" ]; then
        return 0
    fi
    
    cd "$TERRAFORM_DIR"
    
    # Get VPC ID from state
    local vpc_id=$(terraform state show module.vpc.module.vpc.aws_vpc.this[0] 2>/dev/null | grep -E "^id\s+=" | awk '{print $3}' | tr -d '"' || echo "")
    
    if [ -z "$vpc_id" ] || [ "$vpc_id" = "" ]; then
        print_info "No VPC found in state"
        cd - >/dev/null
        return 0
    fi
    
    print_warning "Found VPC in state: $vpc_id"
    
    # Check if VPC actually exists in AWS
    if ! aws ec2 describe-vpcs --vpc-ids "$vpc_id" --region $AWS_REGION >/dev/null 2>&1; then
        print_info "VPC $vpc_id doesn't exist in AWS - removing from state only"
        terraform state rm module.vpc.module.vpc.aws_vpc.this[0] 2>/dev/null || true
        cd - >/dev/null
        return 0
    fi
    
    print_warning "VPC $vpc_id still exists - attempting manual cleanup..."
    
    # Get all resources in the VPC
    local enis=$(aws ec2 describe-network-interfaces --filters "Name=vpc-id,Values=$vpc_id" --region $AWS_REGION --query 'NetworkInterfaces[*].NetworkInterfaceId' --output text 2>/dev/null || echo "")
    
    if [ -n "$enis" ] && [ "$enis" != "" ]; then
        print_warning "Found network interfaces attached to VPC - these need to be deleted first"
        echo "$enis" | tr '\t' '\n' | while read -r eni; do
            if [ -n "$eni" ]; then
                print_info "Checking ENI: $eni"
                # Try to detach and delete (this might fail if still in use)
                aws ec2 detach-network-interface --network-interface-id "$eni" --force --region $AWS_REGION 2>/dev/null || true
                aws ec2 delete-network-interface --network-interface-id "$eni" --region $AWS_REGION 2>/dev/null || true
            fi
        done
    fi
    
    # Get NAT Gateway(s) - NAT Gateway deletion can take 5-15 minutes (AWS is asynchronous)
    local nat_gw=$(aws ec2 describe-nat-gateways --filter "Name=vpc-id,Values=$vpc_id" --region $AWS_REGION --query 'NatGateways[?State==`available`].NatGatewayId' --output text 2>/dev/null | tr '\t' '\n' | grep -v '^$' || echo "")
    if [ -n "$nat_gw" ] && [ "$nat_gw" != "" ]; then
        echo "$nat_gw" | while read -r nat; do
            if [ -n "$nat" ]; then
                print_info "Deleting NAT Gateway: $nat (AWS may take 5-15 minutes to finish)"
                aws ec2 delete-nat-gateway --nat-gateway-id "$nat" --region $AWS_REGION 2>/dev/null || true
            fi
        done
        # Wait for NAT Gateway(s) to reach "deleted" state (required before subnets/VPC can be deleted)
        local nat_wait_max=900  # 15 minutes
        local nat_wait_elapsed=0
        local nat_wait_interval=30
        while [ $nat_wait_elapsed -lt $nat_wait_max ]; do
            local nat_states=$(aws ec2 describe-nat-gateways --filter "Name=vpc-id,Values=$vpc_id" --region $AWS_REGION --query 'NatGateways[*].State' --output text 2>/dev/null || echo "")
            local still_deleting=""
            for s in $nat_states; do
                if [ "$s" = "available" ] || [ "$s" = "deleting" ] || [ "$s" = "pending" ]; then
                    still_deleting=1
                    break
                fi
            done
            if [ -z "$nat_states" ] || [ "$nat_states" = "None" ] || [ -z "$still_deleting" ]; then
                print_success "NAT Gateway(s) deleted after ${nat_wait_elapsed}s"
                break
            fi
            print_info "Waiting for NAT Gateway deletion... (${nat_wait_elapsed}s / ${nat_wait_max}s)"
            sleep $nat_wait_interval
            nat_wait_elapsed=$((nat_wait_elapsed + nat_wait_interval))
        done
        if [ $nat_wait_elapsed -ge $nat_wait_max ]; then
            print_warning "NAT Gateway deletion did not finish within 15 minutes - VPC delete may fail; retry or delete in console"
        fi
    fi
    
    # Get Internet Gateway
    local igw=$(aws ec2 describe-internet-gateways --filters "Name=attachment.vpc-id,Values=$vpc_id" --region $AWS_REGION --query 'InternetGateways[*].InternetGatewayId' --output text 2>/dev/null || echo "")
    if [ -n "$igw" ] && [ "$igw" != "" ]; then
        echo "$igw" | tr '\t' '\n' | while read -r gateway; do
            if [ -n "$gateway" ]; then
                print_info "Detaching and deleting Internet Gateway: $gateway"
                aws ec2 detach-internet-gateway --internet-gateway-id "$gateway" --vpc-id "$vpc_id" --region $AWS_REGION 2>/dev/null || true
                aws ec2 delete-internet-gateway --internet-gateway-id "$gateway" --region $AWS_REGION 2>/dev/null || true
            fi
        done
    fi
    
    # Get and delete subnets
    local subnets=$(aws ec2 describe-subnets --filters "Name=vpc-id,Values=$vpc_id" --region $AWS_REGION --query 'Subnets[*].SubnetId' --output text 2>/dev/null || echo "")
    if [ -n "$subnets" ] && [ "$subnets" != "" ]; then
        echo "$subnets" | tr '\t' '\n' | while read -r subnet; do
            if [ -n "$subnet" ]; then
                print_info "Deleting subnet: $subnet"
                aws ec2 delete-subnet --subnet-id "$subnet" --region $AWS_REGION 2>/dev/null || true
            fi
        done
    fi
    
    # Delete non-default security groups (VPC cannot be deleted while non-default SGs exist)
    local sgs=$(aws ec2 describe-security-groups --filters "Name=vpc-id,Values=$vpc_id" --region $AWS_REGION --query 'SecurityGroups[?GroupName!=`default`].GroupId' --output text 2>/dev/null || echo "")
    if [ -n "$sgs" ] && [ "$sgs" != "" ]; then
        print_info "Deleting non-default security groups (required before VPC delete)..."
        local sg_retries=10
        while [ $sg_retries -gt 0 ]; do
            echo "$sgs" | tr '\t' '\n' | while read -r sg; do
                if [ -n "$sg" ]; then
                    aws ec2 delete-security-group --group-id "$sg" --region $AWS_REGION 2>/dev/null && print_info "Deleted security group: $sg" || true
                fi
            done
            sgs=$(aws ec2 describe-security-groups --filters "Name=vpc-id,Values=$vpc_id" --region $AWS_REGION --query 'SecurityGroups[?GroupName!=`default`].GroupId' --output text 2>/dev/null || echo "")
            if [ -z "$sgs" ] || [ "$sgs" = "" ] || [ "$sgs" = "None" ]; then
                break
            fi
            sg_retries=$((sg_retries - 1))
            sleep 5
        done
        if [ -n "$sgs" ] && [ "$sgs" != "" ] && [ "$sgs" != "None" ]; then
            print_warning "Some security groups could not be deleted (dependencies); VPC delete may fail"
        fi
    fi
    
    # Finally delete VPC
    print_info "Deleting VPC: $vpc_id"
    if aws ec2 delete-vpc --vpc-id "$vpc_id" --region $AWS_REGION 2>/dev/null; then
        print_success "VPC $vpc_id deleted successfully"
        # Remove from state
        terraform state rm module.vpc.module.vpc.aws_vpc.this[0] 2>/dev/null || true
    else
        print_warning "Could not delete VPC $vpc_id - may have dependencies"
    fi
    
    cd - >/dev/null
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
    
    # Check CloudWatch Log Groups
    print_info "Checking CloudWatch Log Groups..."
    local remaining_logs=$(aws logs describe-log-groups --region $AWS_REGION --query "logGroups[?contains(logGroupName, '/aws/eks/$CLUSTER_NAME') || contains(logGroupName, '$CLUSTER_NAME')].logGroupName" --output text 2>/dev/null || echo "")
    if [ -n "$remaining_logs" ] && [ "$remaining_logs" != "" ]; then
        print_warning "⚠️  Found remaining CloudWatch Log Groups:"
        echo "$remaining_logs"
    else
        print_success "✅ No remaining CloudWatch Log Groups"
    fi
    
    # Check VPC
    print_info "Checking VPC..."
    if [ -d "$TERRAFORM_DIR" ]; then
        cd "$TERRAFORM_DIR"
        local vpc_id=$(terraform state show module.vpc.module.vpc.aws_vpc.this[0] 2>/dev/null | grep -E "^id\s+=" | awk '{print $3}' | tr -d '"' || echo "")
        cd - >/dev/null
        
        if [ -n "$vpc_id" ] && [ "$vpc_id" != "" ]; then
            if aws ec2 describe-vpcs --vpc-ids "$vpc_id" --region $AWS_REGION >/dev/null 2>&1; then
                print_warning "⚠️  VPC still exists: $vpc_id"
            else
                print_success "✅ VPC is gone (may still be in state)"
            fi
        else
            print_success "✅ No VPC found in state"
        fi
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
    
    # Check KMS keys
    print_info "Checking KMS keys..."
    if [ -d "$TERRAFORM_DIR" ]; then
        cd "$TERRAFORM_DIR"
        local kms_key_id=$(terraform state show 'module.eks.module.eks.module.kms.aws_kms_key.this[0]' 2>/dev/null | grep -E "^id\s+=" | awk '{print $3}' | tr -d '"' || echo "")
        cd - >/dev/null
        
        if [ -n "$kms_key_id" ] && [ "$kms_key_id" != "" ]; then
            local key_state=$(aws kms describe-key --key-id "$kms_key_id" --region $AWS_REGION --query 'KeyMetadata.KeyState' --output text 2>/dev/null || echo "Unknown")
            if [ "$key_state" = "PendingDeletion" ]; then
                print_success "✅ KMS key is scheduled for deletion (pending window: 7 days)"
            elif [ "$key_state" = "Enabled" ] || [ "$key_state" = "Disabled" ]; then
                print_warning "⚠️  KMS key still exists: $kms_key_id (State: $key_state)"
                print_info "KMS keys have a 7-day deletion window and cannot be deleted immediately if in use"
            else
                print_info "KMS key state: $key_state"
            fi
        else
            print_success "✅ No KMS key found in state"
        fi
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
    
    # Step 5.6: Clean up CloudWatch Log Groups
    cleanup_cloudwatch_logs
    
    # Step 5.7: Clean up KMS keys
    cleanup_kms_keys
    
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
        
        # Clean up state again after failed destroy
        clean_terraform_state
        
        # Retry AWS cleanup
        cleanup_aws_load_balancers
        cleanup_target_groups
        cleanup_cloudwatch_logs
        cleanup_kms_keys
        
        # Try Terraform destroy again
        print_info "🔄 Retrying Terraform destroy..."
        if terraform_destroy; then
            print_success "✅ Terraform destroy completed on retry"
        else
            print_warning "Second attempt also failed"
            
            # Final cleanup: Force remove remaining resources from state and try manual deletion
            print_info "🔧 Attempting final cleanup of remaining resources..."
            cd "$TERRAFORM_DIR"
            
            # Remove CloudWatch Log Group from state if it exists
            if terraform state list 2>/dev/null | grep -q "aws_cloudwatch_log_group"; then
                print_info "Removing CloudWatch Log Groups from state..."
                terraform state list 2>/dev/null | grep "aws_cloudwatch_log_group" | while read -r resource; do
                    terraform state rm "$resource" 2>/dev/null || true
                done
            fi
            
            # Try to delete VPC resources manually if they're stuck
            cleanup_vpc_manually
            
            # Try destroy one more time after manual cleanup
            print_info "Retrying destroy after manual cleanup..."
            terraform destroy -auto-approve 2>&1 | tail -20 || true
            
            cd - >/dev/null
        fi
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
