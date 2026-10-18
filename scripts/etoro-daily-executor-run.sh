#!/usr/bin/env bash
# Runs the eToro index-CFD executor once, then the read-only reconciliation,
# and leaves a run log next to the ledger (porting plan P3; the eToro sibling
# of scripts/daily-executor-run.sh):
#
#   <ledger dir>/run-<UTC>.log              executor output, reconcile output,
#                                           "reconcile=clean|drift|error",
#                                           "upload=ok|failed <dest>" (when uploading),
#                                           "exit=<code>" as the last line
#   <ledger dir>/reconcile/<NY date>.json   the reconcile report (one per run day;
#                                           the P3 exit criterion is five consecutive
#                                           trading days of clean reports)
#
# scripts/install-ops-cron.sh schedules it at 09:35 New York time on weekdays
# (ops-run.sh etoro); the executor itself skips NYSE holidays (the day is already
# recorded) and refuses to trade outside the cash session. By hand, on a cron
# that honours CRON_TZ:
#
#   CRON_TZ=America/New_York
#   35 9 * * 1-5  /path/to/repo/scripts/ops-run.sh etoro
#
# Credentials come from ~/.config/microservices-trading-bot/etoro-demo.env
# (chmod 600; ETORO_PUBLIC_KEY, ETORO_PRIVATE_KEY, ETORO_ENV=demo) unless set
# in the environment. They are never printed or written anywhere else.
#
# With ETORO_EXECUTOR_S3_URI set (P6; e.g. s3://<bucket>/daily-executor/etoro-demo)
# it then copies the ledger directory there, so the off-machine watchdog
# (infrastructure/lambda/ledger-watchdog, LEDGERS …:nyse) sees every run even when
# this workstation is off: ledger.jsonl, risk-state.json (or removes a stale remote
# one, like the Bitso wrapper), candles/*.csv, reconcile/*.json and intents/*.json
# (sync, never --delete: intents are an append-only audit trail that is never removed
# locally, and a remote copy must never be the thing that deletes evidence), and the
# run log LAST, after its "exit=" line, because the watchdog keys on the newest
# run-*.log. The upload never changes the exit code; "upload=failed" is in the log
# and is a ui-alerts warning (etoro.upload). `aws` comes from PATH (ops.env).
#
#   scripts/etoro-daily-executor-run.sh --upload-only
#
# copies the directory as it is (all run logs included) and exits 0/1 by the
# upload's outcome: for the first upload of an existing ledger, or to re-send a copy
# after a failed one. It runs nothing, writes no run log and touches no local file.
#
# Environment:
#   ETORO_EXECUTOR_BIN     executor binary  (default ./etoro-daily-data/etoro-daily-executor,
#                                            relative to services/strategy-executor)
#   ETORO_RECONCILE_BIN    reconcile binary (default ./etoro-daily-data/etoro-reconcile)
#   ETORO_EXECUTOR_LEDGER  ledger           (default ./etoro-daily-data/demo/ledger.jsonl)
#   ETORO_RECONCILE        0 skips the reconciliation
#   ETORO_EXECUTOR_S3_URI  upload target    (default: no upload)
#
# The reconciliation never changes the exit code (the executor's).
set -uo pipefail

cd "$(dirname "$0")/../services/strategy-executor" || exit 2
BIN="${ETORO_EXECUTOR_BIN:-./etoro-daily-data/etoro-daily-executor}"
RBIN="${ETORO_RECONCILE_BIN:-./etoro-daily-data/etoro-reconcile}"
LEDGER="${ETORO_EXECUTOR_LEDGER:-./etoro-daily-data/demo/ledger.jsonl}"
DIR="$(dirname "$LEDGER")"
S3="${ETORO_EXECUTOR_S3_URI:-}"
S3="${S3%/}"

# upload_dir copies everything but the run logs to $S3. Returns 1 when any step failed.
upload_dir() {
  local ok=1
  if [ -f "$LEDGER" ]; then
    aws s3 cp "$LEDGER" "$S3/ledger.jsonl" --only-show-errors || ok=0
  fi
  if [ -f "$DIR/risk-state.json" ]; then
    aws s3 cp "$DIR/risk-state.json" "$S3/risk-state.json" --only-show-errors || ok=0
  else
    # A halt lifted locally must not linger in the copy.
    aws s3 rm "$S3/risk-state.json" --only-show-errors >/dev/null 2>&1 || true
  fi
  if [ -d "$DIR/candles" ]; then
    aws s3 sync "$DIR/candles" "$S3/candles" --exclude '*' --include '*.csv' --only-show-errors || ok=0
  fi
  if [ -d "$DIR/reconcile" ]; then
    aws s3 sync "$DIR/reconcile" "$S3/reconcile" --exclude '*' --include '*.json' --only-show-errors || ok=0
  fi
  if [ -d "$DIR/intents" ]; then
    aws s3 sync "$DIR/intents" "$S3/intents" --exclude '*' --include '*.json' --only-show-errors || ok=0
  fi
  [ "$ok" = 1 ]
}

if [ "${1:-}" = "--upload-only" ]; then
  if [ -z "$S3" ]; then
    echo "etoro-daily-executor-run: --upload-only needs ETORO_EXECUTOR_S3_URI" >&2
    exit 2
  fi
  ok=1
  upload_dir || ok=0
  # Every run log, newest last (the watchdog's baseline); nothing is deleted remotely.
  aws s3 sync "$DIR" "$S3" --exclude '*' --include 'run-*.log' --only-show-errors || ok=0
  if [ "$ok" = 1 ]; then
    echo "upload=ok $S3"
    exit 0
  fi
  echo "upload=failed $S3"
  exit 1
fi

mkdir -p "$DIR/reconcile"
LOG="$DIR/run-$(date -u +%Y%m%dT%H%M%SZ).log"

if [ ! -x "$BIN" ]; then
  echo "etoro-daily-executor-run: $BIN not found (go build -o $BIN ./cmd/etoro-daily-executor)" | tee "$LOG"
  echo "exit=2" | tee -a "$LOG"
  exit 2
fi

"$BIN" -ledger "$LEDGER" "$@" 2>&1 | tee "$LOG"
code=${PIPESTATUS[0]}

demo=0
for a in "$@"; do
  case "$a" in -demo | --demo | -demo=true | --demo=true) demo=1 ;; esac
done
if [ "$demo" = 1 ] && [ "${ETORO_RECONCILE:-1}" != 0 ]; then
  if [ -x "$RBIN" ]; then
    out="$DIR/reconcile/$(TZ=America/New_York date +%F).json"
    "$RBIN" -ledger "$LEDGER" -json "$out" 2>&1 | tee -a "$LOG"
    case "${PIPESTATUS[0]}" in
      0) echo "reconcile=clean $out" | tee -a "$LOG" ;;
      1) echo "reconcile=drift $out" | tee -a "$LOG" ;;
      *) echo "reconcile=error" | tee -a "$LOG" ;;
    esac
  else
    echo "reconcile=error $RBIN not found (go build -o $RBIN ./cmd/etoro-reconcile)" | tee -a "$LOG"
  fi
fi

if [ -n "$S3" ]; then
  if upload_dir; then
    echo "upload=ok $S3" | tee -a "$LOG"
  else
    echo "upload=failed $S3" | tee -a "$LOG"
  fi
fi

echo "exit=$code" | tee -a "$LOG"

if [ -n "$S3" ]; then
  # Last, after the exit line: the watchdog treats the newest run log as the last run.
  aws s3 cp "$LOG" "$S3/$(basename "$LOG")" --only-show-errors || true
fi
exit "$code"
