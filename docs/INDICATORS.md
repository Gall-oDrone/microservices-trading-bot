# Financial Indicators

The platform exposes **financial and institutional indicators** computed from market data (trades, order book) and published to **Prometheus** for monitoring and strategy tuning.

## Where indicators live

- **Implementation:** `shared/pkg/indicators/` — RSI, EMA, SMA, MACD, Bollinger Bands, VWAP, volume, momentum, volatility, spread, order book imbalance, VWAP deviation, order flow imbalance, trade intensity. See [shared/pkg/indicators/README.md](../shared/pkg/indicators/README.md).
- **Backtesting:** The basic (RSI) strategy uses `indicators.RSI()` and overbought/oversold thresholds; see `services/backtesting/internal/strategy/basic_strategy.go`.
- **Live metrics:** The **market-data** service computes indicators on each trade and exposes them as Prometheus gauges with label `book`.

## Prometheus scrape

Ensure Prometheus scrapes the **market-data** service (e.g. `http://market-data:8083/metrics` or your deployment URL). All indicator gauges are prefixed with `market_data_indicator_` and have at least the `book` label.

## Grafana dashboard

Import or use the **Financial Indicators** dashboard:

- **Path:** `monitoring/grafana/dashboards/domain/financial-indicators.json`
- **Panels:** RSI, VWAP, volume, volume spike, momentum (1m/5m/15m), volatility, bid-ask spread (bps, tight/wide), order book imbalance, EMA/SMA short, VWAP deviation, RSI oversold/overbought, Bollinger Bands and width, order flow imbalance, trade intensity (per second and per minute).

## Tier overview

| Tier | Examples |
|------|----------|
| 1 | RSI, VWAP, volume, volume spike, momentum, volatility, spread |
| 2 | Order book imbalance, VWAP deviation |
| 3 | RSI overbought/oversold, Bollinger Bands |
| 4 | Order flow imbalance, trade intensity |

Full list and formulas are in [shared/pkg/indicators/README.md](../shared/pkg/indicators/README.md).
