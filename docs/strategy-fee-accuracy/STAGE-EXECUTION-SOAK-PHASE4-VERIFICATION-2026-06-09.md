# Stage Execution Soak Verification Report — Phase 4 Started

Date: **2026-06-09** (Phase 4 start **17:11 UTC**)  
Repository: `microservices-trading-bot`  
Related: [`STAGE-EXECUTION-SOAK-OPERATOR-GUIDE-2026-06-04.md`](STAGE-EXECUTION-SOAK-OPERATOR-GUIDE-2026-06-04.md), [`STAGE-EXECUTION-SOAK-PHASE3-RECONCILIATION-2026-06-09.md`](STAGE-EXECUTION-SOAK-PHASE3-RECONCILIATION-2026-06-09.md), [`STAGE-EXECUTION-SOAK-VERIFICATION-2026-06-09.md`](STAGE-EXECUTION-SOAK-VERIFICATION-2026-06-09.md)

---

## Timeline

| Milestone | Timestamp (UTC) |
|-----------|-----------------|
| Phase 3 round-trip complete | 2026-06-09 ~16:30 |
| **Phase 4 started** | **2026-06-09T17:11:11Z** |
| Router first live start | `mean_reversion_btc_mxn` for `low_vol_range` |

Operator session: `./scripts/run-stage-execution-soak-2026-06-04.sh phase4-start`

Machine-readable window: `tmp/stage-execution-soak/execution-window.json`

---

## Executive summary

| Gate | Status | Notes |
|------|--------|-------|
| Classification short soak | ✅ **PASS** | 142/142 samples, rate=1.0 |
| Phase 3 round-trip + fee honesty | ✅ **PASS** | See Phase 3 reconciliation doc |
| Phase 4 — router live | ✅ **Started** | `strategy-router DRY_RUN=false` |
| Phase 4 — engine live | ✅ | `trading_engine_dry_run 0` |
| Phase 4 — canonical strategies registered | ✅ | `mean_reversion`, `momentum`, `limit_profit` @ 0.001 BTC |
| Phase 4 — router-managed start | ✅ | Router started `mean_reversion_btc_mxn` |
| Phase 4 — 24–48 h soak gate | 📋 **In progress** | Started 2026-06-09T17:11:11Z |

**Current priority:** Observe ≥ 24 h with router + live engine; watch for regime switches, duplicate strategies, and order failures.

---

## Phase 4 configuration (at start)

| Component | Expected | Actual (17:11 UTC) |
|-----------|----------|---------------------|
| `strategy-router` `DRY_RUN` | `false` | `false` |
| `trading-engine` live | `trading_engine_dry_run 0` | `0` |
| Bash router | Stopped | Stopped |
| Registered strategies | 3 canonical (stopped → router picks) | `mean_reversion_btc_mxn`, `momentum_btc_mxn`, `limit_profit_btc_mxn` |
| `position_size` | 0.001 | **0.001** on all registered |
| Running strategy | Router-selected | `mean_reversion_btc_mxn` |
| Current regime | — | `low_vol_range` |
| Router action | — | `started mean_reversion_btc_mxn` (1 switch) |

### `ROUTE_*` (strategy-router-soak.yaml)

| Regime | Strategy |
|--------|----------|
| `ROUTE_LOW_VOL` / `ROUTE_NEUTRAL` | `mean_reversion_btc_mxn` |
| `ROUTE_TRENDING_UP` / `ROUTE_TRENDING_DOWN` | `momentum_btc_mxn` |
| `ROUTE_HIGH_VOL` | `none` |

---

## Phase 4 pass criteria (24–48 h gate)

1. **Router health** — `strategy_router_evaluation_errors_total` flat; no sustained `not_registered` blocks
2. **Safe switching** — at most one strategy running per book; `has_position` blocks handoff
3. **Engine orders** — `orders_executed_total` increments only on valid signals; `orders_failed_total` stays low
4. **OM sync** — placed orders reach `filled`; no stuck `partially_filled` from float/sync bugs
5. **Session risk** — `session_risk_rejections_total` low with 0.001 BTC sizing
6. **Classification drift** — periodic `./scripts/run-stage-soak-2026-06-02.sh sample` stays ≥ 99%

---

## Monitor commands

```bash
./scripts/run-stage-execution-soak-2026-06-04.sh phase4-status

kubectl -n bitso-trading-dev logs deploy/strategy-router -f | grep -iE 'regime|switch|block|error'

kubectl -n bitso-trading-dev exec deploy/strategy-router -- \
  wget -qO- http://127.0.0.1:8092/metrics | grep -E 'evaluation_errors|switches_total|evaluations_total'

kubectl -n bitso-trading-dev exec deploy/trading-engine -- \
  wget -qO- http://127.0.0.1:8080/metrics | grep -E 'dry_run|orders_|signals_'

./scripts/run-stage-soak-2026-06-02.sh sample   # classification drift
./scripts/run-stage-execution-soak-2026-06-04.sh rollback   # incident rollback
```

---

## Incident rollback

```bash
./scripts/run-stage-execution-soak-2026-06-04.sh rollback
```

Stops Bitso placement (`trading-engine DRY_RUN=true`) and pauses autonomous routing (`strategy-router DRY_RUN=true`). Stop running strategies manually if needed.

---

## Next steps

1. **24–48 h observation** — run `phase4-status` every few hours; log regime distribution and switch count.
2. **First router-managed order** — confirm engine log OID + OM record when a signal fires.
3. **Phase 5** — after Phase 4 short gate PASS, extend to 7-day execution soak per operator guide.

---

## Related documents

- [`STAGE-EXECUTION-SOAK-OPERATOR-GUIDE-2026-06-04.md`](STAGE-EXECUTION-SOAK-OPERATOR-GUIDE-2026-06-04.md) — § Phase 4, § Phase 5
- [`STAGE-EXECUTION-SOAK-PHASE3-RECONCILIATION-2026-06-09.md`](STAGE-EXECUTION-SOAK-PHASE3-RECONCILIATION-2026-06-09.md)
- [`scripts/run-stage-execution-soak-2026-06-04.sh`](../../scripts/run-stage-execution-soak-2026-06-04.sh) — `phase4-start`, `phase4-status`
