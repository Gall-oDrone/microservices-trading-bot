#!/usr/bin/env bash
# Installs the operator cron jobs (docs/frontend/FRONTEND-UI-PLAN-2026-10-03.md §8.9):
#
#   15 6 * * *    scripts/ops-run.sh executor   daily stage run at 00:15 Mexico City (06:15 UTC)
#   */15 * * * *  scripts/ops-run.sh alerts     R3 alerts every 15 minutes
#   30 2 * * *    scripts/ops-run.sh compact    archive compaction refresh (non-destructive;
#                                               02:30 UTC = after the compactor's 2 h settle)
#   7 * * * *     scripts/ops-run.sh sampler    hourly public order-book snapshot (read-only,
#                                               no keys; plan §6.4.13)
#   0 8 1 * *     scripts/ops-run.sh research   monthly execution study (needs --bucket; §6.4.14)
#   30 7 * * 1    scripts/ops-run.sh prereg     weekly pre-registered verdicts; interim / final
#                                               on their dates (§6.4.14)
#
# It builds services/ui-api/bin/ui-alerts (and the executor binary only when it is
# missing, or with --rebuild-executor), and services/strategy-executor/daily-executor-data/
# daily-reconcile (the read-only stage reconciliation daily-executor-run.sh runs after each
# stage run, plan §6.4.10), daily-executor-data/book-sampler and exec-research, writes the jobs' settings to
# ~/.config/microservices-trading-bot/ops.env (chmod 600), and replaces its own block
# in your crontab (between "# BEGIN mtb-ops" and "# END mtb-ops"); other entries are
# kept.
#
# The compactor (services/data-collector/cmd/compact-archive) lives on the collector
# branch, so it is built from --compactor-ref (default origin/feat/intraday-data-collector)
# with git archive into bin/compact-archive, only when missing or with
# --rebuild-compactor; the commit is recorded in bin/compact-archive.ref. The job never
# passes -cutover (ops-run.sh refuses it): deleting the small files stays manual.
#
#   scripts/install-ops-cron.sh --topic-arn arn:aws:sns:us-east-1:<account>:mtb-operator-alerts \
#       --bucket mtb-development-data-archive-<account>
#   scripts/install-ops-cron.sh --print        # show what would be installed
#   scripts/install-ops-cron.sh --uninstall    # remove the block (ops.env is kept)
#
# Options:
#   --topic-arn ARN       SNS topic for alerts (required to install; otherwise alerts go to the log)
#   --bucket NAME         archive bucket: -archive s3://NAME for alerts, the executor uploads
#                         to s3://NAME/daily-executor/stage, and the compaction job runs on it
#   --no-executor         install the alerts job only (plus compaction with --bucket)
#   --no-compact          do not schedule the compaction refresh
#   --no-sampler          do not schedule the hourly order-book sampler
#   --no-research         do not schedule the monthly execution study or the weekly verdicts
#   --rebuild-executor    rebuild services/strategy-executor/daily-executor-data/daily-executor
#                         from the committed HEAD in a throwaway clone, so the code version it
#                         records is the plain commit (never "+dirty" from untracked files;
#                         uncommitted changes are not included); the old binary is kept as
#                         daily-executor.prev-<rev>
#   --compactor-ref REF   git ref holding services/data-collector (see above)
#   --rebuild-compactor   rebuild bin/compact-archive from --compactor-ref
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ENV_FILE="${MTB_OPS_ENV:-$HOME/.config/microservices-trading-bot/ops.env}"
topic="" bucket="" print=0 uninstall=0 executor=1 rebuild=0
sampler=1 research=1 compact=1 compactor_ref="origin/feat/intraday-data-collector" rebuild_compactor=0
while [ $# -gt 0 ]; do
  case "$1" in
    --topic-arn) topic="$2"; shift 2 ;;
    --bucket) bucket="$2"; shift 2 ;;
    --print) print=1; shift ;;
    --uninstall) uninstall=1; shift ;;
    --no-executor) executor=0; shift ;;
    --no-compact) compact=0; shift ;;
    --no-sampler) sampler=0; shift ;;
    --no-research) research=0; shift ;;
    --rebuild-executor) rebuild=1; shift ;;
    --compactor-ref) compactor_ref="$2"; shift 2 ;;
    --rebuild-compactor) rebuild_compactor=1; shift ;;
    -h | --help) sed -n '2,47p' "$0"; exit 0 ;;
    *) echo "unknown option $1" >&2; exit 2 ;;
  esac
done
if [ -z "$bucket" ]; then compact=0; fi

strip_block() { sed '/^# BEGIN mtb-ops/,/^# END mtb-ops/d'; }

if [ "$uninstall" = 1 ]; then
  (crontab -l 2>/dev/null || true) | strip_block | crontab -
  echo "removed the mtb-ops block from the crontab ($ENV_FILE kept)"
  exit 0
fi

if [ "$(date +%z)" != "+0000" ]; then
  echo "warning: this machine's time zone is $(date +%Z) ($(date +%z)); the times below are UTC." >&2
  echo "         Mexico City is UTC-6: adjust the executor hour so it runs at 00:15 Mexico City." >&2
fi

aws_bin="$(command -v aws || true)"
# One hop only: a shim → ~/.local/bin/aws, not into the versioned install dir.
if [ -n "$aws_bin" ] && [ -L "$aws_bin" ]; then
  t="$(readlink "$aws_bin")"
  case "$t" in /*) aws_bin="$t" ;; esac
fi
go_bin="$(command -v go || true)"
path_dirs="$HOME/.local/bin:/usr/local/bin:/usr/bin:/bin"
for b in "$aws_bin" "$go_bin"; do
  if [ -n "$b" ]; then
    d="$(dirname "$b")"
    case ":$path_dirs:" in *":$d:"*) ;; *) path_dirs="$d:$path_dirs" ;; esac
  fi
done

notify="stdout"
[ -n "$topic" ] && notify="sns:$topic"
archive="" s3uri=""
if [ -n "$bucket" ]; then
  archive="s3://$bucket"
  s3uri="s3://$bucket/daily-executor/stage"
fi
ledger="$ROOT/services/strategy-executor/daily-executor-data/stage/ledger.jsonl"

envtext="# Written by scripts/install-ops-cron.sh on $(date -u +%Y-%m-%dT%H:%M:%SZ); read by scripts/ops-run.sh.
PATH=$path_dirs
AWS_PAGER=
UI_ALERTS_NOTIFY=$notify
UI_API_ARCHIVE=$archive
UI_API_LEDGER=$ledger
UI_ALERTS_UI_URL=http://127.0.0.1:5173
DAILY_EXECUTOR_S3_URI=$s3uri
COMPACT_BUCKET=$bucket"

block="# BEGIN mtb-ops: managed by scripts/install-ops-cron.sh; times are UTC (06:15 UTC = 00:15 Mexico City)"
if [ "$executor" = 1 ]; then
  block="$block
15 6 * * * $ROOT/scripts/ops-run.sh executor"
fi
if [ "$compact" = 1 ]; then
  block="$block
30 2 * * * $ROOT/scripts/ops-run.sh compact"
fi
if [ "$sampler" = 1 ]; then
  block="$block
7 * * * * $ROOT/scripts/ops-run.sh sampler"
fi
if [ "$research" = 1 ]; then
  if [ -n "$bucket" ]; then
    block="$block
0 8 1 * * $ROOT/scripts/ops-run.sh research"
  fi
  block="$block
30 7 * * 1 $ROOT/scripts/ops-run.sh prereg"
fi
block="$block
*/15 * * * * $ROOT/scripts/ops-run.sh alerts
# END mtb-ops"

if [ "$print" = 1 ]; then
  printf '%s\n\n--- %s\n%s\n' "$block" "$ENV_FILE" "$envtext"
  exit 0
fi
if [ -z "$topic" ]; then
  echo "warning: no --topic-arn: alerts are only written to the log" >&2
fi

tmps=()
trap 'rm -rf ${tmps[@]+"${tmps[@]}"}' EXIT

echo "building services/ui-api/bin/ui-alerts"
(cd "$ROOT/services/ui-api" && go build -o bin/ui-alerts ./cmd/ui-alerts)
if [ "$executor" = 1 ]; then
  echo "building services/strategy-executor/daily-executor-data/daily-reconcile"
  (cd "$ROOT/services/strategy-executor" && mkdir -p daily-executor-data && go build -o daily-executor-data/daily-reconcile ./cmd/daily-reconcile)
fi
if [ "$sampler" = 1 ]; then
  echo "building services/strategy-executor/daily-executor-data/book-sampler"
  (cd "$ROOT/services/strategy-executor" && mkdir -p daily-executor-data && go build -o daily-executor-data/book-sampler ./cmd/book-sampler)
fi
if [ "$research" = 1 ] && [ -n "$bucket" ]; then
  echo "building services/strategy-executor/daily-executor-data/exec-research"
  (cd "$ROOT/services/strategy-executor" && mkdir -p daily-executor-data && go build -o daily-executor-data/exec-research ./cmd/exec-research)
fi
exe="$ROOT/services/strategy-executor/daily-executor-data/daily-executor"
if [ "$executor" = 1 ] && { [ "$rebuild" = 1 ] || [ ! -x "$exe" ]; }; then
  head="$(git -C "$ROOT" rev-parse HEAD)"
  if [ -n "$(git -C "$ROOT" status --porcelain --untracked-files=no -- services/strategy-executor shared)" ]; then
    echo "warning: uncommitted changes under services/strategy-executor or shared are NOT built" >&2
  fi
  echo "building $exe from HEAD ${head:0:12} (clean clone)"
  src="$(mktemp -d)"
  tmps+=("$src")
  # A real clone (not a worktree): go stamps vcs.* from the nearest .git directory.
  git clone -q --shared --no-checkout "$ROOT" "$src"
  git -C "$src" checkout -q --detach "$head"
  (cd "$src/services/strategy-executor" && go build -o "$exe.new" ./cmd/daily-executor)
  if [ -x "$exe" ]; then
    old="$(go version -m "$exe" | sed -n 's/.*vcs\.revision=\(.\{12\}\).*/\1/p')"
    if go version -m "$exe" | grep -q 'vcs.modified=true'; then old="$old+dirty"; fi
    cp -p "$exe" "$exe.prev-${old:-unknown}"
  fi
  mv -f "$exe.new" "$exe"
fi
comp="$ROOT/bin/compact-archive"
if [ "$compact" = 1 ] && { [ "$rebuild_compactor" = 1 ] || [ ! -x "$comp" ]; }; then
  rev="$(git -C "$ROOT" rev-parse --verify --short "$compactor_ref^{commit}")" || {
    echo "cannot resolve $compactor_ref: git fetch origin, or pass --compactor-ref / --no-compact" >&2
    exit 1
  }
  echo "building $comp from $compactor_ref ($rev)"
  tmp="$(mktemp -d)"
  tmps+=("$tmp")
  # The module replaces bitso-trading-platform/shared => ../../shared, so take both.
  git -C "$ROOT" archive "$rev" services/data-collector shared | tar -x -C "$tmp"
  mkdir -p "$ROOT/bin"
  (cd "$tmp/services/data-collector" && go build -o "$comp" ./cmd/compact-archive)
  printf '%s %s\n' "$compactor_ref" "$rev" >"$comp.ref"
fi

mkdir -p "$(dirname "$ENV_FILE")"
umask 077
printf '%s\n' "$envtext" >"$ENV_FILE"
chmod 600 "$ENV_FILE"
echo "wrote $ENV_FILE"

{ (crontab -l 2>/dev/null || true) | strip_block; printf '%s\n' "$block"; } | crontab -
echo "installed:"
printf '%s\n' "$block"
echo "logs: ${XDG_STATE_HOME:-$HOME/.local/state}/mtb-ops/{executor,alerts,compact}.log"
