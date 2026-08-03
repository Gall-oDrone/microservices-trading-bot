#!/bin/bash

# Deploy the standalone Path A data-collector (EC2 + S3 archive + optional RDS).
#
# End-to-end, SSH-less flow:
#   1. Cross-compile the Go binary for linux/arm64.
#   2. Targeted `terraform apply` of VPC + data-collector modules only
#      (EKS/MSK/Redis are left out).
#   3. Stage the binary in S3 (deploy/ prefix) and pull it onto the instance
#      via SSM Run Command, then (re)start the systemd unit.
#   4. Verify the S3 and Postgres sinks end-to-end.
#
# Requires the data-collector-ec2 module built with enable_ssm=true (default),
# which grants the instance AmazonSSMManagedInstanceCore + s3:GetObject on
# deploy/*. No inbound ports are opened.

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

print_info()    { echo -e "${BLUE}[INFO]${NC} $1"; }
print_success() { echo -e "${GREEN}[SUCCESS]${NC} $1"; }
print_warning() { echo -e "${YELLOW}[WARNING]${NC} $1"; }
print_error()   { echo -e "${RED}[ERROR]${NC} $1"; }

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
TERRAFORM_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
REPO_ROOT="$(cd "$TERRAFORM_ROOT/../.." && pwd)"

case "${1:-}" in
    -h|--help)
        echo "Usage: $0 [environment]"
        echo ""
        echo "  environment   Terraform env under envs/<name> (default: development)"
        echo ""
        echo "Environment variables:"
        echo "  AWS_REGION              AWS region (default: us-east-1)"
        echo "  BITSO_BOOK              Book to verify in S3 path (default: btc_mxn)"
        echo "  DEPLOY_AUTO_CONFIRM=yes Skip the apply confirmation prompt"
        echo "  PLAN_ONLY=1             Show the targeted plan and exit (no apply)"
        echo "  SKIP_APPLY=1            Skip terraform apply; deploy binary + verify against existing infra"
        echo "  S3_FLUSH_WAIT_SECONDS   Seconds to wait for the first S3 flush (default: 120)"
        echo ""
        echo "Examples:"
        echo "  $0                                   # full deploy + verify (prompts before apply)"
        echo "  PLAN_ONLY=1 $0                       # review the plan only"
        echo "  DEPLOY_AUTO_CONFIRM=yes $0 development"
        echo "  SKIP_APPLY=1 $0                      # redeploy binary to a running instance"
        exit 0
        ;;
esac

ENVIRONMENT="${1:-development}"
AWS_REGION="${AWS_REGION:-us-east-1}"
BITSO_BOOK="${BITSO_BOOK:-btc_mxn}"
S3_FLUSH_WAIT_SECONDS="${S3_FLUSH_WAIT_SECONDS:-120}"
ENV_DIR="$TERRAFORM_ROOT/envs/${ENVIRONMENT}"
SVC_DIR="$REPO_ROOT/services/data-collector"
BINARY_PATH="/tmp/data-collector-${ENVIRONMENT}"
DEPLOY_KEY="deploy/data-collector"

# Resolved at runtime from Terraform outputs
BUCKET_NAME=""
INSTANCE_ID=""
RDS_SECRET_ARN=""
RDS_ENDPOINT=""
LAST_SSM_OUT=""
LAST_SSM_ERR=""

# Targeted modules for a data-collector-only apply
TF_TARGETS=(
    "-target=module.vpc"
    "-target=module.data_collector_ec2"
    "-target=module.data_archive_s3"
    "-target=module.data_collector_rds"
)

check_prerequisites() {
    print_info "Checking prerequisites..."
    for bin in aws terraform go; do
        if ! command -v "$bin" >/dev/null 2>&1; then
            print_error "$bin not found"
            exit 1
        fi
    done
    if ! aws sts get-caller-identity >/dev/null 2>&1; then
        print_error "AWS credentials not configured"
        exit 1
    fi
    if [ ! -d "$ENV_DIR" ]; then
        print_error "Terraform env dir not found: $ENV_DIR"
        exit 1
    fi
    if [ ! -d "$SVC_DIR" ]; then
        print_error "Service dir not found: $SVC_DIR"
        exit 1
    fi
    print_success "Prerequisites OK"
}

build_binary() {
    print_info "📦 Cross-compiling data-collector for linux/arm64..."
    ( cd "$SVC_DIR" && GOOS=linux GOARCH=arm64 go build -o "$BINARY_PATH" ./cmd )
    print_success "Built $(du -h "$BINARY_PATH" | awk '{print $1}') binary at $BINARY_PATH"
}

terraform_apply_targeted() {
    cd "$ENV_DIR"
    print_info "📦 Initializing Terraform..."
    terraform init -input=false >/dev/null

    print_info "📋 Planning (targeted: VPC + data-collector EC2 + S3 + RDS)..."
    terraform plan -input=false "${TF_TARGETS[@]}" -out=data-collector.tfplan

    if [ "${PLAN_ONLY:-}" = "1" ]; then
        print_success "PLAN_ONLY=1 — plan written to data-collector.tfplan, not applying"
        exit 0
    fi

    if [ "${DEPLOY_AUTO_CONFIRM:-}" != "yes" ]; then
        print_warning "About to APPLY the plan above. This creates billable resources (EC2 + RDS + S3)."
        local response=""
        if [ ! -t 0 ]; then
            read -r response
        else
            read -p "Type 'yes' to apply: " -r response
        fi
        if [ "$response" != "yes" ]; then
            print_info "Apply cancelled by user"
            rm -f data-collector.tfplan
            exit 0
        fi
    fi

    print_info "🚀 Applying (RDS creation can take 10-15 minutes)..."
    terraform apply -input=false data-collector.tfplan
    rm -f data-collector.tfplan
    print_success "Terraform apply complete"
}

read_outputs() {
    cd "$ENV_DIR"
    BUCKET_NAME=$(terraform output -raw data_archive_bucket 2>/dev/null || echo "")
    INSTANCE_ID=$(terraform output -raw data_collector_instance_id 2>/dev/null || echo "")
    RDS_SECRET_ARN=$(terraform output -raw data_collector_rds_secret_arn 2>/dev/null || echo "")
    RDS_ENDPOINT=$(terraform output -raw data_collector_rds_endpoint 2>/dev/null || echo "")

    for v in BUCKET_NAME INSTANCE_ID; do
        case "${!v}" in ""|null|*Warning*|*Error*)
            print_error "Could not resolve $v from Terraform outputs"; exit 1 ;;
        esac
    done
    print_info "  S3 bucket:    $BUCKET_NAME"
    print_info "  EC2 instance: $INSTANCE_ID"
    print_info "  RDS endpoint: ${RDS_ENDPOINT:-<disabled>}"
}

stage_binary_to_s3() {
    print_info "⬆️  Staging binary to s3://$BUCKET_NAME/$DEPLOY_KEY ..."
    aws s3 cp "$BINARY_PATH" "s3://$BUCKET_NAME/$DEPLOY_KEY" --region "$AWS_REGION" >/dev/null
    print_success "Binary staged in S3"
}

wait_for_ssm() {
    print_info "⏳ Waiting for instance to register with SSM..."
    local waited=0
    while [ $waited -lt 300 ]; do
        local ping
        ping=$(aws ssm describe-instance-information --region "$AWS_REGION" \
            --filters "Key=InstanceIds,Values=$INSTANCE_ID" \
            --query 'InstanceInformationList[0].PingStatus' --output text 2>/dev/null || echo "None")
        if [ "$ping" = "Online" ]; then
            print_success "Instance is Online in SSM"
            return 0
        fi
        sleep 10; waited=$((waited + 10))
        print_info "  ... still waiting for SSM (${waited}s)"
    done
    print_error "Instance did not come Online in SSM within 5 minutes"
    exit 1
}

# ssm_run <description> <remote-bash-script>. Populates LAST_SSM_OUT/LAST_SSM_ERR.
ssm_run() {
    local desc="$1"
    local script="$2"
    local b64 cid status waited
    b64=$(printf '%s' "$script" | base64 -w0)
    cid=$(aws ssm send-command \
        --instance-ids "$INSTANCE_ID" \
        --region "$AWS_REGION" \
        --document-name "AWS-RunShellScript" \
        --comment "$desc" \
        --parameters "commands=echo $b64 | base64 -d | bash" \
        --query 'Command.CommandId' --output text) || return 1

    waited=0
    status="Pending"
    while [ $waited -lt 300 ]; do
        status=$(aws ssm get-command-invocation --command-id "$cid" --instance-id "$INSTANCE_ID" \
            --region "$AWS_REGION" --query 'Status' --output text 2>/dev/null || echo "Pending")
        case "$status" in
            Success|Failed|Cancelled|TimedOut) break ;;
        esac
        sleep 5; waited=$((waited + 5))
    done
    LAST_SSM_OUT=$(aws ssm get-command-invocation --command-id "$cid" --instance-id "$INSTANCE_ID" \
        --region "$AWS_REGION" --query 'StandardOutputContent' --output text 2>/dev/null || echo "")
    LAST_SSM_ERR=$(aws ssm get-command-invocation --command-id "$cid" --instance-id "$INSTANCE_ID" \
        --region "$AWS_REGION" --query 'StandardErrorContent' --output text 2>/dev/null || echo "")
    [ "$status" = "Success" ]
}

deploy_binary() {
    print_info "🚚 Deploying binary onto instance via SSM..."
    local remote
    remote=$(cat <<REMOTE
set -euo pipefail
mkdir -p /opt/data-collector
aws s3 cp s3://$BUCKET_NAME/$DEPLOY_KEY /opt/data-collector/data-collector --region $AWS_REGION
chmod +x /opt/data-collector/data-collector
systemctl daemon-reload || true
systemctl enable data-collector.service || true
systemctl restart data-collector.service
sleep 4
systemctl is-active data-collector.service
REMOTE
)
    if ssm_run "deploy data-collector binary" "$remote"; then
        print_success "Service active: $(echo "$LAST_SSM_OUT" | tail -1)"
    else
        print_error "Binary deploy / service start failed"
        echo "--- stdout ---"; echo "$LAST_SSM_OUT"
        echo "--- stderr ---"; echo "$LAST_SSM_ERR"
        exit 1
    fi
}

verify_health() {
    print_info "🔍 Verifying /healthz and /metrics on the instance..."
    local remote
    remote=$(cat <<'REMOTE'
sleep 2
echo "HEALTHZ:"; curl -s --max-time 5 http://localhost:8085/healthz || echo "(no response)"
echo
echo "METRICS:"; curl -s --max-time 5 http://localhost:8085/metrics | grep -E 'data_collector_trades_received_total|data_collector_seconds_since_last_trade' | grep -v '^#' || echo "(no metrics)"
REMOTE
)
    if ssm_run "verify health/metrics" "$remote"; then
        echo "$LAST_SSM_OUT"
        print_success "Health/metrics reachable"
    else
        print_warning "Health check command failed"
        echo "$LAST_SSM_OUT"; echo "$LAST_SSM_ERR"
    fi
}

verify_s3() {
    print_info "🔍 Waiting ${S3_FLUSH_WAIT_SECONDS}s for first S3 Parquet flush, then checking bucket..."
    sleep "$S3_FLUSH_WAIT_SECONDS"
    local out
    out=$(aws s3 ls "s3://$BUCKET_NAME/trades/book=$BITSO_BOOK/" --recursive --region "$AWS_REGION" 2>/dev/null || echo "")
    if [ -n "$out" ]; then
        print_success "S3 archive objects found under trades/book=$BITSO_BOOK/:"
        echo "$out" | tail -5
    else
        print_warning "No S3 objects yet (low trade volume or flush interval not reached)."
        print_info "Re-check later: aws s3 ls s3://$BUCKET_NAME/trades/book=$BITSO_BOOK/ --recursive"
    fi
}

verify_postgres() {
    if [ -z "$RDS_SECRET_ARN" ] || [ "$RDS_SECRET_ARN" = "null" ]; then
        print_info "Postgres disabled (no RDS secret) — skipping hot-store verification"
        return 0
    fi
    print_info "🔍 Verifying Postgres trades/ws_gaps via SSM (installs psql on the instance)..."
    local remote
    remote=$(cat <<REMOTE
set -e
if ! command -v psql >/dev/null 2>&1; then
  sudo dnf install -y postgresql15 >/dev/null 2>&1 || sudo dnf install -y postgresql16 >/dev/null 2>&1 || sudo dnf install -y postgresql >/dev/null 2>&1 || true
fi
DSN=\$(aws secretsmanager get-secret-value --secret-id '$RDS_SECRET_ARN' --region $AWS_REGION --query SecretString --output text | jq -r '.dsn')
echo "TRADES_COUNT:"; psql "\$DSN" -tAc 'SELECT count(*) FROM trades;' 2>&1 || echo "(query failed)"
echo "GAPS_COUNT:";   psql "\$DSN" -tAc 'SELECT count(*) FROM ws_gaps;' 2>&1 || echo "(query failed)"
echo "LATEST_TRADES:"; psql "\$DSN" -c 'SELECT book,tid,price,amount,maker_side,exchange_ts FROM trades ORDER BY received_at DESC LIMIT 3;' 2>&1 || true
REMOTE
)
    if ssm_run "verify postgres sink" "$remote"; then
        echo "$LAST_SSM_OUT"
        print_success "Postgres hot store reachable"
    else
        print_warning "Postgres verification command failed"
        echo "$LAST_SSM_OUT"; echo "$LAST_SSM_ERR"
    fi
}

main() {
    print_info "🚀 Deploying data-collector to environment: $ENVIRONMENT (region $AWS_REGION)"
    echo ""
    check_prerequisites
    build_binary

    if [ "${SKIP_APPLY:-}" != "1" ]; then
        terraform_apply_targeted
    else
        print_info "SKIP_APPLY=1 — using existing infrastructure"
    fi

    read_outputs
    stage_binary_to_s3
    wait_for_ssm
    deploy_binary
    verify_health
    verify_s3
    verify_postgres

    echo ""
    print_success "🎉 data-collector deploy + verification complete!"
    print_info "Tear down later with: infrastructure/terraform/scripts/cleanup/cleanup-data-collector.sh $ENVIRONMENT"
}

main "$@"
