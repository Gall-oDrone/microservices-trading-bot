# Stage Execution Soak Verification Report — Phase 2 PASS, Phase 3 In Progress

Date: **2026-06-09** (verification run)  
Repository: `microservices-trading-bot`  
Related: [`STAGE-EXECUTION-SOAK-OPERATOR-GUIDE-2026-06-04.md`](STAGE-EXECUTION-SOAK-OPERATOR-GUIDE-2026-06-04.md), [`STAGE-SOAK-VERIFICATION-2026-06-07.md`](STAGE-SOAK-VERIFICATION-2026-06-07.md) (classification PASS), [`STAGE-FINANCIAL-APPROACH-2026-06-04.md`](STAGE-FINANCIAL-APPROACH-2026-06-04.md)

---

## Timeline

| Milestone | Timestamp (UTC) |
|-----------|-----------------|
| Classification short soak PASS | 2026-06-07T22:45:08Z |
| Execution soak Phase 2 started | 2026-06-07T22:54:03Z |
| Phase 2 time gate (≥ 24 h) met | 2026-06-08T22:54:03Z |
| **Phase 2 PASS declared** | **2026-06-09T01:01:22Z** |
| **Phase 3 started** | **2026-06-09T01:06:52Z** |
| **Verification / status check** | **2026-06-09T01:16:06Z** |
| Phase 3 elapsed at verification | **~0.15 h** |

Operator session: Phase 3 started from terminal shell PID **689753** via `./scripts/run-stage-execution-soak-2026-06-04.sh phase3-start`.

Machine-readable window: `tmp/stage-execution-soak/execution-window.json`.

---

## Executive summary

| Gate | Status | Notes |
|------|--------|-------|
| Classification short soak (item 1) | ✅ **PASS** | 140/140 agreement — see 2026-06-07 report |
| Phase 2 — router lifecycle, engine dry | ✅ **PASS** | 26.2 h, 3,137 evaluations, 1 switch, zero errors |
| Phase 2 — no Bitso orders | ✅ | `trading_engine_dry_run 1` throughout Phase 2 |
| Phase 3 — engine live | ✅ **Started** | `trading_engine_dry_run 0` confirmed |
| Phase 3 — one strategy running | ✅ | `mean_reversion_btc_mxn`, `position_size=0.001` |
| Phase 3 — first Stage round-trip | 📋 **Pending** | No orders placed yet at verification |
| Phase 3 — fee honesty on fill | 📋 **Pending** | Awaits first complete round-trip |
| Extended 7-day classification gate | 📋 **In progress** | Continue periodic sampling during execution |

**Current priority:** Observe Phase 3 until the first controlled Stage round-trip completes (engine → Bitso → order-management → fill → realized `fee_rate`).

---

## Phase 2 — router lifecycle (PASS)

Started via `./scripts/run-stage-execution-soak-2026-06-04.sh phase2-start` after classification PASS.

| Metric | Value |
|--------|-------|
| `strategy-router` `DRY_RUN` | `false` |
| `trading-engine` `DRY_RUN` | `true` |
| Elapsed at Phase 3 transition | **26.2 h** |
| 24 h time gate | **PASS** |
| Evaluations | **3,137** (~1 per 30 s) |
| Strategy switches | **1** (`<none>` → `mean_reversion_btc_mxn`) |
| Actions | **1× started**, **3,136× noop** |
| Regime | `low_vol_range` only |
| `strategy_router_evaluation_errors_total` | Flat (none) |
| `strategy_router_blocked_total{reason="dry_run"}` | **Zero** (router not dry) |
| Bitso orders | **None** |
| Deployments | router, engine, executor all **1/1** |

### Verdict — Phase 2

```text
elapsed_hours = 26.2 ≥ 24  ✅
evaluation_errors = 0      ✅
engine_orders = 0          ✅
```

Router lifecycle behaved as designed: one clean start, stable noop convergence, no Bitso placement.

---

## Phase 3 — single-strategy order path (in progress)

Started via `./scripts/run-stage-execution-soak-2026-06-04.sh phase3-start` after Phase 2 time gate passed.

| Component | Expected | Actual at verification |
|-----------|----------|------------------------|
| `strategy-router` `DRY_RUN` | `true` | `true` |
| `trading-engine` `DRY_RUN` | unset / `false` | unset |
| `trading_engine_dry_run` metric | `0` | `0` |
| Running strategies | One | `["mean_reversion_btc_mxn"]` |
| `position_size` | Minimal | `0.001` |
| Strategy param `dry_run` | unset / `false` | `null` |
| `BITSO_API_BASE_URL` | Stage | `https://stage.bitso.com/api` |
| `STAGE_BITSO_API_KEY` | set | `<set>` |
| `ORDER_MANAGEMENT_URL` | set | `http://order-management:8082` |
| Orders placed | Await signal | **0** (engine listening on `trading.signals`) |

Router state after Phase 3 rollout (fresh pod): `dry_run=true`, regime `low_vol_range`, action `noop` — strategy already running from Phase 2, router correctly idle.

### What to validate (Phase 3 pass criteria)

Per [`STAGE-EXECUTION-SOAK-OPERATOR-GUIDE-2026-06-04.md`](STAGE-EXECUTION-SOAK-OPERATOR-GUIDE-2026-06-04.md) § Phase 3:

1. **trading-engine** logs — `Successfully placed BUY order <oid>` (not `[DRY-RUN] Would place`)
2. **order-management** — `Recorded order placed` with `bitso_order_id`
3. **Bitso Stage dashboard** — OID matches engine log
4. **Fill sync** — OM status `submitted` → `filled`
5. **Fee honesty** — realized `fee_rate` on first complete round-trip

---

## What was run (2026-06-09)

```bash
./scripts/run-stage-execution-soak-2026-06-04.sh phase2-status
./scripts/run-stage-execution-soak-2026-06-04.sh phase3-start
./scripts/run-stage-execution-soak-2026-06-04.sh phase3-status
```

---

## Monitor commands

```bash
# Phase 3 status (DRY_RUN switches, running strategies, order metrics)
./scripts/run-stage-execution-soak-2026-06-04.sh phase3-status

# Watch for first live order
kubectl -n bitso-trading-dev logs deploy/trading-engine -f | grep -iE 'order|placed|signal'

# Classification drift check (continue during execution)
./scripts/run-stage-soak-2026-06-02.sh sample
./scripts/run-stage-soak-2026-06-02.sh report

# Fast rollback if needed
./scripts/run-stage-execution-soak-2026-06-04.sh rollback
```

---

## Next steps

1. **Wait for first signal** — `mean_reversion_btc_mxn` must emit a BUY that passes thresholds (`entry_threshold=2`, `min_signal_interval=60`).
2. **Confirm order pipeline** — engine log OID, OM record, Stage dashboard, fill sync.
3. **Fee honesty spot-check** — realized `fee_rate` on fill event; strategy net P&L uses realized legs.
4. **After first round-trip validated** — proceed to Phase 4 (router + engine live) per execution guide.
5. **Continue classification sampling** — extended 7-day gate through ~2026-06-12+.

---

## Related documents

- [`STAGE-EXECUTION-SOAK-OPERATOR-GUIDE-2026-06-04.md`](STAGE-EXECUTION-SOAK-OPERATOR-GUIDE-2026-06-04.md)
- [`STAGE-SOAK-VERIFICATION-2026-06-07.md`](STAGE-SOAK-VERIFICATION-2026-06-07.md)
- [`STAGE-FINANCIAL-APPROACH-2026-06-04.md`](STAGE-FINANCIAL-APPROACH-2026-06-04.md)
- [`scripts/run-stage-execution-soak-2026-06-04.sh`](../../scripts/run-stage-execution-soak-2026-06-04.sh)
- [`../ORDER-FLOW-AND-BITSO-TESTING.md`](../ORDER-FLOW-AND-BITSO-TESTING.md)
