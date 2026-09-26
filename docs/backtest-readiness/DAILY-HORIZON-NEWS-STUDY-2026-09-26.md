# 24-Hour Horizon Study — Yahoo BTC-USD Daily Bars and LLM-Scored News

Date: **2026-09-26**  
Repository: `microservices-trading-bot`  
Branch: `feat/k8s-deployment-manifests`  
Audience: Whoever decides what work happens next on the strategies  
Follows: [`FEES-ENGINE-SPEEDUP-MOMENTUM-2026-09-25.md`](FEES-ENGINE-SPEEDUP-MOMENTUM-2026-09-25.md)

The 2026-09-23 analysis showed that at one-minute bars the cost is ~50× the median move, and that a
longer horizon is the main strategic lever (R3). The trade archive is too short to test that. This study
uses **a year of Yahoo Finance BTC-USD daily bars and LLM-scored crypto news** to ask:

> At a 24-hour horizon, **after Bitso's real costs**, does a simple rule beat simply holding?

Scope was agreed beforehand: offline research only, no changes to the live bot, 2025 as the primary
sample, and Aug–Sep 2026 as a separate out-of-sample check.

---

## 1. Answer

**No rule clearly beat holding, and the news signal was sharply negative.**

| 2025, Bitso costs (176 bps round trip) | Return | Round trips | Time in market | Max drawdown | Costs paid | vs buy & hold | Beats random, same trade count |
|---|---:|---:|---:|---:|---:|---:|---:|
| `buy_and_hold` | **−8.94%** | 1 | 99.7% | 32.2% | 1.7% | — | — |
| `trend_sma50` (long while close > SMA50) | **−3.96%** | 10 | 40.4% | 22.5% | **18.3%** | **+4.98 pp** | 84.5% |
| `news_sentiment` (long while 3-day score > 0) | **−49.36%** | 25 | 66.8% | 53.9% | **31.7%** | **−40.42 pp** | 14.9% |
| `trend_and_news` (both) | −22.65% | 19 | 28.0% | 28.8% | 30.9% | −13.71 pp | 75.8% |

Evidence: [`evidence-2026-09-26/daily-research-bitso-costs.txt`](evidence-2026-09-26/daily-research-bitso-costs.txt).

### 1.1 The trend rule: a hint, not a finding

The trend rule lost less than holding, with a shallower drawdown and 40% of the time in the market.
But it beat **only 84.5%** of 2,000 random strategies making the same 10 round trips. A real edge
should clear 95%. A result this good happens to 1 random strategy in 6.

**Costs are the story again:**

| `trend_sma50`, 2025 | Return |
|---|---:|
| Frictionless ([evidence](evidence-2026-09-26/daily-research-frictionless.txt)) | **+14.61%** |
| After Bitso costs | **−3.96%** |

Ten round trips at 176 bps cost **18.3 percentage points**, enough to turn a rule that beat holding by
22 pp into one that loses money. Moving from 1-minute to daily bars improved the cost-to-move ratio
enormously. It still isn't enough for a rule that trades about monthly on a retail fee schedule.

### 1.2 The news rule: consistently wrong

Going long after net-positive BTC news lost **49%**, and **21% even with zero costs**. It did worse
than 85% of random strategies with the same trade count. In other words, the news was more often
wrong than right about the next few days' direction.

Several explanations fit and can't be told apart here:
- The news is **lagging**: it reports moves that have already happened, so positive coverage clusters
  near local tops.
- It has a **bullish bias**: 58% of scored items are `bullish`, so "positive" is the normal state and
  carries little information.
- The **scores are noisy** at a 1–3 day horizon, even if they carry information over longer periods.

> [!WARNING]
> It is tempting to flip the rule and trade *against* the news. **Do not act on that from this data.**
> Choosing a rule because it looked good on 2025 and then "validating" it on 2025 is exactly the
> overfitting this study was set up to avoid. A contrarian news rule is a hypothesis for data the rule
> has never seen.

### 1.3 Out-of-sample: too short to count

| Aug 30 – Sep 20 2026 (16 bars, 6-day hole) | Return | Round trips |
|---|---:|---:|
| `buy_and_hold` | +1.21% | 1 |
| `trend_sma50` | 0.00% | 0 |
| `news_sentiment` | −6.70% | 3 |
| `trend_and_news` | 0.00% | 0 |

The trend rule needs 50 contiguous days before it can compute its average, so it can't warm up in 16 bars.
The window is reported because it was agreed, but **it can't confirm or refute anything**. The news rule
losing again is consistent with §1.2, but three trades is not evidence.

---

## 2. Method

New CLI: [`cmd/daily-research`](../../services/strategy-executor/cmd/daily-research/main.go).

| Choice | Why |
|---|---|
| Decide at day *t*'s close, **fill at day *t+1*'s open** | Never trade at the close that generated the signal, the most common way daily backtests flatter themselves |
| News counts only if its UTC timestamp is on or before day *t* | No look-ahead. Checked: every item's timestamp matches its partition date. |
| Long-only, all-in / all-out | Bitso spot has no shorting |
| **78 bps taker per leg + 10 bps slippage** | Confirmed production `btc_mxn` rates (2026-09-25 report) |
| Open positions closed at the final close, with costs | No silently dropped exposure |
| SMA 50, 3-day news window, threshold 0: **fixed before running, not tuned** | With ~365 bars, any tuning fits noise |
| 2,000 random strategies with the **same number of round trips** per rule | Separates "beat buy-and-hold" from "got lucky"; controls for trading frequency |
| Trend SMA restarts after gaps longer than 7 days | Stops the average from blending Dec-2025 and Aug-2026 prices across the 8-month data hole |

**Data checks performed:**
- **Price file columns:** the precomputed `sma_20` uses closes up to and including the same day, with no
  future data (304/304 rows verified). The CLI still recomputes its own indicators from raw closes
  rather than trusting the file's columns.
- **News de-duplication:** the daily partitions hold 15,331 rows but only 12,222 unique ids. They are
  de-duplicated by id.
- **News filtering:** only `llm_ticker` ∈ {`BTC`, `BTC-USD`} is used (3,181 of the unique items). Rows
  whose scores are `None`/`nan` are dropped.
- **News score:** `llm_overall_sentiment × llm_confidence`, averaged per UTC day.

**Tests:** [`main_test.go`](../../services/strategy-executor/cmd/daily-research/main_test.go) covers
next-open fills and both-leg costs, the final close-out, a window seeded by the prior day's decision,
the SMA restarting after gaps, random strategies matching the trade count exactly, and news
de-duplication and filtering.

---

## 3. Caveats

- **One year, one regime.** 2025 BTC fell ~9% with a 32% drawdown. A trend rule that sits out declines
  is flattered by that; in a steady bull year, holding usually wins.
- **BTC-USD is not `btc_mxn`.** The price series tracks closely, but the MXN leg adds FX moves. Costs
  are modelled as Bitso's. Actual fills on Bitso would differ.
- **Daily bars.** No intraday stops or exits can be modelled.
- **The 2026 hole.** Jan–Jul 2026 is missing from the price data, so there is no usable out-of-sample
  period yet.
- **A single parameter set.** That is deliberate, but it means that "SMA 50 doesn't clear 95%" is not the
  same as "no trend rule works".

---

## 4. What this means

1. **The horizon lever is real but not sufficient on its own.** Daily bars cut the cost-to-move ratio
   dramatically, and a frictionless trend rule beat holding by 22 pp. At 176 bps round trip, even
   **10 trades a year cost 18 pp**. On this fee schedule, a viable rule has to trade *very* rarely or be
   right by a wide margin. This reinforces R4: **the fee tier or venue decides viability more than any
   signal does.**
2. **The LLM news scores, as used here, are not a buy signal.** They may still help as a *risk filter*
   or over a *longer horizon*. Both are new hypotheses, needing data the rules haven't seen.
3. **Backfilling Jan–Jul 2026** would roughly triple the out-of-sample period and is the cheapest way
   to test the trend hint honestly. That period must be tested **without re-tuning** anything first.
4. **`momentum` remains a no-op** (2026-09-25 report, §3). As agreed, the decision is deferred. Nothing
   here argues for making it more active.

## 5. Reproducing

```bash
# local copies of the daily partitions only (the yearly/weekly roll-ups duplicate them)
aws s3 sync s3://test-financial-stocks-bucket/stocks/transformed/crypto/book=btc-usd/ ./yahoo/prices \
  --exclude "*" --include "*/month=*/day=*/*.csv"
aws s3 sync s3://test-financial-news-bucket/news/transformed/crypto/agentic=true/ ./yahoo/news \
  --exclude "*" --include "*/month=*/day=*/*.csv"

cd services/strategy-executor
go run ./cmd/daily-research -prices ../../yahoo/prices -news ../../yahoo/news
go run ./cmd/daily-research -prices ../../yahoo/prices -news ../../yahoo/news \
  -buy-bps 0 -sell-bps 0 -slippage-bps 0        # frictionless reference
```
