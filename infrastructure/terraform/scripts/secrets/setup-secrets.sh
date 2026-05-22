#!/bin/bash
# Setup script for creating secrets in AWS Secrets Manager
# This script should be run AFTER Terraform infrastructure is deployed
# It creates secrets that External Secrets Operator will sync to Kubernetes

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

# Function to validate secret value
validate_secret_value() {
    local secret_value=$1
    local secret_name=$2
    local allow_empty=${3:-false}
    
    # Check if empty (skip if empty values are allowed)
    if [ -z "$secret_value" ]; then
        if [ "$allow_empty" = "true" ]; then
            return 0
        fi
        print_error "$secret_name cannot be empty"
        return 1
    fi
    
    # Check for null bytes (can cause issues)
    # Note: bash strips null bytes in variables, but we check anyway for safety
    # Use a more reliable method: check if the string contains the null byte pattern
    local contains_null=false
    # Check using od (octal dump) which can detect null bytes
    if printf "%s" "$secret_value" | od -An -tx1 | grep -q " 00 "; then
        contains_null=true
    fi
    # Also check length consistency as a secondary check
    local printf_length=$(printf "%s" "$secret_value" | wc -c)
    local bash_length=${#secret_value}
    
    if [ "$contains_null" = "true" ] || [ "$printf_length" -ne "$bash_length" ]; then
        print_error "$secret_name contains null bytes or invalid characters"
        print_error "  Bash length: ${bash_length}, Printf length: ${printf_length}"
        return 1
    fi
    
    # Check length (AWS Secrets Manager has limits)
    local length=${#secret_value}
    if [ $length -gt 65536 ]; then
        print_error "$secret_name exceeds AWS Secrets Manager limit of 65536 characters"
        return 1
    fi
    
    return 0
}

# ============================================================================
# CONFIGURATION VARIABLES
# ============================================================================
# Set these variables at the top of the script, or leave empty to be prompted
BITSO_KEY=""
BITSO_SECRET=""
REDIS_PASSWORD=""
CLAUDE_KEY=""
CLAUDE_SECRET=""
OPENAI_KEY=""
OPENAI_SECRET=""
ETORO_PUBLIC_KEY=""
ETORO_PRIVATE_KEY=""

# AWS Configuration
AWS_REGION="${AWS_REGION:-us-east-1}"
SECRET_PREFIX="trading-bot"

# ============================================================================
# SCRIPT LOGIC
# ============================================================================

print_info "🔐 Setting up secrets in AWS Secrets Manager..."
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

# Prompt for BITSO_KEY if not set
if [ -z "$BITSO_KEY" ]; then
    print_info "Bitso API Key not set in script variables."
    read -sp "Enter Bitso API Key: " BITSO_KEY
    echo
    if [ -z "$BITSO_KEY" ]; then
        print_error "Bitso API Key cannot be empty"
        exit 1
    fi
else
    print_info "Using Bitso API Key from script variables"
fi

# Prompt for BITSO_SECRET if not set
if [ -z "$BITSO_SECRET" ]; then
    print_info "Bitso API Secret not set in script variables."
    read -sp "Enter Bitso API Secret: " BITSO_SECRET
    echo
    if [ -z "$BITSO_SECRET" ]; then
        print_error "Bitso API Secret cannot be empty"
        exit 1
    fi
else
    print_info "Using Bitso API Secret from script variables"
fi

# Prompt for REDIS_PASSWORD if not set (optional)
if [ -z "$REDIS_PASSWORD" ]; then
    print_info "Redis password not set in script variables (optional)."
    read -sp "Enter Redis Password (press Enter to skip): " REDIS_PASSWORD
    echo
    if [ -z "$REDIS_PASSWORD" ]; then
        print_warning "Redis password will be left empty"
    fi
else
    print_info "Using Redis password from script variables"
fi

# Prompt for CLAUDE_KEY if not set
if [ -z "$CLAUDE_KEY" ]; then
    print_info "Claude API Key not set in script variables."
    read -sp "Enter Claude API Key: " CLAUDE_KEY
    echo
    if [ -z "$CLAUDE_KEY" ]; then
        print_error "Claude API Key cannot be empty"
        exit 1
    fi
else
    print_info "Using Claude API Key from script variables"
fi

# Prompt for CLAUDE_SECRET if not set
if [ -z "$CLAUDE_SECRET" ]; then
    print_info "Claude API Secret not set in script variables."
    read -sp "Enter Claude API Secret: " CLAUDE_SECRET
    echo
    if [ -z "$CLAUDE_SECRET" ]; then
        print_error "Claude API Secret cannot be empty"
        exit 1
    fi
else
    print_info "Using Claude API Secret from script variables"
fi

# Prompt for OPENAI_KEY if not set
if [ -z "$OPENAI_KEY" ]; then
    print_info "OpenAI API Key not set in script variables."
    read -sp "Enter OpenAI API Key: " OPENAI_KEY
    echo
    if [ -z "$OPENAI_KEY" ]; then
        print_error "OpenAI API Key cannot be empty"
        exit 1
    fi
else
    print_info "Using OpenAI API Key from script variables"
fi

# Prompt for OPENAI_SECRET if not set
if [ -z "$OPENAI_SECRET" ]; then
    print_info "OpenAI API Secret not set in script variables."
    read -sp "Enter OpenAI API Secret: " OPENAI_SECRET
    echo
    if [ -z "$OPENAI_SECRET" ]; then
        print_error "OpenAI API Secret cannot be empty"
        exit 1
    fi
else
    print_info "Using OpenAI API Secret from script variables"
fi

# Prompt for ETORO_PUBLIC_KEY if not set (optional; x-api-key / public API key)
if [ -z "$ETORO_PUBLIC_KEY" ]; then
    print_info "eToro Public Key not set in script variables (optional)."
    read -sp "Enter eToro Public Key (press Enter to skip): " ETORO_PUBLIC_KEY
    echo
    if [ -z "$ETORO_PUBLIC_KEY" ]; then
        print_warning "eToro Public Key will be left empty (secret not created)"
    fi
else
    print_info "Using eToro Public Key from script variables"
fi

# Prompt for ETORO_PRIVATE_KEY if not set (optional; x-user-key)
if [ -z "$ETORO_PRIVATE_KEY" ]; then
    print_info "eToro Private Key not set in script variables (optional)."
    read -sp "Enter eToro Private Key (press Enter to skip): " ETORO_PRIVATE_KEY
    echo
    if [ -z "$ETORO_PRIVATE_KEY" ]; then
        print_warning "eToro Private Key will be left empty (secret not created)"
    fi
else
    print_info "Using eToro Private Key from script variables"
fi

# Function to create or update secret
create_or_update_secret() {
    local secret_name=$1
    local secret_value=$2
    local description=$3
    
    print_info "Creating/updating secret: $secret_name"
    
    # Validate secret value
    if ! validate_secret_value "$secret_value" "$secret_name"; then
        return 1
    fi
    
    # Check if secret already exists
    if aws secretsmanager describe-secret --secret-id "$secret_name" --region "$AWS_REGION" 1>/dev/null 2>&1; then
        print_warning "Secret '$secret_name' already exists. Updating..."
        if ! aws secretsmanager update-secret \
            --secret-id "$secret_name" \
            --secret-string "$secret_value" \
            --region "$AWS_REGION" \
            --description "$description" \
            1>/dev/null 2>&1; then
            print_error "Failed to update secret: $secret_name"
            return 1
        fi
        print_success "✅ Updated secret: $secret_name"
    else
        if ! aws secretsmanager create-secret \
            --name "$secret_name" \
            --secret-string "$secret_value" \
            --region "$AWS_REGION" \
            --description "$description" \
            1>/dev/null 2>&1; then
            print_error "Failed to create secret: $secret_name"
            # Show the actual error from AWS CLI
            print_error "AWS CLI error details:"
            aws secretsmanager create-secret \
                --name "$secret_name" \
                --secret-string "$secret_value" \
                --region "$AWS_REGION" \
                --description "$description" 2>&1 | sed 's/^/  /' || true
            return 1
        fi
        print_success "✅ Created secret: $secret_name"
    fi
    return 0
}

# Create Bitso API Key secret
if ! create_or_update_secret \
    "${SECRET_PREFIX}/bitso-api-key" \
    "$BITSO_KEY" \
    "Bitso API Key for trading bot"; then
    print_error "Failed to create/update Bitso API Key secret"
    exit 1
fi

# Create Bitso API Secret
if ! create_or_update_secret \
    "${SECRET_PREFIX}/bitso-api-secret" \
    "$BITSO_SECRET" \
    "Bitso API Secret for trading bot"; then
    print_error "Failed to create/update Bitso API Secret"
    exit 1
fi

# Create Redis password secret (only if provided)
if [ -n "$REDIS_PASSWORD" ]; then
    if ! create_or_update_secret \
        "${SECRET_PREFIX}/redis-password" \
        "$REDIS_PASSWORD" \
        "Redis password for trading bot"; then
        print_error "Failed to create/update Redis password secret"
        exit 1
    fi
else
    print_info "Skipping Redis password secret (not provided)"
fi

# Create Claude API Key secret
if ! create_or_update_secret \
    "${SECRET_PREFIX}/claude-api-key" \
    "$CLAUDE_KEY" \
    "Claude API Key for trading bot"; then
    print_error "Failed to create/update Claude API Key secret"
    exit 1
fi

# Create Claude API Secret
if ! create_or_update_secret \
    "${SECRET_PREFIX}/claude-api-secret" \
    "$CLAUDE_SECRET" \
    "Claude API Secret for trading bot"; then
    print_error "Failed to create/update Claude API Secret"
    exit 1
fi

# Create OpenAI API Key secret
if ! create_or_update_secret \
    "${SECRET_PREFIX}/openai-api-key" \
    "$OPENAI_KEY" \
    "OpenAI API Key for trading bot"; then
    print_error "Failed to create/update OpenAI API Key secret"
    exit 1
fi

# Create OpenAI API Secret
if ! create_or_update_secret \
    "${SECRET_PREFIX}/openai-api-secret" \
    "$OPENAI_SECRET" \
    "OpenAI API Secret for trading bot"; then
    print_error "Failed to create/update OpenAI API Secret"
    exit 1
fi

# Create eToro Public Key secret (only if provided)
if [ -n "$ETORO_PUBLIC_KEY" ]; then
    if ! create_or_update_secret \
        "${SECRET_PREFIX}/etoro-api-key" \
        "$ETORO_PUBLIC_KEY" \
        "eToro public API key (x-api-key) for trading bot"; then
        print_error "Failed to create/update eToro Public Key secret"
        exit 1
    fi
else
    print_info "Skipping eToro Public Key secret (not provided)"
fi

# Create eToro Private Key secret (only if provided)
if [ -n "$ETORO_PRIVATE_KEY" ]; then
    if ! create_or_update_secret \
        "${SECRET_PREFIX}/etoro-user-key" \
        "$ETORO_PRIVATE_KEY" \
        "eToro user key (x-user-key) for trading bot"; then
        print_error "Failed to create/update eToro Private Key secret"
        exit 1
    fi
else
    print_info "Skipping eToro Private Key secret (not provided)"
fi

# Verify secrets were created
print_info "📋 Verifying secrets..."
SECRETS_CREATED=0
VERIFICATION_FAILED=0

for secret in \
    "${SECRET_PREFIX}/bitso-api-key" \
    "${SECRET_PREFIX}/bitso-api-secret" \
    "${SECRET_PREFIX}/claude-api-key" \
    "${SECRET_PREFIX}/claude-api-secret" \
    "${SECRET_PREFIX}/openai-api-key" \
    "${SECRET_PREFIX}/openai-api-secret"; do
    if aws secretsmanager describe-secret --secret-id "$secret" --region "$AWS_REGION" 1>/dev/null 2>&1; then
        print_success "✅ Verified: $secret"
        SECRETS_CREATED=$((SECRETS_CREATED + 1))
    else
        print_error "❌ Failed to verify: $secret"
        VERIFICATION_FAILED=1
        # Show the actual error
        print_error "Verification error details:"
        aws secretsmanager describe-secret --secret-id "$secret" --region "$AWS_REGION" 2>&1 | sed 's/^/  /' || true
    fi
done

if [ -n "$REDIS_PASSWORD" ]; then
    if aws secretsmanager describe-secret --secret-id "${SECRET_PREFIX}/redis-password" --region "$AWS_REGION" 1>/dev/null 2>&1; then
        print_success "✅ Verified: ${SECRET_PREFIX}/redis-password"
        SECRETS_CREATED=$((SECRETS_CREATED + 1))
    else
        print_error "❌ Failed to verify: ${SECRET_PREFIX}/redis-password"
        VERIFICATION_FAILED=1
        # Show the actual error
        print_error "Verification error details:"
        aws secretsmanager describe-secret --secret-id "${SECRET_PREFIX}/redis-password" --region "$AWS_REGION" 2>&1 | sed 's/^/  /' || true
    fi
fi

if [ -n "$ETORO_PUBLIC_KEY" ]; then
    if aws secretsmanager describe-secret --secret-id "${SECRET_PREFIX}/etoro-api-key" --region "$AWS_REGION" 1>/dev/null 2>&1; then
        print_success "✅ Verified: ${SECRET_PREFIX}/etoro-api-key"
        SECRETS_CREATED=$((SECRETS_CREATED + 1))
    else
        print_error "❌ Failed to verify: ${SECRET_PREFIX}/etoro-api-key"
        VERIFICATION_FAILED=1
        print_error "Verification error details:"
        aws secretsmanager describe-secret --secret-id "${SECRET_PREFIX}/etoro-api-key" --region "$AWS_REGION" 2>&1 | sed 's/^/  /' || true
    fi
fi

if [ -n "$ETORO_PRIVATE_KEY" ]; then
    if aws secretsmanager describe-secret --secret-id "${SECRET_PREFIX}/etoro-user-key" --region "$AWS_REGION" 1>/dev/null 2>&1; then
        print_success "✅ Verified: ${SECRET_PREFIX}/etoro-user-key"
        SECRETS_CREATED=$((SECRETS_CREATED + 1))
    else
        print_error "❌ Failed to verify: ${SECRET_PREFIX}/etoro-user-key"
        VERIFICATION_FAILED=1
        print_error "Verification error details:"
        aws secretsmanager describe-secret --secret-id "${SECRET_PREFIX}/etoro-user-key" --region "$AWS_REGION" 2>&1 | sed 's/^/  /' || true
    fi
fi

# Exit with error if verification failed
if [ $VERIFICATION_FAILED -eq 1 ]; then
    print_error "One or more secrets failed verification. Please check the errors above."
    exit 1
fi

echo ""
print_success "🎉 Secret setup complete!"
print_info "Created/updated $SECRETS_CREATED secret(s) in AWS Secrets Manager"
print_info ""
print_warning "⚠️  IMPORTANT: GitHub Actions Role ARN Configuration Required"
print_info ""
print_info "To enable GitHub Actions workflows to authenticate with AWS, you MUST manually"
print_info "copy the IAM role ARN to GitHub Environment Secrets:"
print_info ""
print_info "1. Get the role ARN from Terraform output:"
print_info "   cd infrastructure/terraform/envs/development"
print_info "   terraform output ci_role_arn"
print_info ""
print_info "2. Copy the ARN value (e.g., arn:aws:iam::ACCOUNT_ID:role/mtb-development-github-actions)"
print_info ""
print_info "3. Set it in GitHub:"
print_info "   - Go to: GitHub → Settings → Secrets and variables → Actions"
print_info "   - Click: 'New repository secret' (or add to Environment secrets)"
print_info "   - Name: AWS_GITHUB_ACTIONS_ROLE_ARN"
print_info "   - Value: <paste the ARN from step 1>"
print_info "   - Click: 'Add secret'"
print_info ""
print_info "4. Also set AWS_ACCOUNT_ID secret:"
print_info "   - Name: AWS_ACCOUNT_ID"
print_info "   - Value: <your AWS account ID>"
print_info ""
print_info "⚠️  Without these secrets, GitHub Actions workflows will fail to authenticate!"
print_info ""
print_info "📝 Additional next steps:"
print_info "  1. Deploy your Kubernetes manifests (they will use External Secrets Operator)"
print_info "  2. Verify secrets are synced: kubectl get externalsecret -n <namespace>"
print_info "  3. Check Kubernetes secrets: kubectl get secret trading-secrets -n <namespace>"
echo ""

