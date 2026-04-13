# Production approach: pending BUY lifecycle and venue alignment

This note captures how a production-grade trading stack should handle **`limit_profit`** (and similar) strategies when a **BUY** signal has been sent but the **exchange order** may still be resting—especially around **timeouts**, **cancellation**, and **reconciliation**.

**Related implementation (this repo):**

- Order-management: `POST /api/v1/orders/cancel-by-signal` — cancels on Bitso when `metadata.bitso_order_id` is present and Bitso credentials are configured, then transitions the order to **cancelled** in OM storage.
- Strategy-executor: when `pending_buy_timeout_seconds` > 0 and **`ORDER_MANAGEMENT_BASE_URL`** is set, the timeout path calls that endpoint **before** clearing local `pending_buy` state. If the call fails, **pending state is kept** (no silent local-only abandon).

---

## Principles

1. **Strategy state is not the venue.** Clearing `pending_buy` in memory (or Redis) without cancelling the resting order can cause **duplicate orders**, **late fills**, and **wrong P&amp;L**.

2. **Timeout should drive a coordinated workflow:** *request cancel on the venue (or via OM)* → *confirm terminal state* → *then* allow a new entry cycle.

3. **Signal / order correlation:** Use a stable **`event_id`** (`TradeSignalEvent.event_id`) end-to-end. OM indexes orders by **`signal_id`**; Bitso order id is stored in **`metadata.bitso_order_id`** after placement.

4. **Reconciliation:** A background job or periodic sync (Bitso `/open_orders`, user trades) remains necessary for missed Kafka messages or partial failures.

5. **Observability:** Log and alert on cancel failures, orders pending too long, and mismatches between strategy pending flags and OM order rows.

---

## Configuration

| Component | Variable / setting | Role |
|-----------|---------------------|------|
| **strategy-executor** | `ORDER_MANAGEMENT_BASE_URL` (e.g. `http://order-management:8082`) | Base URL for OM HTTP API (cancel-by-signal). |
| **strategy-executor** | `pending_buy_timeout_seconds` on `limit_profit` | After N seconds without fill, attempt OM cancel then clear local pending if cancel succeeds (when OM URL is set). |
| **order-management** | Bitso API credentials | Required for **venue** cancel via `bitso_order_id`. Without them, cancel-by-signal for a placed limit may fail if `bitso_order_id` is set. |

---

## Operational runbook (summary)

- If **`pending_buy`** is stuck: inspect OM for the order by **`signal_id`**, Bitso Stage UI for the resting order, and strategy-executor logs for cancel errors.
- Do not rely solely on local strategy timeout without **`ORDER_MANAGEMENT_BASE_URL`** and working OM + Bitso cancel path in production.

---

## References

- `docs/LIMIT-PROFIT-ROBUSTNESS.md`
- `docs/LIMIT-PROFIT-STRATEGY.md`
