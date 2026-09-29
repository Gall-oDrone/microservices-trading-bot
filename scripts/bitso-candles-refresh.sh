#!/usr/bin/env bash
#
# Fetch Bitso's daily candles (cmd/bitso-daily) for one or more books and,
# only with --upload, publish them to S3 as one CSV per book. The CSVs are read
# directly by cmd/daily-research (-prices <dir>) and cmd/execution-check.
#
# Safe to run on a schedule: without --upload it never writes to S3, and with
# --upload it refuses to overwrite the published file when the new fetch looks
# worse than what is already there.
#
# Usage:
#   ./scripts/bitso-candles-refresh.sh [--upload] [--force] [--books "btc_mxn btc_usd"] [--work-dir DIR]
#
# Gate, per book, against the currently published object (skipped if none):
#   - the new file must not have FEWER rows
#   - no bar older than 3 days may have changed (Bitso does not revise closed
#     candles; a change means a fetch or labelling bug). --force overrides.
#
# Environment:
#   AWS       aws CLI to use (default: ./.tools/bin/aws if present, else aws)
#   DEST      default s3://test-financial-stocks-bucket/stocks/compacted/bitso/daily/
#   FROM      first date to fetch (default 2017-06-01; Bitso returns what exists)
#
set -euo pipefail

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; BLUE='\033[0;34m'; NC='\033[0m'
info() { echo -e "${BLUE}[INFO]${NC} $1"; }
ok()   { echo -e "${GREEN}[ OK ]${NC} $1"; }
warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }
die()  { echo -e "${RED}[FAIL]${NC} $1" >&2; exit 1; }

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
UPLOAD=0; FORCE=0; BOOKS="btc_mxn btc_usd"; WORK_DIR="${REPO_ROOT}/.bitso-refresh"
while [[ $# -gt 0 ]]; do
  case "$1" in
    --upload) UPLOAD=1; shift ;;
    --force) FORCE=1; shift ;;
    --books) BOOKS="$2"; shift 2 ;;
    --work-dir) WORK_DIR="$2"; shift 2 ;;
    -h|--help) sed -n '2,25p' "$0"; exit 0 ;;
    *) die "unknown argument: $1" ;;
  esac
done
if [[ -z "${AWS:-}" ]]; then
  if [[ -x "${REPO_ROOT}/.tools/bin/aws" ]]; then AWS="${REPO_ROOT}/.tools/bin/aws"; else AWS="aws"; fi
fi
DEST="${DEST:-s3://test-financial-stocks-bucket/stocks/compacted/bitso/daily/}"
FROM="${FROM:-2017-06-01}"
command -v python3 >/dev/null || die "python3 is required"
mkdir -p "${WORK_DIR}/new" "${WORK_DIR}/published"

for book in ${BOOKS}; do
  new="${WORK_DIR}/new/${book}.csv"; old="${WORK_DIR}/published/${book}.csv"
  info "fetching ${book}"
  (cd "${REPO_ROOT}/services/strategy-executor" && go run ./cmd/bitso-daily -book "${book}" -from "${FROM}" -out "${new}")

  rm -f "${old}"
  if "${AWS}" s3 cp "${DEST}${book}.csv" "${old}" --only-show-errors 2>/dev/null; then
    verdict=$(python3 - "${old}" "${new}" <<'EOF'
import csv, sys, datetime
old = {r["date"]: r for r in csv.DictReader(open(sys.argv[1]))}
new = {r["date"]: r for r in csv.DictReader(open(sys.argv[2]))}
cut = (datetime.date.today() - datetime.timedelta(days=3)).isoformat()
cols = ("open", "high", "low", "close")
changed = [d for d in old if d < cut and d in new and any(old[d][c] != new[d][c] for c in cols)]
missing = [d for d in old if d not in new]
print(f"{len(old)} {len(new)} {len(changed)} {len(missing)} {','.join((changed + missing)[:5])}")
EOF
)
    read -r n_old n_new n_changed n_missing examples <<<"${verdict}"
    if (( n_new < n_old || n_missing > 0 )); then
      (( FORCE )) || die "${book}: new file has ${n_new} rows vs ${n_old} published, ${n_missing} published dates missing (${examples:-}); not uploading (--force to override)"
      warn "${book}: fewer rows / missing dates, continuing because of --force"
    fi
    if (( n_changed > 0 )); then
      (( FORCE )) || die "${book}: ${n_changed} closed bars older than 3 days changed (e.g. ${examples}); not uploading (--force to override)"
      warn "${book}: ${n_changed} closed bars changed, continuing because of --force"
    fi
    ok "${book}: ${n_old} -> ${n_new} rows, no closed bar changed"
  else
    warn "${book}: nothing published at ${DEST}${book}.csv yet; first upload"
  fi

  if (( UPLOAD )); then
    "${AWS}" s3 cp "${new}" "${DEST}${book}.csv" --only-show-errors
    ok "uploaded ${DEST}${book}.csv"
  fi
done
(( UPLOAD )) || ok "dry run: files in ${WORK_DIR}/new, S3 untouched (re-run with --upload to publish)"
