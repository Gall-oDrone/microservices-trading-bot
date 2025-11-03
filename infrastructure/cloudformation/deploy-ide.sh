#!/bin/bash
# Deploy and monitor Trading Bot IDE CloudFormation stack
# This script creates the stack, monitors its progress, and retrieves the IDE password upon completion

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# Script configuration
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TEMPLATE_FILE="${SCRIPT_DIR}/trading-bot-ide-cfn.yaml"
STACK_NAME="trading-bot-ide"
AWS_REGION="${AWS_REGION:-us-east-1}"
MAX_WAIT_TIME=3600  # Maximum wait time in seconds (60 minutes)
POLL_INTERVAL=30    # Poll interval in seconds
LOG_FILE="${SCRIPT_DIR}/deploy-ide.log"

# Parameters
REPOSITORY_OWNER="${REPOSITORY_OWNER:-Gall-oDrone}"
REPOSITORY_NAME="${REPOSITORY_NAME:-microservices-trading-bot}"
REPOSITORY_REF="${REPOSITORY_REF:-main}"
INSTANCE_VOLUME_SIZE="${INSTANCE_VOLUME_SIZE:-50}"
EKS_CLUSTER_ID="${EKS_CLUSTER_ID:-microservices-trading-bot}"

# Logging functions
log() {
    local level=$1
    shift
    local message="$@"
    local timestamp=$(date '+%Y-%m-%d %H:%M:%S')
    echo "[$timestamp] [$level] $message" | tee -a "$LOG_FILE"
}

log_info() {
    echo -e "${BLUE}[INFO]${NC} $1" | tee -a "$LOG_FILE"
    log "INFO" "$1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1" | tee -a "$LOG_FILE"
    log "SUCCESS" "$1"
}

log_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1" | tee -a "$LOG_FILE"
    log "WARNING" "$1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1" | tee -a "$LOG_FILE"
    log "ERROR" "$1"
}

log_debug() {
    echo -e "${CYAN}[DEBUG]${NC} $1" | tee -a "$LOG_FILE"
    log "DEBUG" "$1"
}

# Error handling
error_exit() {
    log_error "Script failed at line $1"
    log_error "Stack deployment may be in progress. Check AWS Console or run:"
    log_error "  aws cloudformation describe-stacks --stack-name $STACK_NAME --region $AWS_REGION"
    exit 1
}

trap 'error_exit $LINENO' ERR

# Check prerequisites
check_prerequisites() {
    log_info "Checking prerequisites..."
    
    # Check if AWS CLI is installed
    if ! command -v aws &> /dev/null; then
        log_error "AWS CLI is not installed. Please install AWS CLI first."
        exit 1
    fi
    log_success "AWS CLI is installed"
    
    # Check if jq is installed
    if ! command -v jq &> /dev/null; then
        log_warning "jq is not installed. Password retrieval will be limited."
    else
        log_success "jq is installed"
    fi
    
    # Check AWS credentials
    log_info "Checking AWS credentials..."
    if ! aws sts get-caller-identity &>/dev/null; then
        log_error "AWS credentials are not configured or invalid."
        log_error "Please run: aws configure"
        exit 1
    fi
    
    local aws_account=$(aws sts get-caller-identity --query 'Account' --output text)
    local aws_user=$(aws sts get-caller-identity --query 'Arn' --output text)
    log_success "AWS credentials are valid"
    log_info "AWS Account: $aws_account"
    log_info "AWS User/Role: $aws_user"
    
    # Check if template file exists
    if [ ! -f "$TEMPLATE_FILE" ]; then
        log_error "Template file not found: $TEMPLATE_FILE"
        exit 1
    fi
    log_success "Template file found: $TEMPLATE_FILE"
}

# Check if stack exists
stack_exists() {
    aws cloudformation describe-stacks --stack-name "$STACK_NAME" --region "$AWS_REGION" &>/dev/null
}

# Get stack status
get_stack_status() {
    aws cloudformation describe-stacks \
        --stack-name "$STACK_NAME" \
        --region "$AWS_REGION" \
        --query 'Stacks[0].StackStatus' \
        --output text 2>/dev/null || echo "NONE"
}

# Wait for stack operation to complete
wait_for_stack() {
    local operation=$1  # CREATE or UPDATE
    local start_time=$(date +%s)
    local elapsed=0
    
    log_info "Waiting for stack $operation to complete (this may take 15-30 minutes)..."
    log_info "Monitoring progress every $POLL_INTERVAL seconds..."
    
    while [ $elapsed -lt $MAX_WAIT_TIME ]; do
        local status=$(get_stack_status)
        
        if [ "$status" = "NONE" ]; then
            log_error "Stack does not exist!"
            exit 1
        fi
        
        case "$status" in
            *COMPLETE)
                log_success "Stack $operation completed successfully!"
                return 0
                ;;
            *FAILED)
                log_error "Stack $operation failed!"
                show_stack_events
                exit 1
                ;;
            *ROLLBACK*)
                log_error "Stack is rolling back!"
                show_stack_events
                exit 1
                ;;
            *IN_PROGRESS)
                elapsed=$(($(date +%s) - start_time))
                local minutes=$((elapsed / 60))
                local seconds=$((elapsed % 60))
                log_info "Stack is still $operation in progress... (${minutes}m ${seconds}s elapsed)"
                
                # Show recent events every 5 minutes
                if [ $((elapsed % 300)) -lt $POLL_INTERVAL ]; then
                    show_recent_events 3
                fi
                ;;
            *)
                log_warning "Unknown stack status: $status"
                ;;
        esac
        
        sleep $POLL_INTERVAL
        elapsed=$(($(date +%s) - start_time))
    done
    
    log_error "Timeout waiting for stack $operation to complete (${MAX_WAIT_TIME} seconds)"
    log_error "Current status: $(get_stack_status)"
    show_stack_events
    exit 1
}

# Show recent stack events
show_recent_events() {
    local count=${1:-5}
    log_debug "Recent stack events:"
    aws cloudformation describe-stack-events \
        --stack-name "$STACK_NAME" \
        --region "$AWS_REGION" \
        --max-items "$count" \
        --query 'StackEvents[*].[Timestamp,ResourceStatus,LogicalResourceId]' \
        --output table 2>/dev/null | tee -a "$LOG_FILE" || true
}

# Show all failed stack events
show_stack_events() {
    log_info "Fetching stack events for troubleshooting..."
    aws cloudformation describe-stack-events \
        --stack-name "$STACK_NAME" \
        --region "$AWS_REGION" \
        --query 'StackEvents[?ResourceStatus==`CREATE_FAILED` || ResourceStatus==`UPDATE_FAILED` || ResourceStatus==`DELETE_FAILED`].[Timestamp,ResourceStatus,LogicalResourceId,ResourceStatusReason]' \
        --output table 2>/dev/null | tee -a "$LOG_FILE" || true
}

# Create the CloudFormation stack
create_stack() {
    log_info "Creating CloudFormation stack: $STACK_NAME"
    log_info "Region: $AWS_REGION"
    log_info "Template: $TEMPLATE_FILE"
    
    log_info "Stack parameters:"
    log_info "  RepositoryOwner: $REPOSITORY_OWNER"
    log_info "  RepositoryName: $REPOSITORY_NAME"
    log_info "  RepositoryRef: $REPOSITORY_REF"
    log_info "  InstanceVolumeSize: $INSTANCE_VOLUME_SIZE GB"
    log_info "  EksClusterId: $EKS_CLUSTER_ID"
    
    local stack_id
    if stack_id=$(aws cloudformation create-stack \
        --stack-name "$STACK_NAME" \
        --template-body "file://$TEMPLATE_FILE" \
        --capabilities CAPABILITY_NAMED_IAM \
        --parameters \
            "ParameterKey=RepositoryOwner,ParameterValue=$REPOSITORY_OWNER" \
            "ParameterKey=RepositoryName,ParameterValue=$REPOSITORY_NAME" \
            "ParameterKey=RepositoryRef,ParameterValue=$REPOSITORY_REF" \
            "ParameterKey=InstanceVolumeSize,ParameterValue=$INSTANCE_VOLUME_SIZE" \
            "ParameterKey=EksClusterId,ParameterValue=$EKS_CLUSTER_ID" \
        --region "$AWS_REGION" \
        --query 'StackId' \
        --output text 2>&1); then
        
        log_success "Stack creation initiated!"
        log_info "Stack ID: $stack_id"
        echo "$stack_id"
    else
        log_error "Failed to create stack"
        log_error "$stack_id"
        exit 1
    fi
}

# Update the CloudFormation stack
update_stack() {
    log_info "Updating CloudFormation stack: $STACK_NAME"
    
    local update_output
    if update_output=$(aws cloudformation update-stack \
        --stack-name "$STACK_NAME" \
        --template-body "file://$TEMPLATE_FILE" \
        --capabilities CAPABILITY_NAMED_IAM \
        --parameters \
            "ParameterKey=RepositoryOwner,ParameterValue=$REPOSITORY_OWNER" \
            "ParameterKey=RepositoryName,ParameterValue=$REPOSITORY_NAME" \
            "ParameterKey=RepositoryRef,ParameterValue=$REPOSITORY_REF" \
            "ParameterKey=InstanceVolumeSize,ParameterValue=$INSTANCE_VOLUME_SIZE" \
            "ParameterKey=EksClusterId,ParameterValue=$EKS_CLUSTER_ID" \
        --region "$AWS_REGION" \
        2>&1); then
        
        log_success "Stack update initiated!"
        echo "$update_output"
    else
        if echo "$update_output" | grep -q "No updates are to be performed"; then
            log_warning "No stack updates are needed"
            return 0
        else
            log_error "Failed to update stack"
            log_error "$update_output"
            exit 1
        fi
    fi
}

# Get stack outputs
get_stack_outputs() {
    log_info "Retrieving stack outputs..."
    
    aws cloudformation describe-stacks \
        --stack-name "$STACK_NAME" \
        --region "$AWS_REGION" \
        --query 'Stacks[0].Outputs' \
        --output json 2>/dev/null || {
        log_error "Failed to retrieve stack outputs"
        return 1
    }
}

# Display stack outputs
display_outputs() {
    local outputs=$(get_stack_outputs)
    
    if [ -z "$outputs" ] || [ "$outputs" = "null" ]; then
        log_warning "No stack outputs available yet"
        return 1
    fi
    
    log_success "Stack outputs:"
    echo "$outputs" | jq -r '.[] | "\(.OutputKey): \(.OutputValue)"' 2>/dev/null || \
        echo "$outputs" | jq '.' | tee -a "$LOG_FILE"
    
    # Extract specific values
    local ide_url=$(echo "$outputs" | jq -r '.[] | select(.OutputKey=="IdeUrl") | .OutputValue' 2>/dev/null)
    local secret_name=$(echo "$outputs" | jq -r '.[] | select(.OutputKey=="IdePasswordSecretName") | .OutputValue' 2>/dev/null)
    
    if [ -n "$ide_url" ] && [ "$ide_url" != "null" ]; then
        log_success "IDE URL: $ide_url"
    fi
    
    if [ -n "$secret_name" ] && [ "$secret_name" != "null" ]; then
        log_info "Password secret name: $secret_name"
    fi
}

# Retrieve password from Secrets Manager
retrieve_password() {
    log_info "Retrieving IDE password from AWS Secrets Manager..."
    
    local secret_name="${STACK_NAME}-password"
    
    # Try to get the secret name from stack outputs first
    local outputs=$(get_stack_outputs)
    if [ -n "$outputs" ] && [ "$outputs" != "null" ]; then
        local output_secret_name=$(echo "$outputs" | jq -r '.[] | select(.OutputKey=="IdePasswordSecretName") | .OutputValue' 2>/dev/null)
        if [ -n "$output_secret_name" ] && [ "$output_secret_name" != "null" ]; then
            secret_name="$output_secret_name"
        fi
    fi
    
    log_info "Looking for secret: $secret_name"
    
    local password
    if password=$(aws secretsmanager get-secret-value \
        --secret-id "$secret_name" \
        --region "$AWS_REGION" \
        --query 'SecretString' \
        --output text 2>/dev/null); then
        
        if command -v jq &> /dev/null; then
            password=$(echo "$password" | jq -r '.password' 2>/dev/null || echo "$password")
        fi
        
        if [ -n "$password" ] && [ "$password" != "null" ]; then
            log_success "Password retrieved successfully!"
            echo ""
            echo "=========================================="
            echo -e "${GREEN}IDE Password:${NC} $password"
            echo "=========================================="
            echo ""
            log_info "Password has been displayed above and logged to $LOG_FILE"
            echo "$password" >> "$LOG_FILE"
            return 0
        fi
    fi
    
    log_warning "Could not retrieve password automatically"
    log_info "You can retrieve it manually using:"
    log_info "  aws secretsmanager get-secret-value --secret-id $secret_name --region $AWS_REGION --query 'SecretString' --output text | jq -r '.password'"
    return 1
}

# Main execution
main() {
    log_info "=========================================="
    log_info "Trading Bot IDE Deployment Script"
    log_info "=========================================="
    log_info "Log file: $LOG_FILE"
    echo ""
    
    # Check prerequisites
    check_prerequisites
    echo ""
    
    # Determine if stack exists
    if stack_exists; then
        log_warning "Stack '$STACK_NAME' already exists"
        local current_status=$(get_stack_status)
        log_info "Current stack status: $current_status"
        
        case "$current_status" in
            CREATE_COMPLETE|UPDATE_COMPLETE)
                log_info "Stack is already deployed successfully"
                log_info "Retrieving current outputs and password..."
                display_outputs
                retrieve_password
                exit 0
                ;;
            CREATE_IN_PROGRESS|UPDATE_IN_PROGRESS)
                log_info "Stack operation is already in progress"
                log_info "Monitoring existing stack creation..."
                wait_for_stack "CREATE"
                display_outputs
                retrieve_password
                exit 0
                ;;
            *)
                log_warning "Stack is in state: $current_status"
                read -p "Do you want to update the stack? (y/n): " -n 1 -r
                echo
                if [[ $REPLY =~ ^[Yy]$ ]]; then
                    update_stack
                    wait_for_stack "UPDATE"
                else
                    log_info "Aborted by user"
                    exit 0
                fi
                ;;
        esac
    else
        # Create new stack
        create_stack
        echo ""
        wait_for_stack "CREATE"
    fi
    
    echo ""
    log_success "Stack deployment completed!"
    echo ""
    
    # Display outputs
    display_outputs
    echo ""
    
    # Retrieve password
    retrieve_password
    echo ""
    
    log_success "Deployment script completed successfully!"
    log_info "You can access the IDE using the URL shown above"
    log_info "Full log available at: $LOG_FILE"
}

# Run main function
main "$@"

