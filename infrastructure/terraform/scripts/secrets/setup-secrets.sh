#!/bin/bash
# Setup script for creating secrets in AWS Secrets Manager
# Run AFTER Terraform is deployed. Creates secrets that External Secrets Operator syncs to Kubernetes.
# Required for k8s ExternalSecret: trading-bot/bitso-api-key, trading-bot/bitso-api-secret, trading-bot/redis-password
# (redis-password is always created so ESO sync succeeds; use empty string if not using Redis auth)

set -e

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

print_info()    { echo -e "${BLUE}[INFO]${NC} $1"; }
print_success() { echo -e "${GREEN}[SUCCESS]${NC} $1"; }
print_warning() { echo -e "${YELLOW}[WARNING]${NC} $1"; }
print_error()   { echo -e "${RED}[ERROR]${NC} $1"; }

validate_secret_value() {
  local secret_value=$1
  local secret_name=$2
  local allow_empty=${3:-false}
  if [ -z "$secret_value" ]; then
    if [ "$allow_empty" = "true" ]; then return 0; fi
    print_error "$secret_name cannot be empty"
    return 1
  fi
  local length=${#secret_value}
  if [ "$length" -gt 65536 ]; then
    print_error "$secret_name exceeds AWS Secrets Manager limit of 65536 characters"
    return 1
  fi
  return 0
}

# ---------------------------------------------------------------------------
# Configuration
# ---------------------------------------------------------------------------
BITSO_KEY=""
BITSO_SECRET=""
REDIS_PASSWORD=""
AWS_REGION="${AWS_REGION:-us-east-1}"
SECRET_PREFIX="trading-bot"

print_info "Setting up secrets in AWS Secrets Manager (region: $AWS_REGION, prefix: $SECRET_PREFIX)..."

if ! command -v aws &>/dev/null; then
  print_error "AWS CLI is not installed."
  exit 1
fi
if ! aws sts get-caller-identity &>/dev/null; then
  print_error "AWS credentials not configured."
  exit 1
fi
print_success "AWS CLI configured"

# ---------------------------------------------------------------------------
# Prompt for Bitso credentials
# ---------------------------------------------------------------------------
if [ -z "$BITSO_KEY" ]; then
  read -sp "Enter Bitso API Key: " BITSO_KEY; echo
  if [ -z "$BITSO_KEY" ]; then print_error "Bitso API Key cannot be empty"; exit 1; fi
fi
if [ -z "$BITSO_SECRET" ]; then
  read -sp "Enter Bitso API Secret: " BITSO_SECRET; echo
  if [ -z "$BITSO_SECRET" ]; then print_error "Bitso API Secret cannot be empty"; exit 1; fi
fi
if [ -z "$REDIS_PASSWORD" ]; then
  read -sp "Enter Redis Password (press Enter to leave empty for ESO sync): " REDIS_PASSWORD; echo
  print_info "Redis password will be stored as empty if skipped (ExternalSecret still syncs)."
fi

# ---------------------------------------------------------------------------
# Create or update secret in AWS Secrets Manager
# ---------------------------------------------------------------------------
create_or_update_secret() {
  local secret_name=$1
  local secret_value=$2
  local description=$3
  local allow_empty=${4:-false}
  if ! validate_secret_value "$secret_value" "$secret_name" "$allow_empty"; then
    return 1
  fi
  print_info "Creating/updating secret: $secret_name"
  if aws secretsmanager describe-secret --secret-id "$secret_name" --region "$AWS_REGION" &>/dev/null; then
    aws secretsmanager update-secret \
      --secret-id "$secret_name" \
      --secret-string "$secret_value" \
      --region "$AWS_REGION" \
      --description "$description" >/dev/null || { print_error "Failed to update $secret_name"; return 1; }
    print_success "Updated secret: $secret_name"
  else
    aws secretsmanager create-secret \
      --name "$secret_name" \
      --secret-string "$secret_value" \
      --region "$AWS_REGION" \
      --description "$description" >/dev/null || { print_error "Failed to create $secret_name"; return 1; }
    print_success "Created secret: $secret_name"
  fi
  return 0
}

# Required: Bitso API Key and Secret
create_or_update_secret "${SECRET_PREFIX}/bitso-api-key"   "$BITSO_KEY"    "Bitso API Key for trading bot"    || exit 1
create_or_update_secret "${SECRET_PREFIX}/bitso-api-secret" "$BITSO_SECRET" "Bitso API Secret for trading bot" || exit 1

# Required for ExternalSecret sync: always create redis-password (empty ok)
create_or_update_secret "${SECRET_PREFIX}/redis-password"  "${REDIS_PASSWORD:-}" "Redis password for trading bot (empty if none)" "true" || exit 1

# ---------------------------------------------------------------------------
# Verify
# ---------------------------------------------------------------------------
print_info "Verifying secrets..."
for secret in "${SECRET_PREFIX}/bitso-api-key" "${SECRET_PREFIX}/bitso-api-secret" "${SECRET_PREFIX}/redis-password"; do
  if aws secretsmanager describe-secret --secret-id "$secret" --region "$AWS_REGION" &>/dev/null; then
    print_success "Verified: $secret"
  else
    print_error "Failed to verify: $secret"
    exit 1
  fi
done

echo ""
print_success "Secret setup complete. External Secrets Operator can sync to Kubernetes."
print_info "Verify in cluster: kubectl get externalsecret,secret -n bitso-trading-dev"
echo ""
