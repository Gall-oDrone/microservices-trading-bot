# Strategy Regime Router — Phase 2 (Go service)

Date: 2026-05-22
Repository: `microservices-trading-bot`

## 1) Context

`docs/strategy-fee-accuracy/POINT-10-STRATEGY-REGIME-ROUTER.md` shipped Phase 1 as a self-contained bash script (`scripts/strategy-regime-router.sh`) that polls strategy-executor's existing HTTP surface and converges on the right strategy for the current market regime. Phase 1 was deliberately the smallest thing that could work, so we could let it run on Stage with operators in the loop before committing to a long-lived service.

This doc covers **Phase 2**: the same routing logic re-implemented as a Go microservice (`services/strategy-router/`) that runs inside the cluster, exposes Prometheus metrics, and offers an HTTP control surface for live inspection and on-demand triggers. The Phase 1 script is kept around — it remains the best fit for ad-hoc local sessions and dry-run tuning — but the in-cluster path is now the recommended way to keep the router running 24/7 on Stage and Production.

## 2) Why a service, not "just keep the script"

| Concern | Bash script (Phase 1) | Go service (Phase 2) |
|---------|----------------------|----------------------|
| Lifecycle | Operator-managed (tmux, nohup, laptop) | Kubernetes Deployment (1 replica, restart on crash) |
| Observability | Stdout + `/tmp/strategy-regime-router.log` | Stdout + audit log **+ Prometheus metrics** + HTTP `/api/v1/router/state` |
| Telemetry plotting | grep + awk on the audit log | Native Prometheus → Grafana panel (regime over time, switches, blocked reasons) |
| Inspection during incidents | Tail the log file | `GET /api/v1/router/state` returns the last decision + recent ring buffer |
| Manual override during incidents | Stop + re-run the script | `POST /api/v1/router/run` triggers an immediate evaluation, no daemon restart |
| Unit tests | None — bash | Deterministic Go tests with fake clock + fake client |

Phase 2 does **not** change the routing logic, the thresholds, or the routing-table semantics. It is a one-for-one port of the bash classifier and routing decisions into Go.

## 3) Service surface

### 3.1 HTTP endpoints

| Method | Path | Purpose |
|--------|------|---------|
| `GET` | `/health` | Liveness probe — returns `{status: "healthy", service, book}`. |
| `GET` | `/metrics` | Prometheus exposition (see §3.3). |
| `GET` | `/api/v1/router/state` | Current routing table, last decision, last N decisions, dry-run flag, cooldown setting. |
| `POST` | `/api/v1/router/run` | Run one evaluation cycle on demand and return the resulting `Decision` JSON. |

The `Decision` shape exposed by `/state` and `/run`:

```json
{
  "timestamp": "2026-05-22T12:34:56Z",
  "regime": "trending_up",
  "preferred": "momentum_btc_mxn",
  "current": "mean_reversion_btc_mxn",
  "action": "switched",
  "reason": "switched mean_reversion_btc_mxn → momentum_btc_mxn for regime trending_up",
  "snapshot": {
    "regime": "trending_up",
    "atr_pct": 0.6,
    "ema_dist_pct": 0.5,
    "bollinger_pb": 0.62,
    "rsi": 60,
    "price": 1000000,
    "reason": "EMA distance positive and RSI below overbought"
  }
}
```

`action` is one of: `noop`, `started`, `stopped`, `switched`, `paused`, `blocked`, `dry_run`.

### 3.2 Environment variables (same knobs as the bash router)

| Variable | Default | Purpose |
|----------|---------|---------|
| `STRATEGY_ROUTER_PORT` | `8092` | HTTP port. |
| `STRATEGY_ROUTER_BOOK` | `btc_mxn` | Book the router operates on. |
| `STRATEGY_EXECUTOR_URL` | `http://strategy-executor:8081` | strategy-executor base URL. |
| `ROUTER_INTERVAL_SEC` | `30` | Seconds between evaluation cycles. |
| `COOLDOWN_SECS` | `120` | Minimum seconds between two strategy switches. |
| `DRY_RUN` | `false` | When `true`, evaluations log a decision but never call start/stop. |
| `ROUTER_AUDIT_LOG_PATH` | `/tmp/strategy-regime-router.log` | File for the bash-router-compatible audit log; set to `stdout` to log to stdout. |
| `ROUTER_AUTOSTART` | `true` | When `false`, evaluations only happen via `POST /api/v1/router/run`. |
| `ATR_HIGH_VOL_PCT` | `1.5` | `atr/price * 100 >` this ⇒ `high_vol`. |
| `ATR_LOW_VOL_PCT` | `0.30` | `atr/price * 100 <` this ⇒ candidate for `low_vol_range`. |
| `RSI_OVERBOUGHT` | `70` | Blocks `trending_up` when RSI ≥ this. |
| `RSI_OVERSOLD` | `30` | Blocks `trending_down` when RSI ≤ this. |
| `BB_PB_UPPER` | `0.85` | `low_vol_range` requires `%B <` this. |
| `BB_PB_LOWER` | `0.15` | `low_vol_range` requires `%B >` this. |
| `EMA_DIST_ENTRY_PCT` | `0.10` | Absolute price/EMA distance (percent) needed for a trending verdict. |
| `ROUTE_LOW_VOL` | `mean_reversion_{book}` | Strategy to run in `low_vol_range`. |
| `ROUTE_TRENDING_UP` | `momentum_{book}` | Strategy to run in `trending_up`. |
| `ROUTE_TRENDING_DOWN` | `momentum_{book}` | Strategy to run in `trending_down`. |
| `ROUTE_HIGH_VOL` | `none` | Strategy to run in `high_vol` (`none` = pause). |
| `ROUTE_NEUTRAL` | `mean_reversion_{book}` | Strategy to run in `neutral`. |

Any `ROUTE_*` set to `none` (or empty) is treated as "stop the currently running strategy and stay flat".

### 3.3 Prometheus metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `strategy_router_regime` | gauge | `regime` | 1 for the active regime, 0 for the others (one of `low_vol_range \| trending_up \| trending_down \| high_vol \| neutral`). |
| `strategy_router_evaluations_total` | counter | – | Total router evaluation cycles. |
| `strategy_router_evaluation_errors_total` | counter | – | Cycles that failed (snapshot fetch / list strategies). |
| `strategy_router_switches_total` | counter | `from`, `to`, `regime` | Successful strategy switches. `from=<none>` on the first start; `to=<none>` on a pause. |
| `strategy_router_blocked_total` | counter | `reason` | Switches refused. `reason ∈ {has_position, cooldown, not_registered, dry_run}`. |
| `strategy_router_active_strategy` | gauge | `strategy` | 1 for the strategy the router believes is currently active. Previous labels are zeroed when the active one changes. |
| `strategy_router_evaluation_latency_ms` | histogram | – | Latency of a full evaluation cycle in milliseconds. |

A starter Grafana panel can plot `sum by (regime) (strategy_router_regime)` against `sum by (strategy) (strategy_router_active_strategy)` to visualize regime → strategy correspondence over time, with `strategy_router_switches_total` annotations on top.

## 4) Guardrails (carry-overs from Phase 1)

These are unchanged from the bash router and the original POINT-10 design — re-stated here so reviewers can verify the Go implementation matches the documented behavior:

1. **Has-position guard.** When the currently-running strategy reports `has_position == true` (via `GET /api/v1/strategies/{name}/state`), the router **refuses** to stop or replace it — `Decision.action = "blocked"`, `reason` explains why, `strategy_router_blocked_total{reason="has_position"}` increments. The router will revisit the decision on the next cycle once the position closes.
2. **Cooldown.** The router only performs at most one switch per `COOLDOWN_SECS` window (default 120s). Blocked decisions emit `strategy_router_blocked_total{reason="cooldown"}` so it's easy to see the window holding.
3. **Pre-registration.** The router never creates strategies; if the preferred name isn't returned by `GET /api/v1/strategies`, the cycle is blocked with `reason="not_registered"`. Operators register candidates once via `scripts/start-organic-trading.sh STRATEGY_TYPES=mean_reversion,momentum,limit_profit`.
4. **Dry run.** When `DRY_RUN=true`, the router logs what it would do (and bumps `strategy_router_blocked_total{reason="dry_run"}`) but never calls start/stop. This is the recommended starting mode after a deploy or a threshold change.

## 5) Audit log format

The router writes one JSON line per decision to `ROUTER_AUDIT_LOG_PATH`, plus — for lifecycle-mutating actions — a human-readable line in the **exact same format** as the bash router:

```
2026-05-22T12:34:56Z regime=trending_up stop=mean_reversion_btc_mxn start=momentum_btc_mxn
2026-05-22T12:50:01Z regime=high_vol → pause stop=momentum_btc_mxn
```

That keeps existing Grafana / Loki / promtail parsers working unchanged. The JSON line above each text line is the structured form, with full snapshot data attached for off-line analysis.

## 6) Deployment

A `k8s/base/strategy-router.yaml` manifest registers a single-replica `Deployment` (the router is in-memory stateful — cooldown + recent decisions) plus a `ClusterIP` `Service` exposing port `8092`. `k8s/base/kustomization.yaml` was updated to include both the new manifest and the new image. Build with:

```bash
docker build -t strategy-router:latest -f services/strategy-router/Dockerfile .
```

…and push to ECR through the same pipeline used by the other Go services.

For Stage rollout we recommend:

1. Deploy with `DRY_RUN=true` for at least 24 h; verify `strategy_router_blocked_total{reason="dry_run"}` matches the expected switch count and the regime labels look sane in Grafana.
2. Confirm the regime distribution against `scripts/strategy-regime-router.sh` on a laptop running in parallel — both should converge to the same regime within one polling interval.
3. Flip `DRY_RUN=false`. The has-position guard and cooldown will still prevent any unsafe switch.

## 7) Operations notes

- **Single replica only.** The router holds the cooldown timestamp and recent-decision ring buffer in memory. A second replica would race on the start/stop calls. The K8s manifest pins `replicas: 1`.
- **Restart loses cooldown.** A pod restart resets `lastSwitchAt` to zero, so the very next evaluation can switch immediately. This is intentional: if the router crashed during a switch, we want it to re-converge on the right strategy without waiting two minutes.
- **Strategy executor up-front.** The router degrades gracefully when strategy-executor is unreachable (`action="noop"`, error in `reason`, `strategy_router_evaluation_errors_total` increments). A second consecutive snapshot failure looks identical to the first; there's no exponential back-off because the ticker already paces requests.
- **Disable autonomy quickly.** To disable autonomous routing without redeploying, set `ROUTER_AUTOSTART=false` and restart the pod, or set `DRY_RUN=true` via `kubectl set env deployment/strategy-router DRY_RUN=true`. Both are reversible.

## 8) Relation to existing components

| Component | How the router interacts with it |
|-----------|----------------------------------|
| `services/strategy-executor` | Sole external dependency. Router reads `GET /api/v1/indicators/{book}/snapshot` + `GET /api/v1/strategies` + `GET /api/v1/strategies/{name}/state`, and writes `POST /api/v1/strategies/{name}/start|stop`. |
| `scripts/strategy-regime-router.sh` | Reference implementation kept for local sessions and threshold experimentation. Same env-var surface. |
| `scripts/start-organic-trading.sh` | Pre-registers the candidate strategies before the router becomes useful. Run with `STRATEGY_TYPES=mean_reversion,momentum,limit_profit`. |
| `services/agent-coordinator` | Out of scope for this PR. The doc still flags this as future work: have an LLM agent recommend threshold changes from the audit log (deterministic decisions, AI-assisted tuning). |
| `services/strategy-executor` `meta_router` (future) | The doc keeps this in the future-work section. Phase 2 buys us time to validate the regime model before paying the in-process refactor cost. |

## 9) Verification

```bash
# Unit tests (race detector clean)
( cd services/strategy-router && go test -race -count=1 ./... )

# Build the binary locally
( cd services/strategy-router && go build ./... )

# Smoke test: classifier + router on fake data
( cd services/strategy-router && go test -run TestRunOnce -v ./internal/router/... )

# Validate the K8s manifest renders
kubectl kustomize k8s/base | grep -A1 'name: strategy-router'
```

End-to-end on Stage:

1. `kubectl -n bitso-trading-dev apply -k k8s/overlays/development`
2. `kubectl -n bitso-trading-dev port-forward svc/strategy-router 8092:8092`
3. `curl localhost:8092/api/v1/router/state | jq '.last_decision'`
4. `curl -X POST localhost:8092/api/v1/router/run | jq` — should return a `Decision` with `regime`, `preferred`, `current`, and either `noop` (no switch needed) or `blocked` (guardrail held).

## 10) Future work (still tracked)

- ✅ **Per-book router (2026-05-22).** `STRATEGY_ROUTER_BOOK` accepts comma-separated books; `BookCoordinator` runs one engine per book; metrics include a `book` label. See [`POST-POINT-10-ROADMAP-2026-05-22.md`](POST-POINT-10-ROADMAP-2026-05-22.md) item 10.
- ✅ **Grafana dashboard (2026-05-22).** `monitoring/grafana/dashboards/services/strategy-router.json` — import via `./scripts/grafana-import-dashboard.sh strategy-router`.
- **Meta strategy in-process.** The original POINT-10 doc points at a `meta_router` strategy type as the eventual evolution. Defer until ≥ 4 weeks of clean Stage classification (item 1 in [`POST-POINT-10-IMPLEMENTATION-STATUS-2026-05-25.md`](POST-POINT-10-IMPLEMENTATION-STATUS-2026-05-25.md)).
- **Agent-coordinator threshold tuner.** Plug the audit log into `services/agent-coordinator` so an LLM agent can *recommend* threshold changes off-line. The router itself remains deterministic; only humans (or the agent with an operator approval gate) get to flip the env vars.

## 11) Related files

- `services/strategy-router/cmd/main.go`
- `services/strategy-router/internal/{config,classifier,router,clients,server,metrics}`
- `services/strategy-router/Dockerfile`
- `k8s/base/strategy-router.yaml`
- `k8s/base/kustomization.yaml` (updated)
- `scripts/strategy-regime-router.sh` (Phase 1 reference)
- `docs/strategy-fee-accuracy/POINT-10-STRATEGY-REGIME-ROUTER.md` (Phase 1 design)
- `docs/strategy-fee-accuracy/README.md`
