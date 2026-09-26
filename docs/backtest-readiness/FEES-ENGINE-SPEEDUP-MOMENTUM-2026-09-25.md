# Fee Confirmation, Engine Speed-up, and the `momentum` Diagnosis

Date: **2026-09-25**  
Repository: `microservices-trading-bot`  
Branch: `feat/k8s-deployment-manifests`  
Audience: Whoever decides what work happens next on the strategies  
Follows: [`HARNESS-FIXES-AND-REMEASURE-2026-09-24.md`](HARNESS-FIXES-AND-REMEASURE-2026-09-24.md)

This round covers four of the next steps proposed on 2026-09-24. It also surveys the Yahoo Finance
price and news data offered for step 6.

| Step | What | Result |
|---|---|---|
| 2 | Confirm **production** Bitso fees | ✅ **maker 60 / taker 78 bps**, identical to stage |
| 3 | Correct the fee gate's default cost | ✅ 130 → **156 bps**, pinned by a test |
| 5 | Explain why `momentum` never trades | ✅ **Its entry rule is self-contradictory.** It was true on 0 of 70,591 ticks. |
| 7 | Remove the engine's O(n²) indicator recompute | ✅ **~6 min → 12.5 s** per run, with **bit-identical** results |
| 6 | Longer history from Yahoo data | 🔎 Surveyed (§5); design decision needed before building |

---

## 1. Production fee rates confirmed (step 2)

The production keys in the legacy `golang_server/*-logger*/.env` files were used with
`backtest-archive -fees-from-bitso -bitso-api-base-url https://bitso.com/api`. The credentials were
read into the environment only and never printed. All six keys belong to the same account and return
the same row for `btc_mxn`:

| Role | Code fixture | Stage (2026-09-24) | **Production (2026-09-25)** |
|---|---:|---:|---:|
| Maker | 50 bps | 60 bps | **60 bps** |
| Taker | 65 bps | 78 bps | **78 bps** |

The stage caveat from yesterday's report no longer applies. **The real taker round trip is 156 bps,
or 176 bps with 10 bps of slippage on each leg.** The 50/65 fixture in `shared/pkg/bitso` is stale test
data. It exercises the fee arithmetic, not the tariff, so it was left alone.

## 2. Fee-gate default corrected (step 3)

[`fee_gate.go`](../../services/strategy-executor/internal/strategies/fee_gate.go): the default is now
`DefaultFallbackRoundTripBPS = 156` (78 × 2), and the comment records where the number came from.
Taker is assumed on both legs because the fallback can't know which role a fill will take, and
underestimating cost is the dangerous direction. Slippage is excluded, matching what the live fee
provider returns.

The fallback only applies when the service has no Bitso credentials. With credentials, the live provider
supplies the real rates. The new `TestDefaultFallbackCoversProductionTaker` fails if the default is ever
set below the production taker round trip.

---

## 3. Why `momentum` never enters (step 5)

`momentum`'s entry rule is:

```
long : RSI(14) < 30  AND  price > EMA(20)
short: RSI(14) > 70  AND  price < EMA(20)
```

A new diagnostic, [`momentum_diagnostic_test.go`](../../services/strategy-executor/internal/backtest/momentum_diagnostic_test.go),
replays the archive through the exact backtest indicator pipeline and counts how often each half holds.
Output: [`evidence-2026-09-25/momentum-entry-diagnostic.txt`](evidence-2026-09-25/momentum-entry-diagnostic.txt).

| | RSI condition alone | EMA condition alone | **Both together** | Closest miss |
|---|---:|---:|---:|---|
| Long | 4,413 ticks (6.25%) | 36,527 (51.74%) | **0** | Price was always ≥ 1.19 bps *below* the EMA while RSI < 30 |
| Short | 4,547 ticks (6.44%) | 34,064 (48.26%) | **0** | Price was always ≥ 2.30 bps *above* the EMA while RSI > 70 |

> [!IMPORTANT]
> **This is a logic defect, not a tuning problem.** RSI below 30 means the recent bars were mostly
> down closes, and that almost always puts the price below a 20-bar EMA. The two conditions each fire
> regularly on their own, but essentially never together. The price was always on the "wrong" side of
> the EMA. No threshold change fixes this; the rule has to be redesigned. There are two coherent
> readings:
>
> - **Trend-confirmed pullback:** buy when price > EMA (uptrend) *and* RSI has dipped to a moderate
>   level such as < 45. That is a real momentum-with-pullback strategy.
> - **Oversold reversal:** buy when RSI < 30 *and* price < EMA. That duplicates what `mean_reversion`
>   already does with Bollinger bands.
>
> Choosing between them is strategy design (R5), so it is **your call**, and it hasn't been
> changed. Until it is, `momentum` in production is a no-op that never places an order.

---

## 4. Engine speed-up (step 7)

### 4.1 The problem

On **every trade**, `RunHistorical` fetched every 1-minute bar since the start of the run and
recomputed SMA, EMA, RSI, Bollinger and ATR from scratch. That is O(n²) over a replay: about 6 minutes for
34 days, and hours for the multi-month windows R3 needs.

### 4.2 The fix

New [`incremental_indicators.go`](../../services/strategy-executor/internal/backtest/incremental_indicators.go):

| Indicator | Depends on | Approach |
|---|---|---|
| SMA, Bollinger | The last `period` closes only | Pass exactly that tail to the **unchanged** library code |
| EMA, RSI (Wilder), ATR (Wilder) | The **whole path** from bar 0, because they're recursive | Fold each bar into running state once it closes, using the same floating-point expressions in the same order as the library. Apply the still-forming bar to a *copy* of the state on each tick. |
| VWAP | The last 100 trades | Unchanged |

`ReplayProvider` gained `barsFrom(i)`, so a tick copies only the bars not yet folded, not the whole
history.

### 4.3 Proof that nothing changed

Speed is worthless if it shifts results, because every comparison with earlier runs would silently
break. So the bar is **bit-for-bit equality**, not "close enough":

| Check | Result |
|---|---|
| [`TestIncrementalIndicatorsMatchFullRecompute`](../../services/strategy-executor/internal/backtest/incremental_indicators_test.go): the old and new computers run side by side over 3 × 3,000 bursty synthetic trades (idle gaps, repeated prices), comparing `math.Float64bits` of every indicator at **every tick** | ✅ Identical |
| Mutation check: rewrite the EMA step as the algebraically equal `ema + α(p − ema)` | ❌ Test fails at tick 57 on a **2-ulp** difference (`…5663` vs `…5661`). The test really does detect rounding-level drift. |
| Real archive: scenario (d) from 2026-09-24 re-run on the new engine | ✅ **Every number identical** (the diff is a trailing blank line). [Evidence](evidence-2026-09-25/backtest-d-incremental-engine.txt). |

| | Before | **After** |
|---|---:|---:|
| 5-strategy run, 34 days, 70,708 trades | ~6–7 min | **12.5 s** (~30×) |
| Peak memory | — | 235 MB |

The old full-recompute `indicatorComputer` is kept in `engine.go` as the reference implementation the
test compares against. `RunHistorical` no longer uses it.

---

## 5. The Yahoo Finance data: what's there and how it could plug in (step 6)

### 5.1 Inventory

| Dataset | Location | Coverage | Content |
|---|---|---|---|
| **BTC-USD daily bars** | `s3://test-financial-stocks-bucket/stocks/transformed/crypto/book=btc-usd/` | **All of 2025** (365 days, plus a yearly roll-up). **2026: only mid-Aug → mid-Sep** (Jan–Jul 2026 is missing). | OHLC, adj. close, volume, plus precomputed returns, volatility (20d/60d/Parkinson/Garman-Klass), SMA 20/50/200, EMA 12/26, RSI 14, MACD and Bollinger |
| **News, LLM-annotated** | `s3://test-financial-news-bucket/news/transformed/crypto/agentic=true/` | 2024 roll-up, 2025 daily Jan–Nov and weekly, 2026 Jan–Sep (patchy: no April) | Headline, source, timestamp, and **scored fields**: overall and forward sentiment, surprise, risk, uncertainty, impact strength, immediacy and horizon, confidence, plus a bullish/bearish/neutral `llm_signal` and an `llm_actionable` flag |
| News, raw | `s3://test-financial-news-bucket/news/crypto/` | 2024-05 → 2026-09-22, 43k objects | Headline, content and timestamps only, with no scores |

### 5.2 What it can and cannot answer

- ✅ **It unblocks the 24h horizon of R3 now.** Earlier analysis showed that at 24h, **37% of windows
  can pay the round trip**, against 0.01% at 1 minute. About 400 daily bars is thin, but it's a real
  sample instead of zero.
- ✅ **News is a genuinely new signal.** It's scored and timestamped, and it's independent of the
  price-derived indicators every current strategy uses. It can be joined by date to the daily bars.
- ⚠️ **It's BTC-USD on global venues, not `btc_mxn` on Bitso.** Direction and trend match closely.
  But the price level includes USD/MXN moves, and the *costs* must still be Bitso's (§1). A result on
  BTC-USD is evidence about the *signal*, not about fills on Bitso.
- ⚠️ **Daily bars only.** Intraday entries, exits and stops can't be simulated. Fills would be modelled
  at the next day's open.
- ⚠️ **The gap from Jan to Jul 2026** breaks any continuous series from 2025 into 2026. That window would need
  backfilling from Yahoo before 2026 counts as one sample.
- ⚠️ **Look-ahead risk in the precomputed columns.** Using them safely requires confirming each one is
  computed only from data up to that row, and that news `datetime` values are publication times, not
  scrape times.

---

## 6. State of play

| | Status |
|---|---|
| Fees | Confirmed at 60/78 bps in production. The fee gate defaults to the real cost. |
| Harness | Honest (2026-09-24) and now fast enough for multi-month windows |
| `momentum` | **Broken by design.** It never trades. Needs a decision (§3). |
| Longer history | Available for BTC-USD daily plus news; integration design needs a decision (§5) |

## 7. Reproducing

```bash
cd services/strategy-executor

# §3 momentum diagnostic (needs a local archive sync)
MOMENTUM_DIAG_ARCHIVE=./archive go test ./internal/backtest \
  -run TestMomentumEntryConditionDiagnostic -v

# §4 equivalence test (runs in normal CI)
go test ./internal/backtest -run TestIncrementalIndicatorsMatchFullRecompute -v
```
