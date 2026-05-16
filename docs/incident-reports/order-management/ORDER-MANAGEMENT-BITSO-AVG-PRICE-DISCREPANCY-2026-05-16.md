# Incident: Bitso average fill price vs. submitted limit — P&L discrepancy

**Date:** 2026-05-16  
**Repository:** `microservices-trading-bot`  
**Severity:** High (incorrect P&L and downstream strategy state)  
**Status:** Resolved (code fix merged)  
**Component:** `services/order-management` — Bitso sync job  
**Related:** `docs/LIMIT-PROFIT-STRATEGY.md`, `docs/runbooks/LIMIT-PROFIT-RUNBOOK.md`, `docs/agentic-ai/AGENTIC-AI-PRODUCTION-STRATEGY-2026-05-13.md` (deterministic execution truth)

---

## 1) Summary

Organic `limit_profit` runs showed **average buy/sell prices** in internal logs and in strategy-executor fill events that **matched the submitted limit prices** from the trading engine, while Bitso’s **exported trade history (CSV)** showed **different execution rates** (real fills). That drove **gross and net P&L** inside the platform away from what an operator could reconcile manually from Bitso.

After the fix, order-management records **`avg_price` from `/order_trades`** (VWAP over legs) when syncing filled orders, so strategy-executor P&L and Prometheus gauges align with **venue execution**, not the limit quote alone.

---

## 2) Symptoms

| Observation | Example (from Stage observation) |
|-------------|----------------------------------|
| Internal `avg_price` / `average_price` on BUY | **1,372,600** MXN/BTC |
| Bitso CSV `rate` for the same BUY | **1,372,560** MXN/BTC |
| Internal sell signal limit | **1,370,080** MXN/BTC |
| Bitso CSV `rate` for the SELL | **1,371,650** MXN/BTC |
| Effect | **Gross P&L** computed on wrong prices (e.g. −2.52 MXN vs manual **−0.91** MXN on 0.001 BTC using CSV rates) |
| Downstream | **`OrderFillEvent`** to strategy-executor carried wrong `average_price`; **`limit_profit_daily_realized_pnl_quote`** and session totals drifted from Bitso |

**How to spot it**

1. Compare OM logs `SyncOrderFromBitso: processing` **`avg_price`** to Bitso UI/CSV **`rate`** for the same `bitso_order_id`.
2. If `avg_price` always equals the **limit** from the signal/engine logs, investigate this path (pre-fix).

---

## 3) Root cause

**File:** `services/order-management/internal/sync/bitso_sync.go`  
**Function:** `applyBitsoUserOrder`

The Bitso sync job’s **fast path** (successful `LookupOrders` batch from `/v3/orders/{oids}`) built:

```text
filledAmount = original_amount - unfilled_amount
avgPrice     = uo.Price   // <- submitted LIMIT price on the order object
```

Bitso’s **`UserOrder`** struct only exposes **`price`** as the **order’s limit** (or quoted price). It does **not** expose the **executed volume-weighted average** after matching.

The code then called `SyncOrderFromBitso(ctx, oid, filledAmount, avgPrice, status)`, which **applied fills and published Kafka `OrderFillEvent`** using that limit as if it were the fill average.

**Why reconciliation stopped there**

- Once the order transitioned to **`filled`**, `SyncOrderFromBitso` **skipped further updates** for that order (`order.IsClosed()`).
- The **user-trades poller** later observed the **real** trade with the correct `price`, but **`handleTrade` → `SyncOrderFromBitsoTrades`** often found the order **already closed** and **exited without correcting** stored averages.

So the bug was **order of operations + wrong field**: persist limit-as-fill before real trades were applied, then ignore corrections.

---

## 4) Implemented solution

**Behavior (post-fix)**

1. When **`filledAmount > 0`**, call **`OrderTrades(oid)`** (`/v3/order_trades/{oid}`) and route through **`SyncOrderFromBitsoTrades`**, which already computes **VWAP** over `[]UserOrderTrade`.
2. If trades are **not yet available** (e.g. Bitso **378** “order has not matched yet” / race right after fill), **do not** call `SyncOrderFromBitso` with the limit price. **Defer**; the next sync tick or user-trades poll reconciles with real data.
3. When **`filledAmount == 0`** (open / no-fill), continue to use **`SyncOrderFromBitso`** with **`uo.Price`** only for **status**; the fill path is not taken.

**Tests**

- `services/order-management/internal/sync/bitso_sync_test.go` — regression tests for filled orders using **actual trade prices** vs. limits, deferral when trades missing, and unfilled status path.

**Commit reference (branch `feat/k8s-deployment-manifests`):** `ade4880` — *order-management(sync): use real Bitso fill price, not the submitted limit*.

---

## 5) Verification checklist

After deploying an image that includes the fix:

1. Run an organic `limit_profit` cycle (`scripts/start-organic-trading.sh`).
2. In OM logs, confirm **`SyncOrderFromBitsoTrades: calculated fill`** shows **`avg_price`** matching Bitso trade **`rate`** (within normal rounding).
3. On SELL fill, confirm strategy-executor log **`sell fill processed`** uses **`entry_price` / `exit_price`** from fills, and **`gross_pnl`** equals \((\text{exit} - \text{entry}) \times \text{size}\) before fees.
4. Cross-check **`limit_profit_daily_realized_pnl_quote`** vs. fee model (Bitso `GET /fees` when `use_bitso_fees` is true).

---

## 6) Operational notes

- **Rollout restart** (`kubectl rollout restart deployment/order-management …`) does **not** change image tags; ensure CI/CD has **built and deployed** the fixed digest.
- For **historical** wrong fills already persisted in OM/Redis, prefer **operational correction** scripts or a documented backfill process if you introduce one; this document does not prescribe data migration.
- **Canonical execution truth** remains in deterministic sync paths per `docs/agentic-ai/AGENTIC-AI-PRODUCTION-STRATEGY-2026-05-13.md`; CSV exports are a **secondary** check.

---

## 7) References

| Resource | Path / link |
|----------|-------------|
| Bitso sync job | `services/order-management/internal/sync/bitso_sync.go` |
| VWAP from trades | `services/order-management/internal/manager/order_manager.go` — `SyncOrderFromBitsoTrades` |
| User-trades poll | `services/order-management/internal/sync/user_trades_poller.go` |
| Limit profit behavior | `docs/LIMIT-PROFIT-STRATEGY.md` |
| Runbook | `docs/runbooks/LIMIT-PROFIT-RUNBOOK.md` |
