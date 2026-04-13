# Limit profit strategy — robustness

This document captures operational and behavioral recommendations for the `limit_profit` strategy (`services/strategy-executor/internal/strategies/limit_profit_strategy.go`), including lifecycle controls implemented in code and follow-up work.

**See also:** [LIMIT-PROFIT-STRATEGY.md](LIMIT-PROFIT-STRATEGY.md) (behavior, parameters, organic startup).

---

## 1. Operational foundations (high leverage)

| Area | Recommendation |
|------|------------------|
| **Redis** | Connect strategy-executor to Redis so indicators and optional **`REDIS_LIMIT_PROFIT_STATE_ENABLED`** durable state work. In-memory fallback loses pending/position context across restarts. |
| **Bitso fees API** | Provide **`BITSO_API_KEY`** / **`BITSO_API_SECRET`** on strategy-executor so exit thresholds use **`GET /fees`** when **`use_bitso_fees`** is true. |
| **Order fills** | Prefer the Kafka **`OrderFillEvent`** path into strategy-executor so **`OnOrderFilled`** runs without relying only on HTTP **`POST /api/v1/strategies/order-fill`**. |
| **Exit reference** | For wide books, use **`exit_price_reference`** `bid`, `mid`, or **`min_last_bid`** so exits compare to the book, not only last trade. |
| **Replicas** | Run **strategy-executor at 1 replica** for organic trading so create/start and the poll loop align (see `scripts/start-organic-trading.sh` comments). |

---

## 2. Risk and lifecycle controls (implemented)

The following **`parameters`** keys are supported by `limit_profit`:

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| **`pending_buy_timeout_seconds`** | int | `0` (off) | If a BUY signal was emitted but no fill arrives within this many seconds, the strategy **clears local** `pending_buy` state (no new BUY until the next eligible tick). **Important:** this does **not** cancel a resting order on the exchange; reconcile or cancel via venue/OM if the order may still fill. |
| **`max_position_hold_seconds`** | int | `0` (off) | After a position is open, emit a **SELL** if **`EntryTime`** age exceeds this (time stop). Metadata: **`exit_reason`** = `max_hold`. |
| **`stop_loss_quote`** | float | `0` (off) | In **quote currency per 1 base** (e.g. MXN per BTC for `btc_mxn`). Emit **SELL** when **`compare_price`** ≤ **`entry - stop_loss_quote`** (uses the same compare path as other exits). Metadata: **`exit_reason`** = `stop_loss`. |

**Exit ordering (when in position):** `stop_loss` is evaluated before `max_hold`, then the usual **take-profit** threshold (`compare_price` vs fee-aware threshold). All exits set metadata **`exit_reason`**: `take_profit` | `stop_loss` | `max_hold`.

**State:** **`PendingBuySince`** on `StrategyState` records when a pending BUY was sent (used for timeout; persisted with Redis snapshots).

---

## 3. Remaining gaps and follow-ups

| Topic | Notes |
|-------|--------|
| **Cancel on pending timeout** | Clearing local state does not remove a resting limit on Bitso. A future improvement is **`POST`** cancel by **`event_id`** / order id through order-management or direct venue API, then clear pending. |
| **Cooldown semantics** | `min_signal_interval` gates off **`LastSignalTime`**, which updates on **both** entry and exit signals. For strict “time since last **entry** only,” use separate timestamps (future change). |
| **SELL limit price** | Exit signals still use **`tick_price`** as the signal price field; execution policy remains with trading-engine / venue. Optional: align limit price with **`compare_price`** when using bid/mid references. |
| **bps-based min profit** | Absolute **`min_profit`** in quote units can be normalized with **bps of notional** for different BTC levels (future parameter or derived value). |

---

## 4. Script and API usage

**Organic script** (`scripts/start-organic-trading.sh`) supports optional env vars for `STRATEGY_TYPE=limit_profit`:

- `PENDING_BUY_TIMEOUT_SEC` → `pending_buy_timeout_seconds`
- `MAX_POSITION_HOLD_SEC` → `max_position_hold_seconds`
- `STOP_LOSS_QUOTE` → `stop_loss_quote`

Alternatively, pass the same keys in **`POST /api/v1/strategies`** JSON **`parameters`**.

---

## 5. Monitoring

Watch strategy-executor logs and Grafana for:

- **`exit_reason`** in signal metadata on SELLs.
- Stuck **`pending_buy`** when **`pending_buy_timeout_seconds`** is `0` (no automatic clear).
- Drift between backtest and stage if Redis or fees are misconfigured.
