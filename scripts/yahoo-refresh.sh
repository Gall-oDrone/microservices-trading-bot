#!/usr/bin/env bash
#
# Refresh the compacted Yahoo Finance files that daily-research (and anything
# else) reads: sync the S3 partitions, run yahoo-compact, check the result,
# and, only with --upload, replace the two compacted objects in S3.
#
# S3 cannot append, so new daily partitions are invisible to the compacted
# files until this is re-run. It is safe to run on a schedule: without
# --upload it never writes to S3, and with --upload it refuses to overwrite
# when the new build looks worse than the last one it uploaded.
#
# Usage:
#   ./scripts/yahoo-refresh.sh [--upload] [--work-dir DIR]
#
# Gate (all must hold before --upload writes anything):
#   - no source file failed to parse, for either dataset
#   - unique rows did not DROP versus the last upload recorded in
#     $WORK_DIR/last-upload.json (a drop means the sync or the dedup broke)
#
# Environment:
#   AWS              aws CLI to use (default: ./.tools/bin/aws if present, else aws)
#   PRICES_URI       default s3://test-financial-stocks-bucket/stocks/crypto/
#   NEWS_URI         default s3://test-financial-news-bucket/news/transformed/crypto/agentic=true/
#   PRICES_DEST      default s3://test-financial-stocks-bucket/stocks/compacted/crypto/
#   NEWS_DEST        default s3://test-financial-news-bucket/news/compacted/crypto/agentic=true/
#
# The first full price sync is ~100k objects (~1 GB, ~15 min); later syncs
# only fetch new partitions.
#
set -euo pipefail

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; BLUE='\033[0;34m'; NC='\033[0m'
info() { echo -e "${BLUE}[INFO]${NC} $1"; }
ok()   { echo -e "${GREEN}[ OK ]${NC} $1"; }
warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }
die()  { echo -e "${RED}[FAIL]${NC} $1" >&2; exit 1; }

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
UPLOAD=0
WORK_DIR="${REPO_ROOT}/.yahoo-refresh"
while [[ $# -gt 0 ]]; do
  case "$1" in
    --upload) UPLOAD=1; shift ;;
    --work-dir) WORK_DIR="$2"; shift 2 ;;
    -h|--help) sed -n '2,30p' "$0"; exit 0 ;;
    *) die "unknown argument: $1" ;;
  esac
done

if [[ -z "${AWS:-}" ]]; then
  if [[ -x "${REPO_ROOT}/.tools/bin/aws" ]]; then AWS="${REPO_ROOT}/.tools/bin/aws"; else AWS="aws"; fi
fi
PRICES_URI="${PRICES_URI:-s3://test-financial-stocks-bucket/stocks/crypto/}"
NEWS_URI="${NEWS_URI:-s3://test-financial-news-bucket/news/transformed/crypto/agentic=true/}"
PRICES_DEST="${PRICES_DEST:-s3://test-financial-stocks-bucket/stocks/compacted/crypto/}"
NEWS_DEST="${NEWS_DEST:-s3://test-financial-news-bucket/news/compacted/crypto/agentic=true/}"
command -v jq >/dev/null || die "jq is required"

SRC="${WORK_DIR}/src"; OUT="${WORK_DIR}/out"; STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
REPORT_JSON="${WORK_DIR}/report-${STAMP}.json"; REPORT_TXT="${WORK_DIR}/report-${STAMP}.txt"
mkdir -p "${SRC}/prices" "${SRC}/news" "${OUT}"

info "syncing ${PRICES_URI}"
"${AWS}" s3 sync "${PRICES_URI}" "${SRC}/prices" --only-show-errors
info "syncing ${NEWS_URI}"
"${AWS}" s3 sync "${NEWS_URI}" "${SRC}/news" --only-show-errors

info "compacting"
(cd "${REPO_ROOT}/services/strategy-executor" && go run ./cmd/yahoo-compact \
  -prices-dir "${SRC}/prices" -news-dir "${SRC}/news" \
  -prices-uri "${PRICES_URI}" -news-uri "${NEWS_URI}" \
  -out-dir "${OUT}" -report "${REPORT_JSON}") | tee "${REPORT_TXT}"

# ---- gate -------------------------------------------------------------------
failed=$(jq '[.[].read.files_failed] | add' "${REPORT_JSON}")
[[ "${failed}" == "0" ]] || die "${failed} source file(s) failed to parse; see ${REPORT_TXT}"

current=$(jq -c 'map({(.dataset): .dedup.unique_rows}) | add' "${REPORT_JSON}")
LAST="${WORK_DIR}/last-upload.json"
if [[ -f "${LAST}" ]]; then
  shrunk=$(jq -rn --argjson a "$(cat "${LAST}")" --argjson b "${current}" \
    '[$a | to_entries[] | select(($b[.key] // 0) < .value) | "\(.key): \(.value) -> \($b[.key] // 0)"] | join(", ")')
  [[ -z "${shrunk}" ]] || die "unique rows dropped since last upload (${shrunk}); not uploading"
  ok "no dataset shrank since last upload ($(cat "${LAST}") -> ${current})"
else
  warn "no ${LAST} yet: first run, nothing to compare against"
fi

if [[ "${UPLOAD}" -ne 1 ]]; then
  ok "built ${OUT}; dry run, S3 untouched (re-run with --upload to publish)"
  exit 0
fi

info "uploading"
"${AWS}" s3 cp "${OUT}/yahoo_crypto_daily.parquet" "${PRICES_DEST}" --only-show-errors
"${AWS}" s3 cp "${OUT}/news_crypto_agentic.parquet" "${NEWS_DEST}" --only-show-errors
echo "${current}" > "${LAST}"
ok "uploaded; recorded ${current} in ${LAST}"
