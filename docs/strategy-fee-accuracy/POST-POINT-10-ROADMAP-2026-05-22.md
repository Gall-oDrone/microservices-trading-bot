# Post-POINT-10 Roadmap

Date: 2026-05-22
Repository: `microservices-trading-bot`

## Context

POINT-9 ([`POINT-9-REALIZED-FEES.md`](POINT-9-REALIZED-FEES.md)) and POINT-10 Phase 1 + Phase 2 ([`POINT-10-STRATEGY-REGIME-ROUTER.md`](POINT-10-STRATEGY-REGIME-ROUTER.md), [`STRATEGY-REGIME-ROUTER-SERVICE-2026-05-22.md`](STRATEGY-REGIME-ROUTER-SERVICE-2026-05-22.md)) shipped the fee-honesty wiring and the regime-driven routing service. This document is the prioritized follow-up list that came out of the post-implementation review on 2026-05-22.

The recommendations are grouped by horizon. Items 2–4 are implemented in the same PR that introduces this document; the rest are open work.

## Status legend

| Symbol | Meaning |
|--------|---------|
| ✅ | Completed in this PR |
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

**Owner:** operator. **Blocked by:** items 2–4 below being merged.

### 2. ✅ K8s overlay wiring (this PR)

`k8s/base/kustomization.yaml` already lists `strategy-router.yaml` (from the Phase 2 commit), but `k8s/overlays/{development,staging,production}/kustomization.yaml` were missing the `ECR_REGISTRY_PLACEHOLDER/strategy-router` image rewrite. Without it, `kubectl apply -k k8s/overlays/development` would deploy a pod referencing `strategy-router:latest` which doesn't exist in ECR — the pod would `ImagePullBackOff`.

**Done in this PR:**
- Added `ECR_REGISTRY_PLACEHOLDER/strategy-router → 326105557351.dkr.ecr.us-east-1.amazonaws.com/strategy-router:latest` to the development overlay.
- Added the same entry (with the placeholder kept for the CI/CD pipeline to substitute) in staging and production overlays.

### 3. ✅ ServiceMonitor + Prometheus alerts (this PR)

The router exposes Prometheus metrics on `:8092/metrics` but Prometheus had no `ServiceMonitor` for it, so no series would be ingested. Three new alerts wrap the most common failure modes.

**Done in this PR:**

- New `ServiceMonitor` `strategy-router` in `k8s/monitoring/servicemonitors.yaml` (same shape as the existing `strategy-executor` one).
- New `PrometheusRule` block `strategy-router-alerts` in `k8s/monitoring/prometheus-rules.yaml`:

  | Alert | Expression | For | Severity | Catches |
  |-------|-----------|-----|----------|---------|
  | `RouterBlockedHasPositionStuck` | `increase(strategy_router_blocked_total{reason="has_position"}[30m]) > 60` | 10m | warning | Active strategy stuck with an open position, router can't switch out |
  | `RouterEvaluationsFailing` | `increase(strategy_router_evaluation_errors_total[5m]) > 5` | 2m | warning | strategy-executor unreachable / snapshot failures |
  | `RouterNotEvaluating` | `rate(strategy_router_evaluations_total[5m]) == 0` | 5m | critical | Router loop died (process crashed, AutoStart=false, ticker stalled) |

  Threshold rationale: 60 blocked-cycles in 30 m corresponds to a stuck position for ~30 m at the default 30 s polling interval. A real take-profit / max-hold can take 15 m on a calm regime — alerting at 30 m gives 2× headroom before paging.

### 4. ✅ CI/CD build + deploy wiring (this PR)

`.github/workflows/ecr-publish.yml` builds one ECR image per `services/*/Dockerfile` from a matrix. The matrix didn't include `strategy-router`, so the image never made it to ECR. The post-build "Wait for deployments" loop was also missing the new deployment.

**Done in this PR:**
- Added `strategy-router` to the matrix `service:` list.
- Added `strategy-router` to the `for deploy in …` rollout-wait loop.

After merge, the next push to `feat/k8s-deployment-manifests` (or `main` / `staging`) will produce `326105557351.dkr.ecr.us-east-1.amazonaws.com/strategy-router:<sha>` and roll the deployment in `bitso-trading-dev` automatically.

---

## Short term — Phase 2 deserves a dashboard, Phase 1 of POINT-9 needs to follow the traffic

### 5. 📋 Grafana dashboard for the router

The Phase 2 doc mentions this as future work and it's a 2-hour job now that the metrics are emitted:

- Single-stat: current regime, current active strategy, last switch timestamp, last evaluation latency
- Time-series: regime label over time (one series per regime, stacked), active strategy over time, switches per hour by `from→to`, blocked reasons stacked, p50/p95/p99 evaluation latency
- Table: most recent 20 entries from `strategy_router_switches_total{...}` with delta annotations

**Deliverable:** `services/strategy-router/dashboards/strategy-router.json`, wired into `scripts/grafana-import-dashboard.sh`.

### 6. 📋 Apply POINT-9 fee-honesty to `mean_reversion` and `momentum`

**Biggest correctness gap exposed by Phase 2.** The router will *successfully* route traffic to `mean_reversion` and `momentum` the moment `DRY_RUN=false` flips on Stage — but those two strategies still use **configured** fee assumptions, not the Bitso-realized fee that `limit_profit` now respects after POINT-9.

The plumbing already exists: `OrderFillEvent.FeeRate` / `Liquidity` are populated by `services/order-management/internal/sync/user_trades_poller.go` and consumed by `services/strategy-executor/cmd/main.go` for every fill. All that's needed is the equivalent of `limit_profit_strategy.go`'s `handleBuyFillLocked` / `handleSellFillLocked` overrides in the other two strategies.

**Deliverable:** `docs/strategy-fee-accuracy/POINT-11-FEE-HONESTY-MEAN-REVERSION-MOMENTUM-<date>.md` — same structure as POINT-9. Should keep limit_profit's pattern: store realized fee per leg, use it in any subsequent P&L / threshold computation, fall back to configured rate when realized is unavailable.

### 7. 📋 Hard stop-loss for `momentum`

`docs/MOMENTUM-STRATEGY.md §7` lists this as ⏳ — "handled implicitly by RSI re-cross". With the router now able to hand traffic to `momentum` in `trending_*` regimes, a sudden regime flip with no stop-loss is a real downside (the position sits open until RSI crosses back into neutral, which can take hours).

**Port from `limit_profit`:** `stop_loss_quote`, `max_position_hold_seconds`, `max_daily_loss_quote`. Reuse the same `exit_reason` metadata so dashboards/Grafana don't need new wiring.

---

## Medium term — operability improvements

### 8. 📋 `scripts/start-organic-trading.sh` router-aware mode

Add a `ROUTER_MANAGED=true` env var that *registers* the strategies but doesn't `start` any of them — the router will pick. Today an operator who wants the router to drive has to manually stop everything the script just started.

### 9. 📋 Backtest harness for the classifier

A small Go tool that replays historical indicator snapshots from S3 / Redis through `classifier.Classify` and emits a regime distribution histogram. Lets us tune `ATR_*_PCT` / `BB_PB_*` thresholds against real Stage data instead of guessing. Reuses the existing snapshot exporter in `services/paper-trading-reporter`.

### 10. 📋 Per-book parametrization

Today `STRATEGY_ROUTER_BOOK` pins one book per Deployment. To run on `btc_mxn` + `eth_mxn` we'd need two Deployments. Easy change: accept a comma-separated list, iterate, label every metric with `book="…"`.

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
- [ ] Item 5 (Grafana dashboard) shows regime + active strategy + switches without ad-hoc PromQL
- [ ] Item 6 (POINT-11 fee honesty) has flipped `mean_reversion` and `momentum` to realized-fee P&L
- [ ] Item 7 (momentum stop-loss) has parameter parity with `limit_profit`

Items 8–12 are tracked but **not** required for the milestone.

---

## Related documents

- [`POINT-9-REALIZED-FEES.md`](POINT-9-REALIZED-FEES.md)
- [`POINT-10-STRATEGY-REGIME-ROUTER.md`](POINT-10-STRATEGY-REGIME-ROUTER.md) — Phase 1 design
- [`STRATEGY-REGIME-ROUTER-SERVICE-2026-05-22.md`](STRATEGY-REGIME-ROUTER-SERVICE-2026-05-22.md) — Phase 2 service
- [`README.md`](README.md) — folder index
- [`../MOMENTUM-STRATEGY.md`](../MOMENTUM-STRATEGY.md) §7 — momentum risk roadmap
- [`../LIMIT-PROFIT-IMPROVEMENTS.md`](../LIMIT-PROFIT-IMPROVEMENTS.md) — limit_profit roadmap
- [`../agentic-ai/AGENTIC-AI-INTEGRATION-PLAN-2026-05-07.md`](../agentic-ai/AGENTIC-AI-INTEGRATION-PLAN-2026-05-07.md) — agent approval workflow context
