# Execution research (generated 2026-10-09T21:23:34Z)

Source: `s3://mtb-development-data-archive-326105557351/trades_compacted`. Days are Bitso candle days (open at Mexico City midnight, 06:00 UTC); a day with a gap over 30m0s between its open and the end of the longest window is skipped. Market orders: half-spread + square-root impact (Y = 1.0).

> Research only. Trades are Bitso production public prints; stage fills are thinner. The best bid/ask is the last print with that maker side (else the last trade ± the assumed half-spread); Through/Touch bracket the unknown queue position. Market orders take the ask/bid plus square-root impact. The live executor and the pre-registered costs are unchanged; any execution change needs its own pre-registration.

## btc_mxn

2026-08-19 → 2026-10-08: 111331 trades over 51 days; 41 days used, 9 skipped. ADV 8.89 BTC, daily vol 2.15 %. Half-spread 1.5 bps. Fees: maker 60, taker 78 bps; pre-registered legs: primary 70, secondary 88 bps.

### Start 06:01 UTC, 0.001 BTC per leg

| Schedule | Model | Legs | Maker share | No fallback | Mean | Median | p90 | vs primary | vs open (mean ± sd) |
|---|---|---|---|---|---|---|---|---|---|
| market | through | 82 | 0 % | 0 % | 80.5 | 80.3 | 81.8 | +10.5 | +80.5 ± 6 |
| maker 15m | through | 82 | 73 % | 65 % | 68.4 | 60.0 | 90.2 | -1.6 | +68.4 ± 13 |
| maker 15m | touch | 82 | 75 % | 67 % | 67.8 | 60.0 | 89.9 | -2.2 | +67.8 ± 13 |
| maker 30m | through | 82 | 79 % | 73 % | 67.5 | 60.0 | 91.7 | -2.5 | +67.5 ± 15 |
| maker 30m | touch | 82 | 81 % | 76 % | 66.9 | 60.0 | 91.3 | -3.1 | +66.9 ± 15 |
| maker 1h (today) | through | 82 | 84 % | 80 % | 66.8 | 60.0 | 93.1 | -3.2 | +66.8 ± 17 |
| maker 1h (today) | touch | 82 | 85 % | 82 % | 66.2 | 60.0 | 87.0 | -3.8 | +66.2 ± 16 |
| maker 2h | through | 82 | 89 % | 85 % | 65.2 | 60.0 | 77.3 | -4.8 | +65.2 ± 16 |
| maker 2h | touch | 82 | 90 % | 87 % | 64.6 | 60.0 | 71.9 | -5.4 | +64.6 ± 15 |
| maker 4h | through | 82 | 94 % | 91 % | 65.7 | 60.0 | 60.0 | -4.3 | +65.7 ± 27 |
| maker 4h | touch | 82 | 95 % | 93 % | 64.1 | 60.0 | 60.0 | -5.9 | +64.1 ± 23 |
| maker 8h | through | 82 | 96 % | 94 % | 68.7 | 60.0 | 60.0 | -1.3 | +68.7 ± 50 |
| maker 8h | touch | 82 | 97 % | 95 % | 67.3 | 60.0 | 60.0 | -2.7 | +67.3 ± 48 |
| maker 15m repriced 5m | through | 82 | 75 % | 67 % | 67.7 | 60.2 | 89.9 | -2.3 | +67.7 ± 13 |
| maker 15m repriced 5m | touch | 82 | 79 % | 70 % | 66.6 | 60.0 | 86.9 | -3.4 | +66.6 ± 12 |
| maker 30m repriced 5m | through | 82 | 91 % | 87 % | 65.3 | 60.0 | 81.5 | -4.7 | +65.3 ± 12 |
| maker 30m repriced 5m | touch | 82 | 95 % | 90 % | 64.0 | 60.0 | 79.1 | -6.0 | +64.0 ± 10 |
| maker 1h repriced 5m | through | 82 | 97 % | 94 % | 64.3 | 60.0 | 81.0 | -5.7 | +64.3 ± 10 |
| maker 1h repriced 5m | touch | 82 | 98 % | 98 % | 63.5 | 60.0 | 75.8 | -6.5 | +63.5 ± 9 |
| maker 2h repriced 5m | through | 82 | 99 % | 98 % | 63.9 | 60.0 | 80.5 | -6.1 | +63.9 ± 9 |
| maker 2h repriced 5m | touch | 82 | 100 % | 100 % | 63.1 | 60.0 | 75.8 | -6.9 | +63.1 ± 8 |
| maker 4h repriced 5m | through | 82 | 100 % | 100 % | 63.8 | 60.0 | 80.5 | -6.2 | +63.8 ± 10 |
| maker 4h repriced 5m | touch | 82 | 100 % | 100 % | 63.1 | 60.0 | 75.8 | -6.9 | +63.1 ± 8 |
| maker 8h repriced 5m | through | 82 | 100 % | 100 % | 63.8 | 60.0 | 80.5 | -6.2 | +63.8 ± 10 |
| maker 8h repriced 5m | touch | 82 | 100 % | 100 % | 63.1 | 60.0 | 75.8 | -6.9 | +63.1 ± 8 |
| TWAP 4×15m | through | 82 | 66 % | 15 % | 69.5 | 67.7 | 90.3 | -0.5 | +69.5 ± 16 |
| TWAP 4×15m | touch | 82 | 72 % | 18 % | 68.0 | 65.9 | 88.7 | -2.0 | +68.0 ± 15 |
| TWAP 4×30m | through | 82 | 75 % | 27 % | 68.9 | 65.1 | 98.9 | -1.1 | +68.9 ± 21 |
| TWAP 4×30m | touch | 82 | 80 % | 34 % | 67.1 | 64.8 | 98.1 | -2.9 | +67.1 ± 19 |
| TWAP 4×1h | through | 82 | 77 % | 35 % | 73.1 | 68.9 | 121.5 | +3.1 | +73.1 ± 46 |
| TWAP 4×1h | touch | 82 | 81 % | 41 % | 71.3 | 66.7 | 118.6 | +1.3 | +71.3 ± 44 |
| TWAP 4×2h | through | 82 | 87 % | 55 % | 70.2 | 63.9 | 133.2 | +0.2 | +70.2 ± 66 |
| TWAP 4×2h | touch | 82 | 88 % | 59 % | 69.2 | 63.9 | 130.9 | -0.8 | +69.2 ± 64 |

### Start 06:15 UTC, 0.001 BTC per leg

| Schedule | Model | Legs | Maker share | No fallback | Mean | Median | p90 | vs primary | vs open (mean ± sd) |
|---|---|---|---|---|---|---|---|---|---|
| market | through | 82 | 0 % | 0 % | 80.9 | 80.3 | 81.8 | +10.9 | +80.9 ± 12 |
| maker 15m | through | 82 | 62 % | 54 % | 71.1 | 60.0 | 96.4 | +1.1 | +71.1 ± 19 |
| maker 15m | touch | 82 | 71 % | 63 % | 69.1 | 60.0 | 95.9 | -0.9 | +69.1 ± 19 |
| maker 30m | through | 82 | 72 % | 67 % | 68.8 | 60.0 | 96.9 | -1.2 | +68.8 ± 20 |
| maker 30m | touch | 82 | 80 % | 76 % | 66.9 | 60.0 | 95.1 | -3.1 | +66.9 ± 19 |
| maker 1h (today) | through | 82 | 84 % | 82 % | 66.5 | 60.0 | 89.7 | -3.5 | +66.5 ± 22 |
| maker 1h (today) | touch | 82 | 87 % | 84 % | 64.9 | 60.0 | 89.3 | -5.1 | +64.9 ± 20 |
| maker 2h | through | 82 | 89 % | 87 % | 67.0 | 60.0 | 86.2 | -3.0 | +67.0 ± 25 |
| maker 2h | touch | 82 | 91 % | 89 % | 65.7 | 60.0 | 63.4 | -4.3 | +65.7 ± 24 |
| maker 4h | through | 82 | 90 % | 89 % | 67.4 | 60.0 | 77.0 | -2.6 | +67.4 ± 30 |
| maker 4h | touch | 82 | 93 % | 91 % | 65.6 | 60.0 | 60.0 | -4.4 | +65.6 ± 27 |
| maker 8h | through | 82 | 92 % | 91 % | 70.7 | 60.0 | 60.0 | +0.7 | +70.7 ± 43 |
| maker 8h | touch | 82 | 95 % | 94 % | 67.8 | 60.0 | 60.0 | -2.2 | +67.8 ± 39 |
| maker 15m repriced 5m | through | 82 | 72 % | 62 % | 69.0 | 61.9 | 89.0 | -1.0 | +69.0 ± 17 |
| maker 15m repriced 5m | touch | 82 | 83 % | 74 % | 66.6 | 60.0 | 85.4 | -3.4 | +66.6 ± 16 |
| maker 30m repriced 5m | through | 82 | 84 % | 79 % | 66.7 | 60.0 | 84.6 | -3.3 | +66.7 ± 16 |
| maker 30m repriced 5m | touch | 82 | 94 % | 90 % | 63.9 | 60.0 | 74.8 | -6.1 | +63.9 ± 14 |
| maker 1h repriced 5m | through | 82 | 98 % | 96 % | 64.1 | 60.0 | 77.7 | -5.9 | +64.1 ± 15 |
| maker 1h repriced 5m | touch | 82 | 100 % | 100 % | 62.8 | 60.0 | 74.7 | -7.2 | +62.8 ± 14 |
| maker 2h repriced 5m | through | 82 | 99 % | 98 % | 63.8 | 60.0 | 77.4 | -6.2 | +63.8 ± 15 |
| maker 2h repriced 5m | touch | 82 | 100 % | 100 % | 62.8 | 60.0 | 74.7 | -7.2 | +62.8 ± 14 |
| maker 4h repriced 5m | through | 82 | 100 % | 100 % | 63.7 | 60.0 | 77.3 | -6.3 | +63.7 ± 15 |
| maker 4h repriced 5m | touch | 82 | 100 % | 100 % | 62.8 | 60.0 | 74.7 | -7.2 | +62.8 ± 14 |
| maker 8h repriced 5m | through | 82 | 100 % | 100 % | 63.7 | 60.0 | 77.3 | -6.3 | +63.7 ± 15 |
| maker 8h repriced 5m | touch | 82 | 100 % | 100 % | 62.8 | 60.0 | 74.7 | -7.2 | +62.8 ± 14 |
| TWAP 4×15m | through | 82 | 63 % | 11 % | 70.5 | 69.3 | 92.0 | +0.5 | +70.5 ± 20 |
| TWAP 4×15m | touch | 82 | 71 % | 15 % | 68.3 | 66.2 | 88.7 | -1.7 | +68.3 ± 19 |
| TWAP 4×30m | through | 82 | 73 % | 24 % | 69.8 | 67.6 | 96.0 | -0.2 | +69.8 ± 27 |
| TWAP 4×30m | touch | 82 | 77 % | 30 % | 68.7 | 67.6 | 95.4 | -1.3 | +68.7 ± 26 |
| TWAP 4×1h | through | 82 | 74 % | 28 % | 73.0 | 71.3 | 125.1 | +3.0 | +73.0 ± 46 |
| TWAP 4×1h | touch | 82 | 77 % | 33 % | 71.3 | 70.5 | 108.9 | +1.3 | +71.3 ± 45 |
| TWAP 4×2h | through | 82 | 87 % | 52 % | 70.5 | 66.4 | 139.7 | +0.5 | +70.5 ± 65 |
| TWAP 4×2h | touch | 82 | 89 % | 57 % | 69.6 | 66.4 | 127.5 | -0.4 | +69.6 ± 65 |

### Start 14:00 UTC, 0.001 BTC per leg

| Schedule | Model | Legs | Maker share | No fallback | Mean | Median | p90 | vs primary | vs open (mean ± sd) |
|---|---|---|---|---|---|---|---|---|---|
| market | through | 82 | 0 % | 0 % | 80.9 | 80.3 | 80.3 | +10.9 | +80.9 ± 145 |
| maker 15m | through | 82 | 76 % | 74 % | 71.8 | 60.0 | 102.2 | +1.8 | +71.9 ± 144 |
| maker 15m | touch | 82 | 77 % | 74 % | 71.5 | 60.0 | 102.2 | +1.5 | +71.5 ± 144 |
| maker 30m | through | 82 | 85 % | 83 % | 70.7 | 60.0 | 102.7 | +0.7 | +70.7 ± 146 |
| maker 30m | touch | 82 | 85 % | 83 % | 70.7 | 60.0 | 102.7 | +0.7 | +70.7 ± 146 |
| maker 1h (today) | through | 82 | 89 % | 88 % | 68.4 | 60.0 | 88.8 | -1.6 | +68.4 ± 150 |
| maker 1h (today) | touch | 82 | 89 % | 88 % | 68.3 | 60.0 | 88.0 | -1.7 | +68.4 ± 150 |
| maker 2h | through | 82 | 91 % | 91 % | 66.8 | 60.0 | 60.0 | -3.2 | +66.9 ± 149 |
| maker 2h | touch | 82 | 92 % | 91 % | 66.8 | 60.0 | 60.0 | -3.2 | +66.9 ± 149 |
| maker 4h | through | 82 | 95 % | 95 % | 64.6 | 60.0 | 60.0 | -5.4 | +64.6 ± 150 |
| maker 4h | touch | 82 | 95 % | 95 % | 64.6 | 60.0 | 60.0 | -5.4 | +64.6 ± 150 |
| maker 8h | through | 82 | 98 % | 98 % | 62.8 | 60.0 | 60.0 | -7.2 | +62.9 ± 154 |
| maker 8h | touch | 82 | 98 % | 98 % | 62.8 | 60.0 | 60.0 | -7.2 | +62.9 ± 154 |
| maker 15m repriced 5m | through | 82 | 90 % | 88 % | 71.4 | 60.0 | 98.0 | +1.4 | +71.5 ± 145 |
| maker 15m repriced 5m | touch | 82 | 90 % | 88 % | 71.4 | 60.0 | 98.0 | +1.4 | +71.4 ± 145 |
| maker 30m repriced 5m | through | 82 | 99 % | 99 % | 69.6 | 60.0 | 97.1 | -0.4 | +69.7 ± 145 |
| maker 30m repriced 5m | touch | 82 | 99 % | 99 % | 69.6 | 60.0 | 97.1 | -0.4 | +69.7 ± 145 |
| maker 1h repriced 5m | through | 82 | 100 % | 100 % | 69.6 | 60.0 | 91.9 | -0.4 | +69.6 ± 145 |
| maker 1h repriced 5m | touch | 82 | 100 % | 100 % | 69.5 | 60.0 | 91.9 | -0.5 | +69.6 ± 145 |
| maker 2h repriced 5m | through | 82 | 100 % | 100 % | 69.6 | 60.0 | 91.9 | -0.4 | +69.6 ± 145 |
| maker 2h repriced 5m | touch | 82 | 100 % | 100 % | 69.5 | 60.0 | 91.9 | -0.5 | +69.6 ± 145 |
| maker 4h repriced 5m | through | 82 | 100 % | 100 % | 69.6 | 60.0 | 91.9 | -0.4 | +69.6 ± 145 |
| maker 4h repriced 5m | touch | 82 | 100 % | 100 % | 69.5 | 60.0 | 91.9 | -0.5 | +69.6 ± 145 |
| maker 8h repriced 5m | through | 82 | 100 % | 100 % | 69.6 | 60.0 | 91.9 | -0.4 | +69.6 ± 145 |
| maker 8h repriced 5m | touch | 82 | 100 % | 100 % | 69.5 | 60.0 | 91.9 | -0.5 | +69.6 ± 145 |
| TWAP 4×15m | through | 82 | 79 % | 44 % | 69.7 | 66.3 | 121.8 | -0.3 | +69.7 ± 154 |
| TWAP 4×15m | touch | 82 | 82 % | 45 % | 69.1 | 64.3 | 121.8 | -0.9 | +69.1 ± 154 |
| TWAP 4×30m | through | 82 | 84 % | 50 % | 69.6 | 61.1 | 130.7 | -0.4 | +69.6 ± 164 |
| TWAP 4×30m | touch | 82 | 85 % | 50 % | 68.7 | 61.1 | 130.7 | -1.3 | +68.7 ± 164 |
| TWAP 4×1h | through | 82 | 89 % | 59 % | 67.3 | 63.5 | 151.2 | -2.7 | +67.4 ± 168 |
| TWAP 4×1h | touch | 82 | 89 % | 60 % | 66.8 | 63.5 | 151.2 | -3.2 | +66.9 ± 167 |
| TWAP 4×2h | through | 82 | 92 % | 72 % | 64.7 | 63.8 | 146.1 | -5.3 | +64.8 ± 171 |
| TWAP 4×2h | touch | 82 | 93 % | 73 % | 64.7 | 63.8 | 146.1 | -5.3 | +64.7 ± 171 |

### By size, start 06:15 UTC, Through model (mean cost per leg, bps)

| Size BTC | market | maker 1h (today) | best schedule | best mean | best p90 |
|---|---|---|---|---|---|
| 0.001 | 80.9 | 66.5 | maker 4h repriced 5m | 63.7 | 77.3 |
| 0.01 | 85.8 | 72.7 | maker 8h repriced 5m | 65.3 | 85.8 |
| 0.1 | 101.4 | 90.1 | maker 8h repriced 5m | 72.5 | 131.6 |
| 0.5 | 129.6 | 120.5 | maker 8h repriced 5m | 90.6 | 199.0 |

## btc_usd

2026-09-30 → 2026-10-08: 19804 trades over 9 days; 7 days used, 1 skipped. ADV 12.44 BTC, daily vol 1.53 %. Half-spread 1.3 bps. Fees: maker 30, taker 36 bps; pre-registered legs: primary 40, secondary 46 bps.

### Start 06:01 UTC, 0.001 BTC per leg

| Schedule | Model | Legs | Maker share | No fallback | Mean | Median | p90 | vs primary | vs open (mean ± sd) |
|---|---|---|---|---|---|---|---|---|---|
| market | through | 14 | 0 % | 0 % | 39.3 | 37.4 | 38.6 | -0.7 | +39.3 ± 7 |
| maker 15m | through | 14 | 57 % | 57 % | 35.6 | 30.0 | 57.7 | -4.4 | +35.6 ± 16 |
| maker 15m | touch | 14 | 57 % | 57 % | 35.6 | 30.0 | 57.7 | -4.4 | +35.6 ± 16 |
| maker 30m | through | 14 | 66 % | 64 % | 33.2 | 30.0 | 49.2 | -6.8 | +33.2 ± 14 |
| maker 30m | touch | 14 | 66 % | 64 % | 33.2 | 30.0 | 49.2 | -6.8 | +33.2 ± 14 |
| maker 1h (today) | through | 14 | 79 % | 79 % | 33.3 | 30.0 | 53.2 | -6.7 | +33.3 ± 14 |
| maker 1h (today) | touch | 14 | 79 % | 79 % | 33.2 | 30.0 | 53.2 | -6.8 | +33.2 ± 14 |
| maker 2h | through | 14 | 86 % | 86 % | 33.3 | 30.0 | 52.9 | -6.7 | +33.3 ± 16 |
| maker 2h | touch | 14 | 86 % | 86 % | 33.3 | 30.0 | 52.5 | -6.7 | +33.3 ± 16 |
| maker 4h | through | 14 | 86 % | 86 % | 36.8 | 30.0 | 65.6 | -3.2 | +36.8 ± 24 |
| maker 4h | touch | 14 | 86 % | 86 % | 36.7 | 30.0 | 64.9 | -3.3 | +36.7 ± 24 |
| maker 8h | through | 14 | 86 % | 86 % | 42.6 | 30.0 | 64.5 | +2.6 | +42.6 ± 43 |
| maker 8h | touch | 14 | 86 % | 86 % | 42.5 | 30.0 | 63.9 | +2.5 | +42.5 ± 43 |
| maker 15m repriced 5m | through | 14 | 72 % | 71 % | 35.7 | 30.0 | 52.4 | -4.3 | +35.7 ± 13 |
| maker 15m repriced 5m | touch | 14 | 72 % | 71 % | 35.7 | 30.0 | 52.4 | -4.3 | +35.7 ± 13 |
| maker 30m repriced 5m | through | 14 | 100 % | 100 % | 34.2 | 30.0 | 53.1 | -5.8 | +34.2 ± 12 |
| maker 30m repriced 5m | touch | 14 | 100 % | 100 % | 34.2 | 30.0 | 53.1 | -5.8 | +34.2 ± 12 |
| maker 1h repriced 5m | through | 14 | 100 % | 100 % | 34.2 | 30.0 | 53.1 | -5.8 | +34.2 ± 12 |
| maker 1h repriced 5m | touch | 14 | 100 % | 100 % | 34.2 | 30.0 | 53.1 | -5.8 | +34.2 ± 12 |
| maker 2h repriced 5m | through | 14 | 100 % | 100 % | 34.2 | 30.0 | 53.1 | -5.8 | +34.2 ± 12 |
| maker 2h repriced 5m | touch | 14 | 100 % | 100 % | 34.2 | 30.0 | 53.1 | -5.8 | +34.2 ± 12 |
| maker 4h repriced 5m | through | 14 | 100 % | 100 % | 34.2 | 30.0 | 53.1 | -5.8 | +34.2 ± 12 |
| maker 4h repriced 5m | touch | 14 | 100 % | 100 % | 34.2 | 30.0 | 53.1 | -5.8 | +34.2 ± 12 |
| maker 8h repriced 5m | through | 14 | 100 % | 100 % | 34.2 | 30.0 | 53.1 | -5.8 | +34.2 ± 12 |
| maker 8h repriced 5m | touch | 14 | 100 % | 100 % | 34.2 | 30.0 | 53.1 | -5.8 | +34.2 ± 12 |
| TWAP 4×15m | through | 14 | 59 % | 14 % | 35.5 | 34.6 | 48.5 | -4.5 | +35.5 ± 13 |
| TWAP 4×15m | touch | 14 | 60 % | 14 % | 35.5 | 34.6 | 48.5 | -4.5 | +35.5 ± 13 |
| TWAP 4×30m | through | 14 | 70 % | 14 % | 34.6 | 32.3 | 51.4 | -5.4 | +34.6 ± 16 |
| TWAP 4×30m | touch | 14 | 70 % | 21 % | 34.6 | 32.3 | 51.4 | -5.4 | +34.6 ± 16 |
| TWAP 4×1h | through | 14 | 74 % | 29 % | 36.6 | 31.5 | 70.1 | -3.4 | +36.6 ± 27 |
| TWAP 4×1h | touch | 14 | 76 % | 29 % | 36.5 | 30.8 | 70.1 | -3.5 | +36.5 ± 27 |
| TWAP 4×2h | through | 14 | 86 % | 57 % | 34.7 | 33.8 | 79.3 | -5.3 | +34.7 ± 36 |
| TWAP 4×2h | touch | 14 | 88 % | 57 % | 34.5 | 33.8 | 79.3 | -5.5 | +34.5 ± 36 |

### Start 06:15 UTC, 0.001 BTC per leg

| Schedule | Model | Legs | Maker share | No fallback | Mean | Median | p90 | vs primary | vs open (mean ± sd) |
|---|---|---|---|---|---|---|---|---|---|
| market | through | 14 | 0 % | 0 % | 38.2 | 37.4 | 38.3 | -1.8 | +38.2 ± 17 |
| maker 15m | through | 14 | 64 % | 57 % | 33.7 | 30.0 | 46.7 | -6.3 | +33.7 ± 19 |
| maker 15m | touch | 14 | 64 % | 57 % | 33.7 | 30.0 | 46.7 | -6.3 | +33.7 ± 19 |
| maker 30m | through | 14 | 65 % | 57 % | 33.2 | 30.0 | 43.0 | -6.8 | +33.2 ± 17 |
| maker 30m | touch | 14 | 65 % | 57 % | 33.2 | 30.0 | 43.0 | -6.8 | +33.2 ± 17 |
| maker 1h (today) | through | 14 | 79 % | 71 % | 32.7 | 30.0 | 39.9 | -7.3 | +32.7 ± 18 |
| maker 1h (today) | touch | 14 | 79 % | 71 % | 32.7 | 30.0 | 39.9 | -7.3 | +32.7 ± 18 |
| maker 2h | through | 14 | 84 % | 71 % | 33.1 | 30.0 | 43.6 | -6.9 | +33.1 ± 18 |
| maker 2h | touch | 14 | 84 % | 71 % | 33.1 | 30.0 | 43.6 | -6.9 | +33.1 ± 18 |
| maker 4h | through | 14 | 85 % | 79 % | 35.2 | 30.0 | 48.5 | -4.8 | +35.2 ± 20 |
| maker 4h | touch | 14 | 85 % | 79 % | 35.2 | 30.0 | 48.5 | -4.8 | +35.2 ± 20 |
| maker 8h | through | 14 | 92 % | 86 % | 34.1 | 30.0 | 47.6 | -5.9 | +34.1 ± 18 |
| maker 8h | touch | 14 | 92 % | 86 % | 34.1 | 30.0 | 47.6 | -5.9 | +34.1 ± 18 |
| maker 15m repriced 5m | through | 14 | 79 % | 79 % | 33.1 | 30.0 | 45.6 | -6.9 | +33.1 ± 18 |
| maker 15m repriced 5m | touch | 14 | 79 % | 79 % | 33.1 | 30.0 | 45.6 | -6.9 | +33.1 ± 18 |
| maker 30m repriced 5m | through | 14 | 94 % | 93 % | 32.3 | 30.0 | 39.5 | -7.7 | +32.3 ± 17 |
| maker 30m repriced 5m | touch | 14 | 94 % | 93 % | 32.3 | 30.0 | 39.5 | -7.7 | +32.3 ± 17 |
| maker 1h repriced 5m | through | 14 | 100 % | 100 % | 32.3 | 30.0 | 39.5 | -7.7 | +32.3 ± 17 |
| maker 1h repriced 5m | touch | 14 | 100 % | 100 % | 32.3 | 30.0 | 39.5 | -7.7 | +32.3 ± 17 |
| maker 2h repriced 5m | through | 14 | 100 % | 100 % | 32.3 | 30.0 | 39.5 | -7.7 | +32.3 ± 17 |
| maker 2h repriced 5m | touch | 14 | 100 % | 100 % | 32.3 | 30.0 | 39.5 | -7.7 | +32.3 ± 17 |
| maker 4h repriced 5m | through | 14 | 100 % | 100 % | 32.3 | 30.0 | 39.5 | -7.7 | +32.3 ± 17 |
| maker 4h repriced 5m | touch | 14 | 100 % | 100 % | 32.3 | 30.0 | 39.5 | -7.7 | +32.3 ± 17 |
| maker 8h repriced 5m | through | 14 | 100 % | 100 % | 32.3 | 30.0 | 39.5 | -7.7 | +32.3 ± 17 |
| maker 8h repriced 5m | touch | 14 | 100 % | 100 % | 32.3 | 30.0 | 39.5 | -7.7 | +32.3 ± 17 |
| TWAP 4×15m | through | 14 | 68 % | 7 % | 34.4 | 36.7 | 44.8 | -5.6 | +34.4 ± 13 |
| TWAP 4×15m | touch | 14 | 68 % | 7 % | 34.4 | 36.7 | 44.8 | -5.6 | +34.4 ± 13 |
| TWAP 4×30m | through | 14 | 68 % | 29 % | 37.9 | 38.0 | 56.6 | -2.1 | +37.9 ± 22 |
| TWAP 4×30m | touch | 14 | 68 % | 29 % | 37.9 | 38.0 | 56.6 | -2.1 | +37.9 ± 22 |
| TWAP 4×1h | through | 14 | 84 % | 50 % | 33.8 | 31.7 | 74.8 | -6.2 | +33.8 ± 33 |
| TWAP 4×1h | touch | 14 | 84 % | 50 % | 33.8 | 31.7 | 74.8 | -6.2 | +33.8 ± 33 |
| TWAP 4×2h | through | 14 | 87 % | 57 % | 33.8 | 35.3 | 84.8 | -6.2 | +33.8 ± 38 |
| TWAP 4×2h | touch | 14 | 87 % | 57 % | 33.8 | 35.3 | 84.8 | -6.2 | +33.8 ± 38 |

### Start 14:00 UTC, 0.001 BTC per leg

| Schedule | Model | Legs | Maker share | No fallback | Mean | Median | p90 | vs primary | vs open (mean ± sd) |
|---|---|---|---|---|---|---|---|---|---|
| market | through | 14 | 0 % | 0 % | 39.6 | 37.4 | 47.1 | -0.4 | +39.6 ± 80 |
| maker 15m | through | 14 | 79 % | 79 % | 34.6 | 30.0 | 45.6 | -5.4 | +34.6 ± 77 |
| maker 15m | touch | 14 | 79 % | 79 % | 34.6 | 30.0 | 45.6 | -5.4 | +34.6 ± 77 |
| maker 30m | through | 14 | 79 % | 79 % | 35.4 | 30.0 | 44.7 | -4.6 | +35.4 ± 76 |
| maker 30m | touch | 14 | 79 % | 79 % | 35.4 | 30.0 | 44.7 | -4.6 | +35.4 ± 76 |
| maker 1h (today) | through | 14 | 79 % | 79 % | 35.8 | 30.0 | 40.0 | -4.2 | +35.7 ± 77 |
| maker 1h (today) | touch | 14 | 79 % | 79 % | 35.8 | 30.0 | 40.0 | -4.2 | +35.7 ± 77 |
| maker 2h | through | 14 | 93 % | 93 % | 28.2 | 30.0 | 30.0 | -11.8 | +28.2 ± 80 |
| maker 2h | touch | 14 | 93 % | 93 % | 28.2 | 30.0 | 30.0 | -11.8 | +28.2 ± 80 |
| maker 4h | through | 14 | 100 % | 100 % | 27.8 | 30.0 | 30.0 | -12.2 | +27.8 ± 80 |
| maker 4h | touch | 14 | 100 % | 100 % | 27.8 | 30.0 | 30.0 | -12.2 | +27.8 ± 80 |
| maker 8h | through | 14 | 100 % | 100 % | 27.8 | 30.0 | 30.0 | -12.2 | +27.8 ± 80 |
| maker 8h | touch | 14 | 100 % | 100 % | 27.8 | 30.0 | 30.0 | -12.2 | +27.8 ± 80 |
| maker 15m repriced 5m | through | 14 | 93 % | 93 % | 35.1 | 30.0 | 41.9 | -4.9 | +35.0 ± 79 |
| maker 15m repriced 5m | touch | 14 | 93 % | 93 % | 35.1 | 30.0 | 41.9 | -4.9 | +35.0 ± 79 |
| maker 30m repriced 5m | through | 14 | 100 % | 100 % | 34.5 | 30.0 | 41.9 | -5.5 | +34.5 ± 79 |
| maker 30m repriced 5m | touch | 14 | 100 % | 100 % | 34.5 | 30.0 | 41.9 | -5.5 | +34.5 ± 79 |
| maker 1h repriced 5m | through | 14 | 100 % | 100 % | 34.5 | 30.0 | 41.9 | -5.5 | +34.5 ± 79 |
| maker 1h repriced 5m | touch | 14 | 100 % | 100 % | 34.5 | 30.0 | 41.9 | -5.5 | +34.5 ± 79 |
| maker 2h repriced 5m | through | 14 | 100 % | 100 % | 34.5 | 30.0 | 41.9 | -5.5 | +34.5 ± 79 |
| maker 2h repriced 5m | touch | 14 | 100 % | 100 % | 34.5 | 30.0 | 41.9 | -5.5 | +34.5 ± 79 |
| maker 4h repriced 5m | through | 14 | 100 % | 100 % | 34.5 | 30.0 | 41.9 | -5.5 | +34.5 ± 79 |
| maker 4h repriced 5m | touch | 14 | 100 % | 100 % | 34.5 | 30.0 | 41.9 | -5.5 | +34.5 ± 79 |
| maker 8h repriced 5m | through | 14 | 100 % | 100 % | 34.5 | 30.0 | 41.9 | -5.5 | +34.5 ± 79 |
| maker 8h repriced 5m | touch | 14 | 100 % | 100 % | 34.5 | 30.0 | 41.9 | -5.5 | +34.5 ± 79 |
| TWAP 4×15m | through | 14 | 82 % | 43 % | 34.1 | 34.2 | 67.4 | -5.9 | +34.0 ± 71 |
| TWAP 4×15m | touch | 14 | 82 % | 43 % | 34.1 | 34.2 | 67.4 | -5.9 | +34.0 ± 71 |
| TWAP 4×30m | through | 14 | 87 % | 57 % | 35.9 | 34.5 | 60.8 | -4.1 | +35.8 ± 77 |
| TWAP 4×30m | touch | 14 | 87 % | 57 % | 35.9 | 34.5 | 60.8 | -4.1 | +35.8 ± 77 |
| TWAP 4×1h | through | 14 | 84 % | 50 % | 37.1 | 32.9 | 113.9 | -2.9 | +37.1 ± 85 |
| TWAP 4×1h | touch | 14 | 86 % | 57 % | 36.6 | 32.9 | 111.9 | -3.4 | +36.6 ± 85 |
| TWAP 4×2h | through | 14 | 92 % | 64 % | 32.7 | 30.3 | 113.6 | -7.3 | +32.7 ± 89 |
| TWAP 4×2h | touch | 14 | 93 % | 71 % | 32.4 | 30.3 | 113.6 | -7.6 | +32.4 ± 89 |

### By size, start 06:15 UTC, Through model (mean cost per leg, bps)

| Size BTC | market | maker 1h (today) | best schedule | best mean | best p90 |
|---|---|---|---|---|---|
| 0.001 | 38.2 | 32.7 | maker 30m repriced 5m | 32.3 | 39.5 |
| 0.01 | 41.2 | 35.4 | maker 1h repriced 5m | 33.3 | 42.0 |
| 0.1 | 50.6 | 44.0 | TWAP 4×1h | 38.6 | 93.6 |
| 0.5 | 67.6 | 59.4 | maker 8h repriced 5m | 43.5 | 97.4 |

