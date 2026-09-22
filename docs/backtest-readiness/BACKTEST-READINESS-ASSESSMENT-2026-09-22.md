# Backtest Readiness Assessment — Archive Loader, Fee Gating, Regime Diversity, and First Honest Backtest

Date: **2026-09-22**  
Repository: `microservices-trading-bot`  
Branch: `feat/k8s-deployment-manifests`  
Audience: Operators and developers deciding whether the archived `btc_mxn` trade data can support an economic go/no-go decision

This document reports a five-part investigation run entirely **offline** against already-archived
data. No EKS cluster, Kafka, Redis, or live market-data dependency was used or required. It covers:

1. A loader that feeds the S3 parquet trade archive into the existing backtest engine.
2. A fee-gating review of the strategies — does every entry clear round-trip cost, and does every
   exit refuse to close at a net loss?
3. A regime-diversity analysis — does the archive actually contain enough regime variety to trust a
   backtest?
4. A WebSocket outage (`ws_gaps`) audit — did data loss distort the answer to (3)?
5. Backtest runs per regime against a random-signal baseline.

**Framing (do not conflate):** as in
[`../strategy-fee-accuracy/STAGE-FINANCIAL-APPROACH-2026-06-04.md`](../strategy-fee-accuracy/STAGE-FINANCIAL-APPROACH-2026-06-04.md) §1,
the program has **engineering proof** but not **economic proof**. This document adds tooling toward
economic proof and then reports, honestly, that **the data cannot yet deliver it.**

---

## 1. Verdict summary

| # | Question | Verdict |
|---|----------|---------|
| 1 | Can we load the S3 archive into the backtest engine? | ✅ **Yes.** 26,600 objects → 70,708 trades, 0 failed, 0 dupes, 0 invalid. |
| 2 | Are entries fee-gated and exits loss-protected? | 🟡 **Partially.** `mean_reversion` now gated; `limit_profit` exit already correct; **`momentum` and `limit_profit` entries are NOT gated.** |
| 3 | Does the archive have enough regime diversity? | ❌ **No.** `high_vol` is **14 of 98,874 samples (0.014%)**, all from one ~7-minute window. |
| 4 | Did WebSocket gaps distort the data? | ✅ **No.** All gaps are short and do not overlap the days of interest. |
| 5 | Do the strategies beat a random baseline after real fees? | ❌ **No strategy is profitable.** All lose money at Bitso retail taker fees. |

> [!CAUTION]
> **Headline: no `high_vol` conclusion is trustworthy at any sample size drawn from this window.**
> The archive spans 34 days of an unusually quiet market. Daily mean ATR% ranged 0.031–0.099 against
> a `high_vol` threshold of **1.5%** — never remotely close. Any claim about how a strategy behaves in
> high volatility is unsupported by this dataset.

---

## 2. Step 1 — S3 archive loader

### 2.1 What was built

A new package `internal/backtest/loader` reads the data-collector's parquet archive and produces the
in-memory trade slice consumed by `backtest.NewBacktestDataProvider(trades)`. The backtest engine has
no Redis/Kafka/Postgres dependency, so this is a pure, standalone computation.

| File | Lines | Purpose |
|---|---:|---|
| [`loader.go`](../../services/strategy-executor/internal/backtest/loader/loader.go) | 226 | `ArchiveTrade`, `Normalize()`, `LoadProvider()`, `LoadTradesOnly()`, `Stats` |
| [`parquet.go`](../../services/strategy-executor/internal/backtest/loader/parquet.go) | 101 | `DecodeParquetTrades()` — mirrors the collector's writer schema exactly |
| [`s3.go`](../../services/strategy-executor/internal/backtest/loader/s3.go) | 259 | `ObjectStore` interface, `S3Archive`, `DayPrefixes()`, `AWSObjectStore` |
| [`local.go`](../../services/strategy-executor/internal/backtest/loader/local.go) | 70 | `LocalObjectStore` for `aws s3 sync` copies |
| [`postgres.go`](../../services/strategy-executor/internal/backtest/loader/postgres.go) | 107 | `PostgresSource` + `ErrOutsideRetention` |
| [`pgx_scanner.go`](../../services/strategy-executor/internal/backtest/loader/pgx_scanner.go) | 109 | `QueryTrades`, `QueryWSGaps` |
| [`bars.go`](../../services/strategy-executor/internal/backtest/loader/bars.go) | 77 | `AggregateBars()`, `BarsUpTo()` |
| [`loader_test.go`](../../services/strategy-executor/internal/backtest/loader/loader_test.go) | 533 | Fixture-based tests writing **real** parquet with the collector's writer config |

### 2.2 Correctness details that matter

These are the non-obvious decisions; each one is a bug if got wrong.

- **Maker → taker side inversion.** The archive records `maker_side`. A trade whose *maker* was the
  seller is a *buy* from the taker's perspective. `TakerSide()` performs this inversion; using
  `maker_side` directly would invert every single trade's direction.
- **`exchange_ts`, not `received_at`.** `ToIndicatorTrade()` uses the exchange timestamp. Using our
  own receive time would bake collector latency and WebSocket reconnect jitter into the bar
  boundaries.
- **Midnight-straddle padding.** The collector partitions a batch by the timestamp of the batch's
  **first** trade, so a batch flushed at 00:00:30 lands in the *previous* day's partition. `DayPrefixes()`
  therefore pads the listing window by ±1 day and relies on the range filter to trim. Without this,
  every day boundary silently loses trades.
- **Deduplication and stable ordering.** `Normalize()` dedupes on `tid`, filters to the requested
  range, validates, and performs a stable sort by `(exchange_ts, tid)`. Parquet object listing order
  is not chronological.
- **Retention guard, not silent truncation.** `PostgresSource` returns `ErrOutsideRetention` rather
  than quietly returning a short window. Postgres holds only **7 days**; S3 is the only full-window
  source.
- **`AggregateBars` deliberately does not synthesize empty buckets.** A minute with no trades is
  absent, matching live indicator behaviour rather than inventing flat bars.

### 2.3 Load result over the full window

```
LOAD STATS : book=btc_mxn window=[2026-08-19T00:00:00Z..2026-09-22T23:59:59Z]
             objects=26600/26600 (failed=0) rows=70708 dup=0 out_of_range=0 invalid=0
             returned=70708 span=[2026-08-19T15:40:05Z..2026-09-22T18:24:50Z]
```

> [!NOTE]
> **Compaction has not landed.** 26,600 objects for 70,708 trades is ~2.4 KB and ~2.7 trades per
> object. The loader issues concurrent GETs (default 32) to make this tolerable, but the archive
> layout is the reason a full load is slow.

---

## 3. Step 2 — Fee-gating review

### 3.1 The rule being enforced

Two invariants:

- **Entry:** the expected move must exceed the **round-trip** cost (both legs plus slippage), not just
  one leg.
- **Exit:** a strategy must not voluntarily close at a net loss. The only permitted exception is an
  explicit stop-loss / risk override, which must be evaluated **before** the net-loss guard.

### 3.2 What was built

[`internal/strategies/fee_gate.go`](../../services/strategy-executor/internal/strategies/fee_gate.go) (167 lines)
is a shared, direction-aware helper:

| Symbol | Behaviour |
|---|---|
| `DirectionLong` / `DirectionShort` | Direction is explicit — short entries are scored with short arithmetic |
| `HasRates()` | False when unconfigured, which makes the whole gate a **no-op** |
| `RoundTripRates()` | Both legs, not one |
| `NetPerBase()` | Net P&L per base unit after round-trip cost |
| `EntryClearsCost()` | Fails **open** when rates are unknown (documented) |
| `EntryClearsCostStrict()` | Fails **closed** — for callers that prefer to block |
| `ExitIsNetProfitable()` | Exit-side net-loss check |
| `MinProfitableExit()` | Minimum exit price that clears round-trip cost |

> [!WARNING]
> **Direction-awareness is the critical detail.** An earlier draft of this helper scored SHORT entries
> using LONG fee arithmetic. That bug makes every short look unprofitable and would have **silently
> disabled the entire short side** of every strategy that used it. It was caught and rewritten.

### 3.3 Integration status per strategy

| Strategy | Entry gated? | Exit loss-protected? | Notes |
|---|---|---|---|
| `mean_reversion` | ✅ **Yes** (both directions) | ✅ **Yes** + stop-loss override ordered first | New config: `MinNetProfitBPS`, `FallbackRoundTripBPS`, `StopLossBPS` — all default `0` |
| `limit_profit` | ❌ **No** | ✅ Already correct — `exitPriceThreshold()` uses `bitso.MinExitPriceAfterRoundTrip` | Exit logic deliberately left untouched |
| `momentum` | ❌ **No** | ❌ Not reviewed in depth | **Outstanding work** |

`basic`, `trend`, and `arbitrage` are **legacy** — they implement the old `Strategy` interface and are
not in the production registry ([`enhanced_registry.go`](../../services/strategy-executor/internal/strategies/enhanced_registry.go) registers
only the three above). They were deliberately left untouched.

> [!IMPORTANT]
> **Fee gating is incomplete and this report does not pretend otherwise.** `momentum` and
> `limit_profit` **entries** are still ungated. Rushing untested changes into live trading logic was
> judged worse than shipping a partial, tested change and naming the gap. See §10, item N2.

### 3.4 Backward compatibility

The gate is a **no-op when unconfigured**. All three new `mean_reversion` config fields default to `0`,
and `entryClearsCost()` returns `true` immediately when `!HasRates()`. Existing deployments and tests
are unaffected. Covered by
[`fee_gate_test.go`](../../services/strategy-executor/internal/strategies/fee_gate_test.go) (290 lines) and
[`mean_reversion_fee_gate_test.go`](../../services/strategy-executor/internal/strategies/mean_reversion_fee_gate_test.go) (232 lines) — all passing.

---

## 4. Step 3 — Regime diversity (the headline finding)

### 4.1 Method

A new command
[`cmd/regime-snapshots`](../../services/strategy-executor/cmd/regime-snapshots/main.go) replays the
archive through the **live indicator pipeline** and emits JSONL whose field names match
`classifier.Snapshot`. Those snapshots are then classified by the **existing** standalone
[`cmd/classifier-backtest`](../../services/strategy-router/cmd/classifier-backtest/main.go) in
`-stdin` mode. `classifier.Classify(Snapshot, Thresholds) Decision` is a pure function, so this is an
exact offline reproduction of the live routing decision — not a reimplementation.

Production defaults were used: SMA/EMA/Bollinger 20, RSI 14, ATR 14, BB stddev 2.0,
`INDICATORS_BAR_INTERVAL=1m`, `ROUTER_INTERVAL_SEC=30`.

> [!NOTE]
> `cmd/regime-snapshots/main_test.go` contains `TestSnapshotLineFieldNames`, a tripwire that fails if
> the `classifier.Snapshot` field names drift. The two commands live in **different Go modules**, so a
> rename in the router would otherwise silently produce all-zero snapshots here.

### 4.2 Result

Full archive, 2026-08-19 .. 2026-09-22 — see [`evidence-2026-09-22/regime-distribution.txt`](evidence-2026-09-22/regime-distribution.txt):

| Granularity | Samples | high_vol | low_vol_range | neutral | trend_down | trend_up |
|---|---:|---:|---:|---:|---:|---:|
| **1m bars / 30s cadence** (live-faithful) | 98,874 | **14 (0.014%)** | 71.1% | 15.0% | 6.9% | 7.1% |
| 15m bars / 15m cadence | 3,196 | **0** | 51.1% | 15.6% | 16.1% | 17.2% |
| 1h bars / 1h cadence (macro) | 786 | **3 (0.4%)** | 7.5% | 26.3% | 36.0% | 29.8% |

### 4.3 Why this kills the `high_vol` question

- **All 14 `high_vol` samples come from a single ~7-minute window on 2026-08-26.** That is one event,
  not a sample of a regime. 14 correlated observations from one window carry roughly the statistical
  weight of one.
- **Daily mean ATR% ranged 0.031–0.099** across all 34 days, against a `high_vol` threshold of
  **1.5%** — one to two orders of magnitude away. See
  [`evidence-2026-09-22/daily-coverage-and-atr.json`](evidence-2026-09-22/daily-coverage-and-atr.json).
- The 15m view contains **zero** `high_vol` samples at all.

### 4.4 The volume spike is not a volatility spike

It is tempting to treat 2026-08-21 as the "exciting" day — it has 4,594 trades, **2.3× the 2,020/day
mean**. It is not.

| Day | Trades | ATR% mean | ATR% max | `high_vol` samples | Regime mix |
|---|---:|---:|---:|---:|---|
| 2026-08-21 (volume spike) | 4,594 | 0.099 | 0.258 | **0** | 68.1% low_vol_range — indistinguishable from any other day |
| 2026-09-21 (volume spike) | 4,243 | — | — | **0** | same story |
| 2026-08-26 (only high_vol day) | — | — | **2.254** | 14 (0.5% of that day) | 68.3% low_vol_range |

**Volume ≠ volatility.** Aug 21 had lots of trades at stable prices. Anyone reaching for "the busy day"
as a volatility proxy would reach a wrong conclusion.

---

## 5. Step 4 — WebSocket gap (`ws_gaps`) audit

### 5.1 Method

The RDS instance (`db=trades`, `user=collector`) is **not publicly accessible**, so the audit was run
on the collector EC2 instance (`i-00c71eec12a1c52d7`) via **SSM Run Command**, with the DSN read
inside the instance from Secrets Manager (`mtb-development-data-collector-rds/postgres`). The secret
value was never printed.

### 5.2 Result

Raw output: [`evidence-2026-09-22/ws-gaps-audit.txt`](evidence-2026-09-22/ws-gaps-audit.txt).

| Metric | Value |
|---|---|
| Total gaps recorded | **103** |
| Shortest gap | 4.4 s (4,367 ms) |
| Longest gap | **12.4 s** (12,370 ms) |
| Mean gap | 7.1 s (7,097 ms) |
| **Total outage across the 34-day window** | **731.0 s ≈ 0.025% of elapsed time** |
| Gaps ≥ 15 s | **0** |

Duration distribution:

| Bucket | Gaps |
|---|---:|
| < 5 s | 10 |
| 5–10 s | 86 |
| 10–15 s | 7 |
| ≥ 15 s | **0** |

On the three days singled out in §4:

| Day | Why it matters | Gaps | Worst gap |
|---|---|---:|---:|
| 2026-08-21 | volume spike | 4 | 6.6 s |
| 2026-08-26 | only day with any `high_vol` | 3 | 9.8 s |
| 2026-09-21 | volume spike | 3 | 10.6 s |

> [!NOTE]
> `ws_gaps` is **not** subject to the 7-day retention that applies to the `trades` table — rows are
> present for the full 2026-08-20 .. 2026-09-22 span, so this audit covers the whole archive window.
>
> The count is **103**, not the 100 seen in an earlier pass during this same session: the collector is
> still running and logged three more gaps while the analysis was in progress.

### 5.3 Conclusion

**Gaps are immaterial to the regime conclusion.** Total data loss is **0.025%** of the window, the
worst single outage (12.4 s) is well under one 1m bar, nothing reaches 15 s, and the days that drive
the analysis are among the *cleanest*. The quiet market in §4 is a real property of the period, not an
artefact of missing data.

> [!NOTE]
> This also means the archive is **not** missing a hidden volatile window. We did not fail to capture
> high volatility; it did not occur.

---

## 6. Step 5 — Backtest runs and the random-signal baseline

### 6.1 The null hypothesis

[`internal/backtest/random_baseline.go`](../../services/strategy-executor/internal/backtest/random_baseline.go)
adds a `random_baseline` strategy: a coin flip with a fixed seed (`20260919`), registered through
`init()` into `strategyFactories`. Its purpose is to answer *"is this strategy better than noise?"* —
and, just as importantly, to measure **what the fee drag alone costs**.

A new CLI [`cmd/backtest-archive`](../../services/strategy-executor/cmd/backtest-archive/main.go)
runs the strategies over the archive with per-regime attribution, and labels any regime bucket with
fewer than `-min-samples` (default 30) closed trades as **"DIRECTIONAL ONLY"**.

### 6.2 Results — realistic fees (65 bps/leg taker + 10 bps slippage)

Two runs are reported. Both use the *fixed* harness (see §7). Full output in
[`evidence-2026-09-22/`](evidence-2026-09-22/).

**Run A — production signal throttle** ([`backtest-run-fixed-default-throttle.txt`](evidence-2026-09-22/backtest-run-fixed-default-throttle.txt)):

| Strategy | Trades | Win% | Net P&L (MXN) |
|---|---:|---:|---:|
| mean_reversion | 0 | — | 0.00 |
| momentum | 0 | — | 0.00 |
| limit_profit | 2 | 50.0% | +52.66 |
| random_baseline | 95 | 3.2% | −1,209.71 |

This run is **not usable for expectancy** — the trade counts are an artefact of harness defect #1
(§7.1), not of the strategies' logic.

**Run B — signal throttle disabled** (`-params '{"min_signal_interval":0}'`,
[`backtest-run-fixed-no-throttle.txt`](evidence-2026-09-22/backtest-run-fixed-no-throttle.txt)):

| Strategy | Trades | Wins | Win% | Net P&L (MXN) | Profit factor | Max DD |
|---|---:|---:|---:|---:|---:|---:|
| mean_reversion | 700 | 2 | **0.3%** | **−7,291.40** | 0.00 | 13.38% |
| momentum | 0 | 0 | — | 0.00 | 0.00 | 0.00% |
| limit_profit | 38 | 1 | **2.6%** | **−113.45** | 0.33 | 0.50% |
| random_baseline | 95 | 3 | 3.2% | **−1,209.71** | 0.00 | 2.04% |

### 6.3 Per-regime attribution (Run B)

`mean_reversion` is the only strategy with enough trades for any bucket to clear the 30-sample bar:

| Regime | Trades | Win% | Net P&L | Avg/trade | Verdict |
|---|---:|---:|---:|---:|---|
| low_vol_range | 104 | 0.0% | −1,005.91 | −9.67 | supported by adequate samples |
| neutral | 250 | 0.4% | −2,733.57 | −10.93 | supported by adequate samples |
| trending_down | 344 | 0.3% | −3,543.64 | −10.30 | supported by adequate samples |
| trending_up | 2 | 0.0% | −8.28 | −4.14 | DIRECTIONAL ONLY |

> [!IMPORTANT]
> **There is no `high_vol` row.** Not because the strategy avoided it, but because §4 means there was
> essentially no `high_vol` to trade. The bucket is empty by construction.

### 6.4 Honest reading

- **Every strategy loses money.** `mean_reversion` loses **−10.42 MXN/trade** over 700 trades, with a
  0.3% win rate. `limit_profit` loses **−2.99 MXN/trade** over 38 trades. Neither is a marginal call.
- **The random baseline loses −12.73 MXN/trade.** That number is essentially the **round-trip fee +
  slippage drag** on a 0.001 BTC position. It is the cost of showing up.
- **"Better than random" is not a pass.** The tool prints `mean_reversion … +2.32 vs baseline —
  better than random`. All this says is that `mean_reversion` pays slightly less in fees per trade
  than a coin flip. It is still **decisively unprofitable**. Beating the baseline is necessary, not
  sufficient; the bar is *profitable*, not *less bad than noise*.
- **Beware the 0-trade rows.** The comparison table reports `momentum … +12.73 vs baseline — better
  than random` on **zero trades**. That is a formatting artefact of dividing by zero trades, not a
  result. A strategy that never trades is not beating anything.
- **The win rates are the real tell.** 0.3% and 2.6% win rates mean the strategies are taking the
  round-trip fee hit and almost never capturing a move large enough to clear it. In a market whose
  daily mean ATR% is ~0.06%, a **130 bps** round-trip cost is simply larger than the available edge.

> [!CAUTION]
> These results are a **valid negative for the low-volatility regime only.** They say nothing about
> `high_vol`, and they are produced by a harness with known accounting optimism (§8.2) — meaning the
> true results are, if anything, **slightly worse** than shown.

---

## 7. Harness fidelity defects found

Three defects were found in the backtest harness itself. Two were fixed; one is documented and worked
around.

### 7.1 Wall-clock signal throttle — ⚠️ NOT FIXED (worked around)

`BaseEnhancedStrategy.RecordSignal()` sets `s.state.LastSignalTime = time.Now()`
([`enhanced_strategy.go:292`](../../services/strategy-executor/internal/strategies/enhanced_strategy.go#L292)).
All three strategies then throttle on `time.Since(LastSignalTime)` against `MinSignalInterval`
(default 60 s).

**Effect:** a 34-day replay that completes in ~6 minutes of wall-clock time can emit only ~6 signals.
The simulation's clock is irrelevant; the throttle is measuring how fast the *computer* is.

This is why Run A shows 0–2 trades. Fixing it properly requires injecting a clock into the strategies,
which touches live trading code and was judged out of scope. Worked around with
`-params '{"min_signal_interval":0}'` for Run B.

### 7.2 Wall-clock signal timestamps — ✅ FIXED

Strategies set `Signal.Timestamp = time.Now()`. Any regime attribution keyed on that field buckets
every trade into the few minutes during which the backtest *ran*, producing a meaningless breakdown.

**Fix:** added a `TickTime` field to `SignalRecord`
([`runner.go:54-61`](../../services/strategy-executor/internal/backtest/runner.go#L54-L61)) carrying
the **simulated** market time of the replayed trade, populated at
[`runner.go:182`](../../services/strategy-executor/internal/backtest/runner.go#L182). Per-regime
attribution now uses `TickTime`.

### 7.3 Missing SELL fill notification — ✅ FIXED

The runner notified `OrderFillAware` strategies of **BUY** fills but never **SELL** fills.

**Effect:** a strategy sets `PendingSell` when it decides to exit and only clears it on the sell-fill
callback. With no callback, `PendingSell` stayed set forever, so `OnTick` short-circuited after **one**
round trip. This is why earlier runs produced ~1 trade regardless of settings.

**Fix:** the runner now emits the SELL fill notification. `limit_profit` went from 1 → 2 trades under
the production throttle, and from 1 → 38 with the throttle disabled — confirming the fix.

---

## 8. Pre-existing issues found but deliberately NOT fixed

These were verified to exist on clean `HEAD` (via `git stash`) and are **not** caused by this work.
They are reported rather than silently patched.

### 8.1 `internal/processor` tests do not build on clean HEAD

```
internal/processor/data_processor_test.go:63: unknown field MakerOrderID in struct literal of type models.TradeEvent
internal/processor/data_processor_test.go:64: unknown field TakerOrderID ...
internal/processor/data_processor_test.go:67: unknown field CreatedAt ...
internal/processor/data_processor_test.go:69: unknown field Source ...
internal/processor/data_processor_test.go:70: unknown field Metadata ...
internal/processor/filters_test.go:295:  unknown field Source ...
```

The test file has drifted from `models.TradeEvent`. `go vet ./...` and `go test ./...` both fail on
this package **before** any change in this branch.

### 8.2 Exit P&L subtracts only one commission leg — optimistic bias

[`runner.go:215`](../../services/strategy-executor/internal/backtest/runner.go#L215):

```go
pnl := (price - entryPrice) * positionSize - commission
```

Only the **sell**-leg commission is subtracted from per-trade P&L. The buy leg is deducted from
`balance` ([`runner.go:196`](../../services/strategy-executor/internal/backtest/runner.go#L196)) but
never from the trade's own P&L. Per-trade round-trip cost is therefore **understated by one leg**.

> [!WARNING]
> This biases every reported per-trade number **optimistically**. The already-negative results in §6
> are therefore a **best case**. Fixing this is a change to P&L accounting semantics and was flagged
> rather than patched mid-investigation. See §10, item N3.

### 8.3 Other observations

| Issue | Detail |
|---|---|
| Engine default commission is unrealistic | `CommissionBPS` defaults to **25** vs Bitso retail taker **65**. All runs here explicitly passed `-commission-bps 65`. |
| `limit_profit` `MinProfitBPS` defaults to 0 | Disabled at `limit_profit_strategy.go:78`; `computeMinProfit` falls back to an absolute `MinProfit`. The bps-relative guard is effectively off by default. |
| Backtest engine is O(n²) | Indicators are recomputed over the full bar history on every tick, giving ~6 min per 4-strategy run over 34 days. |

---

## 9. What this does and does not prove

**Proven:**

- The archive can be loaded faithfully and reproducibly into the backtest engine (§2).
- The 2026-08-19 .. 2026-09-22 window is a **low-volatility period**, and that is a genuine market
  property, not a data-collection artefact (§4, §5).
- In that low-volatility regime, at realistic Bitso retail fees, **`mean_reversion` and `limit_profit`
  are unprofitable**, with adequate sample support in `low_vol_range`, `neutral`, and `trending_down`
  for `mean_reversion` (§6).
- The dominant cost is the **130 bps round-trip fee**, which exceeds the typical available move in
  this market (§6.4).

**NOT proven:**

- Anything about `high_vol` behaviour. **14 samples from one 7-minute window is not evidence.**
- Anything about `momentum`, which produced **zero** closed trades in every run.
- That the strategies are unprofitable *in general* — only that they are in **this regime, in this
  window, at these fees**.
- Any expectancy figure to better than one significant figure, given §8.2's one-leg accounting bias
  and §7.1's throttle workaround.

---

## 10. Recommended next steps

| # | Action | Why |
|---|--------|-----|
| **N1** | **Do not size up or go live on these strategies.** | §6 shows negative expectancy with adequate samples in three regimes. |
| **N2** | **Finish fee gating: `momentum` and `limit_profit` entries.** | §3.3. Use `fee_gate.go`; mirror the `mean_reversion` integration and its tests. |
| **N3** | **Fix the one-leg commission asymmetry** at [`runner.go:215`](../../services/strategy-executor/internal/backtest/runner.go#L215). | §8.2. Every per-trade number is optimistic until this lands. |
| **N4** | **Inject a clock into the strategies** so `MinSignalInterval` respects simulated time. | §7.1. Until then no backtest can honour the production throttle. |
| **N5** | **Repair `internal/processor` tests.** | §8.1. `go test ./...` is currently red on `HEAD`. |
| **N6** | **Keep collecting until a genuine `high_vol` period is captured.** | §4. No amount of re-analysis creates volatility that is not in the data. |
| **N7** | **Land archive compaction.** | §2.3. 26,600 objects at ~2.4 KB each makes every load slow. |
| **N8** | **Raise the engine default `CommissionBPS` to a realistic value.** | §8.3. A 25 bps default invites optimistic accidental runs. |
| **N9** | **Investigate why `momentum` never trades.** | §6.2. Zero trades across every configuration is itself a bug signal. |

---

## 11. Reproducing this analysis

No cluster is required. From a machine with AWS read access to the archive bucket:

```bash
# 1. Pull the archive locally (~107 MB, 26,600 objects, ~4 min)
aws s3 sync s3://<archive-bucket>/trades ./archive/trades

cd services/strategy-executor

# 2. Replay the archive through the live indicator pipeline (live-faithful cadence)
go run ./cmd/regime-snapshots \
  -archive ./archive -book btc_mxn \
  -from 2026-08-19 -to 2026-09-22T23:59:59Z \
  -bar-interval 1m -cadence 30s > snapshots_1m_30s.jsonl

# 3. Classify with the router's own pure classifier, emitting regime labels
cd ../strategy-router
go run ./cmd/classifier-backtest -stdin \
  -labels ../strategy-executor/regime_labels.csv \
  < ../strategy-executor/snapshots_1m_30s.jsonl

# 4. Backtest with realistic fees and per-regime attribution
cd ../strategy-executor
go run ./cmd/backtest-archive \
  -archive ./archive -book btc_mxn \
  -from 2026-08-19 -to 2026-09-22T23:59:59Z \
  -labels ./regime_labels.csv \
  -strategies mean_reversion,momentum,limit_profit,random_baseline \
  -commission-bps 65 -slippage-bps 10 -min-samples 30 \
  -params '{"min_signal_interval":0}' \
  -json results.json
```

The `-labels` flag on `classifier-backtest` is **additive** — it writes a `snapshot_at,regime` CSV and
leaves all existing output unchanged.

---

## 12. Change inventory

### Added

| Path | Purpose |
|---|---|
| `services/strategy-executor/internal/backtest/loader/` (8 files, 1,482 lines) | S3/Postgres archive loader + tests |
| `services/strategy-executor/cmd/regime-snapshots/` (506 lines) | Archive → `classifier.Snapshot` JSONL replay + field-name tripwire |
| `services/strategy-executor/cmd/backtest-archive/main.go` (466 lines) | Backtest CLI with per-regime attribution and sample-sufficiency labelling |
| `services/strategy-executor/internal/backtest/random_baseline.go` (146 lines) | Coin-flip null-hypothesis strategy |
| `services/strategy-executor/internal/strategies/fee_gate.go` (167 lines) | Direction-aware round-trip fee gate |
| `services/strategy-executor/internal/strategies/fee_gate_test.go` (290 lines) | Table-driven fee-gate tests |
| `services/strategy-executor/internal/strategies/mean_reversion_fee_gate_test.go` (232 lines) | Integration tests for the gated strategy |

### Modified

| Path | Change |
|---|---|
| `internal/strategies/mean_reversion.go` | Fee gate wired in; new no-op-by-default config; stop-loss override ordered before the net-loss guard |
| `internal/backtest/runner.go` | Added `TickTime` to `SignalRecord` (§7.2); added missing SELL fill notification (§7.3) |
| `strategy-router/cmd/classifier-backtest/main.go` | Additive `-labels` CSV flag; scanner buffer raised to 1 MiB |
| `services/strategy-executor/go.mod` / `go.sum` | Added parquet-go, aws-sdk-go-v2, pgx/v5 |

### Verification status

| Check | Result |
|---|---|
| `strategy-executor` `go build ./...` | ✅ clean |
| `strategy-executor` `go vet ./...` | 🟡 fails only on pre-existing `internal/processor` (§8.1) |
| `strategy-executor` `go test ./...` | 🟡 all pass except pre-existing `internal/processor` build failure (§8.1) |
| `strategy-router` `go build ./...` | ✅ clean |
| `strategy-router` `go test ./...` | ✅ all pass |
