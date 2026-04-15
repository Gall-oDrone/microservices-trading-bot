# Limit profit strategy — robustness

This document captures operational and behavioral recommendations for the `limit_profit` strategy (`services/strategy-executor/internal/strategies/limit_profit_strategy.go`), including lifecycle controls implemented in code and follow-up work.

**See also:**
- [LIMIT-PROFIT-STRATEGY.md](LIMIT-PROFIT-STRATEGY.md) — behavior, parameters, organic startup
- [LIMIT-PROFIT-IMPROVEMENTS.md](LIMIT-PROFIT-IMPROVEMENTS.md) — production improvements roadmap

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
| **`pending_buy_timeout_seconds`** | int | `0` (off) | If a BUY signal was emitted but no fill arrives within this many seconds, the strategy attempts **`POST /api/v1/orders/cancel-by-signal`** on order-management with retry logic; on success it **clears local** `pending_buy`. Prometheus metric tracks failures. |
| **`max_position_hold_seconds`** | int | `0` (off) | After a position is open, emit a **SELL** if **`EntryTime`** age exceeds this (time stop). Metadata: **`exit_reason`** = `max_hold`. |
| **`stop_loss_quote`** | float | `0` (off) | In **quote currency per 1 base** (e.g. MXN per BTC for `btc_mxn`). Emit **SELL** when **`compare_price`** ≤ **`entry - stop_loss_quote`** (uses the same compare path as other exits). Metadata: **`exit_reason`** = `stop_loss`. |
| **`max_daily_loss_quote`** | float | `0` (off) | Session-level circuit breaker. Pauses strategy when cumulative realized losses exceed this amount. Resets at `daily_loss_reset_hour_utc`. |
| **`daily_loss_reset_hour_utc`** | int | `0` | Hour (0-23) when daily loss counter resets. |
| **`min_profit_bps`** | float | `0` (off) | If > 0, computes `min_profit = entry * min_profit_bps / 10000`. Takes precedence over absolute `min_profit`. |
| **`entry_offset_bps`** | float | `0` (off) | If > 0, computes `entry_offset = reference * entry_offset_bps / 10000`. Takes precedence over absolute `entry_offset`. |

**Exit ordering (when in position):** `stop_loss` is evaluated before `max_hold`, then the usual **take-profit** threshold (`compare_price` vs fee-aware threshold). All exits set metadata **`exit_reason`**: `take_profit` | `stop_loss` | `max_hold` | `circuit_breaker`.

**State:** **`PendingBuySince`** on `StrategyState` records when a pending BUY was sent (used for timeout; persisted with Redis snapshots).

---

## 3. Remaining gaps and follow-ups

> **Note:** Many gaps below have been addressed. See [LIMIT-PROFIT-IMPROVEMENTS.md](LIMIT-PROFIT-IMPROVEMENTS.md) for full roadmap.

| Topic | Status | Notes |
|-------|--------|-------|
| **Cancel on pending timeout** | ✅ Improved | Retry logic with backoff + Prometheus metric. See improvements doc §1.2. |
| **SELL limit price alignment** | ✅ Fixed | Signal price uses `comparePrice` when `exit_price_reference` ≠ `last`. See improvements doc §1.1. |
| **bps-based min profit** | ✅ Added | `min_profit_bps` parameter available. See improvements doc §3.1. |
| **bps-based entry offset** | ✅ Added | `entry_offset_bps` parameter available. See improvements doc §3.2. |
| **Session circuit breaker** | ✅ Added | `max_daily_loss_quote` pauses strategy on cumulative loss. See improvements doc §2.1. |
| **Prometheus metrics** | ✅ Added | Entry/exit counters, durations, circuit breaker gauge. See improvements doc §4.1. |
| **Cooldown semantics** | Pending | `min_signal_interval` gates off `LastSignalTime` (both entry and exit). Separate entry-only timestamp is future work. |
| **Partial fill handling** | Pending | Track cumulative fills for correct position size. See improvements doc §1.3. |
| **Trailing stop** | Pending | Lock in gains once profitable. See improvements doc §2.2. |
| **ATR-scaled sizing** | Pending | Volatility-adjusted position sizing. See improvements doc §3.3. |

---

## 4. Script and API usage

**Organic script** (`scripts/start-organic-trading.sh`) supports optional env vars for `STRATEGY_TYPE=limit_profit`:

- `PENDING_BUY_TIMEOUT_SEC` → `pending_buy_timeout_seconds`
- `MAX_POSITION_HOLD_SEC` → `max_position_hold_seconds`
- `STOP_LOSS_QUOTE` → `stop_loss_quote`
- `MAX_DAILY_LOSS_QUOTE` → `max_daily_loss_quote`
- `MIN_PROFIT_BPS` → `min_profit_bps`
- `ENTRY_OFFSET_BPS` → `entry_offset_bps`

Alternatively, pass the same keys in **`POST /api/v1/strategies`** JSON **`parameters`**.

---

## 5. Monitoring

Watch strategy-executor logs and Grafana for:

- **`exit_reason`** in signal metadata on SELLs.
- **`limit_profit_circuit_breaker_active`** gauge (1 = paused due to daily loss).
- **`limit_profit_pending_cancel_failures_total`** counter for stuck pending BUYs.
- **`limit_profit_daily_realized_pnl_quote`** gauge for session P&L tracking.
- Stuck **`pending_buy`** when **`pending_buy_timeout_seconds`** is `0` (no automatic clear).
- Drift between backtest and stage if Redis or fees are misconfigured.
