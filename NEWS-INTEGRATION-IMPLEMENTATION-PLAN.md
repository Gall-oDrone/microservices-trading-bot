# News Integration Implementation Plan

This document outlines the recommended steps to add **crypto news** (and optional sentiment) into the trading pipeline so strategies can use it for filtering, sizing, or sentiment overlay. It follows the same staged approach as [INTRADAY-STRATEGY-IMPLEMENTATION-PLAN.md](INTRADAY-STRATEGY-IMPLEMENTATION-PLAN.md): implement and validate in stage before relying on news in production.

**Principle:** News is an **optional enrichment**. The system continues to operate without it; when the news service or feed is unavailable, strategies fall back to price-only behaviour. Use stage/paper first to validate news-driven behaviour.

**Update (2026-05-22):** Phase 1–2 scaffold implemented as `services/news-publisher` (S3 ETL `test-financial-news-bucket` → Kafka `news.agentic`). See `docs/agentic-ai/AGENTIC-AI-FINANCIAL-NEWS-S3-ETL-2026-05-22.md`.

---

## How News Can Improve Strategies

| Use | Description |
|-----|-------------|
| **Sentiment overlay** | Aggregate sentiment (bullish/neutral/bearish) from headlines or social; bias or filter signals (e.g. allow buys only when sentiment above a threshold, or scale position size by sentiment). |
| **Event / regime filter** | High-impact events (e.g. “Fed”, “regulation”, “exchange hack”) trigger reduced size, wider stops, or a short trading pause to avoid trading into spikes or gaps. |
| **Volatility regime** | Use news intensity or event count as a proxy for regime; e.g. tighten targets in high-news periods or avoid opening new positions immediately after a major headline. |
| **Confirmation** | Use news to confirm or reject a technical signal (e.g. RSI oversold + negative news → skip or smaller size; RSI oversold + no bad news → allow full size). |

---

## Current State (No News)

- **Market data:** market-data service ingests Bitso WebSocket (trades, order book) and publishes to Kafka (`market-data.trades`, etc.). Strategy-executor consumes market data and runs strategies (basic, trend, arbitrage) that today see **only** ticker/trade data.
- **Signals:** Strategy-executor publishes signals to Kafka; trading-engine consumes them and places orders (subject to session risk and config). There is no news or sentiment input anywhere in this path.
- **Backtesting:** Backtesting replays market events (trades) and runs strategies; no historical news or sentiment is available.

---

## Implementation Phases (in order)

### Phase 1: News / Sentiment Service (new microservice)

**Goal:** Add a dedicated service that ingests crypto news (and optionally computes or fetches sentiment), and publishes structured events so other services can consume them without coupling to specific news APIs.

**Tasks:**

1. **Create a new service** (e.g. `services/news` or `services/sentiment`) with:
   - Configurable **sources**: e.g. RSS feeds, CryptoPanic, LunarCrush, or other crypto news/sentiment APIs.
   - **Polling or webhooks** to fetch headlines and/or precomputed sentiment.
   - **Normalized output**: internal event type with at least `timestamp`, `symbol` or `topic` (e.g. btc, eth, general), `sentiment_score` (e.g. -1 to 1 or 0–100), optional `impact` (high/medium/low), optional `headline` or `tags`.
2. **Health and config:** Health endpoint; env-driven config (API keys, feed URLs, poll interval). Failures in fetching should not crash the service; log and optionally expose metrics (e.g. fetch errors).
3. **No Kafka yet:** Service can expose a simple HTTP endpoint (e.g. “current sentiment”) or in-memory cache for Phase 2 consumer to poll, or proceed to Phase 2 and publish to Kafka directly.

**Files to create:** New service under `services/news` (or `sentiment`) with `cmd/main.go`, config, one or more fetchers, optional cache. Document in this plan and in a short README.

**Alternative (minimal):** Skip a new service and have strategy-executor (or trading-engine) call a third-party sentiment API on a timer and cache the result. Simpler but couples that service to the API and does not scale to multiple consumers or backtesting; acceptable only for a quick experiment.

---

### Phase 2: Kafka Topics and Event Schema

**Goal:** Define Kafka topic(s) and a stable event schema for news/sentiment so that strategy-executor, trading-engine, or other consumers can subscribe without depending on the news service’s internal format.

**Tasks:**

1. **Topic(s):** e.g. `news.sentiment` or `news.events`. Configurable via env (e.g. `KAFKA_TOPIC_NEWS_SENTIMENT`). Create topic via Kafka tooling or broker auto-create.
2. **Schema:** JSON payload with at least:
   - `timestamp` (ISO8601 or Unix)
   - `symbol` or `topic` (string, e.g. `btc`, `btc_mxn`, `general`)
   - `sentiment_score` (float, e.g. -1..1 or 0..100)
   - Optional: `impact`, `headline`, `source`, `tags`
3. **Producer:** News service (from Phase 1) publishes to the topic on each fetch or when sentiment/event is updated. Optionally batch by time window to avoid flooding.
4. **Document:** Schema and topic name in this plan and in the news service README.

**Files to touch:** News service (producer), shared or service-local schema/docs. Optionally `shared/pkg/models` for a `NewsEvent` or `SentimentEvent` type if multiple services need it.

---

### Phase 3: Consumer Integration (strategy-executor or trading-engine)

**Goal:** At least one consumer of the news/sentiment stream so that the trading path can use it. Prefer strategy-executor so strategies can read current sentiment; alternatively trading-engine can apply a post-signal filter.

**Tasks:**

1. **Consumer:** Add a Kafka consumer in strategy-executor (or trading-engine) that subscribes to the news topic and updates in-memory state (e.g. “current sentiment per symbol”, “last high-impact event time”).
2. **State:** Keep state minimal: e.g. `last_sentiment[symbol]`, `last_high_impact_at`. TTL or decay optional (e.g. treat sentiment older than 1 hour as “neutral”).
3. **Availability:** If no message received yet or topic empty, treat as “no news” (neutral or no filter). No hard dependency on news for the service to run.
4. **Config:** Enable/disable news consumption via config (e.g. `NEWS_ENABLED`, `KAFKA_TOPIC_NEWS_SENTIMENT`). When disabled, strategies behave as today (price-only).

**Files to touch:** `services/strategy-executor` (or `services/trading-engine`): consumer goroutine, config, in-memory state. No change to market-data or order-management.

---

### Phase 4: Strategy Use of News (input vs gate)

**Goal:** Use news/sentiment in the trading logic. Two patterns; choose one or both.

**Option A — Strategy input (strategies see news):**

- Pass current sentiment (and optional “last high-impact time”) into the strategy layer when executing (e.g. alongside ticker/trade).
- Strategies can require minimum sentiment to take a buy/sell, or scale size by sentiment. Requires extending the strategy interface (e.g. `Execute(ticker, sentiment)` or a `Context` that carries sentiment).
- **Files to touch:** `services/strategy-executor` strategy interface and implementations (basic, trend, arbitrage); manager passes sentiment into strategy. Backtesting would need the same extension (Phase 5).

**Option B — Signal filter (gate):**

- Strategies stay unchanged. After a strategy emits a signal, a **filter** in strategy-executor or trading-engine checks current news/sentiment and can block, delay, or scale the signal (e.g. “no new trades for 15 minutes after high-impact news”).
- **Files to touch:** Filter component in strategy-executor (before publish) or in trading-engine (before place order). Config for thresholds (e.g. cooldown minutes, min sentiment).

**Recommendation:** Start with **Option B** (filter) for minimal change and quick validation; add **Option A** when you want strategy-level logic (e.g. sentiment-biased sizing).

---

### Phase 5: Backtesting with Historical News (optional)

**Goal:** Replay historical news/sentiment in the backtester so that strategies or filters that use news can be tested on past data.

**Tasks:**

1. **Historical storage:** If the news service persists events (e.g. to Redis or a store), expose an API or file format for “sentiment in [start, end]” so the backtesting service can load it. Alternatively, use a static file (e.g. CSV or JSON) with timestamped sentiment for backtest runs.
2. **Backtesting engine:** Extend the event loop to accept a second stream (news/sentiment) or a preloaded series. When processing a market event at time T, look up sentiment at T (or latest before T) and pass it to the strategy or filter.
3. **Strategy interface in backtesting:** If Option A is used in live, backtesting strategies must accept the same sentiment input; if Option B only, backtesting needs a simulated filter that uses the loaded sentiment.
4. **Document:** How to run a backtest with a sentiment file or API (e.g. in `services/backtesting/README.md`).

**Files to touch:** `services/backtesting` (data loader, event loop, optional strategy interface extension). Defer until live news integration (Phases 1–4) is stable.

---

### Phase 6: Observability and Operations

**Goal:** Make news/sentiment visible in operations and debugging.

**Tasks:**

1. **Metrics:** News service: e.g. fetch count, fetch errors, last sentiment per symbol. Consumer (strategy-executor/trading-engine): e.g. “signals filtered by news” count, “current sentiment” gauge.
2. **Grafana:** Add a panel or dashboard row for “News / Sentiment” (current sentiment, last update time, filter stats). Reuse existing Prometheus scrape.
3. **Logging:** Log when a signal is blocked or scaled due to news (at debug or info) so operators can correlate behaviour.
4. **Docs:** Runbook or README section: how to disable news, how to interpret “no sentiment” (e.g. topic empty vs service down).

**Files to touch:** News service metrics, consumer metrics, `monitoring/grafana/dashboards/` (new or extend existing).

---

## Summary Table

| Phase | Description | Outcome | Status |
|-------|-------------|---------|--------|
| 1 | News / sentiment service | New microservice: ingest news, optional sentiment; publish or expose state | Planned |
| 2 | Kafka topics and schema | Topic(s) + JSON schema for news/sentiment events | Planned |
| 3 | Consumer integration | Strategy-executor or trading-engine consumes news; in-memory state | Planned |
| 4 | Strategy use (input vs gate) | Option A: strategies see sentiment; Option B: post-signal filter (block/scale) | Planned |
| 5 | Backtesting with historical news | Load sentiment for date range; replay in backtester | Optional / later |
| 6 | Observability | Metrics, Grafana, logging, runbook | Planned |

---

## Next Steps (after implementation)

1. **Validate in stage:** Run the news service (or mock producer) and strategy-executor/trading-engine in stage; confirm signals are filtered or biased as expected when sentiment is present. Confirm behaviour when news topic is empty or service is down (no hard failure).
2. **Tune thresholds:** Use config for sentiment thresholds and cooldown windows; tune in stage before production.
3. **Production:** Deploy news service and enable consumer in production; store API keys (e.g. CryptoPanic, LunarCrush) in secrets. Monitor Grafana news/sentiment panels.

---

## References

- Project: **INTRADAY-STRATEGY-IMPLEMENTATION-PLAN.md** (phased approach, stage-first, backtesting last).
- Project: **INTRADAY-STRATEGIES.md** (strategy parameters); **services/backtesting/README.md** (strategies and fees).
- Market data flow: `services/market-data` (Kafka publisher); strategy-executor consumes market topics and publishes signals; trading-engine consumes signals and places orders.
- Optional: [CryptoPanic API](https://cryptopanic.com/developers/api/), [LunarCrush](https://lunarcrush.com/developers), or similar for crypto news/sentiment feeds.

---

**Document version:** 1.0  
**Status:** Plan only; no implementation yet. Phases 1–4 are required for live news integration; Phase 5 is optional for backtesting with news; Phase 6 recommended for operations.
