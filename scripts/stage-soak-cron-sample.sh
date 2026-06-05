#!/usr/bin/env bash
# Append one bash↔Go regime agreement sample (intended for cron every 30 min).
# Logs to tmp/stage-soak/cron-sample.log
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
LOG_DIR="${LOG_DIR:-$ROOT/tmp/stage-soak}"
mkdir -p "$LOG_DIR"

exec >>"$LOG_DIR/cron-sample.log" 2>&1
echo "--- $(date -u +%FT%TZ) cron sample ---"

cd "$ROOT"
if ! pgrep -f 'port-forward.*8084:8081' >/dev/null 2>&1 || ! pgrep -f 'scripts/strategy-regime-router.sh' >/dev/null 2>&1; then
  echo "[WARN] soak helpers missing; attempting start-bash"
  ./scripts/run-stage-soak-2026-06-02.sh start-bash || true
fi

./scripts/run-stage-soak-2026-06-02.sh sample || echo "[ERROR] sample failed"
