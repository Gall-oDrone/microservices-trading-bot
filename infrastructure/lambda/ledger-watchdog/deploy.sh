#!/usr/bin/env bash
# Deploys (or removes) the off-machine ledger watchdog: CloudFormation stack mtb-ledger-watchdog
# (Lambda + IAM role + hourly EventBridge rule + log group), see README.md.
#
#   infrastructure/lambda/ledger-watchdog/deploy.sh --bucket <archive bucket> --topic-arn <arn>
#   … --test        after deploying, also publish a test message through the Lambda
#   … --delete      delete the stack (the packaged zip under lambda-artifacts/ is left)
#
# Every deploy runs the unit tests first and ends with one {"dry_run": true} invocation.
#
# Options: --ledgers name=prefix[,…] (default stage=daily-executor/stage), --max-age-hours N (30),
# --region R (us-east-1). The code zip is uploaded to s3://<bucket>/lambda-artifacts/ledger-watchdog/
# by `aws cloudformation package`. Unit tests run first (python3 -m unittest).
set -euo pipefail
export AWS_PAGER=""

HERE="$(cd "$(dirname "$0")" && pwd)"
STACK=mtb-ledger-watchdog
bucket="" topic="" ledgers="stage=daily-executor/stage" max_age=30 region=us-east-1 test=0 delete=0
while [ $# -gt 0 ]; do
  case "$1" in
    --bucket) bucket="$2"; shift 2 ;;
    --topic-arn) topic="$2"; shift 2 ;;
    --ledgers) ledgers="$2"; shift 2 ;;
    --max-age-hours) max_age="$2"; shift 2 ;;
    --region) region="$2"; shift 2 ;;
    --test) test=1; shift ;;
    --delete) delete=1; shift ;;
    -h | --help) sed -n '2,13p' "$0"; exit 0 ;;
    *) echo "unknown option $1" >&2; exit 2 ;;
  esac
done

if [ "$delete" = 1 ]; then
  aws cloudformation delete-stack --region "$region" --stack-name "$STACK"
  aws cloudformation wait stack-delete-complete --region "$region" --stack-name "$STACK"
  echo "deleted stack $STACK"
  exit 0
fi
if [ -z "$bucket" ] || [ -z "$topic" ]; then
  echo "usage: $0 --bucket <archive bucket> --topic-arn <sns topic arn> [--test]" >&2
  exit 2
fi
IFS=, read -ra entries <<<"$ledgers"
for e in "${entries[@]}"; do
  case "${e#*=}" in
    daily-executor/?*) ;;
    *) echo "--ledgers $e: the prefix must be under daily-executor/ (the role can read only there)" >&2; exit 2 ;;
  esac
done

echo "== unit tests"
python3 -m unittest discover -s "$HERE" -q

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
echo "== package (s3://$bucket/lambda-artifacts/ledger-watchdog/)"
(cd "$HERE" && aws cloudformation package --region "$region" --template-file template.yaml \
  --s3-bucket "$bucket" --s3-prefix lambda-artifacts/ledger-watchdog \
  --output-template-file "$tmp/packaged.yaml" >/dev/null)

echo "== deploy stack $STACK"
aws cloudformation deploy --region "$region" --stack-name "$STACK" \
  --template-file "$tmp/packaged.yaml" --capabilities CAPABILITY_NAMED_IAM \
  --no-fail-on-empty-changeset \
  --parameter-overrides "Bucket=$bucket" "TopicArn=$topic" "Ledgers=$ledgers" "MaxAgeHours=$max_age"

echo "== invoke {\"dry_run\": true}"
aws lambda invoke --region "$region" --function-name "$STACK" --cli-binary-format raw-in-base64-out \
  --payload '{"dry_run": true}' "$tmp/out.json" >/dev/null
cat "$tmp/out.json"; echo
if [ "$test" = 1 ]; then
  echo "== invoke {\"test\": true}"
  aws lambda invoke --region "$region" --function-name "$STACK" --cli-binary-format raw-in-base64-out \
    --payload '{"test": true}' "$tmp/out.json" >/dev/null
  cat "$tmp/out.json"; echo
fi
