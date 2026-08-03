#!/bin/bash

# Cleanup script for the Path A data-collector (standalone EC2 + S3 + optional RDS)
#
# This tears down ONLY the data-collector resources (module.data_collector_ec2,
# module.data_archive_s3, module.data_collector_rds) via a *targeted* terraform
# destroy. It intentionally leaves EKS, VPC, and the rest of the environment
# untouched, since the collector is wired into the same env but is not part of
# the trading platform.
#
# Handles the things a plain `terraform destroy` cannot: emptying the S3 archive
# bucket (force_destroy=false), disabling RDS deletion protection, and a manual
# AWS fallback if Terraform state is missing or destroy fails.

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

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TERRAFORM_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"

# Help first — do not treat -h/--help as an environment name or run Terraform
case "${1:-}" in
    -h|--help)
        echo "Usage: $0 [environment]"
        echo ""
        echo "  environment   Terraform env under envs/<name> (default: development)"
        echo ""
        echo "Tears down ONLY the data-collector (EC2 + S3 archive + optional RDS)."
        echo "EKS, VPC, and other environment resources are left untouched."
        echo ""
        echo "Environment variables:"
        echo "  AWS_REGION               AWS region (default: us-east-1)"
        echo "  PROJECT                  Project prefix for name fallbacks (default: mtb)"
        echo "  CLEANUP_VALIDATE_ONLY=1  Run prerequisite/name resolution only (no teardown)"
        echo "  CLEANUP_AUTO_CONFIRM=yes Skip the confirmation prompt"
        echo "  KEEP_S3_ARCHIVE=1        Do NOT empty/delete the S3 archive bucket (preserve data)"
        echo ""
        echo "Examples:"
        echo "  $0"
        echo "  $0 development"
        echo "  CLEANUP_AUTO_CONFIRM=yes $0 development"
        echo "  KEEP_S3_ARCHIVE=1 $0        # keep the Parquet archive, remove compute + RDS"
        exit 0
        ;;
esac

# Configuration
ENVIRONMENT="${1:-development}"
AWS_REGION="${AWS_REGION:-us-east-1}"
PROJECT="${PROJECT:-mtb}"
TERRAFORM_DIR="$TERRAFORM_ROOT/envs/${ENVIRONMENT}"

# Derived (fallback) resource names — mirror infrastructure/terraform/envs/<env>/main.tf.
# local.name = "${project}-${env}"; collector name = "${local.name}-data-collector".
NAME_BASE="${PROJECT}-${ENVIRONMENT}"
COLLECTOR_NAME="${NAME_BASE}-data-collector"
RDS_NAME="${COLLECTOR_NAME}-rds"
SECRET_NAME="${RDS_NAME}/postgres"
LOG_GROUP="/mtb/data-collector/${COLLECTOR_NAME}"

# Terraform module addresses to destroy (targeted).
TF_TARGETS=(
    "module.data_collector_rds"
    "module.data_archive_s3"
    "module.data_collector_ec2"
)

# Values resolved from Terraform outputs (best-effort; empty if state is gone).
BUCKET_NAME=""
INSTANCE_ID=""
RDS_INSTANCE_ID=""
SECRET_ARN=""

command_exists() {
    command -v "$1" >/dev/null 2>&1
}

check_prerequisites() {
    print_info "Checking prerequisites..."

    if ! command_exists aws; then
        print_error "AWS CLI not found"
        exit 1
    fi

    if ! command_exists terraform; then
        print_error "Terraform not found"
        exit 1
    fi

    if ! aws sts get-caller-identity >/dev/null 2>&1; then
        print_error "AWS credentials not configured"
        exit 1
    fi

    print_success "Prerequisites OK"
}

# Resolve resource names/ids from Terraform outputs, falling back to derived names.
resolve_resources() {
    print_info "Resolving data-collector resources..."

    local account_id
    account_id=$(aws sts get-caller-identity --query Account --output text 2>/dev/null || echo "")

    if [ -d "$TERRAFORM_DIR" ]; then
        cd "$TERRAFORM_DIR"
        terraform init >/dev/null 2>&1 || true

        BUCKET_NAME=$(terraform output -raw data_archive_bucket 2>/dev/null || echo "")
        INSTANCE_ID=$(terraform output -raw data_collector_instance_id 2>/dev/null || echo "")
        SECRET_ARN=$(terraform output -raw data_collector_rds_secret_arn 2>/dev/null || echo "")

        # RDS instance id from state (no dedicated env output for it)
        RDS_INSTANCE_ID=$(terraform state show 'module.data_collector_rds[0].aws_db_instance.this' 2>/dev/null \
            | grep -E "^\s*identifier\s+=" | awk -F'"' '{print $2}' || echo "")

        cd - >/dev/null 2>&1
    fi

    # Terraform prints "null"/warnings when the output is absent — treat as empty.
    case "$BUCKET_NAME" in ""|null|*Warning*|*Error*) BUCKET_NAME="" ;; esac
    case "$INSTANCE_ID" in ""|null|*Warning*|*Error*) INSTANCE_ID="" ;; esac
    case "$SECRET_ARN" in ""|null|*Warning*|*Error*) SECRET_ARN="" ;; esac

    # Fallbacks derived from naming convention
    [ -z "$BUCKET_NAME" ] && [ -n "$account_id" ] && BUCKET_NAME="${NAME_BASE}-data-archive-${account_id}"
    [ -z "$RDS_INSTANCE_ID" ] && RDS_INSTANCE_ID="$RDS_NAME"

    print_info "  Environment:   $ENVIRONMENT (region $AWS_REGION)"
    print_info "  Collector name: $COLLECTOR_NAME"
    print_info "  S3 bucket:     ${BUCKET_NAME:-<unknown>}"
    print_info "  EC2 instance:  ${INSTANCE_ID:-<none in state>}"
    print_info "  RDS instance:  $RDS_INSTANCE_ID"
    print_info "  DB secret:     $SECRET_NAME"
    print_info "  Log group:     $LOG_GROUP"
}

confirm() {
    print_warning "⚠️  This will DELETE the data-collector EC2 instance, its IAM role,"
    print_warning "    security groups, CloudWatch logs, the RDS hot store + secret, and"
    if [ "${KEEP_S3_ARCHIVE:-}" = "1" ]; then
        print_warning "    (S3 archive bucket will be PRESERVED — KEEP_S3_ARCHIVE=1)."
    else
        print_warning "    the S3 archive bucket AND ALL ARCHIVED TRADE DATA in it."
    fi
    print_warning "    EKS / VPC / other environment resources are NOT touched."
    echo ""

    local response=""
    if [ "${CLEANUP_AUTO_CONFIRM:-}" = "yes" ]; then
        response="yes"
        print_info "CLEANUP_AUTO_CONFIRM=yes — proceeding without prompt"
    elif [ ! -t 0 ]; then
        read -r response
    else
        read -p "Type 'yes' to proceed: " -r response
    fi

    if [ "$response" != "yes" ]; then
        print_info "Cleanup cancelled by user"
        exit 0
    fi
}

# Empty the S3 archive bucket (force_destroy=false, so Terraform can't delete a
# non-empty bucket). Removes current objects plus any versions/delete markers.
empty_s3_bucket() {
    if [ "${KEEP_S3_ARCHIVE:-}" = "1" ]; then
        print_info "KEEP_S3_ARCHIVE=1 — skipping S3 bucket emptying/deletion"
        return 0
    fi

    if [ -z "$BUCKET_NAME" ]; then
        print_info "No S3 bucket resolved — skipping"
        return 0
    fi

    if ! aws s3api head-bucket --bucket "$BUCKET_NAME" >/dev/null 2>&1; then
        print_info "S3 bucket $BUCKET_NAME does not exist — skipping"
        return 0
    fi

    print_info "🧹 Emptying S3 bucket: $BUCKET_NAME"
    aws s3 rm "s3://${BUCKET_NAME}" --recursive >/dev/null 2>&1 || true

    # Delete any object versions / delete markers (in case versioning was ever on)
    if command_exists jq; then
        local versions
        versions=$(aws s3api list-object-versions --bucket "$BUCKET_NAME" --region "$AWS_REGION" \
            --output json 2>/dev/null || echo "")
        if [ -n "$versions" ]; then
            local to_delete
            to_delete=$(echo "$versions" | jq -c '{Objects: [(.Versions // []) + (.DeleteMarkers // [])
                | .[] | {Key: .Key, VersionId: .VersionId}], Quiet: true}' 2>/dev/null || echo "")
            if [ -n "$to_delete" ] && [ "$to_delete" != '{"Objects":[],"Quiet":true}' ]; then
                echo "$to_delete" | aws s3api delete-objects --bucket "$BUCKET_NAME" \
                    --region "$AWS_REGION" --delete file:///dev/stdin >/dev/null 2>&1 || true
            fi
        fi
    fi

    print_success "S3 bucket emptied"
}

# RDS deletion_protection defaults to false, but disable it defensively so the
# destroy never blocks. Also ensures no final snapshot is required.
disable_rds_deletion_protection() {
    if ! aws rds describe-db-instances --db-instance-identifier "$RDS_INSTANCE_ID" \
        --region "$AWS_REGION" >/dev/null 2>&1; then
        return 0
    fi

    local protected
    protected=$(aws rds describe-db-instances --db-instance-identifier "$RDS_INSTANCE_ID" \
        --region "$AWS_REGION" --query 'DBInstances[0].DeletionProtection' --output text 2>/dev/null || echo "false")

    if [ "$protected" = "True" ] || [ "$protected" = "true" ]; then
        print_info "Disabling RDS deletion protection on $RDS_INSTANCE_ID..."
        aws rds modify-db-instance --db-instance-identifier "$RDS_INSTANCE_ID" \
            --no-deletion-protection --apply-immediately --region "$AWS_REGION" >/dev/null 2>&1 || \
            print_warning "Could not disable deletion protection (continuing)"
    fi
}

# Remove a stale local-backend state lock so destroy can proceed.
handle_state_lock() {
    cd "$TERRAFORM_DIR"
    if [ -f ".terraform.tfstate.lock.info" ]; then
        print_warning "Local state lock file found. Attempting force-unlock..."
        local lock_id
        lock_id=$(grep -oP '"ID"\s*:\s*"\K[0-9a-f-]+' .terraform.tfstate.lock.info 2>/dev/null || true)
        if [ -n "$lock_id" ]; then
            terraform force-unlock -force "$lock_id" 2>/dev/null || true
        fi
        rm -f .terraform.tfstate.lock.info
    fi
    cd - >/dev/null
}

# Targeted terraform destroy of the three data-collector modules only.
terraform_destroy_targeted() {
    if [ ! -d "$TERRAFORM_DIR" ]; then
        print_warning "Terraform dir $TERRAFORM_DIR not found — skipping targeted destroy"
        return 1
    fi

    cd "$TERRAFORM_DIR"
    terraform init >/dev/null 2>&1 || true
    handle_state_lock

    # Nothing to do if the modules aren't in state
    local in_state
    in_state=$(terraform state list 2>/dev/null | grep -E "^module\.data_(collector|archive)" || echo "")
    if [ -z "$in_state" ]; then
        print_info "No data-collector resources found in Terraform state"
        cd - >/dev/null
        return 1
    fi

    local target_args=()
    local t
    for t in "${TF_TARGETS[@]}"; do
        target_args+=("-target=$t")
    done

    print_info "💥 Running targeted terraform destroy for data-collector modules..."
    if timeout 1800 terraform destroy -auto-approve -input=false "${target_args[@]}"; then
        cd - >/dev/null
        print_success "Targeted terraform destroy completed"
        return 0
    fi

    print_warning "Targeted destroy failed; retrying once with -refresh=false..."
    if timeout 1800 terraform destroy -auto-approve -input=false -refresh=false "${target_args[@]}"; then
        cd - >/dev/null
        print_success "Targeted terraform destroy completed (-refresh=false)"
        return 0
    fi

    cd - >/dev/null
    print_warning "Targeted terraform destroy did not fully succeed"
    return 1
}

# Best-effort manual teardown for anything Terraform left behind (or if state is gone).
manual_cleanup() {
    print_info "🔧 Manual fallback cleanup for leftover AWS resources..."

    # 1. Terminate EC2 instance (resolve by tag if not known)
    local instance_id="$INSTANCE_ID"
    if [ -z "$instance_id" ]; then
        instance_id=$(aws ec2 describe-instances --region "$AWS_REGION" \
            --filters "Name=tag:Name,Values=${COLLECTOR_NAME}" "Name=instance-state-name,Values=pending,running,stopping,stopped" \
            --query 'Reservations[].Instances[].InstanceId' --output text 2>/dev/null | tr '\t' '\n' | head -1 || echo "")
    fi
    if [ -n "$instance_id" ] && [ "$instance_id" != "None" ]; then
        print_info "Terminating EC2 instance: $instance_id"
        aws ec2 terminate-instances --instance-ids "$instance_id" --region "$AWS_REGION" >/dev/null 2>&1 || true
        aws ec2 wait instance-terminated --instance-ids "$instance_id" --region "$AWS_REGION" 2>/dev/null || true
    fi

    # 2. Delete RDS instance (skip final snapshot)
    if aws rds describe-db-instances --db-instance-identifier "$RDS_INSTANCE_ID" --region "$AWS_REGION" >/dev/null 2>&1; then
        print_info "Deleting RDS instance: $RDS_INSTANCE_ID"
        aws rds delete-db-instance --db-instance-identifier "$RDS_INSTANCE_ID" \
            --skip-final-snapshot --delete-automated-backups --region "$AWS_REGION" >/dev/null 2>&1 || true
        print_info "Waiting for RDS deletion (this can take several minutes)..."
        aws rds wait db-instance-deleted --db-instance-identifier "$RDS_INSTANCE_ID" --region "$AWS_REGION" 2>/dev/null || true
    fi

    # 3. Delete DB subnet group
    if aws rds describe-db-subnet-groups --db-subnet-group-name "${RDS_NAME}-subnets" --region "$AWS_REGION" >/dev/null 2>&1; then
        print_info "Deleting DB subnet group: ${RDS_NAME}-subnets"
        aws rds delete-db-subnet-group --db-subnet-group-name "${RDS_NAME}-subnets" --region "$AWS_REGION" >/dev/null 2>&1 || true
    fi

    # 4. Delete Secrets Manager secret (immediate)
    local secret_id="${SECRET_ARN:-$SECRET_NAME}"
    if aws secretsmanager describe-secret --secret-id "$secret_id" --region "$AWS_REGION" >/dev/null 2>&1; then
        print_info "Deleting DB secret: $secret_id"
        aws secretsmanager delete-secret --secret-id "$secret_id" \
            --force-delete-without-recovery --region "$AWS_REGION" >/dev/null 2>&1 || true
    fi

    # 5. Delete security groups (RDS first — it references the collector SG)
    delete_sg_by_name "${RDS_NAME}-sg"
    delete_sg_by_name "${COLLECTOR_NAME}-sg"

    # 6. Delete IAM instance profile, inline policy, and role
    if aws iam get-instance-profile --instance-profile-name "${COLLECTOR_NAME}-profile" >/dev/null 2>&1; then
        print_info "Removing IAM instance profile: ${COLLECTOR_NAME}-profile"
        aws iam remove-role-from-instance-profile --instance-profile-name "${COLLECTOR_NAME}-profile" \
            --role-name "${COLLECTOR_NAME}-role" >/dev/null 2>&1 || true
        aws iam delete-instance-profile --instance-profile-name "${COLLECTOR_NAME}-profile" >/dev/null 2>&1 || true
    fi
    if aws iam get-role --role-name "${COLLECTOR_NAME}-role" >/dev/null 2>&1; then
        print_info "Deleting IAM role: ${COLLECTOR_NAME}-role"
        aws iam delete-role-policy --role-name "${COLLECTOR_NAME}-role" \
            --policy-name "${COLLECTOR_NAME}-policy" >/dev/null 2>&1 || true
        aws iam delete-role --role-name "${COLLECTOR_NAME}-role" >/dev/null 2>&1 || true
    fi

    # 7. Delete CloudWatch log group
    if aws logs describe-log-groups --log-group-name-prefix "$LOG_GROUP" --region "$AWS_REGION" \
        --query 'logGroups[0].logGroupName' --output text 2>/dev/null | grep -q "$LOG_GROUP"; then
        print_info "Deleting CloudWatch log group: $LOG_GROUP"
        aws logs delete-log-group --log-group-name "$LOG_GROUP" --region "$AWS_REGION" >/dev/null 2>&1 || true
    fi

    # 8. Delete S3 bucket (already emptied above)
    if [ "${KEEP_S3_ARCHIVE:-}" != "1" ] && [ -n "$BUCKET_NAME" ] && \
        aws s3api head-bucket --bucket "$BUCKET_NAME" >/dev/null 2>&1; then
        print_info "Deleting S3 bucket: $BUCKET_NAME"
        aws s3api delete-bucket --bucket "$BUCKET_NAME" --region "$AWS_REGION" >/dev/null 2>&1 || \
            print_warning "Could not delete bucket $BUCKET_NAME (may be non-empty or region-mismatched)"
    fi

    print_success "Manual fallback cleanup completed"
}

# Delete a security group by name, retrying while dependencies clear.
delete_sg_by_name() {
    local sg_name="$1"
    local sg_id
    sg_id=$(aws ec2 describe-security-groups --region "$AWS_REGION" \
        --filters "Name=group-name,Values=${sg_name}" \
        --query 'SecurityGroups[0].GroupId' --output text 2>/dev/null || echo "")
    if [ -z "$sg_id" ] || [ "$sg_id" = "None" ]; then
        return 0
    fi
    print_info "Deleting security group: $sg_name ($sg_id)"
    local retries=6
    while [ $retries -gt 0 ]; do
        if aws ec2 delete-security-group --group-id "$sg_id" --region "$AWS_REGION" >/dev/null 2>&1; then
            return 0
        fi
        retries=$((retries - 1))
        sleep 10
    done
    print_warning "Could not delete security group $sg_name — it may still have dependencies"
}

verify_cleanup() {
    print_info "🔍 Verifying data-collector cleanup..."
    local remaining=0

    if [ -n "$INSTANCE_ID" ] && [ "$INSTANCE_ID" != "None" ]; then
        local state
        state=$(aws ec2 describe-instances --instance-ids "$INSTANCE_ID" --region "$AWS_REGION" \
            --query 'Reservations[0].Instances[0].State.Name' --output text 2>/dev/null || echo "terminated")
        if [ "$state" != "terminated" ] && [ "$state" != "None" ] && [ -n "$state" ]; then
            print_warning "⚠️  EC2 instance still $state: $INSTANCE_ID"; remaining=1
        fi
    fi

    if aws rds describe-db-instances --db-instance-identifier "$RDS_INSTANCE_ID" --region "$AWS_REGION" >/dev/null 2>&1; then
        print_warning "⚠️  RDS instance still exists: $RDS_INSTANCE_ID (may be deleting)"; remaining=1
    fi

    if [ "${KEEP_S3_ARCHIVE:-}" != "1" ] && [ -n "$BUCKET_NAME" ] && \
        aws s3api head-bucket --bucket "$BUCKET_NAME" >/dev/null 2>&1; then
        print_warning "⚠️  S3 bucket still exists: $BUCKET_NAME"; remaining=1
    fi

    if aws secretsmanager describe-secret --secret-id "$SECRET_NAME" --region "$AWS_REGION" >/dev/null 2>&1; then
        print_warning "⚠️  DB secret still exists: $SECRET_NAME"; remaining=1
    fi

    if [ $remaining -eq 0 ]; then
        print_success "✅ No remaining data-collector resources detected"
    else
        print_info "Some resources may still be deleting asynchronously (RDS can take minutes)."
    fi
}

main() {
    print_info "🚀 Data-collector cleanup for environment: $ENVIRONMENT"
    echo ""

    check_prerequisites
    resolve_resources

    if [ "${CLEANUP_VALIDATE_ONLY:-}" = "1" ]; then
        print_success "CLEANUP_VALIDATE_ONLY=1 — resolution OK, no teardown performed"
        exit 0
    fi

    echo ""
    confirm
    echo ""

    # 1. Empty the archive bucket so Terraform can delete it
    empty_s3_bucket

    # 2. Make sure RDS won't block on deletion protection
    disable_rds_deletion_protection

    # 3. Targeted terraform destroy (preferred, keeps state consistent)
    if terraform_destroy_targeted; then
        # 4. Sweep for anything left behind
        manual_cleanup
    else
        print_warning "Falling back to manual AWS teardown..."
        manual_cleanup
    fi

    echo ""
    verify_cleanup

    echo ""
    print_success "🎉 Data-collector cleanup completed!"
    print_info "EKS, VPC, and other environment resources were left untouched."
}

main "$@"
