#!/usr/bin/env bash
# Runs the daily-executor once and leaves a run log that ui-api's Data health
# page reads (docs/frontend/FRONTEND-UI-PLAN-2026-10-03.md §4.5):
#
#   <ledger dir>/run-<UTC>.log   the executor's output, then
#                                "upload=ok|failed <dest>" (when uploading), then
#                                "exit=<code>" as the last line.
#
# With DAILY_EXECUTOR_S3_URI set (e.g.
# s3://mtb-development-data-archive-<account>/daily-executor/stage), it then
# copies the ledger, the halt file (or removes a stale remote one), the candle
# CSVs and the run log there, so ui-api can read the ledger from S3
# (ui-api -ledgers stage=s3://…/daily-executor/stage/).
#
# The upload never changes the run's exit code; a failed upload is recorded in
# the log and shown as a failure on the Data health page.
#
# After a -stage run it also reconciles the ledger against Bitso stage
# (cmd/daily-reconcile, read-only; plan §6.4.10): its output goes into the run
# log, then "reconcile=ok|breaks|error"; the JSON report is
# <ledger dir>/reconcile.json (uploaded with the rest). Like the upload, it
# never changes the exit code. DAILY_RECONCILE=0 skips it.
#
# Usage (from anywhere; extra flags go to the executor):
#   scripts/daily-executor-run.sh -stage
#   DAILY_EXECUTOR_S3_URI=s3://bucket/daily-executor/stage scripts/daily-executor-run.sh -stage
#
# Environment:
#   DAILY_EXECUTOR_BIN     executor binary   (default ./daily-executor-data/daily-executor,
#                                             relative to services/strategy-executor)
#   DAILY_EXECUTOR_LEDGER  ledger path       (default ./daily-executor-data/stage/ledger.jsonl)
#   DAILY_EXECUTOR_S3_URI  upload target     (default: no upload)
#   DAILY_RECONCILE_BIN    reconcile binary  (default ./daily-executor-data/daily-reconcile;
#                                             missing: "reconcile=error" with the build command)
#   DAILY_RECONCILE        0 to skip the reconciliation
set -uo pipefail

cd "$(dirname "$0")/../services/strategy-executor" || exit 2
BIN="${DAILY_EXECUTOR_BIN:-./daily-executor-data/daily-executor}"
LEDGER="${DAILY_EXECUTOR_LEDGER:-./daily-executor-data/stage/ledger.jsonl}"
DIR="$(dirname "$LEDGER")"
mkdir -p "$DIR"
LOG="$DIR/run-$(date -u +%Y%m%dT%H%M%SZ).log"

if [ ! -x "$BIN" ]; then
  echo "daily-executor-run: $BIN not found or not executable (go build -o $BIN ./cmd/daily-executor)" | tee "$LOG"
  echo "exit=2" | tee -a "$LOG"
  exit 2
fi

"$BIN" -ledger "$LEDGER" "$@" 2>&1 | tee "$LOG"
code=${PIPESTATUS[0]}

stage=0
for a in "$@"; do
  case "$a" in -stage | --stage | -stage=true | --stage=true) stage=1 ;; esac
done
if [ "$stage" = 1 ] && [ "${DAILY_RECONCILE:-1}" != 0 ]; then
  RBIN="${DAILY_RECONCILE_BIN:-./daily-executor-data/daily-reconcile}"
  if [ -x "$RBIN" ]; then
    "$RBIN" -ledger "$LEDGER" -out "$DIR/reconcile.json" 2>&1 | tee -a "$LOG"
    case "${PIPESTATUS[0]}" in
      0) echo "reconcile=ok" | tee -a "$LOG" ;;
      3) echo "reconcile=breaks $DIR/reconcile.json" | tee -a "$LOG" ;;
      *) echo "reconcile=error" | tee -a "$LOG" ;;
    esac
  else
    echo "reconcile=error $RBIN not found (go build -o $RBIN ./cmd/daily-reconcile)" | tee -a "$LOG"
  fi
fi

if [ -n "${DAILY_EXECUTOR_S3_URI:-}" ]; then
  dest="${DAILY_EXECUTOR_S3_URI%/}"
  ok=1
  if [ -f "$LEDGER" ]; then
    aws s3 cp "$LEDGER" "$dest/ledger.jsonl" --only-show-errors || ok=0
  fi
  if [ -f "$DIR/risk-state.json" ]; then
    aws s3 cp "$DIR/risk-state.json" "$dest/risk-state.json" --only-show-errors || ok=0
  else
    # A halt lifted locally must not linger in the copy.
    aws s3 rm "$dest/risk-state.json" --only-show-errors >/dev/null 2>&1 || true
  fi
  if [ -d "$DIR/candles" ]; then
    aws s3 sync "$DIR/candles" "$dest/candles" --exclude '*' --include '*.csv' --only-show-errors || ok=0
  fi
  if [ -f "$DIR/reconcile.json" ]; then
    aws s3 cp "$DIR/reconcile.json" "$dest/reconcile.json" --only-show-errors || ok=0
  fi
  if [ "$ok" = 1 ]; then
    echo "upload=ok $dest" | tee -a "$LOG"
  else
    echo "upload=failed $dest" | tee -a "$LOG"
  fi
fi

echo "exit=$code" | tee -a "$LOG"

if [ -n "${DAILY_EXECUTOR_S3_URI:-}" ]; then
  aws s3 cp "$LOG" "${DAILY_EXECUTOR_S3_URI%/}/$(basename "$LOG")" --only-show-errors || true
fi
exit "$code"
