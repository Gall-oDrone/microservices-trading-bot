#!/bin/bash

# Cleanup script for AWS Secrets Manager secrets
# This script deletes secrets created by setup-secrets.sh
# Use with caution - this permanently deletes secrets!

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
AWS_REGION="${AWS_REGION:-us-east-1}"
SECRET_PREFIX="trading-bot"

# List of secrets to delete
SECRETS=(
    "${SECRET_PREFIX}/bitso-api-key"
    "${SECRET_PREFIX}/bitso-api-secret"
    "${SECRET_PREFIX}/redis-password"
    "${SECRET_PREFIX}/anthropic-api-key"
    "${SECRET_PREFIX}/openai-api-key"
    "${SECRET_PREFIX}/etoro-api-key"
    "${SECRET_PREFIX}/etoro-user-key"
)

print_info "🗑️  Cleaning up secrets in AWS Secrets Manager..."
print_info "Region: $AWS_REGION"
print_info "Secret prefix: $SECRET_PREFIX"

# Check prerequisites
if ! command -v aws &> /dev/null; then
    print_error "AWS CLI is not installed. Please install AWS CLI first."
    exit 1
fi

# Check AWS credentials
if ! aws sts get-caller-identity &>/dev/null; then
    print_error "AWS credentials not configured. Please run 'aws configure' or set AWS credentials."
    exit 1
fi

print_success "AWS CLI is installed and configured"

# Confirm deletion
print_warning "⚠️  WARNING: This will permanently delete the following secrets:"
for secret in "${SECRETS[@]}"; do
    echo "  - $secret"
done
echo ""
if [ "${CLEANUP_AUTO_CONFIRM:-}" = "yes" ]; then
    CONFIRM="yes"
    print_info "CLEANUP_AUTO_CONFIRM=yes — proceeding without prompt"
elif [ ! -t 0 ]; then
    read -r CONFIRM
else
    read -p "Are you sure you want to delete these secrets? (yes/no): " CONFIRM
fi

if [ "$CONFIRM" != "yes" ]; then
    print_info "Cleanup cancelled by user"
    exit 0
fi

# Function to delete secret
delete_secret() {
    local secret_name=$1
    
    # Check if secret exists
    if aws secretsmanager describe-secret --secret-id "$secret_name" --region "$AWS_REGION" &>/dev/null; then
        print_info "Deleting secret: $secret_name"
        
        # First, remove the deletion date (if scheduled for deletion)
        aws secretsmanager restore-secret \
            --secret-id "$secret_name" \
            --region "$AWS_REGION" \
            2>/dev/null || true
        
        # Delete the secret
        if aws secretsmanager delete-secret \
            --secret-id "$secret_name" \
            --region "$AWS_REGION" \
            --force-delete-without-recovery \
            >/dev/null 2>&1; then
            print_success "✅ Deleted secret: $secret_name"
            return 0
        else
            print_error "❌ Failed to delete secret: $secret_name"
            return 1
        fi
    else
        print_warning "Secret '$secret_name' does not exist, skipping..."
        return 0
    fi
}

# Delete all secrets
DELETED_COUNT=0
FAILED_COUNT=0

for secret in "${SECRETS[@]}"; do
    if delete_secret "$secret"; then
        DELETED_COUNT=$((DELETED_COUNT + 1))
    else
        FAILED_COUNT=$((FAILED_COUNT + 1))
    fi
done

echo ""
if [ $FAILED_COUNT -eq 0 ]; then
    print_success "🎉 Secret cleanup complete!"
    print_info "Successfully deleted $DELETED_COUNT secret(s)"
else
    print_warning "⚠️  Cleanup completed with errors"
    print_info "Deleted: $DELETED_COUNT, Failed: $FAILED_COUNT"
    exit 1
fi

print_info ""
print_info "📝 Note: If secrets were synced to Kubernetes via External Secrets Operator,"
print_info "   you may also want to delete the Kubernetes secrets:"
print_info "   kubectl delete secret trading-secrets -n <namespace>"
echo ""

