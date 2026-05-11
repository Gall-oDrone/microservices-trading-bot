#!/usr/bin/env bash
#
# Collect strategy-executor paper / organic state and upload JSON to S3.
# Requires: Go 1.22+, AWS credentials with s3:PutObject (and s3:CreateBucket if ensure-bucket).
#
# Usage:
#   AWS_REGION=us-east-1 ./scripts/export-paper-trading-snapshot.sh
#   STRATEGY_EXECUTOR_URL=http://127.0.0.1:8084 AWS_REGION=us-east-1 ./scripts/export-paper-trading-snapshot.sh
#
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT/services/paper-trading-reporter"
exec go run ./cmd/paper-trading-reporter "$@"
