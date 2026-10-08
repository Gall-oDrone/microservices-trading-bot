# Operations Environment Variables

Quick reference for env vars used by the trading bot, especially for **intraday**, **order sync**, and **Bitso testing**. See service-specific READMEs for full config.

---

## Trading-engine

| Variable | Default | Description |
|----------|---------|-------------|
| `BITSO_API_BASE_URL` | `https://stage.bitso.com/api` | Bitso API base. In live mode (no `DRY_RUN`) the engine refuses any host but `stage.bitso.com` unless `TRADING_ENGINE_ALLOW_PRODUCTION=1`. |
| `STAGE_BITSO_API_KEY` | - | Bitso **stage** API key (required for placing orders). |
| `STAGE_BITSO_APISECRET` | - | Bitso **stage** API secret. |
| `DRY_RUN` | - | Set to `true` or `1` to log orders only; no Bitso `PlaceOrder` calls. |
| `ORDER_MANAGEMENT_URL` | - | Base URL of order-management (e.g. `http://order-management:8082`) for pre-trade validation and session risk (daily loss / drawdown). **Required in live mode** (the engine refuses to start without it since R5, 2026-10-08). |
| `TRADING_MAX_DAILY_LOSS` | `500` | Max realized loss per session, in the book's quote currency (MXN for btc_mxn). Must be > 0; a value that does not parse stops start-up. |
| `TRADING_MAX_DRAWDOWN_PCT` | `10` | Max drawdown from peak equity, percent in (0, 100]. |
| `TRADING_ENGINE_ALLOW_PRODUCTION` | - | `1` allows a non-stage `BITSO_API_BASE_URL` in live mode. Leave unset: trading stays on stage until production is approved (decided 2026-10-08). |
| `TRADING_RISK_POLICY` | built-in | Per-order policy file in the shared format (`shared/pkg/risk`, same JSON as the daily-executor). Unset: built-in `trading-engine-default-2026-10-08` (btc_mxn 0.1 BTC / 10,000 MXN per order, 500 bps from the touch mid). A file that is missing, invalid or has unknown fields stops start-up. |
| `TRADING_HALT_FILES` | - | Comma-separated operator halt files (`risk-state.json`). Any one halted, unreadable or invalid blocks every new order (metric reason `risk_policy`). Point it at the ledger halt files ui-api writes so **HALT ALL** stops the engine. Live mode without it logs a warning. |
| `KAFKA_BROKERS` | - | Kafka broker list. |
| `KAFKA_TOPIC_SIGNALS` | `trading.signals` | Topic to consume trade signals from. |
| `KAFKA_TOPIC_ORDERS_PLACED` | `trading.orders.placed` | Topic to publish placed orders to (for order-management). |

---

## Order-management

| Variable | Default | Description |
|----------|---------|-------------|
| `KAFKA_BROKERS` | - | Kafka broker list. |
| `KAFKA_TOPIC_ORDERS_PLACED` | `trading.orders.placed` | Topic to consume placed orders from (must match trading-engine). |
| `KAFKA_CONSUMER_GROUP` | `order-management-group` | Consumer group; orders-placed consumer uses `-orders-placed` suffix. |
| `STAGE_BITSO_API_KEY` | - | Bitso stage API key; set to enable Bitso sync job (poll order status). |
| `STAGE_BITSO_APISECRET` | - | Bitso stage API secret. |
| `BITSO_API_BASE_URL` | `https://stage.bitso.com/api` | Bitso API base for sync job. |
| `MAX_POSITION_SIZE` | `1.0` | BTC per book. Without `TRADING_RISK_POLICY` this becomes `default.max_position_btc` of the env-built policy `order-management-env`. |
| `MAX_ORDER_VALUE` | `100000` | Quote currency per order (`default.max_order_notional`); the validator also enforces it. |
| `MAX_OPEN_ORDERS` | `10` | Firm-wide open orders (`portfolio.max_open_orders`). Kept even with a policy file that has no `portfolio` section. |
| `MAX_ORDERS_PER_MINUTE` | `60` | Firm-wide accepted orders in a 60 s sliding window, once per signal (`portfolio.max_orders_per_minute`). Per pod. Kept like `MAX_OPEN_ORDERS`. |
| `TRADING_RISK_POLICY` | - | Same file and format as trading-engine. Set: it is order-management's limit set (violation `shared_policy:<rule>`); per book it may add `max_open_orders` and `max_orders_per_minute`, and a `portfolio` section sets the firm-wide ones. Unset: the policy is built from the four env limits above (plan §6.4.6). |
| `TRADING_HALT_FILES` | - | Same list as trading-engine. A halted, unreadable or invalid file rejects every new order in `CheckRisk` (defence in depth behind trading-engine). |
| `MARKET_DATA_URL` | - | market-data base URL (k8s: `http://market-data:8083`). Positions are marked at its `/api/v1/ticker` mid for exposure and VaR; unset or unavailable: entry price, flagged `position_mark_fallback`. |
| `RISK_PORTFOLIO_INTERVAL` | `30s` | How often exposure and VaR are recomputed (Go duration, at least 1s). |
| `RISK_VAR_DAILY_VOL` | `0.04` | Daily return volatility assumed in the 1-day 99 % VaR, as a ratio in (0, 1]. A model parameter: review it against realized vol. |
| `RISK_VAR_DAILY_VOL_BOOKS` | - | Per-book overrides, e.g. `btc_usd=0.03,btc_mxn=0.035`. |
| `RISK_VAR_LIMITS` | - | VaR limit per quote currency, e.g. `MXN=20000,USD=1000`. Alerts at 80 % / 100 %; nothing is blocked. Any `RISK_*` value that does not parse stops start-up. |

---

## Session risk (daily loss / drawdown)

- **Trading-engine:** `ORDER_MANAGEMENT_URL` (required in live mode) lets the engine call `GET /api/v1/risk/session` and `POST /api/v1/orders/validate` before placing orders; an error from either blocks the order.
- **Limits:** `TRADING_MAX_DAILY_LOSS` and `TRADING_MAX_DRAWDOWN_PCT` (defaults 500 and 10%). They can no longer be 0 (disabled).
- **Exposure:** order-management serves `GET /api/v1/risk/exposure?book=btc_mxn` (position, open orders, position limit and utilization), the numbers its risk check uses.
- **Pre-trade race:** if trading-engine validates while order-management's signal consumer is still checking the same signal (row `pending`), validation waits up to 2 s for the outcome and never approves a row that is rejected or still pending.
- **Shared policy and halt (R5b):** both services read `TRADING_RISK_POLICY` and `TRADING_HALT_FILES`. trading-engine checks first (with the touch mid as the fat-finger reference and the position from `/api/v1/risk/exposure`, failing closed if it is unknown); order-management checks again in `CheckRisk`. A sell that only reduces the position is never blocked by a size limit, only by a halt or the price guard.

---

## CORS (market-data, backtesting, api-gateway)

| Variable | Default | Meaning |
|---|---|---|
| `CORS_ALLOWED_ORIGINS` | unset | Comma-separated exact origins, e.g. `https://ops.example.com,http://127.0.0.1:5173`. Each must be `scheme://host[:port]` (http or https, no path). `*` is refused. Unset or invalid: no CORS headers are sent, so browsers reach the service same-origin only (an invalid value is logged as "CORS disabled"). The operator UI uses ui-api, which is same-origin and needs none of this. |

The api-gateway defaults also match the k8s Services: `SERVICE_PORT` 8085, `ORDER_MANAGEMENT_URL` `http://localhost:8082`, `STRATEGY_EXECUTOR_URL` `http://localhost:8081`, `MARKET_DATA_URL` `http://localhost:8083`.

---

## See also

- **docs/ORDER-FLOW-AND-BITSO-TESTING.md** — when orders hit Bitso testing, how to validate the flow, and Bitso dashboard.
- **INTRADAY-STRATEGY-IMPLEMENTATION-PLAN.md** — phases and status.
- **scripts/run-one-backtest.sh** — run one backtest and print the report (Phase 6 workflow).
