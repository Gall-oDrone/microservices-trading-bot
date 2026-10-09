#!/usr/bin/env bash
# Cron entry point for the operator jobs installed by scripts/install-ops-cron.sh
# (docs/frontend/FRONTEND-UI-PLAN-2026-10-03.md §8.9):
#
#   scripts/ops-run.sh executor   the daily stage run (scripts/daily-executor-run.sh -stage),
#                                 uploading to $DAILY_EXECUTOR_S3_URI when set
#   scripts/ops-run.sh alerts     R3: services/ui-api/bin/ui-alerts (notify on new,
#                                 escalated, reminder or resolved alerts)
#   scripts/ops-run.sh compact    the archive compactor's non-destructive refresh
#                                 (bin/compact-archive -bucket $COMPACT_BUCKET): writes
#                                 trades_compacted/ + manifests, never deletes. -cutover
#                                 is refused here; it stays a manual, reviewed step
#                                 (docs/data-collector/S3-COMPACTION-2026-09-22.md).
#   scripts/ops-run.sh sampler    hourly public order-book snapshot (read-only, no keys;
#                                 plan §6.4.13): services/strategy-executor/daily-executor-data/
#                                 book-sampler appends to daily-executor-data/book-samples/<book>.jsonl
#   scripts/ops-run.sh research   monthly execution study (plan §6.4.14): daily-executor-data/
#                                 exec-research over the last 90 days of $COMPACT_BUCKET (read-only)
#                                 with the sampler's spreads, into daily-executor-data/exec-studies/
#                                 <date>/. Until 2027-01-10 it leaves out the repriced schedules: the
#                                 re-peg pre-registration's forward window stays blind.
#   scripts/ops-run.sh prereg     weekly pre-registered verdicts (scripts/prereg-evaluation.sh) into
#                                 daily-executor-data/prereg/<date>-<phase>/; the phase follows the
#                                 calendar: interim once on/after 2027-03-26, final once on/after
#                                 2027-09-26, as-of otherwise (the 8 newest as-of runs are kept)
#
# Settings come from $MTB_OPS_ENV (default ~/.config/microservices-trading-bot/ops.env),
# written by the installer: PATH (cron's is minimal), UI_ALERTS_NOTIFY, UI_API_ARCHIVE,
# UI_API_LEDGER, DAILY_EXECUTOR_S3_URI, COMPACT_BUCKET. Output is appended to
# ${XDG_STATE_HOME:-~/.local/state}/mtb-ops/<job>.log (rotated at 5 MB).
set -uo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ENV_FILE="${MTB_OPS_ENV:-$HOME/.config/microservices-trading-bot/ops.env}"
if [ -r "$ENV_FILE" ]; then
  set -a
  # shellcheck disable=SC1090
  . "$ENV_FILE"
  set +a
fi
STATE="${XDG_STATE_HOME:-$HOME/.local/state}/mtb-ops"
mkdir -p "$STATE"

job="${1:-}"
shift || true
case "$job" in
  executor | alerts | compact | sampler | research | prereg) ;;
  *)
    echo "usage: $0 executor|alerts|compact|sampler|research|prereg [flags…]" >&2
    exit 2
    ;;
esac

LOG="$STATE/$job.log"
if [ -f "$LOG" ] && [ "$(stat -c %s "$LOG")" -gt 5242880 ]; then
  mv -f "$LOG" "$LOG.1"
fi
exec >>"$LOG" 2>&1

case "$job" in
  executor)
    echo "== $(date -u +%Y-%m-%dT%H:%M:%SZ) ops-run executor"
    "$ROOT/scripts/daily-executor-run.sh" -stage "$@"
    code=$?
    echo "== exit $code"
    exit "$code"
    ;;
  alerts)
    cd "$ROOT/services/ui-api" || exit 2
    exec "${UI_ALERTS_BIN:-./bin/ui-alerts}" "$@"
    ;;
  sampler)
    cd "$ROOT/services/strategy-executor" || exit 2
    exec "${BOOK_SAMPLER_BIN:-./daily-executor-data/book-sampler}" -dir ./daily-executor-data/book-samples "$@"
    ;;
  research)
    echo "== $(date -u +%Y-%m-%dT%H:%M:%SZ) ops-run research"
    cd "$ROOT/services/strategy-executor" || exit 2
    bucket="${COMPACT_BUCKET:-}"
    if [ -z "$bucket" ]; then
      echo "COMPACT_BUCKET is not set (re-run scripts/install-ops-cron.sh --bucket …)"
      exit 2
    fi
    today="$(date -u +%F)"
    out="./daily-executor-data/exec-studies/$today"
    mkdir -p "$out"
    blind=()
    if [[ "$today" < "2027-01-10" ]]; then
      # EXECUTION-PREREGISTRATION-REPEG-2026-10-09.md: no repriced schedule and no
      # paired comparison may be looked at before the forward window closes.
      blind=(-schedules "market,maker 15m,maker 30m,maker 1h,maker 2h,maker 4h,maker 8h,TWAP 4×15m,TWAP 4×30m,TWAP 4×1h,TWAP 4×2h" -compare-alt "")
    fi
    "${EXEC_RESEARCH_BIN:-./daily-executor-data/exec-research}" -bucket "$bucket" \
      -from "$(date -u -d '90 days ago' +%F)" -book-samples ./daily-executor-data/book-samples \
      "${blind[@]}" -out-json "$out/exec-research.json" -out-md "$out/exec-research.md" "$@"
    code=$?
    echo "== exit $code ($out)"
    exit "$code"
    ;;
  prereg)
    echo "== $(date -u +%Y-%m-%dT%H:%M:%SZ) ops-run prereg"
    dir="$ROOT/services/strategy-executor/daily-executor-data/prereg"
    mkdir -p "$dir"
    today="$(date -u +%F)"
    phase="as-of"
    if [[ ! "$today" < "2027-09-26" ]]; then
      if compgen -G "$dir/*-final" >/dev/null; then
        echo "the final evaluation is already in $dir; nothing to do"
        exit 0
      fi
      phase="final"
    elif [[ ! "$today" < "2027-03-26" ]] && ! compgen -G "$dir/*-interim" >/dev/null; then
      phase="interim"
    fi
    "$ROOT/scripts/prereg-evaluation.sh" --phase "$phase" --out "$dir/$today-$phase" "$@"
    code=$?
    # Keep the 8 newest progress reports; interim and final runs are never pruned.
    ls -1d "$dir"/*-as-of 2>/dev/null | sort | head -n -8 | xargs -r rm -rf --
    echo "== exit $code ($dir/$today-$phase)"
    exit "$code"
    ;;
  compact)
    echo "== $(date -u +%Y-%m-%dT%H:%M:%SZ) ops-run compact"
    for a in "$@"; do
      case "$a" in
        *cutover*)
          echo "refusing $a: cutover deletes the small files and is never scheduled"
          exit 2
          ;;
      esac
    done
    bucket="${COMPACT_BUCKET:-}"
    if [ -z "$bucket" ]; then
      echo "COMPACT_BUCKET is not set (re-run scripts/install-ops-cron.sh --bucket …)"
      exit 2
    fi
    "${COMPACT_ARCHIVE_BIN:-$ROOT/bin/compact-archive}" -bucket "$bucket" "$@" </dev/null
    code=$?
    echo "== exit $code"
    exit "$code"
    ;;
esac
