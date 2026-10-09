# Execution research (generated 2026-10-09T23:06:58Z)

Source: `s3://mtb-development-data-archive-326105557351/trades_compacted`. Days are Bitso candle days (open at Mexico City midnight, 06:00 UTC); a day with a gap over 30m0s between its open and the end of the longest window is skipped. Market orders: half-spread + square-root impact (Y = 1.0).

> Research only. Trades are Bitso production public prints; stage fills are thinner. The best bid/ask is the last print with that maker side (else the last trade ± the assumed half-spread); Through/Touch bracket the unknown queue position. Market orders take the ask/bid plus square-root impact. The live executor and the pre-registered costs are unchanged; any execution change needs its own pre-registration.

## btc_mxn

2026-08-19 → 2026-10-08: 111331 trades over 51 days; 49 days used, 1 skipped. ADV 8.89 BTC, daily vol 2.15 %. Half-spread 1.5 bps (flag). Fees: maker 60, taker 78 bps; pre-registered legs: primary 70, secondary 88 bps.

### Start 06:15 UTC, 0.001 BTC per leg

| Schedule | Model | Legs | Maker share | No fallback | Mean | Median | p90 | vs primary | vs open (mean ± sd) |
|---|---|---|---|---|---|---|---|---|---|
| market | through | 98 | 0 % | 0 % | 80.8 | 80.3 | 81.8 | +10.8 | +80.8 ± 12 |
| maker 1h (today) | through | 98 | 85 % | 83 % | 66.1 | 60.0 | 89.6 | -3.9 | +66.1 ± 22 |
| maker 1h (today) | touch | 98 | 87 % | 85 % | 64.7 | 60.0 | 87.3 | -5.3 | +64.7 ± 19 |
| maker 1h repriced 5m | through | 98 | 98 % | 96 % | 64.2 | 60.0 | 77.5 | -5.8 | +64.2 ± 15 |
| maker 1h repriced 5m | touch | 98 | 99 % | 99 % | 63.0 | 60.0 | 74.8 | -7.0 | +63.0 ± 14 |

### By size, start 06:15 UTC, Through model (mean cost per leg, bps)

| Size BTC | market | maker 1h (today) | best schedule | best mean | best p90 |
|---|---|---|---|---|---|
| 0.001 | 80.8 | 66.1 | maker 1h repriced 5m | 64.2 | 77.5 |
| 0.01 | 85.7 | 72.2 | maker 1h repriced 5m | 69.1 | 92.1 |

### Paired: maker 1h repriced 5m against maker 1h (alt − base, bps per leg; negative = alt cheaper)

| Start | Size BTC | Model | Legs | Base mean | Alt mean | Diff | SE | p (alt cheaper) | Alt cheaper / tie | Base p90 | Alt p90 |
|---|---|---|---|---|---|---|---|---|---|---|---|
| 06:15 | 0.001 | through | 98 | 66.1 | 64.2 | -1.84 | 1.24 | 0.069 | 32 % / 32 % | 89.6 | 77.5 |
| 06:15 | 0.001 | touch | 98 | 64.7 | 63.0 | -1.74 | 1.10 | 0.057 | 30 % / 40 % | 87.3 | 74.8 |
| 06:15 | 0.01 | through | 98 | 72.2 | 69.1 | -3.14 | 1.25 | 0.006 | 55 % / 9 % | 101.6 | 92.1 |
| 06:15 | 0.01 | touch | 98 | 69.7 | 64.8 | -4.97 | 1.26 | 0.000 | 56 % / 15 % | 99.0 | 79.7 |

## btc_usd

2026-09-30 → 2026-10-08: 19804 trades over 9 days; 8 days used, 0 skipped. ADV 12.44 BTC, daily vol 1.53 %. Half-spread 1.3 bps (flag). Fees: maker 30, taker 36 bps; pre-registered legs: primary 40, secondary 46 bps.

### Start 06:15 UTC, 0.001 BTC per leg

| Schedule | Model | Legs | Maker share | No fallback | Mean | Median | p90 | vs primary | vs open (mean ± sd) |
|---|---|---|---|---|---|---|---|---|---|
| market | through | 16 | 0 % | 0 % | 38.1 | 37.4 | 38.0 | -1.9 | +38.1 ± 16 |
| maker 1h (today) | through | 16 | 82 % | 75 % | 32.4 | 30.0 | 38.2 | -7.6 | +32.4 ± 17 |
| maker 1h (today) | touch | 16 | 82 % | 75 % | 32.4 | 30.0 | 38.2 | -7.6 | +32.4 ± 17 |
| maker 1h repriced 5m | through | 16 | 100 % | 100 % | 32.6 | 30.0 | 40.8 | -7.4 | +32.6 ± 18 |
| maker 1h repriced 5m | touch | 16 | 100 % | 100 % | 32.6 | 30.0 | 40.8 | -7.4 | +32.6 ± 18 |

### By size, start 06:15 UTC, Through model (mean cost per leg, bps)

| Size BTC | market | maker 1h (today) | best schedule | best mean | best p90 |
|---|---|---|---|---|---|
| 0.001 | 38.1 | 32.4 | maker 1h | 32.4 | 38.2 |
| 0.01 | 41.1 | 34.7 | maker 1h repriced 5m | 32.9 | 40.8 |

### Paired: maker 1h repriced 5m against maker 1h (alt − base, bps per leg; negative = alt cheaper)

| Start | Size BTC | Model | Legs | Base mean | Alt mean | Diff | SE | p (alt cheaper) | Alt cheaper / tie | Base p90 | Alt p90 |
|---|---|---|---|---|---|---|---|---|---|---|---|
| 06:15 | 0.001 | through | 16 | 32.4 | 32.6 | +0.24 | 1.84 | 0.552 | 31 % / 50 % | 38.2 | 40.8 |
| 06:15 | 0.001 | touch | 16 | 32.4 | 32.6 | +0.24 | 1.84 | 0.552 | 31 % / 50 % | 38.2 | 40.8 |
| 06:15 | 0.01 | through | 16 | 34.7 | 32.9 | -1.82 | 1.84 | 0.161 | 56 % / 19 % | 50.4 | 40.8 |
| 06:15 | 0.01 | touch | 16 | 34.7 | 32.8 | -1.90 | 1.87 | 0.155 | 56 % / 19 % | 50.4 | 40.8 |

