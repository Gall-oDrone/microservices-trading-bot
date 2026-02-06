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

### Phase 1: Bitso Environment Configuration (env-driven) ✅

**Goal:** Make the Bitso API base URL configurable via environment so you can switch between stage and production without code changes.

**Tasks:**

1. ~~Add `BITSO_API_BASE_URL` to shared config (and optionally to trading-engine config).~~ **Done.** `shared/pkg/config/config.go`: `BitsoAPIBaseURL` loaded from `BITSO_API_BASE_URL`; default `https://stage.bitso.com/api`.
2. ~~In `services/trading-engine/cmd/main.go`, replace the hardcoded `SetAPIBaseURL(...)` with the value from config/env.~~ **Done.** Uses `cfg.BitsoAPIBaseURL`.
3. Document in README / CONFIG:
   - Use `BITSO_API_BASE_URL=https://stage.bitso.com/api` and stage API keys (`STAGE_BITSO_API_KEY`, `STAGE_BITSO_API_SECRET`) for testing.
   - Use `BITSO_API_BASE_URL=https://bitso.com/api` and production API keys only when intentionally going live.
4. Market-data does not call Bitso REST (WebSocket only); no change needed there.

**Files touched:** `shared/pkg/config/config.go`, `services/trading-engine/cmd/main.go`, this plan.

---

### Phase 2: Paper Trading / Dry-Run Mode ✅

**Goal:** Test intraday strategies end-to-end without real funds. This is achieved by using **Bitso’s testing environment**, not by adding a local dry-run flag.

**Bitso testing environment:** [Set up your testing environment](https://docs.bitso.com/bitso-api/docs/set-up-your-testing-environment) — use a separate account and API credentials on **stage** (`https://stage.bitso.com`). Orders sent to the stage API are executed in the test environment only; no production funds are used.

**How this project does it:**

- The shared Bitso client (`shared/pkg/bitso/`) and trading-engine use the **configurable API base URL** (Phase 1).
- Default is **`https://stage.bitso.com/api`**; set `BITSO_API_BASE_URL` only when switching to production.
- Use **stage API keys** (`STAGE_BITSO_API_KEY`, `STAGE_BITSO_APISECRET`) with the stage URL. The trading-engine is already wired to stage credentials and stage URL (see `services/trading-engine/cmd/main.go` and `shared/pkg/config`).

**Conclusion:** Paper trading = point the app at Bitso’s testing environment (stage URL + stage keys). No additional dry-run flag or “simulate without calling Bitso” mode is required for this phase.

**Optional enhancement — local dry-run (no Bitso API calls):** For CI, local runs, or an extra safety layer where you never hit Bitso at all, you can add a **dry-run mode** that skips calling `PlaceOrder`:

- Env: `DRY_RUN=true` or config: `DryRun bool`.
- In the executor: if dry-run is set, log the intended order (book, side, amount, price, reason) and return success without calling `bitsoClient.PlaceOrder()`. Optionally persist “would have placed” orders to Redis or a log for downstream metrics/tests.
- Use case: automated tests, or running the engine without a Bitso account. Not required for Phase 2; implement when needed.

---

### Phase 3: Order & Fill Sync (Position Sync from Bitso)

**Goal:** Keep order-management's view of orders and positions in sync with Bitso so that positions and P&L (realized/unrealized) are accurate for intraday metrics.

**Tasks:**

1. **When orders are placed:** ~~Ensure every order placed by the trading-engine is also sent to order-management (e.g. via Kafka or HTTP) with Bitso order ID.~~ **Done.** Trading-engine publishes to Kafka topic `trading.orders.placed` (config: `KAFKA_TOPIC_ORDERS_PLACED`) after each successful `PlaceOrder`. Payload: `order_id`, `book`, `side`, `amount`, `price`, `strategy`. Order-management can consume this topic to create order records (consumer implementation pending).
2. **Sync order status and fills from Bitso:**
   - Option A: **Polling** – A small job or loop in trading-engine or order-management that periodically calls Bitso’s `LookupOrder` / `LookupOrders` (and possibly `OrderTrades`) for open/recent orders and updates order-management (status, filled amount, fill price). **Not yet implemented.**
   - Option B: **Webhooks** – If Bitso supports order/fill webhooks, subscribe and push updates to order-management.
3. **Update positions:** When an order moves to filled (or partial fill), order-management should update order status and call position logic (e.g. `UpdateFromFill`). Depends on (2).
4. **Persistence:** Order-management currently uses in-memory repositories; switch to Redis when needed for production.

**Files touched:** `shared/pkg/config` (`KafkaTopicOrdersPlaced`), `services/trading-engine` (Kafka producer, publish after PlaceOrder), executor returns order ID; **order-management** consumer for `trading.orders.placed` (`internal/consumer/orders_placed.go`), Bitso sync job (`internal/sync/bitso_sync.go`, polls LookupOrders and updates orders via `SyncOrderFromBitso`). Set `STAGE_BITSO_API_KEY` and `STAGE_BITSO_APISECRET` (and optionally `BITSO_API_BASE_URL`) in order-management to enable the sync job.

---

### Phase 4: Daily Loss Limit & Max Drawdown (Live Risk) ✅

**Goal:** Add intraday risk controls so that if daily loss or drawdown exceeds limits, the system stops or pauses trading automatically.

**Tasks:**

1. **Daily loss limit:** **Done.** `TradingConfig.MaxDailyLoss` (currency units; 0 = disabled). Before placing an order, trading-engine calls `SessionRiskProvider.GetSessionRisk()` (e.g. order-management `GET /api/v1/risk/session`) and `Executor.CheckSessionLimits(dailyRealizedPnL, drawdownPct)`; if daily realized P&L ≤ -MaxDailyLoss, execution is rejected.
2. **Max drawdown:** **Done.** `TradingConfig.MaxDrawdownPct` (0–100; 0 = disabled). Same check: if drawdown % ≥ MaxDrawdownPct, execution is rejected.
3. **Configuration:** **Done.** `shared/pkg/models/trading_config.go`: `MaxDailyLoss`, `MaxDrawdownPct`. Trading-engine executor uses them when `CheckSessionLimits` is called.
4. **Metrics:** Already in place (Phase 5): order-management exposes daily P&L and drawdown to Prometheus and Grafana. Order-management also exposes **GET /api/v1/risk/session** returning `daily_realized_pnl` and `drawdown_percent` for the trading-engine to use when a `SessionRiskProvider` is wired (e.g. HTTP client to order-management).

**Files touched:** `shared/pkg/models/trading_config.go`, `services/trading-engine/internal/execution` (CheckSessionLimits, SessionRiskProvider), `services/trading-engine/internal/engine` (call provider + CheckSessionLimits before execute), `services/order-management/internal/metrics` (SessionSnapshot), `services/order-management/internal/server` (GET /api/v1/risk/session). To enable limits: set MaxDailyLoss/MaxDrawdownPct in config and wire a SessionRiskProvider in trading-engine (e.g. HTTP client to order-management).

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

#### Phase 5 Implementation Status

| Item | Status | Location |
|------|--------|----------|
| Shared types & interfaces (MonetaryAmount, PnLRecorder, etc.) | Done | `shared/pkg/metrics/` |
| Prometheus gauges/counters for intraday | Done | `services/order-management/internal/metrics/prometheus.go` |
| IntradayAggregator (calculation logic) | Done | `services/order-management/internal/metrics/intraday_aggregator.go` |
| Manager records trade closed on fill | Done | `services/order-management/internal/manager/order_manager.go` |
| Wire aggregator in app | Done | Aggregator created in `cmd/main.go`, OrderManager constructed with it; manager started on app start |
| Feed equity & unrealized P&amp;L | Done | `feedIntradayMetrics()` goroutine every 60s calls `GetPositionSummary`, then `RecordDailyUnrealizedPnL` and `RecordEquityUpdate` |
| Unit tests (aggregator, manager) | Done | `services/order-management/internal/testing/`, `internal/testing/metrics/` |
| Integration tests | Done | `services/order-management/integration/` (see `testing/integration/order-management/README.md`) |
| Grafana dashboards | Done | `monitoring/grafana/dashboards/trading-metrics.json` (Intraday / P&amp;L row: daily realized/unrealized P&amp;L, drawdown, equity, trades today, wins/losses, win rate %) |

#### Remaining Phase 5 Tasks

- None. Dashboard includes stat and time-series panels for all intraday metrics (see `monitoring/grafana/dashboards/trading-metrics.json`).

---

### Phase 6: Backtesting (last)

**Goal:** Use the existing backtesting framework to validate intraday strategies on historical data after they have been run in stage/paper and risk controls are in place.

**Tasks:**

1. **Run backtests** for any new intraday strategy (e.g. session-based, time-of-day, or existing basic/trend/arbitrage with intraday parameters). Use `services/backtesting/`: run with market-data API (`MarketDataProvider`) or file-based data (`FileProvider`). See `services/backtesting/README.md` and `scripts/run_tests.sh` for tests and coverage.
2. **Compare** backtest metrics (max drawdown, Sharpe, win rate) with what you observe in stage/paper (Grafana intraday panels and order-management metrics).
3. **Document** how to run backtests and how results map to live metrics:
   - **Run backtests:** Start the backtesting service (and market-data if using HTTP provider); POST `/api/v1/backtests` with config (book, strategy, date range, data source). Get results via GET `/api/v1/backtests/{id}/results` and GET `/api/v1/backtests/{id}/report?format=text|json|html`.
   - **Map to live:** Phase 5 metrics (daily realized P&L, drawdown %, trades today, win rate) in Grafana correspond to backtest report fields (Total Return, Max Drawdown, Win Rate, Total Trades). Compare session-level backtest runs with same strategy parameters to stage/paper runs.

**Files to touch:** `services/backtesting/` (existing), this plan, optional: short “Backtesting for intraday” section in `services/backtesting/README.md` or `GETTING_STARTED.md`.

**Phase 6 workflow — run one backtest and compare to Grafana**

1. Start dependencies: market-data (if using HTTP provider), Redis, backtesting service.
2. Create backtest: `POST /api/v1/backtests` (book, strategy, start_date, end_date, initial_balance). Note `id`.
3. Poll `GET /api/v1/backtests/{id}` until status is `completed`.
4. Get results: `GET /api/v1/backtests/{id}/results` or `GET /api/v1/backtests/{id}/report?format=text`.
5. Compare to Grafana: Trading Platform Metrics dashboard — backtest Total Return / Max Drawdown / Win Rate / Total Trades vs Intraday row (Daily Realized P&L, Drawdown %, Trades Today, Win Rate Today %).

**Script:** `scripts/run-one-backtest.sh [BASE_URL]` — creates a backtest, polls until completed, then prints the text report (default BASE_URL=http://localhost:8084).

---

## Summary Table

| Phase | Description                    | Outcome                                      | Status        |
|-------|--------------------------------|----------------------------------------------|---------------|
| 1     | Bitso env-driven URL           | Switch stage/production via config           | Done          |
| 2     | Paper trading / dry-run        | Use Bitso testing env (stage URL + keys)     | Done (stage)  |
| 3     | Order & fill sync              | Publish + consumer + Bitso sync job         | Done          |
| 4     | Daily loss & drawdown limits   | Config + check before execute; OM session API | Done          |
| 5     | Intraday metrics               | Observability for live intraday trading      | Done (incl. Grafana dashboards) |
| 6     | Backtesting                    | Historical validation; doc in plan           | Documented    |

---

## References

- [Bitso: Set Up Your Testing Environment](https://docs.bitso.com/bitso-api/docs/set-up-your-testing-environment)
- [Bitso: API Overview](https://docs.bitso.com/bitso-api/docs/api-overview)
- Project: `DEVELOPMENT-ROADMAP.md`, `REMAINING-PHASES-CHECKLIST.md`
- Bitso client: `shared/pkg/bitso/client.go` (`SetAPIBaseURL`, `LookupOrder`, `LookupOrders`, `OrderTrades`)

---

**Document version:** 1.5  
**Status:** Phases 1–6 complete. Phase 3: order-placed consumer + Bitso sync job done. SessionRiskProvider (ORDER_MANAGEMENT_URL) and Phase 6 workflow (run backtest, compare to Grafana) documented and implemented.
