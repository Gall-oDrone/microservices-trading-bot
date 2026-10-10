#!/usr/bin/env bash
# Runs the eToro index-CFD executor once, then the read-only reconciliation,
# and leaves a run log next to the ledger (porting plan P3; the eToro sibling
# of scripts/daily-executor-run.sh):
#
#   <ledger dir>/run-<UTC>.log              executor output, reconcile output,
#                                           "reconcile=clean|drift|error", "exit=<code>"
#   <ledger dir>/reconcile/<NY date>.json   the reconcile report (one per run day;
#                                           the P3 exit criterion is five consecutive
#                                           trading days of clean reports)
#
# Schedule it at 09:35 New York time on weekdays; the executor itself skips
# NYSE holidays (the day is already recorded) and refuses to trade outside the
# cash session:
#
#   CRON_TZ=America/New_York
#   35 9 * * 1-5  /path/to/repo/scripts/etoro-daily-executor-run.sh -demo
#
# Credentials come from ~/.config/microservices-trading-bot/etoro-demo.env
# (chmod 600; ETORO_PUBLIC_KEY, ETORO_PRIVATE_KEY, ETORO_ENV=demo) unless set
# in the environment. They are never printed or written anywhere else.
#
# Environment:
#   ETORO_EXECUTOR_BIN     executor binary  (default ./etoro-daily-data/etoro-daily-executor,
#                                            relative to services/strategy-executor)
#   ETORO_RECONCILE_BIN    reconcile binary (default ./etoro-daily-data/etoro-reconcile)
#   ETORO_EXECUTOR_LEDGER  ledger           (default ./etoro-daily-data/demo/ledger.jsonl)
#   ETORO_RECONCILE        0 skips the reconciliation
#
# The reconciliation never changes the exit code (the executor's).
set -uo pipefail

cd "$(dirname "$0")/../services/strategy-executor" || exit 2
BIN="${ETORO_EXECUTOR_BIN:-./etoro-daily-data/etoro-daily-executor}"
RBIN="${ETORO_RECONCILE_BIN:-./etoro-daily-data/etoro-reconcile}"
LEDGER="${ETORO_EXECUTOR_LEDGER:-./etoro-daily-data/demo/ledger.jsonl}"
DIR="$(dirname "$LEDGER")"
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

echo "exit=$code" | tee -a "$LOG"
exit "$code"
