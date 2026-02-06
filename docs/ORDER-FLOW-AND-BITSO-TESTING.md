# Order Flow and Bitso Testing Environment

This doc explains **when orders are sent to Bitso’s testing environment** and how to validate the full order flow (trading-engine → Kafka → order-management → Bitso sync).

---

## When do orders go to the Bitso testing environment?

Orders are sent to **Bitso’s testing (stage) environment** when **all** of the following are true:

1. **Trading-engine is running** and consuming signals (e.g. from Kafka topic `trading.signals`).
2. **DRY_RUN is not set** — if `DRY_RUN=true` (or `1`), the engine only logs “Would place …” and does **not** call the Bitso API.
3. **Bitso client is pointed at stage** — the engine uses `BITSO_API_BASE_URL` (default `https://stage.bitso.com/api`) and **stage** API keys: `STAGE_BITSO_API_KEY` and `STAGE_BITSO_APISECRET`.

So: with default config and no `DRY_RUN`, any order the trading-engine places is sent to **Bitso stage**. You can confirm this in:

- **Trading-engine logs** — look for `Successfully placed BUY order <oid> for btc_mxn` (or SELL). The `<oid>` is the Bitso order ID.
- **Bitso testing dashboard** — log in to the [Bitso testing environment](https://stage.bitso.com) with your **testing** account and check Open Orders / Order History. Orders from this app will appear there when they are placed without `DRY_RUN`.

If you see `[DRY-RUN] Would place …` in the logs, no order is sent to Bitso (stage or production).

---

## Validating the full order flow

After deployment (or locally with Kafka + Redis), you can validate:

1. **Trading-engine places an order**
   - Ensure strategy-executor (or another producer) sends a signal to `trading.signals`.
   - Ensure trading-engine has Kafka and Bitso stage credentials; leave `DRY_RUN` unset to hit Bitso.
   - Check trading-engine logs for `Successfully placed … order <oid>`.

2. **Order appears in order-management**
   - Order-management must consume from `trading.orders.placed` (env `KAFKA_TOPIC_ORDERS_PLACED`, default `trading.orders.placed`).
   - Check order-management logs for `Recorded order placed` with `bitso_order_id`.
   - Optionally call order-management’s API (e.g. list orders) and confirm the new order with that Bitso order ID.

3. **Bitso sync job updates status (optional)**
   - If order-management has `STAGE_BITSO_API_KEY` and `STAGE_BITSO_APISECRET` (and optionally `BITSO_API_BASE_URL`) set, the Bitso sync job runs every 60s and calls Bitso’s `LookupOrders` for active orders.
   - Order status (and fills) in order-management should move from `submitted` to `accepted` / `partially_filled` / `filled` or `cancelled` as on Bitso.

4. **Bitso testing dashboard**
   - In the Bitso **testing** site, open Orders / History and match the order IDs from the trading-engine logs. That confirms the order was received by Bitso stage.

---

## Relevant environment variables

| Service           | Variable                    | Purpose |
|-------------------|----------------------------|---------|
| trading-engine    | `DRY_RUN`                  | Set to `true` or `1` to log only, no Bitso API calls. |
| trading-engine    | `BITSO_API_BASE_URL`       | Default `https://stage.bitso.com/api`; use production URL only when going live. |
| trading-engine    | `STAGE_BITSO_API_KEY`      | Stage API key (used with stage URL). |
| trading-engine    | `STAGE_BITSO_APISECRET`   | Stage API secret. |
| trading-engine    | `ORDER_MANAGEMENT_URL`     | Base URL of order-management for session risk (e.g. `http://order-management:8080`). |
| trading-engine    | `KAFKA_TOPIC_ORDERS_PLACED` | Topic for publishing placed orders (default `trading.orders.placed`). |
| order-management  | `KAFKA_TOPIC_ORDERS_PLACED` | Same topic; must match trading-engine. |
| order-management  | `STAGE_BITSO_API_KEY`      | Required for Bitso sync job. |
| order-management  | `STAGE_BITSO_APISECRET`    | Required for Bitso sync job. |
| order-management  | `BITSO_API_BASE_URL`       | Optional; default `https://stage.bitso.com/api`. |

See also **docs/OPERATIONS-ENV-VARS.md** for a full list of operations-related env vars.
