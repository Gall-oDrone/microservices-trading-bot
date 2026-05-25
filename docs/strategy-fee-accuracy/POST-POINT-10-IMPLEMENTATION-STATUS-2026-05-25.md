# Post-POINT-10 Implementation Status

Date: 2026-05-25  
Repository: `microservices-trading-bot`  
Branch: `feat/k8s-deployment-manifests`

## Summary

Items **2–10** from [`POST-POINT-10-ROADMAP-2026-05-22.md`](POST-POINT-10-ROADMAP-2026-05-22.md) are implemented and verified on this branch. **Item 1** (Stage soak with `DRY_RUN=true`, bash vs Go classifier agreement) remains **operator-owned** and is intentionally out of scope for this engineering pass.

POINT-9 (realized fees) and POINT-10 Phase 1 (bash router) + Phase 2 (Go `strategy-router` service) were delivered in prior commits on the same branch; this document records the post-POINT-10 closure work and how to run it on Stage.

---

## Roadmap item status

| # | Item | Status | Evidence |
|---|------|--------|----------|
| 1 | Stage soak (`DRY_RUN=true`, 24–48 h, bash + Go agreement) | 📋 Operator | See §Operator next steps |
| 2 | K8s overlay ECR image rewrite for `strategy-router` | ✅ | `k8s/overlays/{development,staging,production}/kustomization.yaml` |
| 3 | ServiceMonitor + Prometheus alerts | ✅ | `k8s/monitoring/servicemonitors.yaml`, `prometheus-rules.yaml` (`strategy-router-alerts`) |
| 4 | CI/CD ECR build + rollout wait | ✅ | `.github/workflows/ecr-publish.yml` matrix + deploy loop |
| 5 | Grafana dashboard | ✅ | `monitoring/grafana/dashboards/services/strategy-router.json` |
| 6 | POINT-11 fee honesty (`mean_reversion`, `momentum`) | ✅ | `fee_aware.go`, `OrderFillAware` in both strategies — [`POINT-11-FEE-HONESTY-MEAN-REVERSION-MOMENTUM-2026-05-22.md`](POINT-11-FEE-HONESTY-MEAN-REVERSION-MOMENTUM-2026-05-22.md) |
| 7 | Momentum lifecycle parity | ✅ | `stop_loss_quote`, `max_position_hold_seconds`, `max_daily_loss_quote`, `exit_reason` in `momentum_strategy.go` |
| 8 | `ROUTER_MANAGED` organic startup | ✅ | `scripts/start-organic-trading.sh` |
| 9 | Classifier backtest CLI | ✅ | `services/strategy-router/cmd/classifier-backtest/` |
| 10 | Per-book routing | ✅ | `BookCoordinator`, `STRATEGY_ROUTER_BOOK` comma-separated, `book` label on metrics |

---

## Verification (2026-05-25)

```bash
# strategy-executor strategies
( cd services/strategy-executor && go test -count=1 ./internal/strategies/... )

# strategy-router (race detector)
( cd services/strategy-router && go test -race -count=1 ./... )

# K8s renders strategy-router
kubectl kustomize k8s/overlays/development | grep -A2 'name: strategy-router'

# Classifier backtest (requires port-forward to strategy-executor)
go run ./services/strategy-router/cmd/classifier-backtest/main.go \
  -url http://127.0.0.1:8084 -book btc_mxn -samples 50
```

All unit tests above passed on 2026-05-25 in the development environment.

---

## End-to-end Stage workflow (router-managed)

### 1. Register strategies (no auto-start)

```bash
NAMESPACE=bitso-trading-dev BOOK=btc_mxn \
  ROUTER_MANAGED=true \
  ./scripts/start-organic-trading.sh
```

This registers `mean_reversion`, `limit_profit`, and `momentum` (override with `STRATEGY_TYPES=...`) without calling `start`.

### 2. Deploy or confirm strategy-router

```bash
kubectl -n bitso-trading-dev apply -k k8s/overlays/development
kubectl -n bitso-trading-dev rollout status deploy/strategy-router
```

For first deploy after threshold changes, run with `DRY_RUN=true` on the router Deployment until regimes look sane (item 1).

### 3. Inspect router state

```bash
kubectl -n bitso-trading-dev port-forward svc/strategy-router 8092:8092
curl -s http://127.0.0.1:8092/api/v1/router/state | jq
```

### 4. Grafana

```bash
GRAFANA_URL=http://localhost:3001 ./scripts/grafana-import-dashboard.sh strategy-router
```

Watch `strategy_router_regime`, `strategy_router_active_strategy`, `strategy_router_blocked_total`, and the three alerts in `strategy-router-alerts`.

---

## Milestone gate

| Criterion | Status |
|-----------|--------|
| CI builds and deploys `strategy-router` | ✅ |
| ECR image rewrite in all overlays | ✅ |
| Prometheus scrapes + alerts armed | ✅ |
| Grafana dashboard for router | ✅ |
| POINT-11 fee honesty on MR + momentum | ✅ |
| Momentum lifecycle parity with `limit_profit` | ✅ |
| ≥ 7 days bash + Go classifier agreement (item 1) | 📋 Pending operator |

Items 8–10 are complete and not required for the milestone gate above, but they are production-ready on this branch.

---

## Deferred (not in items 2–10)

| Topic | Doc | Notes |
|-------|-----|-------|
| Stage soak (item 1) | This doc §Operator | Manual validation |
| `agent-coordinator` threshold tuner | POST-POINT-10 §11 | Needs approval workflow + audit history |
| In-process `meta_router` | POINT-10 §6 | Defer until ≥ 4 weeks clean Stage classification |
| `momentum_*` dedicated Prometheus counters | `MOMENTUM-STRATEGY.md` §7 | Generic `strategy_executor_signals_generated_total` suffices for now |
| Fee assumption vs realized histogram | `POINT-9-REALIZED-FEES.md` §7 | Alert when configured liquidity drifts from fills |
| FINANCIAL guide Phase 4 scripts | `FINANCIAL-STRATEGY-IMPLEMENTATION-GUIDE.md` | `backtest-strategy.sh` / `promote-strategy-to-stage.sh` not yet added; use `validate-limit-profit-backtest.sh` + backtest API |

---

## Operator next steps (recommended)

1. **Item 1 — Stage soak:** Deploy `strategy-router` with `DRY_RUN=true` for 24–48 h alongside `DRY_RUN=true ./scripts/strategy-regime-router.sh`. Compare `/tmp/strategy-regime-router.log` with `GET /api/v1/router/state` → `last_decisions`. Target ≥ 99 % regime agreement per poll cycle.
2. **Flip live routing:** Set `DRY_RUN=false` on `strategy-router` only after soak passes; keep `ROUTER_MANAGED=true` organic registration.
3. **Monitor:** Import `strategy-router` Grafana dashboard; confirm `RouterNotEvaluating` and `RouterEvaluationsFailing` stay quiet.
4. **Fee honesty spot-check:** On first round-trip under router-managed momentum or mean_reversion, confirm `trading.order.fills` events carry `fee_rate` and strategy logs show net P&L using realized legs.
5. **Merge PR:** Open or merge `feat/k8s-deployment-manifests` → `main` so CI publishes `strategy-router:<sha>` on every push.

---

## Related documents

- [`POST-POINT-10-ROADMAP-2026-05-22.md`](POST-POINT-10-ROADMAP-2026-05-22.md) — original prioritized list
- [`README.md`](README.md) — folder index
- [`POINT-9-REALIZED-FEES.md`](POINT-9-REALIZED-FEES.md)
- [`POINT-10-STRATEGY-REGIME-ROUTER.md`](POINT-10-STRATEGY-REGIME-ROUTER.md)
- [`STRATEGY-REGIME-ROUTER-SERVICE-2026-05-22.md`](STRATEGY-REGIME-ROUTER-SERVICE-2026-05-22.md)
- [`POINT-11-FEE-HONESTY-MEAN-REVERSION-MOMENTUM-2026-05-22.md`](POINT-11-FEE-HONESTY-MEAN-REVERSION-MOMENTUM-2026-05-22.md)
- [`../MOMENTUM-STRATEGY.md`](../MOMENTUM-STRATEGY.md)
- [`../LIMIT-PROFIT-ROBUSTNESS.md`](../LIMIT-PROFIT-ROBUSTNESS.md)
- [`../ORGANIC-TRADING-STARTUP.md`](../ORGANIC-TRADING-STARTUP.md)
