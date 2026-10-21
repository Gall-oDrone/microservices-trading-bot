# Dual-Momentum Study: NSDQ100 / SPX500 / cash on eToro, CFD and ETF products, against holding QQQ / SPY and the frozen SMA50 rule

Date: **2026-10-21** · Branch: `feat/etoro` · Candidate cell #2 of the next-steps plan (after the SMA50 index-CFD forward test)
Evidence: [`evidence-2026-10-21/`](evidence-2026-10-21/) (`rotation-report.txt`, `rotation-results.json`; input data = the committed Yahoo CSVs of [`evidence-2026-10-10/data`](evidence-2026-10-10/data), SHA-256 in `rotation-results.json.params.data_sha256`)
Engine: [`cmd/rotation-research`](../../services/strategy-executor/cmd/rotation-research/main.go) on [`shared/pkg/rotation`](../../shared/pkg/rotation/rotation.go) (the rule) and [`cfdsim.SimulateRotation`](../../shared/pkg/cfdsim/rotation.go) (the simulator). With one asset the simulator reproduces the frozen `cfdsim.Simulate` bit for bit (`TestRotationMatchesSimulate`); `cfdsim.Simulate` and `dailyrule` are untouched.

> [!IMPORTANT]
> **Verdict, in one paragraph.** On the **ETF product** (QQQ / SPY x1, real asset, no financing) the 12-1 dual-momentum rule is the first rule in this repository's studies that **beats holding SPY over the long run after costs** (since 2000: +1,130 % vs SPY +782 % and QQQ +955 %, max drawdown 50 % vs 55 % / 80 %), and it beats **97.7 % of 2,000 permutations** of its own monthly choices over that span. But the edge is concentrated: it comes from being in cash in 2000–02, 2008 and 2022. In any single year it beats SPY only **15 of 26** times and QQQ **6 of 26**; over rolling one-year windows it beats SPY in **56 %**, QQQ in **27 %**, with a median of **+1.4 pp vs SPY and 0.0 pp vs QQQ**. Its one-year drawdown is *higher* than SPY's two years in three.
> On the **CFD product** the same rule returns +199 % since 2000: financing eats 82 % of the ETF result. **If a demo cell is run, it is the ETF cell.** The CFD version is dominated and should not be pre-registered.

> [!NOTE]
> The primary variant (12-month lookback, skipping the last month, 0 % hurdle — the literature's "12-1") was fixed in code **before** the first run and is not the best variant on this data (`dm_12`, no skip, is). It stays primary. Choosing the best after the fact would be the parameter shopping the pre-registrations exist to prevent.

---

## 1. Question

Cell #2 of the next-steps plan asked whether a rule that keeps more exposure than SMA50 and trades rarely can beat the honest alternative. The rule, decided **once a month** on the last NYSE trading day:

- **Relative momentum:** compute each asset's return over the lookback (12 months = 252 bars, skipping the most recent 21 for the primary variant); pick the stronger of NSDQ100 and SPX500.
- **Absolute momentum:** hold it only if that return beats the cash hurdle (0 %, or the 13-week T-bill return over the same span in the `_tbill` variants); otherwise hold cash.
- **Timing / size:** decide at the month-end close, fill at the next bar's open, x1, all-in / all-out, one position at a time. Between decisions the target is carried unchanged.

Six variants were run (`dm_12_1`, `dm_12_1_tbill`, `dm_12`, `dm_6`, `dm_6_tbill`, `dm_3`); `dm_12_1` is primary for base rates, the random baseline and sensitivity.

Does that beat (a) holding QQQ, (b) holding SPY, (c) the frozen SMA50 rule on QQQ / SPY?

## 2. Data and cost model

| Input | Source | Span |
|---|---|---|
| CFD proxy for long history | Yahoo `^NDX`, `^GSPC`, aligned on common dates | 1985-10 → 2026-10-09 (10,336 bars) |
| ETFs: signal on **raw closes**, P&L on **dividend-adjusted** bars | Yahoo QQQ / SPY, aligned | 1999-03 → 2026-10-09 (6,940 bars) |
| eToro CFD bars | [`docs/etoro/evidence-2026-10-10/daily_*.csv`](../etoro/evidence-2026-10-10/) | 2022-12 → 2026-10-09 (955 aligned) |
| Financing reference | Yahoo `^IRX` | 1970 → |

Signals are computed on raw closes because that is what a live route serves; the live ETF route is not dividend-adjusted.

**Costs** (CFD: the frozen block of the 2026-10-12 pre-registration; ETF: the in-session capture of 2026-10-21, [`docs/etoro/evidence-2026-10-21/`](../etoro/evidence-2026-10-21/)):

- CFD: round-trip spread **15.5 / 14.8 bps**, half per leg; financing `(^IRX + 4.338 %)/365` per calendar night (the markup that makes today's rate equal the measured 2.3 bps), weekends three nights.
- ETF: **15 bps of notional per order** (eToro's preview: 1.65 USD on 1,100 USD, 1.50 on 1,000 — proportional, not the fixed 1.50 USD the first draft of this study assumed) + 0.28 / 0.26 bps bid/ask; no financing; dividends through adjusted closes.
- Switching NSDQ100 → SPX500 at one open pays both legs.
- Random baseline: **2,000 permutations** of the primary variant's own month-by-month targets inside the window (same number of months in each asset and in cash, shuffled order), seed 1. It asks "is the *timing* worth anything, given the composition?". A year with a single target throughout has nothing to permute and is marked n/a.

## 3. Results by window — primary variant `dm_12_1`

Returns in % over the window from equity 1.0. Full tables with all six variants in `rotation-report.txt`.

| Window | **Rotation ETF** | Rotation CFD | QQQ hold | SPY hold | SMA50 on QQQ | Rot. ETF max DD | SPY max DD | QQQ max DD | Beats random |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| Since ETF inception (2000-05 →) | **1,130.5** | 199.2 | 954.6 | 781.6 | 264.3 | 50.0 % | 55.2 % | 80.5 % | **97.7 %** |
| 2000-07 – 2009 | **−15.5** | −47.4 | −49.8 | −9.5 | 14.5 | 50.0 % | 55.2 % | 80.5 % | 72.2 % |
| 2010 – 2019 | **311.1** | 142.2 | 408.0 | 248.8 | 47.7 | 22.8 % | 19.4 % | 22.8 % | 39.2 % |
| 2020 – 2026 | **211.2** | 114.3 | 263.3 | 163.9 | 114.6 | 28.6 % | 33.7 % | 35.1 % | 77.0 % |
| Last 10 y | **388.4** | 177.8 | 573.1 | 320.5 | 185.4 | 28.6 % | 33.7 % | 35.1 % | 63.5 % |
| Last 5 y | **82.3** | 35.0 | 114.6 | 89.8 | 63.3 | 22.9 % | 24.5 % | 35.1 % | 67.2 % |
| Last 3 y | **104.3** | 60.4 | 110.3 | 88.2 | 48.5 | 21.1 % | 18.8 % | 22.8 % | 56.8 % |
| eToro CFD bars 2024-01 → (CFD only) | — | 42.5 | — | — | — | 22.5 % | — | — | 74.0 % |

On eToro's own bars the CFD rule earned 3.5 pp *more* than on index bars over the same dates (bar-source effect, opposite in sign to the SMA50 study's).

What the table says:
- **The long-run result is a 2000–02 / 2008 / 2022 result.** In 2010–2019, the longest bull market in the sample, the rule trailed QQQ by 97 pp and trailed it again in 2020–2026 and the last 3 / 5 / 10 years. It beat SPY in every window except 2000–2009 (where it beat QQQ by 34 pp instead).
- **The CFD product does not work.** Financing is 169 % of starting equity since 2000 and 107 % over the last ten years; even with zero financing the CFD rule (999 %) still trails the ETF rule (1,130 %) because dividends are missing. The CFD rotation beats SPY in **32 %** of rolling years.
- **Dual momentum beats the frozen SMA50 rule** on the same ETF by a wide margin in every window (e.g. last 10 y: 388 % vs 185 %) with **13 round trips instead of 78**. The 12-month lookback with monthly decisions is simply a much cheaper trend filter than a daily 50-day SMA.
- **It is not less risky than SPY on a one-year horizon.** Its max drawdown beat SPY's in only 10 of 26 calendar years and 34 % of rolling windows. The drawdown protection shows up only in the crashes.

## 4. Base rates for a pre-registration (ETF product, primary variant)

### 4.1 Calendar years 2001–2026

| Hypothesis | Pass if | Years passing |
|---|---|---|
| **H1** rotation reduces risk | max DD **<** SPY hold max DD | **10 / 26** |
| **H2** beats the broad benchmark | return **>** SPY hold | **15 / 26** |
| **H2b** beats the honest alternative | return **>** QQQ hold | **6 / 26** |
| **H3** timing is not luck | beats ≥ 95 % of permutations | **0 / 17** applicable |
| **H4** beats the rule already under test | return **>** SMA50 on QQQ (same costs) | **19 / 26** |

H2b passes in 2001, 2002, 2006, 2008, 2016, 2022 — four of the six are bear years. H3 never passes in a single year: twelve monthly decisions are not enough to separate timing from composition, so H3 belongs to a multi-year evaluation, not to a one-year test.

### 4.2 Rolling 252-bar windows, step 21 (305 windows)

| H1 | H2 | H2b | H4 | CFD rotation > SPY | Median rot − SPY | Median rot − QQQ |
|---|---|---|---|---|---|---|
| 33.8 % | **56.4 %** | 27.2 % | **66.9 %** | 31.5 % | **+1.40 pp** | 0.00 pp |

The median vs QQQ is exactly 0.00 pp because the rule holds QQQ the whole year in a large share of windows (the n/a years of §4.1): in a calm bull year the rule *is* QQQ minus two commissions.

## 5. Cost sensitivity, primary variant (returns %)

| Scenario | Since 2000, rot. CFD | rot. ETF | SPY | QQQ | Last 10 y, rot. CFD | rot. ETF |
|---|---:|---:|---:|---:|---:|---:|
| Base | 199.2 | 1,130.5 | 781.6 | 954.6 | 177.8 | 388.4 |
| CFD spread ×0.5 / ×2 | 206.5 / 185.1 | — | — | — | 180.6 / 172.2 | — |
| Financing markup ×0.5 / ×1.5 | 374.4 / 85.8 | — | — | — | 232.3 / 131.0 | — |
| No financing | 999.1 | — | — | — | 371.3 | — |
| ETF fee ×2 (30 bps) / ×0.5 (7.5 bps) | — | 1,021.0 / 1,189.1 | — | — | — | 369.7 / 398.0 |

The spread is irrelevant (13 round trips in ten years). Financing decides the CFD product. The ETF commission is second order: doubling it to 30 bps per order costs about 19 pp over ten years (388 % → 370 %).

## 6. What happens next

1. **Do not pre-register the CFD version.** It is dominated at every cost scenario short of "no financing", and even then by the ETF.
2. **An ETF dual-momentum cell is worth a demo pre-registration**, judged on paper like the others. The ETF route was evidenced on demo on 2026-10-21 for cell #1 (live daily bars for QQQ 3006 / SPY 3000, in-session costs, x1 real-asset round trip: [`docs/etoro/evidence-2026-10-21/`](../etoro/evidence-2026-10-21/)); the same capture serves this cell.
3. **Expected outcome of a one-year test, from §4:** H2 passes with about 56–58 % probability, H2b about 23–27 %, H4 about 67–73 %, H1 about 34–38 %. A one-year pass on H2 would be weak evidence; the test's real value is a clean year of unseen data toward a multi-year H3.
4. **The executor change is a joint decision across two books** (one position at a time, month-end only), which the current per-instrument `etoro-daily-executor` does not do. It is deferred until the pre-registration exists; the paper series needs nothing more than `rotation.Targets` on the saved candles.
5. **Variant freeze:** `dm_12_1`, 0 % hurdle. `dm_12` and the T-bill hurdle are reported, not chosen.

## 7. Reproduce

```bash
cd services/strategy-executor
go test ../../shared/pkg/rotation ../../shared/pkg/cfdsim
go run ./cmd/rotation-research -data ../../docs/backtest-readiness/evidence-2026-10-10/data \
  -etoro ../../docs/etoro/evidence-2026-10-10 -out ../../docs/backtest-readiness/evidence-2026-10-21 \
  -sims 2000 -seed 1
```

The committed CSVs reproduce `rotation-report.txt` exactly (their SHA-256 are in `rotation-results.json`). Yahoo is a research-only input: no live decision depends on it.
