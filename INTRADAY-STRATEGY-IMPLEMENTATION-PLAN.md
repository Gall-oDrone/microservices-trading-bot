# Intraday Strategy & Metrics Implementation Plan

This document outlines the recommended steps to safely implement trading strategies and metrics for intraday trades. It follows the [Bitso Testing Environment](https://docs.bitso.com/bitso-api/docs/set-up-your-testing-environment) and [API Overview](https://docs.bitso.com/bitso-api/docs/api-overview) practices.

**Principle:** Use the Bitso testing (stage) environment and add safety layers before running new strategies with real value. Backtesting is planned last so that strategies are validated in stage/paper first.

---

## Current Bitso Integration

- **REST API base URL** is configurable via `SetAPIBaseURL()` in `shared/pkg/bitso/client.go`.
- **Testing environment:** `https://stage.bitso.com/api` (production: `https://bitso.com/api`).
- The **trading-engine** already uses the stage URL and stage credentials (`STAGE_BITSO_API_KEY`, `STAGE_BITSO_APISECRET`) in `services/trading-engine/cmd/main.go`.
- You need separate Bitso accounts and API keys for testing vs production; use stage keys when pointing at `stage.bitso.com`.

---

## Implementation Phases (in order)

### Phase 1: Bitso Environment Configuration (env-driven)

**Goal:** Make the Bitso API base URL configurable via environment so you can switch between stage and production without code changes.

**Tasks:**

1. Add `BITSO_API_BASE_URL` to shared config (and optionally to trading-engine config).
   - Default: `https://stage.bitso.com/api` for safety.
   - Production: `https://bitso.com/api`.
2. In `services/trading-engine/cmd/main.go`, replace the hardcoded `SetAPIBaseURL("https://stage.bitso.com/api")` with the value from config/env.
3. Document in README / CONFIG:
   - Use `BITSO_API_BASE_URL=https://stage.bitso.com/api` and stage API keys for testing.
   - Use production URL and production API keys only when intentionally going live.
4. If market-data or other services call Bitso REST, ensure they respect the same base URL where applicable.

**Files to touch:** `shared/pkg/config/config.go`, `services/trading-engine/cmd/main.go`, README or CONFIG docs.

---

### Phase 2: Paper Trading / Dry-Run Mode

**Goal:** Allow the trading engine to simulate orders without calling Bitso’s `PlaceOrder`, so new intraday strategies can be tested end-to-end without sending orders to stage (or production).

**Tasks:**

1. Add a **dry-run** (paper trading) flag to the trading engine, e.g.:
   - Env: `DRY_RUN=true` or config: `DryRun bool`.
2. In the executor (`services/trading-engine/internal/execution/executor.go`):
   - If dry-run: log the intended order (book, side, amount, price, reason) and return success without calling `bitsoClient.PlaceOrder()`.
   - If not dry-run: keep current behavior (call Bitso).
3. Optionally:
   - Maintain a simple in-memory or Redis “simulated” order log for dry-run so downstream metrics/tests can see “would have placed” orders.
4. Document:
   - How to run in dry-run for local and K8s (e.g. set `DRY_RUN=true` in deployment or env).

**Files to touch:** `services/trading-engine/internal/execution/executor.go`, `services/trading-engine/internal/config` (if exists) or main config, deployment manifests (env example), README.

---

### Phase 3: Order & Fill Sync (Position Sync from Bitso)

**Goal:** Keep order-management's view of orders and positions in sync with Bitso so that positions and P&L (realized/unrealized) are accurate for intraday metrics.

**Tasks:**

1. **When orders are placed:** Ensure every order placed by the trading-engine is also sent to order-management (e.g. via Kafka or HTTP) with Bitso order ID, so order-management has a record of “our” orders.
2. **Sync order status and fills from Bitso:**
   - Option A: **Polling** – A small job or loop in trading-engine or order-management that periodically calls Bitso’s `LookupOrder` / `LookupOrders` (and possibly `OrderTrades`) for open/recent orders and updates order-management (status, filled amount, fill price).
   - Option B: **Webhooks** – If Bitso supports order/fill webhooks, subscribe and push updates to order-management.
3. **Update positions:** When an order moves to filled (or partial fill), order-management should:
   - Update the order’s status and fill info.
   - Call position logic (e.g. `UpdateFromFill`) so that positions and P&amp;L are updated in real time.
4. **Persistence:** Ensure order-management uses a persistent store (e.g. Redis) for positions/orders if it currently uses only in-memory, so sync state survives restarts.

**Files to touch:** `services/trading-engine` (publish placed orders to order-management), `services/order-management` (reconciliation/sync job or webhook handler, position update on fill), shared Bitso client usage for `LookupOrder(s)` and `OrderTrades`.

---

### Phase 4: Daily Loss Limit & Max Drawdown (Live Risk)

**Goal:** Add intraday risk controls so that if daily loss or drawdown exceeds limits, the system stops or pauses trading automatically.

**Tasks:**

1. **Daily loss limit:**
   - Define a configurable max daily loss (e.g. in MXN or % of starting equity).
   - At strategy-executor or trading-engine level, before executing a new signal, compute "realized P&L today" (from positions/orders closed today) and optionally unrealized.
   - If daily loss >= limit, reject new trades and optionally emit an alert.
2. **Max drawdown:**
   - Track peak equity (or peak balance) over a rolling window (e.g. daily or since start of session).
   - If current equity drops below `peak - max_drawdown_pct` (or absolute amount), pause trading and optionally alert.
3. **Configuration:** Add these to `TradingConfig` or risk config (e.g. `MaxDailyLoss`, `MaxDrawdownPct`), and wire them in the risk manager used at execution time.
4. **Metrics:** Expose Prometheus gauges for “daily P&amp;L” and “current drawdown” so monitoring and dashboards can show risk state.

**Files to touch:** `shared/pkg/models/trading_config.go`, `services/strategy-executor/internal/risk/manager.go`, and optionally `services/order-management/internal/risk/risk_manager.go`; metrics in strategy-executor or order-management.

---

### Phase 5: Intraday Metrics & Observability

**Goal:** Expose metrics that support intraday strategy tuning and monitoring (without changing strategy logic yet).

**Tasks:**

1. **Metrics to add (examples):**
   - Daily realized P&amp;L (gauge or counter).
   - Daily unrealized P&amp;L (gauge).
   - Current drawdown (absolute and/or %).
   - Number of trades today (per book, per strategy).
   - Win rate today (if you have trade outcome data).
2. **Where:** Prefer strategy-executor and/or order-management; use existing Prometheus patterns already in the codebase.
3. **Dashboards:** Add or update Grafana panels for these so operators can monitor intraday risk and performance.

**Files to touch:** `services/strategy-executor/internal/metrics/`, `services/order-management/internal/metrics/`, `monitoring/` or Grafana JSON.

---

### Phase 6: Backtesting (last)

**Goal:** Use the existing backtesting framework to validate intraday strategies on historical data after they have been run in stage/paper and risk controls are in place.

**Tasks:**

1. Run backtests for any new intraday strategy (e.g. session-based, time-of-day, or existing basic/trend/arbitrage with intraday parameters).
2. Compare backtest metrics (e.g. max drawdown, Sharpe, win rate) with what you observe in stage/paper.
3. Document how to run backtests and how results map to the live metrics added in Phase 5.

**Files to touch:** `services/backtesting/`, documentation (this file or README).

---

## Summary Table

| Phase | Description                    | Outcome                                      |
|-------|--------------------------------|----------------------------------------------|
| 1     | Bitso env-driven URL           | Switch stage/production via config           |
| 2     | Paper trading / dry-run        | Test strategies without placing orders      |
| 3     | Order & fill sync              | Accurate positions and P&amp;L for intraday  |
| 4     | Daily loss & drawdown limits   | Automatic risk halt                         |
| 5     | Intraday metrics               | Observability for live intraday trading      |
| 6     | Backtesting                    | Historical validation of strategies         |

---

## References

- [Bitso: Set Up Your Testing Environment](https://docs.bitso.com/bitso-api/docs/set-up-your-testing-environment)
- [Bitso: API Overview](https://docs.bitso.com/bitso-api/docs/api-overview)
- Project: `DEVELOPMENT-ROADMAP.md`, `REMAINING-PHASES-CHECKLIST.md`
- Bitso client: `shared/pkg/bitso/client.go` (`SetAPIBaseURL`, `LookupOrder`, `LookupOrders`, `OrderTrades`)

---

**Document version:** 1.0  
**Status:** Planning
