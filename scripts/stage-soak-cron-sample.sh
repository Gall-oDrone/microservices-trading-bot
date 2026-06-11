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
pf_port="${STRATEGY_EXECUTOR_LOCAL_PORT:-8084}"
if ! curl -fsS --max-time 2 "http://127.0.0.1:${pf_port}/health" >/dev/null 2>&1; then
  echo "[WARN] port-forward missing; starting strategy-executor :${pf_port}"
  kubectl -n "${NAMESPACE:-bitso-trading-dev}" port-forward svc/strategy-executor "${pf_port}:8081" \
    >>"$LOG_DIR/port-forward.log" 2>&1 &
  echo $! >"$LOG_DIR/port-forward.pid"
  sleep 3
fi

./scripts/run-stage-soak-2026-06-02.sh sample || echo "[ERROR] sample failed"
