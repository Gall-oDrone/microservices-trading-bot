# High Trading Frequency Implementation Plan

This document outlines the recommended phases to implement higher trading frequency on top of the existing microservices stack (market-data, strategy-executor, trading-engine, order-management) and Bitso integration.

**Principle:** Increase throughput and reduce per-order latency in controlled phases while keeping risk controls (daily loss, drawdown, rate limits) and validating each step in stage before production.

**Related:** [INTRADAY-STRATEGY-IMPLEMENTATION-PLAN.md](INTRADAY-STRATEGY-IMPLEMENTATION-PLAN.md) (risk, metrics, order sync); [BACKTESTING-METRICS-AND-EXPORT.md](BACKTESTING-METRICS-AND-EXPORT.md) (backtest export). This plan builds on the current pipeline: **market-data (WebSocket → Kafka) → strategy-executor → Kafka (signals) → trading-engine → Bitso REST (PlaceOrder)** and order-management for risk and sync.

---

## Current State (Frequency-Relevant)

- **Market-data:** WebSocket from Bitso → trade/order-book processing → Kafka. Latency is tracked (`GetLatencyMs`, `AverageLatencyMs`). No intrinsic rate cap in the processor.
- **Strategy-executor:** Consumes from Kafka; **RateLimitFilter** limits events per time window **per book** (`internal/processor/filters.go`). Publishes signals to Kafka. Latency metrics: `StrategyLatency`, `SignalProcessingLatency`, `KafkaProducerLatency`.
- **Trading-engine:** **Single-threaded** signal processing: one signal at a time from `signalChan`. Per signal: trading hours → **Bitso REST Ticker** → validate price → **HTTP to order-management** (session risk) → **Bitso REST PlaceOrder**. Signals are **dropped** if the channel is full after 5s timeout. `services/trading-engine/internal/engine/engine.go` (`signalProcessor`, `processTradeSignal`, `kafkaConsumerLoop`).
- **Order-management:** **MaxOrdersPerMinute** (default **60**), **MaxOpenOrders** (default 10). Risk checks (including rate limit) run per order when trading-engine calls risk/session. `services/order-management/internal/risk/risk_manager.go`; config: `MAX_ORDERS_PER_MINUTE`, `MAX_OPEN_ORDERS`.
- **Bitso client:** **Single ticket** by default → one `PlaceOrder` at a time. Optional burst rate (default 0). PlaceOrder is REST. `shared/pkg/bitso/client.go` (`defaultTickets = 1`, `tickets` channel).

**Bottlenecks today:** One signal processed at a time; multiple round-trips per order (ticker, session risk, place order); 60 orders/min cap in order-management; one concurrent Bitso order. Exchange (Bitso) REST rate limits also apply; check [Bitso API documentation](https://docs.bitso.com/bitso-api/docs/api-overview) for current limits.

---

## Implementation Phases (in order)

### Phase 1: Observability and Baseline

**Goal:** Establish metrics and a baseline so you can measure the impact of each later phase and detect regressions (drops, latency spikes, errors).

**Tasks:**

1. **Confirm existing metrics** are scraped and visible in Grafana:
   - Trading-engine: `signals_received_total`, `signals_processed_total`, `signals_dropped_total` (by reason), `signal_processing_duration_seconds`, `orders_executed_total`, PlaceOrder latency.
   - Strategy-executor: `StrategyLatency`, `SignalProcessingLatency`, `KafkaProducerLatency`, `MarketDataProcessingLatency`.
   - Order-management: risk violations (e.g. `rate_limit`), session risk endpoint usage.
   - Market-data: trade/ticker latency, throughput.
2. **Add or expose** (if missing) a simple dashboard or panel for “signals in vs signals out vs dropped” and p50/p95 latency for signal processing and order execution.
3. **Run a baseline** in stage: fixed strategy and book, note signals/min, orders/min, drop count, and latency percentiles before any high-frequency changes. Document in this plan or a runbook.

**Files touched:** Monitoring/Grafana dashboards (e.g. `monitoring/grafana/dashboards/`), optional: trading-engine or strategy-executor metrics (if new counters are needed). This plan.

**Exit criteria:** Baseline numbers documented; alerts or panels in place for drop rate and latency.

---

### Phase 2: Configurable Rate Limits (Order-Management & Strategy-Executor)

**Goal:** Make rate limits configurable so that when you enable higher frequency, you can raise them deliberately (within Bitso and risk constraints) instead of being stuck at defaults.

**Tasks:**

1. **Order-management:** Ensure **MaxOrdersPerMinute** and **MaxOpenOrders** are read from env/config (already in `services/order-management/internal/config/config.go`: `MAX_ORDERS_PER_MINUTE`, `MAX_OPEN_ORDERS`). Document safe ranges: e.g. MaxOrdersPerMinute &lt;= Bitso’s documented limit, and that raising limits increases exposure. Add validation if needed (e.g. cap MaxOrdersPerMinute to a configured maximum).
2. **Strategy-executor:** Ensure **RateLimitFilter** parameters (max events per time window per book) are configurable (e.g. from config or strategy config) so you can increase or bypass the filter for high-frequency books. If the filter is hardcoded, add config/env (e.g. `STRATEGY_RATE_LIMIT_MAX_EVENTS`, `STRATEGY_RATE_LIMIT_WINDOW`) and wire them into the processor pipeline.
3. **Document** in README or CONFIG: which env vars control order and signal rate limits; that raising them is required for high frequency but must stay within Bitso limits and risk policy.

**Files touched:** `services/order-management/internal/config/config.go` (and validation), `services/strategy-executor` (config + wiring of RateLimitFilter), this plan, README or CONFIG.

**Exit criteria:** All rate limits configurable; docs updated; no change in behavior until env is explicitly set.

---

### Phase 3: Bitso Client Concurrency

**Goal:** Allow multiple concurrent `PlaceOrder` calls so the trading-engine is not limited to one order at a time by the client.

**Tasks:**

1. **Expose concurrency (tickets)** in the Bitso client: add config or constructor parameter to set the number of tickets (e.g. `SetConcurrency(n)` or pass to `NewClient`). Keep default 1 for backward compatibility.
2. **Trading-engine / shared config:** Add env (e.g. `BITSO_PLACE_ORDER_CONCURRENCY` or in shared config) and pass it into the Bitso client used by the trading-engine. Ensure the value is &lt;= Bitso’s allowed concurrent requests (or their documented rate limit expressed as concurrent requests).
3. **Safety:** Document that increasing concurrency can trigger Bitso rate limits or errors if too high; recommend starting with 2–3 and tuning based on errors and latency.
4. **Optional:** If the client has a “burst rate” (delay between requests), make it configurable so it can be tuned for higher throughput where allowed by the exchange.

**Files touched:** `shared/pkg/bitso/client.go` (ticket pool size, optional burst rate), `shared/pkg/config` or trading-engine config (concurrency env), `services/trading-engine/cmd/main.go` (wire concurrency into client), this plan.

**Exit criteria:** Concurrency configurable; default remains 1; doc and safety notes in place.

---

### Phase 4: Trading Engine — Concurrent Signal Processing and Ticker Cache

**Goal:** Process multiple signals concurrently and remove the per-signal Bitso Ticker REST call so that latency and throughput are not limited by a single-threaded loop and repeated ticker fetches.

**Tasks:**

1. **Concurrent signal processing:** Replace the single goroutine that reads from `signalChan` with a **worker pool** (e.g. N workers, N configurable via env). Each worker processes one signal at a time; multiple workers allow multiple orders in flight. Ensure thread-safe access to engine state and executor (e.g. Bitso client already serializes via tickets; balance cache may need locking). Keep the existing 5s timeout for sending to the channel or replace with backpressure (e.g. block or requeue) so signals are not dropped unnecessarily when running at high frequency.
2. **Ticker cache:** Introduce a **ticker cache** in the trading-engine (or use last price from market-data): refresh ticker (or mid) periodically (e.g. every 1–5s) or on receipt of a trade from Kafka/market-data, and use the cached value in `validateSignalPrice` instead of calling `te.bitsoClient.Ticker(book)` on every signal. Reduces latency and avoids extra REST calls. Fallback: if cache is stale or empty, call Ticker once (and optionally refresh cache).
3. **Configuration:** Add env vars for worker count (e.g. `TRADING_ENGINE_SIGNAL_WORKERS`) and ticker cache TTL or refresh interval. Document recommended values for “high frequency” (e.g. workers 2–5 to start).
4. **Metrics:** Ensure existing metrics (signals processed, dropped, latency) remain correct with multiple workers (e.g. use atomic or per-worker aggregation). Add a gauge for “current in-flight signals” or “queue depth” if useful.

**Files touched:** `services/trading-engine/internal/engine/engine.go` (worker pool, ticker cache, validateSignalPrice using cache), `services/trading-engine/cmd/main.go` or config (worker count, cache TTL), this plan.

**Exit criteria:** Multiple workers process signals concurrently; ticker is not fetched per signal; config and docs updated; baseline re-run shows higher orders/min and similar or better latency.

---

### Phase 5: Session Risk Caching (Reduce Per-Order HTTP)

**Goal:** Avoid a blocking HTTP call to order-management for session risk on every signal; use a local cache refreshed periodically or on a sliding window so that high-frequency execution does not depend on order-management response latency every time.

**Tasks:**

1. **Cache design:** In the trading-engine, maintain a **session risk cache**: last known `daily_realized_pnl` and `drawdown_percent`, with a timestamp. Before placing an order, use cached values if they are fresh (e.g. &lt; 10–30s old); otherwise call `SessionRiskProvider.GetSessionRisk` once, update the cache, then use the new values for `CheckSessionLimits`. Optionally refresh cache in the background on a timer (e.g. every 15s) when the engine is active.
2. **Safety:** If cache is stale and the next refresh fails, either reject the order (safe) or use last known values with a “stale” flag and log. Document that aggressive caching (long TTL) can allow a short burst beyond session limits before the next refresh; keep TTL conservative (e.g. 15–30s) unless risk policy allows.
3. **Configuration:** Add env (e.g. `SESSION_RISK_CACHE_TTL_SECONDS`). Default 0 = no cache (current behavior). Document in CONFIG/README.
4. **Metrics:** Optionally expose cache hit/miss and refresh errors so operators can monitor staleness.

**Files touched:** `services/trading-engine/internal/engine/engine.go` (or a small `sessionrisk` package): cache struct, refresh logic, use in `processTradeSignal`; config for TTL; this plan.

**Exit criteria:** Session risk can be cached with configurable TTL; default remains “no cache”; docs and safety notes in place; baseline re-run shows reduced latency when cache is enabled.

---

### Phase 6: Kafka and Infrastructure Tuning

**Goal:** Ensure Kafka and deployment do not become the bottleneck when signal and order rates increase.

**Tasks:**

1. **Kafka consumer (trading-engine):** Tune consumer fetch size and timeout so that signals are delivered with minimal delay (e.g. reduce poll timeout or increase fetch size within reason). Ensure consumer group has enough partitions for the signals topic so that multiple engine instances (if any) can scale. Document recommended settings for high-frequency (e.g. in README or OPERATIONS).
2. **Kafka producer (strategy-executor):** Similarly tune batch size and linger so that signals are published with low latency. Existing `KafkaProducerLatency` metric can be used to validate.
3. **Deployment:** Prefer colocation: run market-data, strategy-executor, trading-engine, and order-management in the same region/AZ to reduce network latency. Document this as a recommendation for high-frequency.
4. **Optional:** Add or document alerts for consumer lag on the signals topic so that backlog is detected quickly.

**Files touched:** Config or Helm/env for Kafka consumer and producer settings; documentation (README, OPERATIONS, or this plan); optional alert rules.

**Exit criteria:** Kafka settings documented and applied where needed; no consumer lag under expected load; latency from signal publish to engine consume is acceptable.

---

### Phase 7: Validation, Rollback, and Documentation

**Goal:** Provide a clear validation checklist and rollback procedure so that high-frequency mode can be turned on safely and reverted if issues appear.

**Tasks:**

1. **Validation checklist:** Document steps to validate high-frequency in stage: (1) Set new env vars (rate limits, concurrency, workers, ticker cache, session risk cache). (2) Run the pipeline and compare to baseline: orders/min, signals dropped, p95 latency, error rate. (3) Verify order-management and Bitso sync still correct; verify session risk and daily loss/drawdown still enforced (e.g. trigger a test that should be blocked). (4) Run for a sustained period (e.g. 1 hour) and check stability.
2. **Rollback:** Document how to revert: set concurrency and workers back to 1, disable caches (TTL 0), restore original rate limits. Note that rollback is config-only if no schema or contract changes were made.
3. **Runbook:** Add a short “High-frequency mode” section to the operations runbook: when to use it, what to monitor, and how to roll back. Reference this plan and INTRADAY-STRATEGY-IMPLEMENTATION-PLAN.md for risk and stage validation.

**Files touched:** This plan, optional runbook (e.g. `docs/` or `REMAINING-PHASES-CHECKLIST.md`).

**Exit criteria:** Checklist and rollback steps documented; runbook updated.

---

## Summary Table

| Phase | Description | Outcome | Status |
|-------|-------------|---------|--------|
| 1 | Observability and baseline | Metrics and baseline numbers; alerts for drops/latency | Pending |
| 2 | Configurable rate limits | Order-management and strategy-executor limits configurable via env | Pending |
| 3 | Bitso client concurrency | Multiple concurrent PlaceOrder calls; configurable tickets | Pending |
| 4 | Concurrent signal processing + ticker cache | Worker pool; no per-signal Ticker REST call | Pending |
| 5 | Session risk caching | Optional cache for session risk to reduce per-order HTTP | Pending |
| 6 | Kafka and infrastructure tuning | Consumer/producer and deployment tuned for low latency | Pending |
| 7 | Validation, rollback, documentation | Checklist, rollback procedure, runbook | Pending |

---

## Next Steps After Implementation

- **Stage validation:** Run the full pipeline in stage with high-frequency settings; confirm orders/min and latency meet targets and that risk limits still apply.
- **Bitso limits:** Always stay within Bitso’s documented API rate limits; monitor for rate-limit errors and back off or reduce concurrency if needed.
- **Production:** Enable high-frequency in production only after stage validation and with the same risk controls (MaxDailyLoss, MaxDrawdownPct, session risk). Prefer a feature flag or env switch so rollback is instant (config-only).

---

## References

- [Bitso API Overview](https://docs.bitso.com/bitso-api/docs/api-overview) (rate limits, order placement)
- [INTRADAY-STRATEGY-IMPLEMENTATION-PLAN.md](INTRADAY-STRATEGY-IMPLEMENTATION-PLAN.md) — risk (Phase 4), order sync (Phase 3), metrics (Phase 5)
- Trading-engine: `services/trading-engine/internal/engine/engine.go` (signalProcessor, processTradeSignal, kafkaConsumerLoop)
- Order-management risk: `services/order-management/internal/risk/risk_manager.go` (CheckRateLimit, CheckOrderLimits)
- Strategy-executor: `services/strategy-executor/internal/processor/filters.go` (RateLimitFilter)
- Bitso client: `shared/pkg/bitso/client.go` (tickets, PlaceOrder)

---

**Document version:** 1.0  
**Status:** Plan defined; all phases pending. Implement in order; validate in stage after each phase before proceeding.
