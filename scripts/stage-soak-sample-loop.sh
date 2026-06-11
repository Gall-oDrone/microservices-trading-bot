#!/usr/bin/env bash
# Background sampler: one agreement sample every 30 minutes for the short soak window.
# Start: ./scripts/stage-soak-sample-loop.sh start
# Stop:  ./scripts/stage-soak-sample-loop.sh stop
# Status: ./scripts/stage-soak-sample-loop.sh status
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
LOG_DIR="${LOG_DIR:-$ROOT/tmp/stage-soak}"
PID_FILE="$LOG_DIR/sample-loop.pid"
LOG_FILE="$LOG_DIR/sample-loop.log"
INTERVAL_SEC="${SOAK_SAMPLE_INTERVAL_SEC:-1800}"

start_loop() {
  mkdir -p "$LOG_DIR"
  if [[ -f "$PID_FILE" ]] && kill -0 "$(cat "$PID_FILE")" 2>/dev/null; then
    echo "Sample loop already running (PID $(cat "$PID_FILE"))"
    exit 0
  fi
  nohup "$ROOT/scripts/stage-soak-sample-loop.sh" run >>"$LOG_FILE" 2>&1 &
  echo $! >"$PID_FILE"
  echo "Started sample loop PID $(cat "$PID_FILE") (every ${INTERVAL_SEC}s) → $LOG_FILE"
}

stop_loop() {
  if [[ -f "$PID_FILE" ]]; then
    kill "$(cat "$PID_FILE")" 2>/dev/null || true
    rm -f "$PID_FILE"
    echo "Stopped sample loop"
  else
    pkill -f 'stage-soak-sample-loop.sh run' 2>/dev/null || true
    echo "No PID file; pkill attempted"
  fi
}

status_loop() {
  if [[ -f "$PID_FILE" ]] && kill -0 "$(cat "$PID_FILE")" 2>/dev/null; then
    echo "Sample loop running PID $(cat "$PID_FILE")"
  else
    echo "Sample loop not running"
  fi
  if [[ -f "$ROOT/tmp/stage-soak/soak-window.json" ]]; then
    cat "$ROOT/tmp/stage-soak/soak-window.json"
  fi
  if [[ -f "$ROOT/tmp/stage-soak/agreement-samples.jsonl" ]]; then
    echo "Samples: $(wc -l <"$ROOT/tmp/stage-soak/agreement-samples.jsonl" | tr -d ' ') lines"
    tail -1 "$ROOT/tmp/stage-soak/agreement-samples.jsonl" 2>/dev/null || true
  fi
}

ensure_port_forward() {
  local pf_port="${STRATEGY_EXECUTOR_LOCAL_PORT:-8084}"
  if curl -fsS --max-time 2 "http://127.0.0.1:${pf_port}/health" >/dev/null 2>&1; then
    return 0
  fi
  if [[ -f "$LOG_DIR/port-forward.pid" ]] && kill -0 "$(cat "$LOG_DIR/port-forward.pid")" 2>/dev/null; then
    sleep 1
    curl -fsS --max-time 3 "http://127.0.0.1:${pf_port}/health" >/dev/null 2>&1 && return 0
  fi
  echo "[WARN] port-forward down; starting strategy-executor :${pf_port}" >>"$LOG_FILE"
  kubectl -n "${NAMESPACE:-bitso-trading-dev}" port-forward svc/strategy-executor "${pf_port}:8081" \
    >>"$LOG_DIR/port-forward.log" 2>&1 &
  echo $! >"$LOG_DIR/port-forward.pid"
  for _ in 1 2 3 4 5; do
    sleep 1
    curl -fsS --max-time 2 "http://127.0.0.1:${pf_port}/health" >/dev/null 2>&1 && return 0
  done
  echo "[ERROR] port-forward failed on :${pf_port}" >>"$LOG_FILE"
  return 1
}

run_loop() {
  cd "$ROOT"
  while true; do
    echo "--- $(date -u +%FT%TZ) loop sample ---" >>"$LOG_FILE"
    ensure_port_forward || true
    ./scripts/run-stage-soak-2026-06-02.sh sample >>"$LOG_FILE" 2>&1 || true
    sleep "$INTERVAL_SEC"
  done
}

cmd="${1:-status}"
case "$cmd" in
  start) start_loop ;;
  stop) stop_loop ;;
  status) status_loop ;;
  run) run_loop ;;
  *) echo "Usage: $0 {start|stop|status}"; exit 1 ;;
esac
