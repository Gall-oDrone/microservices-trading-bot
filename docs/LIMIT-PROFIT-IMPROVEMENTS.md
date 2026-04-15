# Limit Profit Strategy — Production Improvements

This document tracks improvements to the `limit_profit` strategy for production readiness. Items are prioritized by financial/operational impact.

**See also:**
- [LIMIT-PROFIT-STRATEGY.md](LIMIT-PROFIT-STRATEGY.md) — behavior, parameters, usage
- [LIMIT-PROFIT-ROBUSTNESS.md](LIMIT-PROFIT-ROBUSTNESS.md) — operational foundations, lifecycle controls

---

## Priority 1: Execution Model Fixes (Critical)

### 1.1 Align SELL limit price with compare_price ✅

**Problem:** Exit profitability is evaluated against `comparePrice` (bid/mid/min_last_bid), but the SELL signal uses `tickPrice` as the limit price. This can result in signaling "profitable" based on bid while the order executes at a different price.

**Fix:** When `exit_price_reference` ≠ `last`, set the signal `Price` field to `comparePrice` so downstream execution matches the threshold logic.

**Status:** Implemented

---

### 1.2 Mandatory venue cancel on pending timeout ✅

**Problem:** `pending_buy_timeout_seconds` attempts to cancel via order-management, but on failure it leaves local state stuck (no retry, no metric).

**Fix:**
- Retry cancel up to 3 times with exponential backoff
- Add Prometheus metric `limit_profit_pending_cancel_failures_total`
- Only clear local pending state after successful cancel or max retries exhausted

**Status:** Implemented

---

### 1.3 Partial fill handling ✅

**Problem:** `OnOrderFilled` assumes full fill; partial fills cause position size drift.

**Fix:**
- Track cumulative filled amount in state (`cumulativeFilledAmt`, `targetOrderSize`)
- Update `PositionSize` incrementally on each partial fill event with weighted average entry price
- Only clear pending state when cumulative fill reaches target (with 0.1% tolerance)
- Position state updated even on partial fills for accurate exit sizing

**Status:** Implemented

---

## Priority 2: Risk Controls (High Impact)

### 2.1 Session-level circuit breaker (max_daily_loss_quote) ✅

**Problem:** `stop_loss_quote` limits per-trade loss but nothing caps session-level cumulative losses.

**Fix:** Add `max_daily_loss_quote` parameter:
- Track `DailyRealizedLoss` in strategy state
- Pause strategy (stop emitting signals) when cumulative loss exceeds limit
- Reset at configurable time (default: midnight UTC) or via API

**Parameters:**
- `max_daily_loss_quote`: float64 (0 = disabled)
- `daily_loss_reset_hour_utc`: int (0-23, default 0)

**Status:** Implemented

---

### 2.2 Trailing stop ✅

**Problem:** No mechanism to lock in gains once position is profitable.

**Fix:** Add `trailing_stop_quote` parameter:
- Once unrealized P&L exceeds `trailing_stop_activation_quote`, start tracking high-water mark
- Emit SELL when price drops `trailing_stop_quote` below high-water mark
- State persisted: `trailingStopActive`, `trailingHighWater`
- Exit reason: `trailing_stop`

**Parameters:**
- `trailing_stop_quote`: float64 (0 = disabled)
- `trailing_stop_activation_quote`: float64 (minimum profit before trailing activates)

**Status:** Implemented

---

### 2.3 Max concurrent pending orders ✅

**Problem:** If fills are slow and timeout clears local state, multiple BUY signals could stack.

**Fix:** Add `max_pending_orders` parameter (default 1); track `pendingOrderCount`; reject new entry if at limit.

**Status:** Implemented

---

## Priority 3: Threshold & Sizing Model (Medium Impact)

### 3.1 BPS-based min_profit ✅

**Problem:** Fixed `min_profit` in quote currency doesn't scale with BTC price movements.

**Fix:** Add `min_profit_bps` parameter:
- If > 0, compute `min_profit = entry * min_profit_bps / 10000`
- Takes precedence over absolute `min_profit` when set

**Status:** Implemented

---

### 3.2 BPS-based entry_offset ✅

**Problem:** Fixed `entry_offset` doesn't adapt to price levels.

**Fix:** Add `entry_offset_bps` parameter:
- If > 0, compute `entry_offset = reference * entry_offset_bps / 10000`
- Takes precedence over absolute `entry_offset` when set

**Status:** Implemented

---

### 3.3 ATR-scaled position sizing ✅

**Problem:** Fixed `position_size` doesn't account for volatility; risk per trade varies.

**Fix:** Add volatility-adjusted sizing mode:
- `sizing_mode`: "fixed" | "atr_scaled"
- `target_risk_quote`: target quote-currency risk per trade
- `atr_multiplier`: multiplier for ATR (default 1.0)
- `atr_period`: period for ATR indicator (default 14, uses service default)
- Compute: `position_size = target_risk_quote / (ATR * atr_multiplier)`
- Falls back to fixed size if ATR unavailable or computed size exceeds max

**Status:** Implemented

---

## Priority 4: Observability (Medium Impact)

### 4.1 Prometheus metrics ✅

**Problem:** No strategy-specific metrics for dashboards and alerting.

**Fix:** Add metrics:
- `limit_profit_entry_signals_total{strategy,book}`
- `limit_profit_exit_signals_total{strategy,book,reason}` (take_profit, stop_loss, max_hold, circuit_breaker)
- `limit_profit_pending_buy_duration_seconds{strategy,book}` (histogram)
- `limit_profit_position_hold_duration_seconds{strategy,book}` (histogram)
- `limit_profit_pending_cancel_failures_total{strategy,book}`
- `limit_profit_daily_realized_pnl_quote{strategy,book}` (gauge)
- `limit_profit_circuit_breaker_active{strategy,book}` (gauge, 0/1)

**Status:** Implemented

---

### 4.2 Structured logging ✅

**Problem:** Only `log.Printf` on cancel failure; hard to correlate events.

**Fix:** Integrated with existing zerolog-based logger package:
- Added `SetLogger()` method to inject logger
- Helper `logger()` method adds strategy/book context automatically
- Circuit breaker and cancel retry events use structured fields
- Log levels: WARN for failures/breaker, INFO for success

**Status:** Implemented (using zerolog via internal/logger package)

---

### 4.3 Entry signal metadata completeness

**Problem:** Entry metadata lacks fee model and expected liquidity for audit trail.

**Fix:** Add to entry signal metadata:
- `buy_liquidity_expected`
- `fee_model`
- `use_bitso_fees`

**Status:** Implemented

---

## Priority 5: Script Enhancements

### 5.1 Indicator warm-up check ✅

**Problem:** Strategy may spin without signals if indicators haven't computed yet.

**Fix:** In `start-organic-trading.sh`, curl `/api/v1/indicators/{book}/snapshot` before starting; warn if empty.

**Status:** Implemented

---

### 5.2 Dry-run flag ✅

**Problem:** No way to test signal generation without executing orders.

**Fix:**
- Added `dry_run` parameter to strategy config (parsed in Initialize)
- Entry and exit signals include `metadata.dry_run=true` when enabled
- Script env `DRY_RUN=true` sets parameter; prints warning at startup

**Status:** Implemented

---

### 5.3 Cleanup on Ctrl-C ✅

**Problem:** Trap only kills port-forward; leftover strategy may confuse next run.

**Fix:**
- Added `CLEANUP_ON_EXIT=true` env variable
- Trap calls `curl -X DELETE` on the strategy before killing port-forward
- Handles INT, TERM, and normal EXIT signals

**Status:** Implemented

---

## Priority 6: Backtest Integration (High Impact)

### 6.1 BacktestDataProvider for limit_profit ✅

**Problem:** No unified code path for backtest vs live; can't validate strategy offline.

**Fix:** Implemented in `internal/backtest/` package:
- `BacktestDataProvider`: replays historical trades, simulates ticker
- `Runner`: executes strategy against data, computes metrics
- `BacktestResult`: comprehensive statistics (Sharpe, Sortino, win rate, drawdown)
- Uses same `OnTick`/`OnOrderFilled` code path as live

**Files:**
- `services/strategy-executor/internal/backtest/data_provider.go`
- `services/strategy-executor/internal/backtest/runner.go`

**Status:** Implemented

---

### 6.2 Promotion gate script ✅

**Problem:** No automated validation before Stage deployment.

**Fix:** Created `scripts/validate-limit-profit-backtest.sh`:
- Fetches backtest results from API
- Validates gates: Sharpe > 1.0, max DD < 10%, win rate > 40%, profit factor > 1.1, min trades > 50
- All thresholds configurable via env/flags
- Exit non-zero if gates fail

**Status:** Implemented

---

## Implementation Checklist

| Item | Priority | Status | PR/Commit |
|------|----------|--------|-----------|
| SELL price alignment | P1 | ✅ | — |
| Mandatory venue cancel | P1 | ✅ | — |
| Partial fill handling | P1 | ✅ | — |
| Session circuit breaker | P2 | ✅ | — |
| Trailing stop | P2 | ✅ | — |
| Max concurrent pending | P2 | ✅ | — |
| min_profit_bps | P3 | ✅ | — |
| entry_offset_bps | P3 | ✅ | — |
| ATR-scaled sizing | P3 | ✅ | — |
| Prometheus metrics | P4 | ✅ | — |
| Structured logging | P4 | ✅ | — |
| Entry metadata | P4 | ✅ | — |
| Indicator warm-up | P5 | ✅ | — |
| Dry-run flag | P5 | ✅ | — |
| Cleanup on Ctrl-C | P5 | ✅ | — |
| BacktestDataProvider | P6 | ✅ | — |
| Promotion gate script | P6 | ✅ | — |

---

## Metrics Reference

After implementation, add these to Grafana dashboard `strategy-executor.json`:

```promql
# Entry/exit rates
rate(limit_profit_entry_signals_total[5m])
rate(limit_profit_exit_signals_total[5m])

# Exit reasons breakdown
sum by (reason) (rate(limit_profit_exit_signals_total[5m]))

# Pending buy duration (p95)
histogram_quantile(0.95, rate(limit_profit_pending_buy_duration_seconds_bucket[5m]))

# Circuit breaker status
limit_profit_circuit_breaker_active

# Daily P&L
limit_profit_daily_realized_pnl_quote
```
