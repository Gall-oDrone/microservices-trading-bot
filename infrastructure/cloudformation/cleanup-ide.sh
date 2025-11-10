#!/bin/bash
# Delete Trading Bot IDE CloudFormation stacks in the correct order

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
STACK_NAME="trading-bot-ide"
IAM_STACK_NAME="trading-bot-ide-iam"
CLOUDFRONT_STACK_NAME="trading-bot-ide-cloudfront"
AWS_REGION="${AWS_REGION:-us-east-1}"
MAX_WAIT_TIME=3600   # Maximum wait time in seconds (60 minutes)
POLL_INTERVAL=30     # Poll interval in seconds
LOG_FILE="${SCRIPT_DIR}/cleanup-ide.log"
FORCE_DELETE=0

# Logging helpers
log() {
    local level=$1
    shift
    local message="$*"
    local timestamp
    timestamp=$(date '+%Y-%m-%d %H:%M:%S')
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
    local line_no=$1
    log_error "Script failed at line $line_no"
    log_error "Cleanup may be incomplete. Review CloudFormation stacks manually:"
    log_error "  aws cloudformation describe-stacks --stack-name $CLOUDFRONT_STACK_NAME --region $AWS_REGION"
    log_error "  aws cloudformation describe-stacks --stack-name $STACK_NAME --region $AWS_REGION"
    log_error "  aws cloudformation describe-stacks --stack-name $IAM_STACK_NAME --region $AWS_REGION"
    exit 1
}

trap 'error_exit $LINENO' ERR

usage() {
    cat <<EOF
Usage: $(basename "$0") [options]

Options:
  -r, --region <region>   AWS region (default: $AWS_REGION)
  -f, --force             Do not prompt for confirmation
  -h, --help              Show this help message
EOF
}

parse_args() {
    while [[ $# -gt 0 ]]; do
        case "$1" in
            -r|--region)
                AWS_REGION="$2"
                shift 2
                ;;
            -f|--force)
                FORCE_DELETE=1
                shift
                ;;
            -h|--help)
                usage
                exit 0
                ;;
            *)
                log_error "Unknown option: $1"
                usage
                exit 1
                ;;
        esac
    done
}

# Check prerequisites
check_prerequisites() {
    log_info "Checking prerequisites..."

    if ! command -v aws >/dev/null 2>&1; then
        log_error "AWS CLI is not installed. Please install it first."
        exit 1
    fi
    log_success "AWS CLI is installed"

    log_info "Validating AWS credentials..."
    if ! aws sts get-caller-identity --region "$AWS_REGION" >/dev/null 2>&1; then
        log_error "AWS credentials are not configured or invalid for region $AWS_REGION"
        exit 1
    fi

    local aws_account aws_user
    aws_account=$(aws sts get-caller-identity --query 'Account' --output text --region "$AWS_REGION")
    aws_user=$(aws sts get-caller-identity --query 'Arn' --output text --region "$AWS_REGION")
    log_success "AWS credentials ok (Account: $aws_account, Principal: $aws_user)"
}

stack_exists() {
    local stack_name=$1
    aws cloudformation describe-stacks --stack-name "$stack_name" --region "$AWS_REGION" >/dev/null 2>&1
}

get_stack_status() {
    local stack_name=$1
    aws cloudformation describe-stacks \
        --stack-name "$stack_name" \
        --region "$AWS_REGION" \
        --query 'Stacks[0].StackStatus' \
        --output text 2>/dev/null || echo "NONE"
}

show_recent_events() {
    local count=$1
    local stack_name=$2
    log_debug "Recent events for $stack_name:"
    aws cloudformation describe-stack-events \
        --stack-name "$stack_name" \
        --region "$AWS_REGION" \
        --max-items "$count" \
        --query 'StackEvents[*].[Timestamp,ResourceStatus,LogicalResourceId,ResourceStatusReason]' \
        --output table 2>/dev/null | tee -a "$LOG_FILE" || true
}

show_stack_events() {
    local stack_name=$1
    log_info "Fetching failure events for stack $stack_name..."
    aws cloudformation describe-stack-events \
        --stack-name "$stack_name" \
        --region "$AWS_REGION" \
        --query 'StackEvents[?ends_with(ResourceStatus, `_FAILED`)].[Timestamp,ResourceStatus,LogicalResourceId,ResourceStatusReason]' \
        --output table 2>/dev/null | tee -a "$LOG_FILE" || true
}

wait_for_stack_deletion() {
    local stack_name=$1
    local start_time elapsed status

    start_time=$(date +%s)
    log_info "Waiting for stack '$stack_name' to delete..."

    while true; do
        status=$(get_stack_status "$stack_name")

        if [[ "$status" == "NONE" ]]; then
            log_success "Stack '$stack_name' deleted successfully"
            return 0
        fi

        case "$status" in
            DELETE_COMPLETE)
                log_success "Stack '$stack_name' deleted successfully"
                return 0
                ;;
            *FAILED)
                log_error "Stack '$stack_name' deletion failed (status: $status)"
                show_stack_events "$stack_name"
                exit 1
                ;;
            *IN_PROGRESS)
                elapsed=$(( $(date +%s) - start_time ))
                local minutes=$(( elapsed / 60 ))
                local seconds=$(( elapsed % 60 ))
                log_info "Deletion in progress... (${minutes}m ${seconds}s elapsed)"
                ;;
            *)
                log_warning "Stack '$stack_name' status: $status"
                ;;
        esac

        if (( $(date +%s) - start_time > MAX_WAIT_TIME )); then
            log_error "Timeout waiting for stack '$stack_name' to delete (${MAX_WAIT_TIME}s)"
            show_stack_events "$stack_name"
            exit 1
        fi

        sleep "$POLL_INTERVAL"
    done
}

delete_stack() {
    local stack_name=$1

    if ! stack_exists "$stack_name"; then
        log_warning "Stack '$stack_name' does not exist. Skipping."
        return 0
    fi

    local status
    status=$(get_stack_status "$stack_name")
    log_info "Current status of '$stack_name': $status"

    case "$status" in
        DELETE_IN_PROGRESS)
            log_warning "Deletion already in progress for '$stack_name'. Waiting for completion."
            wait_for_stack_deletion "$stack_name"
            return 0
            ;;
        *_IN_PROGRESS)
            log_warning "Stack '$stack_name' is currently busy ($status). Waiting before delete."
            wait_for_stack_deletion "$stack_name"
            ;;
    esac

    log_info "Deleting stack '$stack_name'..."
    if aws cloudformation delete-stack \
        --stack-name "$stack_name" \
        --region "$AWS_REGION"; then
        log_success "Delete request accepted for '$stack_name'"
        wait_for_stack_deletion "$stack_name"
    else
        log_error "Failed to request deletion for '$stack_name'"
        exit 1
    fi
}

prompt_confirmation() {
    if [[ $FORCE_DELETE -eq 1 ]]; then
        return
    fi

    cat <<EOF
The following CloudFormation stacks will be deleted in region '$AWS_REGION':
  1. $CLOUDFRONT_STACK_NAME (CloudFront distribution and DNS)
  2. $STACK_NAME (IDE infrastructure, EC2 instance, VPC, Secrets Manager)
  3. $IAM_STACK_NAME (IAM roles and profiles)

This operation is destructive and cannot be undone.
EOF

    read -r -p "Type 'delete' to proceed: " confirmation
    if [[ "$confirmation" != "delete" ]]; then
        log_info "Operation cancelled by user."
        exit 0
    fi
}

main() {
    parse_args "$@"

    log_info "=========================================="
    log_info "Trading Bot IDE Cleanup Script"
    log_info "=========================================="
    log_info "Log file: $LOG_FILE"
    echo ""

    check_prerequisites
    echo ""

    prompt_confirmation
    echo ""

    # Delete stacks in reverse dependency order
    delete_stack "$CLOUDFRONT_STACK_NAME"
    echo ""
    delete_stack "$STACK_NAME"
    echo ""
    delete_stack "$IAM_STACK_NAME"
    echo ""

    log_success "Cleanup completed!"
    log_info "All targeted stacks have been deleted (or did not exist)."
    log_info "Review the AWS Console to confirm resources are removed."
}

main "$@"

