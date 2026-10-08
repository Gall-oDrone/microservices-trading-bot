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

---

## Session risk (daily loss / drawdown)

- **Trading-engine:** `ORDER_MANAGEMENT_URL` (required in live mode) lets the engine call `GET /api/v1/risk/session` and `POST /api/v1/orders/validate` before placing orders; an error from either blocks the order.
- **Limits:** `TRADING_MAX_DAILY_LOSS` and `TRADING_MAX_DRAWDOWN_PCT` (defaults 500 and 10%). They can no longer be 0 (disabled).
- **Exposure:** order-management serves `GET /api/v1/risk/exposure?book=btc_mxn` (position, open orders, position limit and utilization), the numbers its risk check uses.
- **Pre-trade race:** if trading-engine validates while order-management's signal consumer is still checking the same signal (row `pending`), validation waits up to 2 s for the outcome and never approves a row that is rejected or still pending.

---

## See also

- **docs/ORDER-FLOW-AND-BITSO-TESTING.md** — when orders hit Bitso testing, how to validate the flow, and Bitso dashboard.
- **INTRADAY-STRATEGY-IMPLEMENTATION-PLAN.md** — phases and status.
- **scripts/run-one-backtest.sh** — run one backtest and print the report (Phase 6 workflow).
