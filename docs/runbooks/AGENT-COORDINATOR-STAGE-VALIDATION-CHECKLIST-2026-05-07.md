# Agent + Coordinator Stage Validation Checklist

Date: 2026-05-07  
Scope: Validate `ops-agent` and `agent-coordinator` during a 30-minute stage run with `dry-run=false`.

## 1) Preconditions

- Stage credentials configured and isolated from production.
- `start-organic-trading.sh` executed in stage environment.
- `ops-agent` and `agent-coordinator` deployed and reachable.
- Prometheus scraping both services.
- Kill switch path known and ready.

## 2) Environment Variables (example)

```bash
export OPS_AGENT_BASE_URL="http://localhost:8090"
export COORDINATOR_BASE_URL="http://localhost:8091"
export PROM_BASE_URL="http://localhost:9090"
```

## 3) Baseline Health Checks (before run)

```bash
curl -fsS "$OPS_AGENT_BASE_URL/health"
curl -fsS "$COORDINATOR_BASE_URL/health"
curl -fsS "$OPS_AGENT_BASE_URL/metrics" >/dev/null
curl -fsS "$COORDINATOR_BASE_URL/metrics" >/dev/null
```

Expected:

- Both `/health` endpoints return HTTP 200.
- Both `/metrics` endpoints are reachable.

## 4) Smoke Test Coordinator API (before run)

```bash
curl -sS -X POST "$COORDINATOR_BASE_URL/api/v1/coordinator/run" \
  -H "Content-Type: application/json" \
  -d '{
    "source":"manual-stage-validation",
    "severity":"warning",
    "title":"preflight coordinator smoke test",
    "description":"validate multi-agent dispatch and aggregation",
    "labels":{"env":"stage","test":"true"}
  }'
```

Expected:

- HTTP 200 response with:
  - `id`
  - `summary`
  - `agent_reports`
  - `recommendations`

## 5) Start 30-minute Stage Run

Use another terminal/session:

```bash
./scripts/start-organic-trading.sh
```

Run in stage config with `dry-run=false` according to your runtime environment.

## 6) Observe Every 5 Minutes

### 6.1 Prometheus counters

```bash
curl -sS --get "$PROM_BASE_URL/api/v1/query" --data-urlencode 'query=agent_runs_total'
curl -sS --get "$PROM_BASE_URL/api/v1/query" --data-urlencode 'query=agent_failures_total'
curl -sS --get "$PROM_BASE_URL/api/v1/query" --data-urlencode 'query=coordinator_runs_total'
curl -sS --get "$PROM_BASE_URL/api/v1/query" --data-urlencode 'query=coordinator_failures_total'
```

### 6.2 Latency snapshots

```bash
curl -sS --get "$PROM_BASE_URL/api/v1/query" --data-urlencode 'query=histogram_quantile(0.95, sum(rate(agent_run_latency_ms_bucket[5m])) by (le))'
curl -sS --get "$PROM_BASE_URL/api/v1/query" --data-urlencode 'query=histogram_quantile(0.95, sum(rate(coordinator_run_latency_ms_bucket[5m])) by (le))'
```

### 6.3 On-demand coordinator run during live load

```bash
curl -sS -X POST "$COORDINATOR_BASE_URL/api/v1/coordinator/run" \
  -H "Content-Type: application/json" \
  -d '{
    "source":"manual-live-check",
    "severity":"critical",
    "title":"live validation incident",
    "description":"ensure coordinator and child agents still respond under load",
    "labels":{"env":"stage","window":"live"}
  }'
```

Expected:

- Runs succeed without 5xx spikes.
- Failures remain at 0, or any increase is explained by logs/incidents.

## 7) Post-run Summary

Collect final values:

```bash
curl -sS --get "$PROM_BASE_URL/api/v1/query" --data-urlencode 'query=agent_runs_total'
curl -sS --get "$PROM_BASE_URL/api/v1/query" --data-urlencode 'query=agent_failures_total'
curl -sS --get "$PROM_BASE_URL/api/v1/query" --data-urlencode 'query=coordinator_runs_total'
curl -sS --get "$PROM_BASE_URL/api/v1/query" --data-urlencode 'query=coordinator_failures_total'
```

Report:

- total runs per service
- total failures per service
- P95 latency estimates
- top error categories observed (if any)
- missed incidents or wrong recommendations (qualitative notes)

## 8) Pass/Fail Criteria

Pass:

- No unhandled crashes for `ops-agent` or `agent-coordinator`
- Coordinator can produce report payloads during the run
- Failure counters remain stable/low and explainable
- Latency stays within expected operational bounds

Fail:

- Repeated 5xx from agent endpoints
- Coordinator cannot dispatch to child agent
- Persistent failure counter growth without clear cause

## 9) Fast Rollback

- Stop organic trading process.
- Disable external triggering of coordinator endpoints.
- Keep services in read-only mode.
- Escalate with logs + metric snapshots.
