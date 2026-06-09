# Stage Execution Soak — Phase 3 Reconciliation (2026-06-09)

Date: **2026-06-09** (reconciliation run ~15:35–16:00 UTC)  
Repository: `microservices-trading-bot`  
Related: [`STAGE-EXECUTION-SOAK-VERIFICATION-2026-06-09.md`](STAGE-EXECUTION-SOAK-VERIFICATION-2026-06-09.md), [`STAGE-EXECUTION-SOAK-OPERATOR-GUIDE-2026-06-04.md`](STAGE-EXECUTION-SOAK-OPERATOR-GUIDE-2026-06-04.md)

---

## Summary

Phase 3 proved the live Stage order path (engine → Bitso → OM) but left a **stuck SELL** at `partially_filled` (0.0001 / 0.001 BTC) with repeated Bitso **312** lookup errors and strategy state drift (`has_position=true`, `pending_sell=true`). This pass ships an **order-management partial-fill fix**, redeploys OM, restarts `market-data`, and resets `mean_reversion_btc_mxn` so Phase 3 can continue cleanly.

---

## Code changes (order-management)

### 1. Incremental partial-fill sync (`SyncOrderFromBitsoTrades`)

**Problem:** User-trades poller delivered multiple single-leg SELL fills in a burst (~04:00 UTC). Each poll passed one trade to `SyncOrderFromBitsoTrades`, which **summed only the current leg** instead of accumulating against `order.FilledAmount`. Redis showed `filled_amount: 0.0001` while Bitso had more legs.

**Fix (`order_manager.go`):**

- Track synced trade TIDs in `metadata.synced_trade_tids`.
- Single-leg polls: `filledMajor = prevFilled + newLeg` (incremental).
- Multi-leg `/order_trades` snapshot: sum all legs (authoritative cumulative).
- Skip already-synced TIDs to prevent double-counting.
- Use `tradeMajorAbs()` for SELL legs (Bitso reports negative major).

### 2. Bitso 312 stale-order handling (`bitso_sync.go`)

**Problem:** Completed/evicted OIDs return error **312** on `LookupOrders`. OM retried forever without closing the local order.

**Fix:**

- Detect batch `LookupOrders` 312 via `bitso_errors.go` helper.
- Fall back to `OrderTrades`; if empty after 312 eviction, increment stale retry counter.
- After `maxStaleRetries` (10), call `MarkOrderStale` → `cancelled` with `stale_reason: missing_from_bitso_after_retries`.

### 3. Tests

- `manager_test.go`: `TestSyncOrderFromBitsoTrades_incrementalPollLegs` — regression for Stage soak partial SELL burst.
- `bitso_sync_test.go`: 312 eviction + `OrderTrades` fallback path.

---

## Operator actions (this session)

```bash
# 1. Build + deploy OM (image tag = git SHA; see k8s/overlays/development/kustomization.yaml)
# 2. Full reconciliation
./scripts/reconcile-stage-execution-soak-2026-06-09.sh reconcile-all

# Or step-by-step:
./scripts/reconcile-stage-execution-soak-2026-06-09.sh check
./scripts/reconcile-stage-execution-soak-2026-06-09.sh restart-market-data
# wait ~2 min for OM stale-mark on SELL OID
./scripts/reconcile-stage-execution-soak-2026-06-09.sh reset-strategy
```

---

## Pre-reconciliation state (15:35 UTC)

| Item | Value |
|------|-------|
| Phase 3 elapsed | ~14.5 h |
| BUY `oVs9Fk5oEnzkfDqo` | **filled** 0.001 BTC @ 1,095,010 |
| SELL `VgdJKh6fKC2c1xrZ` | **partially_filled** 0.0001 / 0.001 |
| OM 312 errors (5 min) | ~30 |
| Strategy | `has_position=true`, `pending_sell=true`, `trade_count=0` |
| market-data Last Trade | ~7 h stale |
| Router | blocked (`mean_reversion holds a position`) |

---

## Post-reconciliation expectations

| Check | Expected |
|-------|----------|
| SELL OID | `cancelled` (stale) or `filled` if Bitso `/order_trades` returns remaining legs |
| Strategy | `has_position=false`, `pending_sell=false` after `reset-strategy` |
| market-data | `Last Trade` within minutes of restart |
| OM 312 spam | Stops once SELL leaves active-order list |
| Next signal | 0.001 BTC, passes OM validation (~1.1k MXN) |

---

## Phase 3 pass criteria (unchanged)

1. Engine log: `Successfully placed … order <oid>` (not dry-run)
2. OM: `Linked Bitso order to signal order`
3. Fill sync: `submitted` → `filled` on both legs of a round-trip
4. Fee honesty: `fee_rate` on `trading.order.fills` events
5. Strategy `trade_count` increments after complete exit

**Status after this pass:** Phase 3 still **in progress** — infrastructure and first live orders proven; awaiting a **clean** post-reconciliation round-trip before declaring PASS and moving to Phase 4.

---

## Monitor

```bash
./scripts/run-stage-execution-soak-2026-06-04.sh phase3-status
./scripts/reconcile-stage-execution-soak-2026-06-09.sh check
kubectl -n bitso-trading-dev logs deploy/order-management -f | grep -iE 'stale|fill|312'
```
