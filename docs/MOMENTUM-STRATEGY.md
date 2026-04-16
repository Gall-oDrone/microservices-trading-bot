# `momentum` strategy

**Type id:** `momentum` (registered in `strategy-executor`).

**Source:** `services/strategy-executor/internal/strategies/momentum_strategy.go`

A trend-following intraday strategy that combines an **RSI** oscillator with an **EMA** trend filter. It enters on RSI extremes that **agree with** the prevailing EMA trend (avoiding mean-reversion against a strong trend) and exits when RSI returns to a neutral band.

**See also:**
- [FINANCIAL-STRATEGY-IMPLEMENTATION-GUIDE.md](FINANCIAL-STRATEGY-IMPLEMENTATION-GUIDE.md) — overall framework, lifecycle, validation gates
- [LIMIT-PROFIT-STRATEGY.md](LIMIT-PROFIT-STRATEGY.md) — sibling strategy with a fee-aware limit-order entry/exit model
- [ORGANIC-TRADING-STARTUP.md](ORGANIC-TRADING-STARTUP.md) — how the organic loop drives `OnTick`
- [INDICATORS.md](INDICATORS.md) — indicator semantics, periods, storage

---

## 1. Behavior

The strategy reads two indicators per tick from the indicator service:

- **RSI** (period configured at the **service** level, default `14`)
- **EMA** (period configured at the **service** level, default `20`)

> The strategy parameters `rsi_period` and `ema_period` are accepted, persisted, and surfaced in signal metadata for audit, but the indicator helpers `GetRSI`/`GetEMA` currently read using the service-wide configured periods. To use a different period today, set it on the strategy-executor (`INDICATOR_RSI_PERIOD`, `INDICATOR_EMA_PERIOD`) — per-strategy periods are tracked as a future enhancement.

### Signal generation

| Condition (no position) | Action |
|-------------------------|--------|
| `RSI < oversold_level` **and** `price > EMA` | Emit **BUY** (oversold bounce **with** uptrend filter) |
| `RSI > overbought_level` **and** `price < EMA` | Emit **SELL** (overbought reversal **with** downtrend filter) |
| Otherwise | No signal |

### Exit logic (in position)

When a position is open, the strategy emits an exit when **RSI returns to neutral**:

```
oversold_level < RSI < overbought_level
```

- A **long** position is closed with a **SELL**.
- A **short** position is closed with a **BUY**.

Exit signals carry the unrealized P&L (`unrealized_pnl`) and the entry price in metadata so downstream consumers can audit the round trip.

### Confidence

A 0.5 – 0.95 score is computed from:

1. **RSI extremity** — how far past the threshold the reading is.
2. **EMA agreement** — `+0.1` boost when `price`/`EMA` divergence reinforces the side (uptrend > 1% on BUY, downtrend > 1% on SELL).

The `min_confidence` parameter drops signals below the configured score (default `0` = emit everything).

### Cooldown

`min_signal_interval` (seconds) gates **all** signal emissions (entry and exit alike). Every emitted signal updates `state.LastSignalTime`.

### Schedule

The strategy honours the standard `schedule` block (`active_hours`, `timezone`, `active_days`). Outside the schedule `OnTick` returns no signal.

---

## 2. Parameters (JSON `parameters`)

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `rsi_period` | int | `14` | RSI period for audit/metadata (effective period set at service level) |
| `overbought_level` | number | `70` | RSI threshold for SELL entries / exit upper bound |
| `oversold_level` | number | `30` | RSI threshold for BUY entries / exit lower bound |
| `ema_period` | int | `20` | EMA period for audit/metadata (effective period set at service level) |
| `min_signal_interval` | int | `60` | Seconds between successive signals |
| `min_confidence` | number | `0` | Drop signals with confidence below this floor (0 to 1) |
| `position_size` | number | `0.001` | Order size in base currency |
| `dry_run` | bool | `false` | Tag emitted signals with `metadata.dry_run=true` (trading-engine should skip) |

### Signal metadata fields

Every entry signal includes:

| Key | Description |
|-----|-------------|
| `rsi` | RSI value at signal time |
| `ema` | EMA value at signal time |
| `signal_type` | `entry_long` / `entry_short` / `exit` |
| `rsi_period` / `ema_period` | Configured periods (audit) |
| `dry_run` | Present and `true` only when the strategy was started in dry-run |

Exit signals add `entry_price` and `unrealized_pnl`.

---

## 3. Indicator prerequisites

The momentum strategy will return **no signal** until both indicators are warm. Practically this means:

- `market-data` is publishing trades for the configured `book`
- The strategy-executor indicator service has computed at least one RSI and one EMA value for the book (visible at `GET /api/v1/indicators/{book}/snapshot`)

`scripts/start-organic-trading.sh` reports the indicator snapshot count before creating the strategy and warns when the snapshot is empty.

---

## 4. Organic startup

```bash
STRATEGY_TYPE=momentum ./scripts/start-organic-trading.sh
```

To run momentum **alongside** another strategy in the same script invocation, use the new `STRATEGY_TYPES` variable (comma-separated list):

```bash
STRATEGY_TYPES=mean_reversion,momentum ./scripts/start-organic-trading.sh
```

When `STRATEGY_TYPES` is set, the script derives a unique name per type (`organic_<type>_<timestamp>`) and ignores `STRATEGY_NAME`.

### Optional env variables

| Variable | Maps to parameter |
|----------|-------------------|
| `RSI_PERIOD` | `rsi_period` |
| `OVERBOUGHT_LEVEL` | `overbought_level` |
| `OVERSOLD_LEVEL` | `oversold_level` |
| `EMA_PERIOD` | `ema_period` |
| `MOMENTUM_MIN_SIGNAL_INTERVAL` | `min_signal_interval` (momentum-only; distinct from `MIN_SIGNAL_INTERVAL` used by `limit_profit`) |
| `MOMENTUM_MIN_CONFIDENCE` | `min_confidence` |
| `POSITION_SIZE` | `position_size` |
| `DRY_RUN` | `dry_run` |
| `CLEANUP_ON_EXIT` | (script-only) delete the strategy on Ctrl-C / EXIT |
| `BOOK` | book |
| `STRATEGY_NAME` | strategy name (single-type mode only) |

---

## 5. HTTP API example

Create directly via the strategy-executor API:

```bash
curl -sS -X POST http://127.0.0.1:8084/api/v1/strategies \
  -H 'Content-Type: application/json' \
  -d '{
    "name": "momentum_btc_mxn_demo",
    "type": "momentum",
    "book": "btc_mxn",
    "parameters": {
      "rsi_period": 14,
      "overbought_level": 70,
      "oversold_level": 30,
      "ema_period": 20,
      "min_signal_interval": 60,
      "min_confidence": 0.6,
      "position_size": 0.001,
      "dry_run": false
    }
  }'

curl -sS -X POST http://127.0.0.1:8084/api/v1/strategies/momentum_btc_mxn_demo/start
```

Inspect:

```bash
curl -sS http://127.0.0.1:8084/api/v1/strategies/momentum_btc_mxn_demo | jq
```

---

## 6. Conservative parameter grid (suggested for stage observation)

| Profile | rsi_period | overbought | oversold | ema_period | min_signal_interval | min_confidence |
|---------|-----------|------------|----------|------------|--------------------|----------------|
| **Conservative** | 14 | 75 | 25 | 50 | 300 | 0.7 |
| **Moderate** | 14 | 70 | 30 | 20 | 60 | 0.6 |
| **Aggressive** | 9 | 65 | 35 | 10 | 30 | 0.5 |

Conservative requires extreme RSI plus a slow trend filter — far fewer signals, much higher conviction. Aggressive trades faster on noisier RSI extremes; expect more whipsaws in flat regimes.

---

## 7. Risk and lifecycle (current scope)

| Topic | Status | Notes |
|-------|--------|-------|
| RSI/EMA entry + exit | ✅ | Implemented |
| EMA trend filter on entries | ✅ | Implemented |
| Schedule / `min_signal_interval` cooldown | ✅ | Implemented |
| `min_confidence` floor | ✅ | Implemented |
| `dry_run` flag | ✅ | Implemented (entry + exit metadata) |
| Hard stop-loss in quote terms | ⏳ | Not yet — handled implicitly by RSI re-cross. Track in roadmap below. |
| Per-strategy ATR-scaled sizing | ⏳ | Future — `limit_profit` already implements this; reuse the same model. |
| Session circuit breaker (`max_daily_loss_quote`) | ⏳ | Future — port from `limit_profit`. |
| Per-strategy RSI/EMA periods | ⏳ | Indicator service uses global periods today. |
| Prometheus metrics (`momentum_*`) | ⏳ | The strategy is covered by generic `strategy_executor_signals_generated_total{strategy,side}`; dedicated counters are tracked for a future PR. |

When the strategy moves toward production, mirror the `limit_profit` lifecycle controls (see [LIMIT-PROFIT-IMPROVEMENTS.md](LIMIT-PROFIT-IMPROVEMENTS.md)).

---

## 8. Backtesting

The momentum strategy implements `EnhancedStrategy` and is wired through the same registry used by `limit_profit`, so the existing backtest harness (`services/strategy-executor/internal/backtest/`) can replay historical trades into `OnTick` directly. Validate against the gates in [FINANCIAL-STRATEGY-IMPLEMENTATION-GUIDE.md §Validation Gates](FINANCIAL-STRATEGY-IMPLEMENTATION-GUIDE.md#validation-gates) before promoting to stage observation.

Suggested gate floors for an initial pass:

- Sharpe Ratio > **1.0**
- Max Drawdown < **10%**
- Win Rate > **40%** (momentum tends to have lower hit rate than mean reversion but larger winners)
- Profit Factor > **1.2**
- Min trades > **50** to keep the sample meaningful

---

## 9. Operational checks

Before relying on momentum signals, verify:

1. `kubectl -n <ns> logs deploy/strategy-executor | grep -i 'Connected to Redis'` — required for indicator persistence.
2. `GET /api/v1/indicators/{book}/snapshot` returns non-empty RSI and EMA fields.
3. `GET /api/v1/strategies/<name>` shows `running: true` and the expected `parameters`.
4. After warm-up, `kubectl logs deploy/strategy-executor` shows `Published BUY/SELL signal for <book>` lines tied to `strategy=<name>`.

If signals never appear in flat markets, that is **expected**: momentum requires RSI to first pierce a threshold and the price/EMA cross to agree.
