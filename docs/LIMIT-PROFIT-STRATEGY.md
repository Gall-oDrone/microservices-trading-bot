# `limit_profit` strategy

**Type id:** `limit_profit` (registered in `strategy-executor`).

## Behavior

1. **Reference price** — either:
   - **`last_trade`**: latest trade price from the tick stream (default), or  
   - **`vwap`**: VWAP from the indicator service for the book (falls back to last trade if VWAP unavailable).

2. **Entry (flat)** — place a **BUY** limit at  
   `reference_price + entry_offset`  
   with size `position_size`. Each BUY signal carries a unique `event_id` in metadata; that id is published on `trading.signals` as `TradeSignalEvent.event_id` so order-management can link the Bitso order. The strategy stays **`pending_buy`** until a **fill** is reported.

3. **Fill notification** — `POST /api/v1/strategies/order-fill` with JSON:

   | Field | Required | Description |
   |-------|----------|-------------|
   | `event_id` | yes | Same as signal `metadata.event_id` |
   | `book` | yes | e.g. `btc_mxn` |
   | `side` | yes | `buy` for entry fill |
   | `average_price` | yes | Fill average (entry) |
   | `filled_amount` | yes | Base amount filled |
   | `liquidity` | no | `maker` or `taker` — **actual** role for this fill (overrides configured buy leg when present) |
   | `buy_fee_rate` | no | Decimal fraction of notional actually charged on the buy (e.g. from ledger); overrides API buy-leg fee when set |

4. **Exit (in position)** — each tick computes:

   - **Exit threshold** (minimum price to allow a SELL signal):  
     `MinExitPriceAfterRoundTrip(entry, buy_fee_rate, sell_fee_rate) + min_profit + fee`  
     where rates come from **`GET /api/v3/fees`** via `maker_fee_decimal` / `taker_fee_decimal` and the configured **buy/sell liquidity** roles (`buy_liquidity`, `sell_liquidity`), unless `buy_fee_rate` was supplied on the fill or `use_bitso_fees` is false (then manual `fee` + `fee_bps` model).

   - **Compare price** — controlled by **`exit_price_reference`**:
     - `last` (default): latest trade price from the tick.
     - `bid`: best bid from market-data ticker (models lifting the bid when selling aggressively).
     - `mid`: `(bid + ask) / 2`.
     - `min_last_bid`: `min(last trade, bid)` — conservative when the book is wide.

   Exit triggers when **`compare_price >= threshold`**.

5. **P&amp;L metadata** — `gross_quote_pnl` and `net_quote_pnl` use the same **exit price** as the reference mode (last vs bid/mid) when computing net after fees.

6. **Cooldown** — `min_signal_interval` applies between **entry** cycles; exit is not gated by it.

## Parameters (JSON `parameters`)

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `reference` | string | `last_trade` | `last_trade` or `vwap` |
| `entry_offset` | number | `500` | Added to reference for BUY limit (absolute quote currency) |
| `entry_offset_bps` | number | `0` | If >0, `entry_offset = reference * bps / 10000` (takes precedence over absolute) |
| `min_profit` | number | `5000` | Added on top of fee break-even threshold (absolute quote currency) |
| `min_profit_bps` | number | `0` | If >0, `min_profit = entry * bps / 10000` (takes precedence over absolute) |
| `fee` | number | `0` | Extra margin on the threshold (slippage / safety) |
| `fee_bps` | number | `0` | Manual mode only: symmetric bps on entry |
| `use_bitso_fees` | bool | `true` | Use cached GET `/fees` when credentials exist |
| `buy_liquidity` | string | `maker` | `maker` or `taker` — fee column for the **buy** leg if not overridden by fill |
| `sell_liquidity` | string | `taker` | `maker` or `taker` — fee column for the **sell** leg |
| `exit_price_reference` | string | `last` | `last`, `bid`, `mid`, `min_last_bid` (needs market-data ticker) |
| `position_size` | number | `0.001` | Order size (major) |
| `min_signal_interval` | number | `60` | Seconds between entry signals |
| `pending_buy_timeout_seconds` | number | `0` | Cancel + clear local pending BUY if no fill after N seconds (with retry logic) |
| `pending_cancel_max_retries` | number | `3` | Max cancel attempts before giving up |
| `max_position_hold_seconds` | number | `0` | Time stop: SELL after position held this long |
| `stop_loss_quote` | number | `0` | Stop: SELL when compare price ≤ entry − this amount (quote per base) |
| `max_daily_loss_quote` | number | `0` | Circuit breaker: pause strategy when cumulative session loss exceeds this |
| `daily_loss_reset_hour_utc` | number | `0` | Hour (0-23 UTC) when daily loss counter resets |
| `trailing_stop_quote` | number | `0` | Exit when price drops this much below high-water mark (after activation) |
| `trailing_stop_activation_quote` | number | `0` | Minimum unrealized profit before trailing stop activates |
| `max_pending_orders` | number | `1` | Maximum concurrent pending BUY orders |
| `sizing_mode` | string | `fixed` | `fixed` or `atr_scaled` for volatility-adjusted sizing |
| `target_risk_quote` | number | `0` | Target quote-currency risk per trade (for `atr_scaled` mode) |
| `atr_multiplier` | number | `1.0` | Multiplier for ATR when computing position size |
| `atr_period` | number | `14` | Period for ATR indicator (uses service default) |
| `dry_run` | bool | `false` | If true, signals include `metadata.dry_run=true` (trading-engine should skip execution) |

## Organic startup

```bash
STRATEGY_TYPE=limit_profit ./scripts/start-organic-trading.sh
```

Optional env variables:

| Variable | Maps to parameter |
|----------|-------------------|
| `ENTRY_OFFSET` | `entry_offset` |
| `ENTRY_OFFSET_BPS` | `entry_offset_bps` |
| `MIN_PROFIT_LP` | `min_profit` |
| `MIN_PROFIT_BPS` | `min_profit_bps` |
| `FEE_LP` | `fee` |
| `FEE_BPS_LP` | `fee_bps` |
| `BUY_LIQUIDITY` | `buy_liquidity` |
| `SELL_LIQUIDITY` | `sell_liquidity` |
| `EXIT_PRICE_REF` | `exit_price_reference` |
| `LP_REFERENCE` | `reference` |
| `MIN_SIGNAL_INTERVAL` | `min_signal_interval` |
| `PENDING_BUY_TIMEOUT_SEC` | `pending_buy_timeout_seconds` |
| `PENDING_CANCEL_MAX_RETRIES` | `pending_cancel_max_retries` |
| `MAX_POSITION_HOLD_SEC` | `max_position_hold_seconds` |
| `STOP_LOSS_QUOTE` | `stop_loss_quote` |
| `MAX_DAILY_LOSS_QUOTE` | `max_daily_loss_quote` |
| `DAILY_LOSS_RESET_HOUR_UTC` | `daily_loss_reset_hour_utc` |
| `TRAILING_STOP_QUOTE` | `trailing_stop_quote` |
| `TRAILING_STOP_ACTIVATION_QUOTE` | `trailing_stop_activation_quote` |
| `MAX_PENDING_ORDERS` | `max_pending_orders` |
| `SIZING_MODE` | `sizing_mode` |
| `TARGET_RISK_QUOTE` | `target_risk_quote` |
| `ATR_MULTIPLIER` | `atr_multiplier` |
| `ATR_PERIOD` | `atr_period` |
| `DRY_RUN` | `dry_run` |
| `CLEANUP_ON_EXIT` | (script-only) delete strategy on exit |
| `BOOK` | book |
| `STRATEGY_NAME` | strategy name |

Robustness notes: [LIMIT-PROFIT-ROBUSTNESS.md](LIMIT-PROFIT-ROBUSTNESS.md).  
Improvements roadmap: [LIMIT-PROFIT-IMPROVEMENTS.md](LIMIT-PROFIT-IMPROVEMENTS.md).

## Bitso API env (strategy-executor)

| Variable | Purpose |
|----------|---------|
| `BITSO_API_KEY` | With `BITSO_API_SECRET`, enables `GET /fees` |
| `BITSO_API_SECRET` | HMAC secret |
| `BITSO_API_BASE_URL` | Optional (e.g. stage API prefix) |
| `BITSO_FEES_CACHE_TTL` | Cache TTL for `/fees` payload (default `1h`) |

## Durable state (Redis) and automated fills (Kafka)

When Redis is **connected** and **`REDIS_LIMIT_PROFIT_STATE_ENABLED`** is true (default), strategy-executor persists `limit_profit` snapshot state under keys `strategy-executor:limit_profit:{strategyName}` (pending BUY / `event_id`, open position, entry price, measured buy fee / liquidity). Restarts reload this before ticks run; **Remove strategy** or **Reset** clears the key.

**Order-management** publishes **`OrderFillEvent`** to **`KAFKA_TOPIC_ORDER_FILLS`** (default `trading.order.fills`) when an order **first** reaches fully **filled** (`signal_id` must match the original signal `event_id`). **Strategy-executor** consumes that topic (consumer group suffix `-order-fills`) and calls the same path as the HTTP fill handler. Disable the consumer with **`KAFKA_ORDER_FILLS_CONSUMER_ENABLED=false`** if you rely only on **`POST /api/v1/strategies/order-fill`**. Disable OM publishing with **`KAFKA_ORDER_FILLS_PUBLISH_ENABLED=false`**.

## Operational notes

- **Ticker-based exits** (`bid` / `mid` / `min_last_bid`) require market-data **`GET /api/v1/ticker/{book}`** reachable from strategy-executor (same base URL as indicators). If the ticker call fails, the strategy falls back to **last** and records `exit_price_reference` accordingly in metadata.
- **SELL price alignment** — when `exit_price_reference` is `bid`, `mid`, or `min_last_bid`, the SELL signal `Price` field uses `compare_price` (not `tick_price`) for execution consistency with the profitability evaluation.
- **Circuit breaker** — when `max_daily_loss_quote` is set and cumulative realized losses exceed it, the strategy stops emitting entry signals. If a position is open when the breaker trips, it will exit immediately with `exit_reason: circuit_breaker`. The counter resets at `daily_loss_reset_hour_utc` (default midnight UTC).
- **Pending buy cancel retry** — when `pending_buy_timeout_seconds` fires, the strategy attempts to cancel the order via order-management with retries (up to `pending_cancel_max_retries`). Only on successful cancel (or exhausted retries) does it clear local pending state.
- **Without Redis durable state** — behavior is in-memory only; restarts clear pending fills, position overrides, and daily loss counters.

## Prometheus metrics

The strategy exposes the following metrics (when `LimitProfitMetrics` is wired):

| Metric | Type | Description |
|--------|------|-------------|
| `limit_profit_entry_signals_total` | counter | Entry (BUY) signals emitted |
| `limit_profit_exit_signals_total` | counter | Exit (SELL) signals by reason label |
| `limit_profit_pending_buy_duration_seconds` | histogram | Time from entry signal to fill/timeout |
| `limit_profit_position_hold_duration_seconds` | histogram | Time from fill to exit |
| `limit_profit_pending_cancel_failures_total` | counter | Failed cancel attempts after retries exhausted |
| `limit_profit_daily_realized_pnl_quote` | gauge | Session P&L (negative = loss) |
| `limit_profit_circuit_breaker_active` | gauge | 1 if circuit breaker is tripped |
