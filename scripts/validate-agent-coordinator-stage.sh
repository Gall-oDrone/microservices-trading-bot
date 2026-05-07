#!/usr/bin/env bash
# Validate ops-agent and agent-coordinator behavior in stage.
# This script is read-only against agent services (it does trigger coordinator run endpoint).
#
# Usage:
#   OPS_AGENT_BASE_URL=http://localhost:8090 \
#   COORDINATOR_BASE_URL=http://localhost:8091 \
#   PROM_BASE_URL=http://localhost:9090 \
#   ./scripts/validate-agent-coordinator-stage.sh

set -euo pipefail

OPS_AGENT_BASE_URL="${OPS_AGENT_BASE_URL:-http://localhost:8090}"
COORDINATOR_BASE_URL="${COORDINATOR_BASE_URL:-http://localhost:8091}"
PROM_BASE_URL="${PROM_BASE_URL:-http://localhost:9090}"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

print_info() { echo -e "${BLUE}[INFO]${NC} $1"; }
print_ok() { echo -e "${GREEN}[OK]${NC} $1"; }
print_warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }
print_err() { echo -e "${RED}[ERR]${NC} $1"; }

need_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    print_err "Missing required command: $1"
    exit 1
  fi
}

check_http() {
  local url="$1"
  local code
  code="$(curl -s -o /dev/null -w "%{http_code}" --max-time 8 "$url" || true)"
  if [[ "$code" == "200" ]]; then
    print_ok "$url -> 200"
  else
    print_warn "$url -> $code"
  fi
}

query_prom() {
  local q="$1"
  local response
  response="$(curl -sS --get "$PROM_BASE_URL/api/v1/query" --data-urlencode "query=$q" || true)"
  echo "$response" | jq -r '.status as $s | if $s=="success" then (.data.result | length | tostring) + " result(s)" else "query failed" end'
}

run_coordinator_smoke() {
  local payload
  payload='{
    "source":"script-stage-validation",
    "severity":"warning",
    "title":"coordinator smoke test",
    "description":"validate dispatch and aggregation",
    "labels":{"env":"stage","script":"validate-agent-coordinator-stage.sh"}
  }'

  local resp
  resp="$(curl -sS -X POST "$COORDINATOR_BASE_URL/api/v1/coordinator/run" \
    -H "Content-Type: application/json" \
    -d "$payload")"

  local id summary reports
  id="$(echo "$resp" | jq -r '.id // empty')"
  summary="$(echo "$resp" | jq -r '.summary // empty')"
  reports="$(echo "$resp" | jq -r '.agent_reports | length // 0')"

  if [[ -n "$id" && -n "$summary" ]]; then
    print_ok "Coordinator run succeeded (id=$id, agent_reports=$reports)"
  else
    print_warn "Coordinator run response missing expected fields"
    echo "$resp"
  fi
}

main() {
  need_cmd curl
  need_cmd jq

  print_info "ops-agent: $OPS_AGENT_BASE_URL"
  print_info "coordinator: $COORDINATOR_BASE_URL"
  print_info "prometheus: $PROM_BASE_URL"

  print_info "Checking health and metrics endpoints..."
  check_http "$OPS_AGENT_BASE_URL/health"
  check_http "$COORDINATOR_BASE_URL/health"
  check_http "$OPS_AGENT_BASE_URL/metrics"
  check_http "$COORDINATOR_BASE_URL/metrics"

  print_info "Running coordinator smoke test..."
  run_coordinator_smoke

  print_info "Querying key Prometheus series..."
  print_info "agent_runs_total => $(query_prom 'agent_runs_total')"
  print_info "agent_failures_total => $(query_prom 'agent_failures_total')"
  print_info "coordinator_runs_total => $(query_prom 'coordinator_runs_total')"
  print_info "coordinator_failures_total => $(query_prom 'coordinator_failures_total')"

  print_ok "Validation checklist completed."
  print_info "For 30-minute run, repeat this script every 5 minutes and compare counters/latency trends."
}

main "$@"
