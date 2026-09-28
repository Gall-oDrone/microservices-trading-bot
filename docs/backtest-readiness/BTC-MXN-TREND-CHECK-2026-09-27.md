# SMA50 Trend Rule on Bitso `btc_mxn` — Does the BTC-USD Result Transfer?

Date: **2026-09-27**  
Repository: `microservices-trading-bot`  
Branch: `feat/k8s-deployment-manifests`  
Audience: Whoever decides what work happens next on the strategies  
Follows: [`FULL-HISTORY-DAILY-STUDY-2026-09-27.md`](FULL-HISTORY-DAILY-STUDY-2026-09-27.md)

The full-history study found that SMA50 on Yahoo BTC-USD beat holding over 2018–2026 after Bitso
costs. It listed three checks to make before that could matter:
1. Re-run on **`btc_mxn`**, the market the bot actually trades.
2. Price the rule at **maker** fees.
3. Pre-register a forward test.

This document covers (1) and (2). The forward test is registered separately in
[`FORWARD-TEST-PREREGISTRATION-SMA50-2026-09-27.md`](FORWARD-TEST-PREREGISTRATION-SMA50-2026-09-27.md).

Same rule, same parameters, same engine. **Nothing was tuned.**

---

## 1. Answer

**No. On `btc_mxn` the trend rule loses to holding over 2018–2026 at every realistic fee level.**
The signal still carries timing information: frictionless, it beats holding by a wide margin and
beats 97.8% of random same-trade-count strategies. But on this series it trades 104 times instead of
77, and at Bitso's fees that is more than the edge can pay for.

| `btc_mxn`, 2018-01-01 → 2026-09-26 | Round trip | Hold | Trend | Trend − hold | Trend max DD (hold 82.0%) |
|---|---:|---:|---:|---:|---:|
| Taker 78 bps + 10 slippage | 176 bps | +374.0% | +129.8% | **−244 pp** | 66.3% |
| Maker 60 bps + 10 slippage | 140 bps | +375.7% | +235.2% | **−141 pp** | 63.7% |
| Maker 60 bps, no slippage | 120 bps | +376.7% | +313.2% | **−63 pp** | 62.5% |
| Frictionless (reference) | 0 | +382.4% | +1,344.8% | +962 pp | 54.3% |

104 round trips; beats 97.8% of random same-trade-count strategies in every row.

For comparison, the same rule on **BTC-USD** at maker 60 bps makes +1,506% against +512% for holding.
Evidence: [`evidence-2026-09-27/`](evidence-2026-09-27/) (`btc-mxn-*.txt`, `btc-usd-maker.txt`).

At maker fees the rule's drawdown is still ~20 pp shallower than holding's. That is the only thing
left of the previous study's conclusion, and it costs 63–141 pp of return.

### 1.1 Year by year (taker, 176 bps)

| Year | Hold `btc_mxn` | Trend `btc_mxn` | Trips | Hold DD | Trend DD | *Trend − hold on BTC-USD* |
|---|---:|---:|---:|---:|---:|---:|
| 2018 | −77.1% | −47.7% | 11 | 81.8% | **53.9%** | *+29.1 pp* |
| 2019 | +88.6% | +42.5% | 9 | 49.7% | 54.5% | *−25.1 pp* |
| 2020 | +316.5% | +170.0% | 11 | 42.5% | **26.9%** | *−11.3 pp* |
| 2021 | +64.9% | +63.7% | 11 | 53.7% | **39.3%** | *+39.6 pp* |
| 2022 | −66.1% | −53.5% | 14 | 68.9% | **55.0%** | *+7.6 pp* |
| 2023 | +110.2% | +55.7% | 9 | 23.0% | 26.8% | *−31.9 pp* |
| 2024 | +167.4% | +23.2% | 17 | 22.3% | 46.0% | *−52.0 pp* |
| 2025 | −20.7% | −23.5% | 16 | 32.3% | **31.5%** | *−6.5 pp* |
| 2026 to Sep 26 | −7.0% | −2.6% | 9 | 39.7% | **24.8%** | *+14.9 pp* |

- **Beat holding:** 3 of 9 years on `btc_mxn` at taker fees (2018, 2022, 2026), against 4 of 9 on
  BTC-USD. At maker + slippage it is **5 of 9** (adding 2021 and 2025). It wins the flat and down
  years, but loses every strong bull year (2019, 2020, 2023, 2024) by 42–137 pp, which is why the
  full span loses.
- **Smaller drawdown:** 6 of 9 years on `btc_mxn`, against 9 of 9 on BTC-USD.
- **Out-of-sample 2026:** −2.6% against −7.0% for holding at taker fees. At maker fees it is +2.4%
  against −6.5%. It beats 82.5% of random strategies, below the 95% bar.

### 1.2 Why the two series disagree

The same rule on two series of the same asset gives very different answers. That is itself a
finding: **the BTC-USD result was fragile**. Three contributors, measured rather than assumed:

1. **More crossings, so more trades.** Close crossed SMA50 **33 times in 2024 and 32 in 2025** on
   `btc_mxn`, against 15–21 a year in 2017–2021. Each crossing pair is a round trip at 120–176 bps.
   USD/MXN moves add crossings that BTC-USD doesn't have.
2. **The Yahoo 2024 hole flattered the BTC-USD result.** BTC-USD is missing Jun 4 – Aug 19 2024.
   Bitso's series is complete, and that choppy summer is where `btc_mxn` trend made 17 trips and
   finished 144 pp behind holding.
3. **Different day boundaries.** Bitso days close at Mexico City midnight (05:00–06:00 UTC); Yahoo
   days close at UTC midnight. A trend rule that crosses on one close and not the other is
   near its decision boundary. This is small per trade, but it shows how close to the threshold
   many of these decisions are.

The peso also matters for the benchmark: in 2025 `btc_mxn` holding lost 20.7% while BTC-USD lost
8.0%. That is the peso strengthening, not a strategy effect.

---

## 2. Data

New CLI: [`cmd/bitso-daily`](../../services/strategy-executor/cmd/bitso-daily/main.go). It fetches
Bitso's own daily candles from the public, unauthenticated `/api/v3/ohlc` endpoint and writes a CSV
that `daily-research` reads directly.

| Check | Result |
|---|---|
| Span | 2017-05-31 → 2026-09-26, **3,406 bars, no missing days** |
| OHLC consistency (low ≤ open, close ≤ high) | 0 violations |
| Days with < 50 trades | 0 |
| Open vs previous close gap > 5% | 0 |
| Largest daily moves | 2017-07-17 +34.8%, 2020-03-12 −32.2%, 2018-02-05 −30.3%: all real events |
| Latest close vs BTC-USD × USD/MXN | 1,493,130 MXN ≈ 84.5k × ~17.7 ✓ |

- **Day labelling:** each bar carries its Mexico City date. Bar *D* closes at 05:00–06:00 UTC on
  *D+1*, so news with a UTC date ≤ *D* was published before the close. There is no look-ahead in
  `trend_and_news`.
- **In-progress bucket:** today's bucket is dropped, because its "close" is a live price.
- **Snapshot for audit:** [`btc_mxn_daily_bitso.csv`](evidence-2026-09-27/btc_mxn_daily_bitso.csv)
  (sha256 prefix `b85ee055a653bf87`).

**Tests:** [`main_test.go`](../../services/strategy-executor/cmd/bitso-daily/main_test.go) covers
Mexico-date labelling on both sides of the 2022 DST change, dropping the in-progress bucket,
de-duplicating chunk overlaps, and gap-free request chunking.

---

## 3. Caveats

- **Maker fills are assumed, not modelled.** The maker rows assume every entry and exit rests and
  fills at the open. In practice some would miss, or would need to cross the spread. The maker rows
  are therefore an **upper bound** on what this rule can earn.
- **Bitso's early history is thin.** 2017–2019 `btc_mxn` volume was a fraction of today's, and
  wide spreads then are not in the model.
- **One parameter set.** SMA50 was fixed a priori. Trying other lengths on this same data now would
  be tuning. Any variant has to be proposed and tested the way the forward test is (see its
  document, §5).
- **News rows** are reported in the evidence files for completeness. They are negative, as before.

---

## 4. What this means

1. **Do not build SMA50 as a live `btc_mxn` strategy.** On the market it would trade, it underperforms
   holding over 8.7 years at every fee level Bitso offers. The previous study's recommendation (§4.1–2)
   is withdrawn.
2. **The timing signal is real but too expensive at this trade frequency.** Frictionless it wins by
   962 pp and it beats 97.8% of random timing. The binding constraint is, again, **fees × trade
   count**, as in every study in this series.
3. **The forward test stays**, but as a cheap paper record, not as a step towards capital. It is the
   only honest way to evaluate any future variant, and it costs nothing to run. See the
   [pre-registration](FORWARD-TEST-PREREGISTRATION-SMA50-2026-09-27.md).
4. **Where this leaves strategy work.** No rule tested so far beats holding `btc_mxn` after costs.
   Lines of work that could change that:
   - a lower fee tier, which depends on volume;
   - a rule that trades far less often (a variant must be written down *before* it is tested on
     data it hasn't seen);
   - accepting a drawdown-reduction objective instead of a return objective. That is a product
     decision, not a research result.

---

## 5. Reproducing

```bash
cd services/strategy-executor
go run ./cmd/bitso-daily -book btc_mxn -from 2017-06-01 -out ../../bitso/btc_mxn_daily.csv
W=2025-01-01:2025-12-31,2026-01-01:2026-09-26,2018-01-01:2018-12-31,2019-01-01:2019-12-31,\
2020-01-01:2020-12-31,2021-01-01:2021-12-31,2022-01-01:2022-12-31,2023-01-01:2023-12-31,\
2024-01-01:2024-12-31,2018-01-01:2026-09-26
go run ./cmd/daily-research -prices ../../bitso -windows $W                                        # taker
go run ./cmd/daily-research -prices ../../bitso -windows $W -buy-bps 60 -sell-bps 60 -slippage-bps 0   # maker
go run ./cmd/daily-research -prices ../../bitso -windows $W -buy-bps 60 -sell-bps 60 -slippage-bps 10  # maker + slip
go run ./cmd/daily-research -prices ../../bitso -windows $W -buy-bps 0 -sell-bps 0 -slippage-bps 0     # frictionless
```

In the evidence files the first window is labelled `IN-SAMPLE` and the rest `OUT-OF-SAMPLE`. That is
the CLI's convention. The full span (last window) includes every year and is not out-of-sample.
