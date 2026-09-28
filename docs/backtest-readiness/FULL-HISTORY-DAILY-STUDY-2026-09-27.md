# Full-History Daily Study — BTC-USD 2018–2026 on the Refreshed Compacted Files

Date: **2026-09-27**  
Repository: `microservices-trading-bot`  
Branch: `feat/k8s-deployment-manifests`  
Audience: Whoever decides what work happens next on the strategies  
Follows: [`DAILY-HORIZON-NEWS-STUDY-2026-09-26.md`](DAILY-HORIZON-NEWS-STUDY-2026-09-26.md)

The 2026-09-26 study ended with three next steps: backfill Jan–Jul 2026, automate the refresh of the
compacted files, and hold off on new signal work until unseen data existed. This document covers what
happened when those were done.

The backfill turned out to be unnecessary: `s3://…/stocks/crypto/book=btc-usd/` already holds
2018-01-01 → today. The 2026-09-26 study had read `stocks/transformed/`, which only goes back to
2025. Rebuilding the compacted file from the full `stocks/crypto/` tree gives **3,072 BTC-USD daily
bars**, which is 8.7 years instead of 1.

Same rules, same parameters, same costs. **Nothing was tuned.**

> [!WARNING]
> **Did not transfer to `btc_mxn` (checked the same day).** On Bitso's own `btc_mxn` daily candles
> the same rule trades 104 times instead of 77 and **loses to holding over 2018–2026 at every Bitso
> fee level**: +130% vs +374% at taker, +235% vs +376% at maker + slippage. The §4 recommendation
> to pursue it is withdrawn. See
> [`BTC-MXN-TREND-CHECK-2026-09-27.md`](BTC-MXN-TREND-CHECK-2026-09-27.md).

---

## 1. Answer

**Over the long run, the SMA50 trend rule is the first thing in this series that looks like a real
edge after Bitso costs. The news rule stayed negative. The 2025 trend "hint" from the previous study
was a warm-up artefact.**

| 2018-01-01 → 2026-09-26, Bitso costs (176 bps round trip) | Return | Round trips | Time in market | Max drawdown | Costs paid | Beats random, same trade count |
|---|---:|---:|---:|---:|---:|---:|
| `buy_and_hold` | +508.6% | 1 | 100% | **81.5%** | 6.3% | — |
| `trend_sma50` | **+940.3%** | 77 | 49.2% | **64.7%** | 722.7% | **99.9%** |
| Frictionless `trend_sma50` (reference) | +3,958% | 77 | 49.2% | 56.4% | 0 | 99.9% |

Evidence: [`full-span-bitso-costs.txt`](evidence-2026-09-27/full-span-bitso-costs.txt),
[`full-span-frictionless.txt`](evidence-2026-09-27/full-span-frictionless.txt).

"Costs paid" is in percent of *starting* capital, so it compounds with the equity. Costs took roughly
three-quarters of the frictionless gain. What remains still beats holding by ~430 pp, with a shallower
worst drawdown.

### 1.1 Year by year: it wins by losing less

| Year, Bitso costs | Hold | Trend | Trend − hold | Beats random | Hold max DD | Trend max DD |
|---|---:|---:|---:|---:|---:|---:|
| 2018 | −73.0% | −43.9% | **+29.1 pp** | 73.7% | 81.5% | **43.9%** |
| 2019 | +88.6% | +63.5% | −25.1 pp | 79.8% | 49.0% | 47.4% |
| 2020 | +296.0% | +284.7% | −11.3 pp | 99.2% | 51.9% | **18.8%** |
| 2021 | +56.9% | +96.5% | **+39.6 pp** | 94.2% | 53.1% | **28.6%** |
| 2022 | −64.9% | −57.3% | **+7.6 pp** | 30.0% | 66.9% | 57.4% |
| 2023 | +150.9% | +119.0% | −31.9 pp | 97.2% | 20.1% | 14.1% |
| 2024 ⚠ | +117.1% | +65.1% | −52.0 pp | 79.5% | 26.2% | 24.2% |
| 2025 | −8.0% | −14.5% | −6.5 pp | 73.3% | 32.2% | 22.5% |
| 2026 to Sep 26 | −5.2% | **+9.7%** | **+14.9 pp** | 90.1% | 39.6% | **22.0%** |

⚠ 2024 has a 77-day hole (Jun 4 – Aug 19). The trend rule's SMA restarts after gaps, so it sits out
~50 days after the hole while holding does not.

Evidence: [`by-year-bitso-costs.txt`](evidence-2026-09-27/by-year-bitso-costs.txt),
[`by-year-frictionless.txt`](evidence-2026-09-27/by-year-frictionless.txt).

Trend beat holding in only **4 of 9 years**, but it had a **smaller drawdown in all 9**. That is the
textbook shape of trend following: it lags in bull years and cuts losses in bear years (2018, 2022).
Over a multi-year span the avoided losses compound, which is where the +430 pp comes from. Judged
one year at a time, the rule mostly looks like a loser. Judged over a cycle, it wins.

### 1.2 The previous study's 2025 trend result was a cold-start artefact

The 2026-09-26 study reported `trend_sma50` at **−3.96%** in 2025, beating holding by 5 pp. With
2024 history available, the rule is already warm on 2025-01-01. The same year then gives **−14.51%**,
**losing** to holding by 6.5 pp.

| 2025, Bitso costs | Cold start (2026-09-26 study) | Warm start (this study) |
|---|---:|---:|
| `buy_and_hold` | −8.94% | −7.97% |
| `trend_sma50` | −3.96% / 10 trips / 84.5% | **−14.51% / 13 trips / 73.3%** |

This was verified directly: the new file, truncated at 2025-01-01, reproduces the old numbers exactly.
377 of the 380 overlapping bars are identical; the other 3 are partial-day bars that were re-fetched.
So the change comes entirely from warm-up. With a cold start, the rule could not trade until ~Feb 20
and so missed a losing stretch of Jan–Feb 2025. A live trader would have had the 2024 history, so
**the warm-start number is the right one**. The "hint" in the previous study did not exist. Buy-and-
hold also moves slightly, because the window now enters at 2025-01-01's open rather than a day later.

### 1.3 Out-of-sample 2026: now long enough to count, and it agrees

Jan 1 – Sep 26 2026 (255 bars, one 13-day hole in June) was never used to choose anything:

| 2026, Bitso costs | Return | Round trips | Max DD | Beats random |
|---|---:|---:|---:|---:|
| `buy_and_hold` | −5.23% | 1 | 39.6% | — |
| `trend_sma50` | **+9.68%** | 8 | 22.0% | 90.1% |
| `news_sentiment` | −31.49% | 13 | 37.7% | 30.8% |
| `trend_and_news` | −5.55% | 6 | 15.7% | 67.3% |

The trend rule is positive after costs in a down year, with half of holding's drawdown. At 90% it is
close to the 95% bar but does not clear it on its own.

### 1.4 The news rule: still negative, less clear-cut out of sample

- **In the news era** (2024-11-20 → 2026-09-26, the whole span with news): −71.3% after costs,
  beating only **1.6%** of random same-trade-count strategies
  ([evidence](evidence-2026-09-27/news-era-bitso-costs.txt)).
- **2026 alone, out of sample:** −31.5%, but that beats 30.8% of random strategies. It lost money,
  but not by more than chance would explain.

The out-of-sample year weakens the "anti-informative" reading from 2025 to "no demonstrated value".
A contrarian rule is still not supported: flipping a signal that is indistinguishable from random
gives another random signal, and it would pay the same costs.

---

## 2. Data refresh

The user pushed new partitions (news through 2026-09-27, prices 2026-09-20 → 09-27). The compacted
files were rebuilt and re-published:

| Dataset | Source objects | Rows read | Unique | vs file previously in S3 | Span |
|---|---:|---:|---:|---|---|
| Prices | 101,766 | 121,461 | **101,388** (60 books) | +154 rows, 0 removed, 21 bars updated | 2018-01-01 → 2026-09-27 |
| News | 607 | 52,549 | **18,166** | +68 articles, 0 removed | 2024-11-20 → 2026-09-27 |

- For BTC, the only updated bar is 2026-09-20. The earlier copy had been fetched before that day
  closed, and yahoo-compact prefers the later fetch.
- The previous S3 objects were backed up locally before being overwritten.
- Full report: [`yahoo-compact-report.txt`](evidence-2026-09-27/yahoo-compact-report.txt).
- Uploaded to `s3://test-financial-stocks-bucket/stocks/compacted/crypto/yahoo_crypto_daily.parquet`
  and `s3://test-financial-news-bucket/news/compacted/crypto/agentic=true/news_crypto_agentic.parquet`.

**BTC-USD gaps still present:** 2024-06-04 → 08-19 (77 d), 2026-06-15 → 06-27 (13 d), and a
scattering of 1–4 day holes in 2023–24. There are 120 missing days in 3,072.

### 2.1 Automated refresh

New script: [`scripts/yahoo-refresh.sh`](../../scripts/yahoo-refresh.sh). It runs sync → `yahoo-compact`
→ gate → upload. By default it is a **dry run** and never writes to S3. With `--upload` it publishes
only if:
- no source file failed to parse, and
- no dataset's unique row count fell below the last upload it recorded. A drop means the sync or the
  dedup broke, and it would otherwise overwrite good data with bad.

Both paths were exercised: a clean dry run, and a fake "last upload" with more rows, which the gate
refused. The first price sync is ~1 GB and ~15 min; later runs only fetch new partitions. To run it
daily, use `/schedule` (e.g. "every day at 06:00 UTC run `./scripts/yahoo-refresh.sh --upload`").

---

## 3. Caveats

- **The trend result's strength comes from compounding bear-market avoidance.** Most of the edge is
  2018 and 2022, when holding lost 65–73%. If the next cycle has no deep bear market, holding will
  probably win.
- **SMA50 is not truly unseen.** It was fixed before this data was opened, but it is also the most
  famous trend parameter, and trend following on BTC 2018–2022 is well documented. The honest
  evidence that is actually new is 2026 (+14.9 pp, 90%) and the drawdown pattern across all 9 years.
- **The random baseline's 99.9% over the full span** partly reflects that a rule in the market during
  uptrends beats random timing in any strongly trending asset. The per-year figures (30–99%) are the
  more conservative read.
- **BTC-USD is not `btc_mxn`.** The MXN leg adds FX moves, and Bitso fills and spreads would differ.
  Costs are modelled as Bitso's production taker rates.
- **Costs are brutal at this fee tier.** 77 round trips cost 7× the starting capital (compounded).
  Any lower fee tier or maker execution changes the result a lot. At zero cost the rule makes +3,958%.
- **Data holes** (the 2024 77-day and 2026 13-day holes) affect trend and hold differently; see the
  ⚠ note in §1.1.
- **Daily bars, long-only, all-in / all-out.** No intraday stops, no position sizing.

---

## 4. What this means

1. **A slow trend filter is the only candidate so far that survives Bitso costs over a full cycle.**
   Its value is **drawdown control**: smaller losses in 9/9 years, at the price of lagging bull years.
   This is the first result in the backtest-readiness series worth a forward test.
2. **The live strategies are not this rule.** The live bot trades 1-minute bars. This result supports
   R3 (a longer horizon) far more strongly than 2026-09-26 did, but using it means a new daily strategy,
   not tuning an existing one.
3. **Before any capital:**
   - Re-run on **`btc_mxn` daily bars**, from Bitso OHLC or by converting BTC-USD with USD/MXN.
   - Keep 2026-09-27 onward as a **pre-registered forward test**: SMA50, next-open fills, no
     parameter changes.
   - Price the rule at **maker** fees (60 bps). That is realistic for a rule that trades ~9 times a
     year and can rest limit orders.
4. **News:** no demonstrated value as a daily long signal. Deprioritise it unless a new hypothesis,
   such as a risk filter or a longer horizon, is written down *before* it is tested.
5. **`momentum`** remains deferred, as agreed.

---

## 5. Reproducing

```bash
# Refresh + compact (dry run; add --upload to publish)
./scripts/yahoo-refresh.sh

# Or use the published files directly
aws s3 cp s3://test-financial-stocks-bucket/stocks/compacted/crypto/yahoo_crypto_daily.parquet ./yahoo/
aws s3 cp s3://test-financial-news-bucket/news/compacted/crypto/agentic=true/news_crypto_agentic.parquet ./yahoo/

cd services/strategy-executor
P="-prices ../../yahoo/yahoo_crypto_daily.parquet -news ../../yahoo/news_crypto_agentic.parquet"
go run ./cmd/daily-research $P -windows 2018-01-01:2026-09-26                        # full span
go run ./cmd/daily-research $P -windows 2025-01-01:2025-12-31,2026-01-01:2026-09-26,\
2018-01-01:2018-12-31,2019-01-01:2019-12-31,2020-01-01:2020-12-31,2021-01-01:2021-12-31,\
2022-01-01:2022-12-31,2023-01-01:2023-12-31,2024-01-01:2024-12-31                    # by year
go run ./cmd/daily-research $P -windows 2024-11-20:2026-09-26                        # news era
# add  -buy-bps 0 -sell-bps 0 -slippage-bps 0  for the frictionless reference
```

The out-of-sample window ends 2026-09-26 so that it doesn't close out at 2026-09-27's partial
(intraday) bar.
