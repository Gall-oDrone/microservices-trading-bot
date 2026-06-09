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

## Post-reconciliation results (~16:30 UTC)

| Check | Result |
|-------|--------|
| OM image deployed | `order-management:7fae8b0` |
| SELL `VgdJKh6fKC2c1xrZ` | **filled** 0.001 BTC (11 trade TIDs synced) |
| BUY `oVs9Fk5oEnzkfDqo` | **filled** 0.001 BTC (unchanged) |
| Strategy `mean_reversion_btc_mxn` | `has_position=false`, `trade_count=1`, no pending sell |
| market-data | Restarted; cluster price aligned with Bitso (~1,064,130 MXN) |
| OM 312 errors | **0** (last 5 min) |

### Follow-up fix (`7fae8b0`)

Initial deploy (`5618afa`) accumulated partial legs correctly but **Redis updates failed** when cumulative fill barely exceeded `0.001` by float noise (`0.0010000000000000002 > 0.001`). `NormalizeFilledAmount()` + epsilon validation in `order.Validate()` resolved persistence; SELL transitioned to `filled` within ~30s of rollout.

---

## Phase 3 pass criteria (unchanged)

1. Engine log: `Successfully placed … order <oid>` (not dry-run)
2. OM: `Linked Bitso order to signal order`
3. Fill sync: `submitted` → `filled` on both legs of a round-trip
4. Fee honesty: `fee_rate` on `trading.order.fills` events
5. Strategy `trade_count` increments after complete exit

**Status after this pass:** First **complete Stage round-trip** reconciled (BUY + SELL filled, fee metadata on both legs, strategy `trade_count=1`). Phase 3 spot-check **substantially complete** — operator should confirm Bitso Stage dashboard OIDs match logs, then proceed to Phase 4 per execution guide.

---

## Monitor

```bash
./scripts/run-stage-execution-soak-2026-06-04.sh phase3-status
./scripts/reconcile-stage-execution-soak-2026-06-09.sh check
kubectl -n bitso-trading-dev logs deploy/order-management -f | grep -iE 'stale|fill|312'
```
