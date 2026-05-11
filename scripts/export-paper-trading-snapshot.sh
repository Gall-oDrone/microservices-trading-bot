#!/usr/bin/env bash
#
# Collect strategy-executor paper / organic state and upload JSON to S3.
# Requires: Go 1.22+, AWS credentials with s3:PutObject (and s3:CreateBucket if ensure-bucket).
#
# Usage:
#   AWS_REGION=us-east-1 ./scripts/export-paper-trading-snapshot.sh
#   STRATEGY_EXECUTOR_URL=http://127.0.0.1:8084 AWS_REGION=us-east-1 ./scripts/export-paper-trading-snapshot.sh
#
# EXPORT_PAPER_SNAPSHOT_TO_S3:
#   1 (recommended for uploads) — require AWS_REGION; upload snapshot to S3 (default reporter behavior).
#   0 — collect only; pass -dry-run (JSON to stdout, no S3).
#   unset — same as historical behavior: upload when AWS_REGION is set; otherwise the Go tool errors on upload.
#
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT/services/paper-trading-reporter"

EXTRA=()
if [[ "${EXPORT_PAPER_SNAPSHOT_TO_S3:-}" == "0" ]]; then
  EXTRA+=(-dry-run)
elif [[ "${EXPORT_PAPER_SNAPSHOT_TO_S3:-}" == "1" ]]; then
  if [[ -z "${AWS_REGION:-}" ]]; then
    echo "error: EXPORT_PAPER_SNAPSHOT_TO_S3=1 requires AWS_REGION to be set" >&2
    exit 2
  fi
fi

exec go run ./cmd/paper-trading-reporter "${EXTRA[@]}" "$@"
