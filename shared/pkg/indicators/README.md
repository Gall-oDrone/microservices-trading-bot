# Financial Indicators

Shared package of technical and institutional indicators used by **market-data** (Prometheus metrics), **backtesting**, and **strategy-executor** for signal generation and analysis.

## Tiers

| Tier | Indicators | Description |
|------|------------|-------------|
| **1** | RSI, EMA, SMA, VWAP, Volume, Volume spike, Momentum (1m/5m/15m), Volatility, Bid-ask spread (tight/wide) | Core price and volume metrics |
| **2** | Order book imbalance, VWAP deviation | Institutional-style metrics |
| **3** | RSI overbought/oversold signals, Bollinger Bands | Short-term and band indicators |
| **4** | Order flow imbalance, Trade intensity (per sec/min) | Order flow and activity |

## Package layout

| File | Contents |
|------|----------|
| `types.go` | `PriceVolume`, `OHLCV`, `BidAsk`, `OrderBookSnapshot` |
| `rsi.go` | `RSI()`, `RSIStateFromLevels()`, overbought/oversold thresholds |
| `ema.go` | `EMA()`, `EMASeries()` |
| `sma.go` | `SMA()`, `SMASeries()` |
| `macd.go` | `MACD()`, `MACDSeries()` |
| `bollinger.go` | `BollingerBands()` |
| `vwap.go` | `VWAP()`, `VWAPFromSlices()` |
| `volume.go` | `Volume()`, `VolumeSpike()`, `VolumeSpikeRatio()` |
| `momentum.go` | `Momentum()`, `MomentumMulti()` (1m, 5m, 15m) |
| `volatility.go` | `Volatility()` (std dev of returns) |
| `spread.go` | `Spread()`, `SpreadBps()`, `SpreadStateFromBps()` |
| `orderbook_imbalance.go` | `OrderBookImbalance()` |
| `vwap_deviation.go` | `VWAPDeviation()`, `VWAPDeviationBps()` |
| `order_flow_imbalance.go` | `OrderFlowImbalance()` |
| `trade_intensity.go` | `TradeIntensityPerSecond()`, `TradeIntensityPerMinute()`, `TradeCountAndWindow()` |

## Usage

- **Backtesting:** `strategy/basic_strategy.go` uses `indicators.RSI(prices, period)` for overbought/oversold signals.
- **Market-data:** On each trade, `IndicatorRecorder` updates rolling windows per book and sets Prometheus gauges (`market_data_indicator_*`). See `services/market-data/internal/metrics/indicators.go`.
- **Strategy-executor:** Can import this package to compute indicators from ticker/trade streams when implementing indicator-based strategies.

## Prometheus metrics (market-data)

All gauges have label `book`. Names:

- `market_data_indicator_rsi`, `market_data_indicator_ema_short`, `market_data_indicator_sma_short`
- `market_data_indicator_vwap`, `market_data_indicator_volume`, `market_data_indicator_volume_spike`
- `market_data_indicator_momentum_1m`, `_5m`, `_15m`
- `market_data_indicator_volatility`, `market_data_indicator_spread_bps`, `_spread_tight`, `_spread_wide`
- `market_data_indicator_orderbook_imbalance`, `market_data_indicator_vwap_deviation`
- `market_data_indicator_bollinger_upper`, `_middle`, `_lower`, `_width`
- `market_data_indicator_rsi_oversold`, `market_data_indicator_rsi_overbought`
- `market_data_indicator_order_flow_imbalance`
- `market_data_indicator_trade_intensity_per_sec`, `market_data_indicator_trade_intensity_per_min`

## Grafana

Dashboard **Financial Indicators** (`monitoring/grafana/dashboards/domain/financial-indicators.json`) provides panels for all of the above. Ensure Prometheus is scraping the market-data service (`/metrics`).

## Parameters (defaults in market-data recorder)

- RSI period: 14; oversold 30, overbought 70
- EMA short: 12 (MACD fast 12, slow 26, signal 9)
- Bollinger: period 20, 2 std dev
- Volatility: 20-period returns std dev
- Spread: tight ≤5 bps, wide ≥50 bps
- Volume spike: current window volume > 2× previous half-window average

## References

- [INTRADAY-STRATEGY-IMPLEMENTATION-PLAN.md](../../../INTRADAY-STRATEGY-IMPLEMENTATION-PLAN.md) (repo root)
- [services/backtesting/README.md](../../../services/backtesting/README.md)
- [services/market-data](../../../services/market-data)
