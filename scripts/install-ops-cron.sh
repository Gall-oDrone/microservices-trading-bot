#!/usr/bin/env bash
# Installs the operator cron jobs (docs/frontend/FRONTEND-UI-PLAN-2026-10-03.md §8.9):
#
#   15 6 * * *    scripts/ops-run.sh executor   daily stage run at 00:15 Mexico City (06:15 UTC)
#   */15 * * * *  scripts/ops-run.sh alerts     R3 alerts every 15 minutes
#
# It builds services/ui-api/bin/ui-alerts (and the executor binary only when it is
# missing, or with --rebuild-executor), writes the jobs' settings to
# ~/.config/microservices-trading-bot/ops.env (chmod 600), and replaces its own block
# in your crontab (between "# BEGIN mtb-ops" and "# END mtb-ops"); other entries are
# kept.
#
#   scripts/install-ops-cron.sh --topic-arn arn:aws:sns:us-east-1:<account>:mtb-operator-alerts \
#       --bucket mtb-development-data-archive-<account>
#   scripts/install-ops-cron.sh --print        # show what would be installed
#   scripts/install-ops-cron.sh --uninstall    # remove the block (ops.env is kept)
#
# Options:
#   --topic-arn ARN       SNS topic for alerts (required to install; otherwise alerts go to the log)
#   --bucket NAME         archive bucket: -archive s3://NAME for alerts, and the executor uploads
#                         to s3://NAME/daily-executor/stage
#   --no-executor         install the alerts job only
#   --rebuild-executor    rebuild services/strategy-executor/daily-executor-data/daily-executor
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ENV_FILE="${MTB_OPS_ENV:-$HOME/.config/microservices-trading-bot/ops.env}"
topic="" bucket="" print=0 uninstall=0 executor=1 rebuild=0
while [ $# -gt 0 ]; do
  case "$1" in
    --topic-arn) topic="$2"; shift 2 ;;
    --bucket) bucket="$2"; shift 2 ;;
    --print) print=1; shift ;;
    --uninstall) uninstall=1; shift ;;
    --no-executor) executor=0; shift ;;
    --rebuild-executor) rebuild=1; shift ;;
    -h | --help) sed -n '2,25p' "$0"; exit 0 ;;
    *) echo "unknown option $1" >&2; exit 2 ;;
  esac
done

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
DAILY_EXECUTOR_S3_URI=$s3uri"

block="# BEGIN mtb-ops: managed by scripts/install-ops-cron.sh; times are UTC (06:15 UTC = 00:15 Mexico City)"
if [ "$executor" = 1 ]; then
  block="$block
15 6 * * * $ROOT/scripts/ops-run.sh executor"
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

echo "building services/ui-api/bin/ui-alerts"
(cd "$ROOT/services/ui-api" && go build -o bin/ui-alerts ./cmd/ui-alerts)
exe="$ROOT/services/strategy-executor/daily-executor-data/daily-executor"
if [ "$executor" = 1 ] && { [ "$rebuild" = 1 ] || [ ! -x "$exe" ]; }; then
  echo "building $exe"
  (cd "$ROOT/services/strategy-executor" && go build -o daily-executor-data/daily-executor ./cmd/daily-executor)
fi

mkdir -p "$(dirname "$ENV_FILE")"
umask 077
printf '%s\n' "$envtext" >"$ENV_FILE"
chmod 600 "$ENV_FILE"
echo "wrote $ENV_FILE"

{ (crontab -l 2>/dev/null || true) | strip_block; printf '%s\n' "$block"; } | crontab -
echo "installed:"
printf '%s\n' "$block"
echo "logs: ${XDG_STATE_HOME:-$HOME/.local/state}/mtb-ops/{executor,alerts}.log"
