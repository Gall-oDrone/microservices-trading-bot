# `limit_profit` strategy

**Type id:** `limit_profit` (registered in `strategy-executor`).

## Behavior

1. **Reference price** — either:
   - **`last_trade`**: latest trade price from the tick stream (default), or  
   - **`vwap`**: VWAP from the indicator service for the book (falls back to last trade if VWAP unavailable).

2. **Entry (flat)** — place a **BUY** limit at  
   `reference_price + entry_offset`  
   with size `position_size`. Each BUY signal carries a unique `event_id` in metadata; that id is published on `trading.signals` as `TradeSignalEvent.event_id` so order-management can link the Bitso order. The strategy stays **`pending_buy`** until a **fill** is reported — it does **not** treat the position as open until then.

3. **Fill notification** — report the exchange fill (average price and amount) so the strategy can open the tracked long and use the **fill price** as `entry_price`:
   - **HTTP:** `POST /api/v1/strategies/order-fill` with JSON  
     `{ "event_id": "<same as signal>", "book": "btc_mxn", "side": "buy", "average_price": ..., "filled_amount": ... }`  
   - A future Kafka consumer (or OM webhook) can call the same path or an equivalent internal hook so fills are automatic in production.

4. **Exit (in position)** — after the fill, compare `last_price` to an **exit threshold**:

   - **Bitso fees (preferred):** if `use_bitso_fees` is true (default) and strategy-executor has **Bitso API credentials**, it calls **`GET /api/v3/fees`**, caches the response, and uses **`maker_fee_decimal`** and **`taker_fee_decimal`** for the book. The strategy assumes the **buy** was maker and the **sell** is taker, so the minimum exit last price for fee break-even is  
     `entry × (1 + maker) / (1 − taker)`  
     (see `shared/pkg/bitso.MinExitPriceAfterFees`). The exit threshold is that value **plus** `min_profit` **plus** optional extra margin `fee`.

   - **Manual fallback:** if credentials are missing, `use_bitso_fees` is false, or `/fees` fails, the threshold is  
     `entry + min_profit + fee + (entry × 2 × fee_bps / 10_000)`  
     (same as the previous manual-only model).

   **Metadata** on the SELL signal includes `fee_model` (`bitso_api` vs `manual_estimate`), `gross_quote_pnl`, and when available **`net_quote_pnl`** (quote P&amp;L after maker/taker rates from `/fees`).

5. **Cooldown** — `min_signal_interval` (seconds) applies between **signals** (including after an exit before a new entry). Exit is **not** delayed by this interval once the profit condition is met.

## Parameters (JSON `parameters`)

| Field | Type | Default (code) | Description |
|-------|------|----------------|-------------|
| `reference` | string | `last_trade` | `last_trade` or `vwap` |
| `entry_offset` | number | `500` | Added to reference for BUY limit (same units as book price) |
| `min_profit` | number | `5000` | Added on top of fee break-even (same price units as the book) |
| `fee` | number | `0` | Extra margin always added to the computed threshold (slippage / safety) |
| `fee_bps` | number | `0` | **Manual mode only:** symmetric bps cushion (ignored when Bitso `/fees` is used successfully) |
| `use_bitso_fees` | bool | `true` | Use cached GET `/fees` when credentials are configured |
| `position_size` | number | `0.001` | Order size (major, e.g. BTC for `btc_mxn`) |
| `min_signal_interval` | number | `60` | Seconds between signals (mainly between cycles) |

Tune `entry_offset`, `min_profit`, and `position_size` for **Bitso Stage** liquidity and **order-management** minimums.

## Organic startup

```bash
STRATEGY_TYPE=limit_profit ./scripts/start-organic-trading.sh
```

Optional env overrides: `ENTRY_OFFSET`, `MIN_PROFIT_LP`, `FEE_LP`, `FEE_BPS_LP`, `LP_REFERENCE`, `MIN_SIGNAL_INTERVAL`, `BOOK`, `STRATEGY_NAME`.

### Bitso API env (strategy-executor)

| Variable | Purpose |
|----------|---------|
| `BITSO_API_KEY` | With `BITSO_API_SECRET`, enables authenticated `GET /fees` for maker/taker rates |
| `BITSO_API_SECRET` | HMAC secret for private API |
| `BITSO_API_BASE_URL` | Optional; default `https://bitso.com/api` (set stage URL when testing against Bitso stage) |
| `BITSO_FEES_CACHE_TTL` | Cache duration for the full `/fees` payload (default `1h`) |

## Remaining limitations

- **Assumed roles:** exit math assumes **maker** on the entry fill and **taker** on the exit. If the buy takes liquidity or the sell rests, realized fees differ.
- **`/fees` snapshot:** tier rates come from the API; **actual** debits on fills are in Bitso trades/ledger and can differ slightly.
- **Manual mode `fee_bps`:** symmetric estimate on `entry` only; ignored when `/fees` succeeds.
- **Spread / slippage:** only `last` trade price is compared; no order-book depth model.
- **One** logical position at a time; state is in-memory (restart loses position unless you add persistence).
- Same caveats as other strategies: pre-trade validation, `DRY_RUN`, and Kafka path must be healthy for live orders.
