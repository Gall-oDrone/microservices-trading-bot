# Operations Environment Variables

Quick reference for env vars used by the trading bot, especially for **intraday**, **order sync**, and **Bitso testing**. See service-specific READMEs for full config.

---

## Trading-engine

| Variable | Default | Description |
|----------|---------|-------------|
| `BITSO_API_BASE_URL` | `https://stage.bitso.com/api` | Bitso API base; use production URL only when going live. |
| `STAGE_BITSO_API_KEY` | - | Bitso **stage** API key (required for placing orders). |
| `STAGE_BITSO_APISECRET` | - | Bitso **stage** API secret. |
| `DRY_RUN` | - | Set to `true` or `1` to log orders only; no Bitso `PlaceOrder` calls. |
| `ORDER_MANAGEMENT_URL` | - | Base URL of order-management (e.g. `http://order-management:8080`) for session risk (daily loss / drawdown limits). |
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

- **Trading-engine:** set `ORDER_MANAGEMENT_URL` so the engine can call `GET /api/v1/risk/session` before placing orders.
- **Limits:** configure `MaxDailyLoss` and `MaxDrawdownPct` in the trading config (e.g. in code or via config loader when supported). Zero means disabled.

---

## See also

- **docs/ORDER-FLOW-AND-BITSO-TESTING.md** — when orders hit Bitso testing, how to validate the flow, and Bitso dashboard.
- **INTRADAY-STRATEGY-IMPLEMENTATION-PLAN.md** — phases and status.
- **scripts/run-one-backtest.sh** — run one backtest and print the report (Phase 6 workflow).
