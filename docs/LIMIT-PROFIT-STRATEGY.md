# `limit_profit` strategy

**Type id:** `limit_profit` (registered in `strategy-executor`).

## Behavior

1. **Reference price** — either:
   - **`last_trade`**: latest trade price from the tick stream (default), or  
   - **`vwap`**: VWAP from the indicator service for the book (falls back to last trade if VWAP unavailable).

2. **Entry (flat)** — place a **BUY** limit at  
   `reference_price + entry_offset`  
   with size `position_size`. The strategy then tracks an **open long** at that limit price (optimistic; real fills depend on Bitso and the trading-engine).

3. **Exit (in position)** — on each new trade price, if  
   `last_price >= entry_price + min_profit`,  
   emit a **SELL** at the current last price and clear the position.

4. **Cooldown** — `min_signal_interval` (seconds) applies between **signals** (including after an exit before a new entry). Exit is **not** delayed by this interval once the profit condition is met.

## Parameters (JSON `parameters`)

| Field | Type | Default (code) | Description |
|-------|------|----------------|-------------|
| `reference` | string | `last_trade` | `last_trade` or `vwap` |
| `entry_offset` | number | `500` | Added to reference for BUY limit (same units as book price) |
| `min_profit` | number | `5000` | Minimum **price** move above entry before SELL (gross, before fees) |
| `position_size` | number | `0.001` | Order size (major, e.g. BTC for `btc_mxn`) |
| `min_signal_interval` | number | `60` | Seconds between signals (mainly between cycles) |

Tune `entry_offset`, `min_profit`, and `position_size` for **Bitso Stage** liquidity and **order-management** minimums.

## Organic startup

```bash
STRATEGY_TYPE=limit_profit ./scripts/start-organic-trading.sh
```

Optional env overrides: `ENTRY_OFFSET`, `MIN_PROFIT_LP`, `LP_REFERENCE`, `MIN_SIGNAL_INTERVAL`, `BOOK`, `STRATEGY_NAME`.

## Risks / limits

- Does not model **fees** explicitly; increase `min_profit` to cover spread + fees.
- **One** logical position at a time; state is in-memory (restart loses position unless you add persistence).
- Same caveats as other strategies: pre-trade validation, `DRY_RUN`, and Kafka path must be healthy for live orders.
