# Post-POINT-10 Roadmap

Date: 2026-05-22  
Repository: `microservices-trading-bot`

## Context

POINT-9 ([`POINT-9-REALIZED-FEES.md`](POINT-9-REALIZED-FEES.md)) and POINT-10 Phase 1 + Phase 2 ([`POINT-10-STRATEGY-REGIME-ROUTER.md`](POINT-10-STRATEGY-REGIME-ROUTER.md), [`STRATEGY-REGIME-ROUTER-SERVICE-2026-05-22.md`](STRATEGY-REGIME-ROUTER-SERVICE-2026-05-22.md)) shipped the fee-honesty wiring and the regime-driven routing service. This document is the prioritized follow-up list that came out of the post-implementation review on 2026-05-22.

Items 2–10 (except the Stage soak in item 1) are implemented on branch `feat/k8s-deployment-manifests`.

**Status snapshot (2026-05-25):** See [`POST-POINT-10-IMPLEMENTATION-STATUS-2026-05-25.md`](POST-POINT-10-IMPLEMENTATION-STATUS-2026-05-25.md) for verification commands, Stage workflow, and operator next steps.

## Status legend

| Symbol | Meaning |
|--------|---------|
| ✅ | Completed |
| 🔄 | In progress |
| 📋 | Planned / not yet started |
| ⏸️ | Deferred — depends on something else |

---

## Now (this week) — close the loop on Phase 2

### 1. 📋 Stage soak with `DRY_RUN=true`

Deploy `strategy-router` to `bitso-trading-dev` and let it run for 24–48 h **alongside** `scripts/strategy-regime-router.sh` (also in dry-run, from a laptop). Goal: confirm both classifiers pick the same regime on the same indicator snapshots.

**Acceptance:**
- `strategy_router_regime{regime=…}` and the bash audit log agree for ≥ 99 % of poll cycles.
- `strategy_router_evaluation_errors_total` stays flat (no snapshot fetch failures).
- `strategy_router_evaluations_total` rate matches `1 / ROUTER_INTERVAL_SEC` (i.e. 1 / 30 = ~0.033 Hz at default).

**Owner:** operator. **Not blocked** — K8s/CI/monitoring wiring is merged; this is manual validation.

### 2. ✅ K8s overlay wiring

`k8s/base/kustomization.yaml` already lists `strategy-router.yaml` (from the Phase 2 commit), but `k8s/overlays/{development,staging,production}/kustomization.yaml` were missing the `ECR_REGISTRY_PLACEHOLDER/strategy-router` image rewrite. Without it, `kubectl apply -k k8s/overlays/development` would deploy a pod referencing `strategy-router:latest` which doesn't exist in ECR — the pod would `ImagePullBackOff`.

**Done:**
- Added `ECR_REGISTRY_PLACEHOLDER/strategy-router → 326105557351.dkr.ecr.us-east-1.amazonaws.com/strategy-router:latest` to the development overlay.
- Added the same entry (with the placeholder kept for the CI/CD pipeline to substitute) in staging and production overlays.

### 3. ✅ ServiceMonitor + Prometheus alerts

The router exposes Prometheus metrics on `:8092/metrics` but Prometheus had no `ServiceMonitor` for it, so no series would be ingested. Three new alerts wrap the most common failure modes.

**Done:**

- New `ServiceMonitor` `strategy-router` in `k8s/monitoring/servicemonitors.yaml` (same shape as the existing `strategy-executor` one).
- New `PrometheusRule` block `strategy-router-alerts` in `k8s/monitoring/prometheus-rules.yaml`:

  | Alert | Expression | For | Severity | Catches |
  |-------|-----------|-----|----------|---------|
  | `RouterBlockedHasPositionStuck` | `sum(increase(strategy_router_blocked_total{reason="has_position"}[30m])) > 60` | 10m | warning | Active strategy stuck with an open position, router can't switch out |
  | `RouterEvaluationsFailing` | `sum(increase(strategy_router_evaluation_errors_total[5m])) > 5` | 2m | warning | strategy-executor unreachable / snapshot failures |
  | `RouterNotEvaluating` | `sum(rate(strategy_router_evaluations_total[5m])) == 0` | 5m | critical | Router loop died (process crashed, AutoStart=false, ticker stalled) |

  Alert expressions use `sum()` because metrics are labeled with `book` after item 10 (per-book routing).

  Threshold rationale: 60 blocked-cycles in 30 m corresponds to a stuck position for ~30 m at the default 30 s polling interval. A real take-profit / max-hold can take 15 m on a calm regime — alerting at 30 m gives 2× headroom before paging.

### 4. ✅ CI/CD build + deploy wiring

`.github/workflows/ecr-publish.yml` builds one ECR image per `services/*/Dockerfile` from a matrix. The matrix didn't include `strategy-router`, so the image never made it to ECR. The post-build "Wait for deployments" loop was also missing the new deployment.

**Done:**
- Added `strategy-router` to the matrix `service:` list.
- Added `strategy-router` to the `for deploy in …` rollout-wait loop.

After merge, the next push to `feat/k8s-deployment-manifests` (or `main` / `staging`) will produce `326105557351.dkr.ecr.us-east-1.amazonaws.com/strategy-router:<sha>` and roll the deployment in `bitso-trading-dev` automatically.

---

## Short term — Phase 2 deserves a dashboard, Phase 1 of POINT-9 needs to follow the traffic

### 5. ✅ Grafana dashboard for the router

**Done:**
- `monitoring/grafana/dashboards/services/strategy-router.json` — regime, active strategy, switches, blocked reasons, evaluation latency percentiles.
- `scripts/grafana-import-dashboard.sh strategy-router` imports the dashboard.

Import example:

```bash
GRAFANA_URL=http://localhost:3001 ./scripts/grafana-import-dashboard.sh strategy-router
```

### 6. ✅ Apply POINT-9 fee-honesty to `mean_reversion` and `momentum`

**Done:** [`POINT-11-FEE-HONESTY-MEAN-REVERSION-MOMENTUM-2026-05-22.md`](POINT-11-FEE-HONESTY-MEAN-REVERSION-MOMENTUM-2026-05-22.md)

- Shared `fee_aware.go` helpers (`RealizedQuotePnL`, per-leg fee storage).
- `mean_reversion` and `momentum` implement `OrderFillAware`; exits wait for confirmed SELL fills.

### 7. ✅ Hard stop-loss for `momentum`

**Done** in `momentum_strategy.go`: `stop_loss_quote`, `max_position_hold_seconds`, `max_daily_loss_quote`, `daily_loss_reset_hour_utc`, `exit_reason` metadata.

**Organic startup:** `scripts/start-organic-trading.sh` exposes `MOMENTUM_STOP_LOSS_QUOTE`, `MOMENTUM_MAX_POSITION_HOLD_SEC`, `MOMENTUM_MAX_DAILY_LOSS_QUOTE`, `MOMENTUM_DAILY_LOSS_RESET_HOUR_UTC` (0 = disabled).

---

## Medium term — operability improvements

### 8. ✅ `scripts/start-organic-trading.sh` router-aware mode

**Done:** `ROUTER_MANAGED=true` registers strategies (default: `mean_reversion,limit_profit,momentum`) via `POST /api/v1/strategies` but skips `start`. The in-cluster `strategy-router` (or manual `POST …/start`) picks the active strategy.

Example:

```bash
ROUTER_MANAGED=true BOOK=btc_mxn ./scripts/start-organic-trading.sh
```

### 9. ✅ Backtest harness for the classifier

**Done:** `services/strategy-router/cmd/classifier-backtest/`

Replays indicator snapshots through `classifier.Classify` and prints a regime histogram.

```bash
# Live samples from strategy-executor
go run ./services/strategy-router/cmd/classifier-backtest/main.go \
  -url http://127.0.0.1:8084 -book btc_mxn -samples 100

# JSONL replay from stdin (e.g. paper-trading-reporter export)
cat snapshots.jsonl | go run ./services/strategy-router/cmd/classifier-backtest/main.go -stdin
```

### 10. ✅ Per-book parametrization

**Done:**
- `STRATEGY_ROUTER_BOOK` accepts comma-separated books (`btc_mxn,eth_mxn`).
- `BookCoordinator` runs one `Engine` per book; all Prometheus metrics include a `book` label.
- `GET /api/v1/router/state` returns `last_decisions` (one entry per book).

---

## Long term — the rest of POINT-10's future-work list

### 11. ⏸️ `agent-coordinator` threshold tuner

Hook the router's JSON audit log into `services/agent-coordinator`. The LLM agent *recommends* threshold changes from a week of audit data; operator (or future approval gate per [`agentic-ai/AGENTIC-AI-INTEGRATION-PLAN-2026-05-07.md`](../agentic-ai/AGENTIC-AI-INTEGRATION-PLAN-2026-05-07.md) Phase 2) accepts via env-var change. Router itself stays deterministic — only humans (or the agent with approval) get to flip the env vars.

**Depends on:** item 11 of the agentic-ai plan (approval workflow) and at least one week of clean Stage audit logs (item 1 above).

### 12. ⏸️ In-process `meta_router` strategy type

The original POINT-10 doc deliberately deferred this. Worth revisiting once we have ≥ 4 weeks of clean Stage classification — the rewrite cost (position handoff, fill correlation across inner strategies, P&L attribution, nested Redis state) is justifiable only if the external router proves the model first.

**Depends on:** item 1 (Stage soak) with ≥ 4 weeks of data; item 5 (dashboard) to make regression analysis tractable.

---

## Acceptance criteria for the post-POINT-10 milestone

The post-POINT-10 milestone is "done" when:

- [x] `strategy-router` builds and deploys via CI on every push (item 4)
- [x] Image is rewritten in all three overlays (item 2)
- [x] Prometheus scrapes the router and the three alerts are armed (item 3)
- [ ] Item 1 (Stage soak) has logged ≥ 7 days of agreeing classifications between bash + Go routers
- [x] Item 5 (Grafana dashboard) shows regime + active strategy + switches without ad-hoc PromQL
- [x] Item 6 (POINT-11 fee honesty) has flipped `mean_reversion` and `momentum` to realized-fee P&L
- [x] Item 7 (momentum stop-loss) has parameter parity with `limit_profit`

Items 8–10 are **complete** but not required for the milestone gate above.

---

## Related documents

- [`POINT-9-REALIZED-FEES.md`](POINT-9-REALIZED-FEES.md)
- [`POINT-11-FEE-HONESTY-MEAN-REVERSION-MOMENTUM-2026-05-22.md`](POINT-11-FEE-HONESTY-MEAN-REVERSION-MOMENTUM-2026-05-22.md)
- [`POINT-10-STRATEGY-REGIME-ROUTER.md`](POINT-10-STRATEGY-REGIME-ROUTER.md) — Phase 1 design
- [`STRATEGY-REGIME-ROUTER-SERVICE-2026-05-22.md`](STRATEGY-REGIME-ROUTER-SERVICE-2026-05-22.md) — Phase 2 service
- [`README.md`](README.md) — folder index
- [`../MOMENTUM-STRATEGY.md`](../MOMENTUM-STRATEGY.md) §7 — momentum risk roadmap
- [`../LIMIT-PROFIT-IMPROVEMENTS.md`](../LIMIT-PROFIT-IMPROVEMENTS.md) — limit_profit roadmap
- [`../agentic-ai/AGENTIC-AI-INTEGRATION-PLAN-2026-05-07.md`](../agentic-ai/AGENTIC-AI-INTEGRATION-PLAN-2026-05-07.md) — agent approval workflow context
