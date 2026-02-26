# Intraday Strategy Parameters & Optimization

This document describes how to tune and optimize the **strategy-executor** strategies for intraday trading. Strategies are now **config-driven**: parameters can be set via `TradingConfig.Parameters` (or strategy-executor config / API) so you can optimize without code changes.

See also: [INTRADAY-STRATEGY-IMPLEMENTATION-PLAN.md](INTRADAY-STRATEGY-IMPLEMENTATION-PLAN.md) for pipeline, risk limits, and backtesting.

---

## Strategy parameters (strategy-executor)

All three strategies read from `config.Parameters` when created. Unset keys use the defaults below.

### Basic strategy

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `trade_interval_minutes` | number or duration string (e.g. `"5m"`) | 5 | Minimum minutes between signals. |
| `profit_target_pct` | float (decimal, e.g. 0.02 = 2%) | 0.02 | Price-increase threshold for sell. |
| `stop_loss_pct` | float (decimal) | 0.01 | Price-decrease threshold for buy. |

If these are not set, the strategy falls back to `TradingConfig.StopLossPercent` and `TradingConfig.TakeProfitPercent` (converted from percent to decimal).

### Trend strategy

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `trade_interval_minutes` | number or duration string | 10 | Minimum minutes between signals. |
| `trend_period_minutes` | number or duration string | 30 | Window used for trend and history size. |
| `momentum_threshold_pct` | float (decimal) | 0.015 | Minimum momentum (1.5%) to trigger. |

### Arbitrage strategy

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `trade_interval_minutes` | number or duration string | 1 | Minimum minutes between checks. |
| `profit_threshold_pct` | float (decimal) | 0.005 | Min move (0.5%) for arbitrage signal. |
| `spread_threshold_pct` | float (decimal) | 0.002 | Min spread (0.2%) to consider. |

---

## Intraday presets (suggested)

Use these as starting points for **intraday** (shorter holding, tighter targets). Tune further with backtests and Grafana intraday metrics.

### Basic – intraday

- `trade_interval_minutes`: **3** (more frequent than default 5)
- `profit_target_pct`: **0.01** (1% take profit)
- `stop_loss_pct`: **0.005** (0.5% stop)

### Trend – intraday

- `trade_interval_minutes`: **5**
- `trend_period_minutes`: **15**
- `momentum_threshold_pct`: **0.01** (1%)

### Arbitrage – intraday

- `trade_interval_minutes`: **1** (keep frequent)
- `profit_threshold_pct`: **0.003** (0.3%)
- `spread_threshold_pct`: **0.001** (0.1%)

---

## How to set parameters

1. **Strategy-executor config**  
   In `Strategy.Parameters` (or env-driven config that populates `GetTradingConfig().Parameters`), e.g.:

   ```json
   {
     "default_strategy": "basic",
     "parameters": {
       "trade_interval_minutes": 3,
       "profit_target_pct": 0.01,
       "stop_loss_pct": 0.005
     }
   }
   ```

2. **Trading engine / API**  
   When starting or updating a strategy, pass a `TradingConfig` whose `Parameters` map includes the keys above. The strategy-executor creates strategies via `CreateStrategy(name, config)`, so any config source that builds `TradingConfig` with `Parameters` will drive these values.

3. **Backtesting**  
   Backtesting uses its own strategy layer and params (e.g. `rsi_period` for its basic strategy). For **parameter optimization** use the backtesting service’s optimizer API with the appropriate strategy param names for that service (see `services/backtesting/README.md`).

---

## Trading session (intraday window)

The **trading engine** already enforces session hours: it checks `config.IsWithinTradingHours()` before processing. Set `TradingConfig.StartTime` and `EndTime` to your intraday window (e.g. 09:00–18:00 local). Strategy-executor does not receive tick-by-tick time; the engine only runs when within that window.

---

## Optimization workflow

1. **Backtest** with real data (market-data → Redis) using the backtesting service and its strategy params. Compare results to Grafana intraday panels (see INTRADAY-STRATEGY-IMPLEMENTATION-PLAN.md Priority 2).
2. **Tune** strategy-executor parameters (e.g. intraday presets above) via config or API.
3. **Run in stage** with Bitso stage URL and keys; watch Daily Realized P&L, Drawdown %, Trades Today, Win Rate in Grafana.
4. **Adjust** `MaxDailyLoss` and `MaxDrawdownPct` in trading config as needed (Phase 4).

---

**Document version:** 1.0  
**See also:** `services/strategy-executor/internal/strategies/params.go`, `shared/pkg/models/trading_config.go`, `INTRADAY-STRATEGY-IMPLEMENTATION-PLAN.md`.
