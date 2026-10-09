# SMA50 forward tests: progress report

Generated 2026-10-09T21:32:32Z; data through 2026-10-08. Schema `prereg-report/v1`.

> As of 2026-10-08: a progress report on an incomplete window. Nothing is decided before 2027-09-26.

## btc_mxn

Pre-registration: [FORWARD-TEST-PREREGISTRATION-SMA50-2026-09-27.md](../FORWARD-TEST-PREREGISTRATION-SMA50-2026-09-27.md).

### Primary costs (70 bps per leg, decides)

Window 2026-09-27 → 2027-09-26, 12 bars so far, 1 round trips (an open position counts as one).

| | Criterion | Trend | Benchmark | Result |
|---|---|---:|---:|---|
| H1 | trend max drawdown < hold max drawdown (%) | 5.14 | 5.14 | fail (tie: the criterion is strict) |
| H2 | trend return > hold return (%) | -1.29 | -1.29 | fail (tie: the criterion is strict) |
| H3 | trend beats >= 95% of 2,000 random same-trade-count strategies (btc_mxn, seed 1) | 34.55 | 95.00 | fail |

### Secondary costs (88 bps per leg, reported)

Window 2026-09-27 → 2027-09-26, 12 bars so far, 1 round trips (an open position counts as one).

| | Criterion | Trend | Benchmark | Result |
|---|---|---:|---:|---|
| H1 | trend max drawdown < hold max drawdown (%) | 5.14 | 5.14 | fail (tie: the criterion is strict) |
| H2 | trend return > hold return (%) | -1.65 | -1.65 | fail (tie: the criterion is strict) |
| H3 | trend beats >= 95% of 2,000 random same-trade-count strategies (btc_mxn, seed 1) | 34.55 | 95.00 | fail |

Reading of the primary outcome, as registered: None pass, or only H3: close the line of work. (not a decision: window incomplete)

## btc_usd

Pre-registration: [FORWARD-TEST-PREREGISTRATION-SMA50-BTCUSD-2026-09-29.md](../FORWARD-TEST-PREREGISTRATION-SMA50-BTCUSD-2026-09-29.md).

### Primary costs (40 bps per leg, decides)

Window 2026-09-29 → 2027-09-26, 10 bars so far, 1 round trips (an open position counts as one).

| | Criterion | Trend | Benchmark | Result |
|---|---|---:|---:|---|
| H1 | trend max drawdown (USD) < hold btc_usd max drawdown (%) | 4.22 | 4.22 | fail (tie: the criterion is strict) |
| H2 | trend return in MXN after two 60 bps conversions > hold btc_mxn return (%) | -2.47 | -1.88 | fail (USD return -2.07 %, USD/MXN 18.0115 -> 18.1552 (2026-10-08)) |
| H3 | trend beats >= 95% of 2,000 random same-trade-count strategies (btc_usd, seed 1) | 24.90 | 95.00 | fail |

### Secondary costs (46 bps per leg, reported)

Window 2026-09-29 → 2027-09-26, 10 bars so far, 1 round trips (an open position counts as one).

| | Criterion | Trend | Benchmark | Result |
|---|---|---:|---:|---|
| H1 | trend max drawdown (USD) < hold btc_usd max drawdown (%) | 4.22 | 4.22 | fail (tie: the criterion is strict) |
| H2 | trend return in MXN after two 78 bps conversions > hold btc_mxn return (%) | -2.94 | -2.23 | fail (USD return -2.19 %, USD/MXN 18.0115 -> 18.1552 (2026-10-08)) |
| H3 | trend beats >= 95% of 2,000 random same-trade-count strategies (btc_usd, seed 1) | 24.90 | 95.00 | fail |

Reading of the primary outcome, as registered: H2 fails: close the btc_usd variant. (not a decision: window incomplete)

