# Backtest Readiness

This folder holds the work that answers one question:

> **Can we trust a backtest run against the archived `btc_mxn` trade data to tell us whether our
> strategies are economically viable?**

It is the offline / historical-data counterpart to [`../strategy-fee-accuracy/`](../strategy-fee-accuracy/),
which covers the live Stage soak program. Both folders use the same framing, and it is important not
to conflate the two kinds of proof:

| Kind of proof | Question it answers | Where it lives |
|---|---|---|
| **Engineering proof** | Does the pipeline classify → route → order → account for fees correctly? | [`../strategy-fee-accuracy/`](../strategy-fee-accuracy/) |
| **Economic proof** | Is per-regime expectancy positive *after real fees*? | this folder |

See [`../strategy-fee-accuracy/STAGE-FINANCIAL-APPROACH-2026-06-04.md`](../strategy-fee-accuracy/STAGE-FINANCIAL-APPROACH-2026-06-04.md) §1
for the original statement of that distinction.

---

## Documents

Read these in order.

| Document | Date | Summary |
|---|---|---|
| [`BACKTEST-READINESS-ASSESSMENT-2026-09-22.md`](BACKTEST-READINESS-ASSESSMENT-2026-09-22.md) | 2026-09-22 | Archive loader, fee-gating review, regime-diversity analysis, WebSocket gap audit, and the first honest backtest + random baseline run. **Verdict: the archive cannot support a `high_vol` conclusion, and all three live strategies lose money after real fees.** |
| [`PATH-TO-PROFITABILITY-2026-09-23.md`](PATH-TO-PROFITABILITY-2026-09-23.md) | 2026-09-23 | Answers *why* they lose, using a move-size distribution and a perfect-foresight oracle. **Verdict: a horizon/cost mismatch, not a tuning problem** — at 1m bars the cost is 50× the median move. Ranks the fixes, and shows that simply enabling the existing fee gate swings `mean_reversion` from −7,291 to +64 MXN. |
| [`HARNESS-FIXES-AND-REMEASURE-2026-09-24.md`](HARNESS-FIXES-AND-REMEASURE-2026-09-24.md) | 2026-09-24 | Implements R1 (fee gate on by default, `momentum` gated), R2 (two-leg commission, simulated clock, `buy_and_hold` baseline, processor tests) and R4 (maker/taker in the backtest, real Bitso rates), then re-measures. **Verdict: the fixed harness doubles every ungated loss; gated strategies are near break-even on 3–11 trades, and buy-and-hold beats them all by ~10×.** Real stage fees are 60/78 bps, not 50/65. |
| [`FEES-ENGINE-SPEEDUP-MOMENTUM-2026-09-25.md`](FEES-ENGINE-SPEEDUP-MOMENTUM-2026-09-25.md) | 2026-09-25 | Confirms **production** fees (maker 60 / taker 78 bps), corrects the gate default to 156 bps, makes the engine **~30× faster with bit-identical results**, and shows **`momentum` can never enter**: its RSI and EMA conditions held together on 0 of 70,591 ticks. Surveys the Yahoo BTC-USD daily + LLM-scored news data for a 24h-horizon study. |
| [`DAILY-HORIZON-NEWS-STUDY-2026-09-26.md`](DAILY-HORIZON-NEWS-STUDY-2026-09-26.md) | 2026-09-26 | Tests R3's 24h horizon on a year of Yahoo BTC-USD daily bars + LLM-scored news, with Bitso costs, next-open fills and a random same-trade-count baseline. **Verdict: no rule clearly beats holding.** SMA50 trend: −3.96% vs −8.94% hold, but only beats 84.5% of random (+14.6% frictionless; costs ate 18 pp). The news-sentiment rule lost 50% and is anti-informative at 1–3 days (worse than 97.4% of random). *Corrected 2026-09-27: news is now de-duplicated by URL, not by the non-unique `id`; the conclusion held.* |
| [`FULL-HISTORY-DAILY-STUDY-2026-09-27.md`](FULL-HISTORY-DAILY-STUDY-2026-09-27.md) | 2026-09-27 | Refreshes the compacted Yahoo files (adds `scripts/yahoo-refresh.sh`, a gated sync → compact → upload) and finds BTC-USD daily data back to **2018**. Same untuned rules, Bitso costs. **Verdict: SMA50 trend beats holding over 2018–2026 (+940% vs +509%, beats 99.9% of random, drawdown 65% vs 82%), but only 4 of 9 years; its edge is drawdown control (lower DD in 9/9 years).** Out-of-sample 2026: +9.7% vs −5.2% hold (90%). The 2026-09-26 2025 trend "hint" was a cold-start artefact (warm: −14.5%). News: still negative, and indistinguishable from random in 2026. |
| [`BTC-MXN-TREND-CHECK-2026-09-27.md`](BTC-MXN-TREND-CHECK-2026-09-27.md) | 2026-09-27 | Re-runs the rule on Bitso's own `btc_mxn` daily candles (new `cmd/bitso-daily`, 2017–2026, no gaps) at taker, maker and frictionless costs. **Verdict: it does not transfer.** 104 trips vs 77; loses to holding over 2018–2026 at every Bitso fee level (maker + slippage +235% vs +376%). The timing signal is real (frictionless +1,345%, beats 97.8% of random) but fees × trade count kill it. The BTC-USD result was fragile (partly flattered by the Yahoo 2024 data hole). Do not build it live. |
| [`FORWARD-TEST-PREREGISTRATION-SMA50-2026-09-27.md`](FORWARD-TEST-PREREGISTRATION-SMA50-2026-09-27.md) | 2026-09-27 | **Frozen** paper forward test of SMA50 on `btc_mxn` from 2026-09-27: rule, costs (maker + slippage primary), interim look 2027-03-26, evaluation 2027-09-26, and pass criteria H1 (drawdown), H2 (return), H3 (≥ 95% vs random), with backtest base rates. |
| [`VOLUME-AND-ACTIVE-STRATEGIES-STUDY-2026-10-03.md`](VOLUME-AND-ACTIVE-STRATEGIES-STUDY-2026-10-03.md) | 2026-10-03 | Tests volume-spike rules, volume-confirmed SMA50 entries, a trend ensemble and weekly volatility targeting on Bitso daily candles, with a pre-declared 2-year holdout (new `cmd/weekly-research`). **Verdict: nothing profits every week; volume spikes and extra activity lose to fees; SMA50 with entries needing volume ≥ 1.5× beat SMA50 and holding on both books in development and holdout, so it is a candidate for a new pre-registration.** Also lists 2024–2026 literature found via Google Scholar. |
| [`EXECUTION-RESEARCH-2026-10-09.md`](EXECUTION-RESEARCH-2026-10-09.md) | 2026-10-09 | Replays execution schedules over the archived production trades (new `cmd/exec-research`). **Verdict: today's schedule (post-only 60 min, then market) costs 66.5 / 32.7 bps per leg at stage size, inside the registered 70 / 40, so the 118 bps stage leg is stage's thin book. Re-pegging every 5 minutes lowers the mean and the tail (needs its own pre-registration); capacity is about 0.01 BTC per btc_mxn leg.** |
| [`EXECUTION-PREREGISTRATION-REPEG-2026-10-09.md`](EXECUTION-PREREGISTRATION-REPEG-2026-10-09.md) | 2026-10-09 | **Frozen** forward test of re-pegging the maker order every 5 minutes, against today's schedule, on the archived production trades from 2026-10-10 to 2027-01-09 (blind until then). E1: lower mean cost per btc_mxn stage leg (paired, one-sided p < 0.05); E2: lower p90; E3: no worse at 0.01 BTC. Only if all pass does the executor gain the option. The SMA50 tests are unaffected. |
| [`INDEX-CFD-SMA50-STUDY-2026-10-10.md`](INDEX-CFD-SMA50-STUDY-2026-10-10.md) | 2026-10-10 | eToro port (`feat/etoro`, phase P2): the frozen SMA50 rule on the **NSDQ100 / SPX500 index CFDs** (new `shared/pkg/cfdsim` with spread + nightly financing, `cmd/index-research`), against holding the CFD and holding **QQQ / SPY** x1. **Verdict: it does not make money.** It loses to the ETF in 79 % / 88 % of rolling years and to the CFD hold in about 70–77 %. Its one robust property is lower drawdown (≈ 70 % of years). Multi-year CFD holds are margin-closed-out by financing. No cost scenario reverses this. |
| [`FORWARD-TEST-PREREGISTRATION-SMA50-INDEX-CFD-2026-10-12.md`](FORWARD-TEST-PREREGISTRATION-SMA50-INDEX-CFD-2026-10-12.md) | 2026-10-10 / costs 2026-10-12 | Paper forward test of SMA50 on eToro NSDQ100 and SPX500 bars, 2026-10-13 → 2027-10-12 (interim 2027-04-12), 1,100 USD x1 per instrument. H1 drawdown, H2 vs CFD hold, **H2b vs QQQ / SPY hold**, H3 ≥ 95 % vs random. Cost cells are provisional until the weekday re-measure on 2026-10-12, then frozen. Demo-account fills are reported but do not decide. |

## Evidence

Raw command output backing each assessment is stored in a dated `evidence-YYYY-MM-DD/` subfolder
next to the document that cites it:

- [`evidence-2026-09-22/`](evidence-2026-09-22/) — regime distribution, daily ATR coverage, `ws_gaps` audit, backtest runs
- [`evidence-2026-09-23/`](evidence-2026-09-23/) — edge analysis (move distribution + oracle), fee-gate-enabled backtest
- [`evidence-2026-09-24/`](evidence-2026-09-24/) — six-scenario re-measure on the fixed harness (Bitso maker/taker rates, flat 65 bps, fee-blind, fully ungated)
- [`evidence-2026-09-25/`](evidence-2026-09-25/) — `momentum` entry-condition diagnostic, scenario (d) re-run on the incremental engine (identical numbers, with timing)
- [`evidence-2026-09-26/`](evidence-2026-09-26/) — 24h-horizon study on Yahoo BTC-USD daily + news (Bitso costs and frictionless reference; `*-corrected-2026-09-27.txt` supersede the originals)
- [`evidence-2026-09-27/`](evidence-2026-09-27/) — `yahoo-compact` refresh report; 2018–2026 full-span, by-year and news-era runs (Bitso costs and frictionless); `btc_mxn` Bitso daily snapshot and runs at taker / maker / maker + slippage / frictionless; BTC-USD at maker
- [`evidence-2026-10-03/`](evidence-2026-10-03/) — `weekly-research` output for `btc_mxn` and `btc_usd` (development + holdout, base and stress costs, post-hoc volume-threshold sensitivity); daily candle snapshots with SHA-256; `research-run/v1` JSON twins (`*.json` base costs, `*-stress-<bps>bps.json` stress costs), regenerated 2026-10-07 with byte-identical text output
- [`evidence-2026-10-09/`](evidence-2026-10-09/) — `exec-research` output (markdown and JSON); `prereg-as-of/`: the first automated progress report of both SMA50 forward tests (`scripts/prereg-evaluation.sh`), with the fetched CSVs, `daily-research` text and JSON, the `mxn-terms.py` cross-check and SHA256SUMS
- [`evidence-2026-10-10/`](evidence-2026-10-10/) — `index-research` study output (`report.txt`, `results.json`) and its Yahoo inputs (`data/`: ^NDX, ^GSPC, QQQ, SPY, ^IRX) with SHA256SUMS. The eToro bars and costs it reads are in [`../etoro/evidence-2026-10-10/`](../etoro/evidence-2026-10-10/)

Evidence files are committed verbatim (only ANSI colour codes and per-tick log spam are stripped) so
the numbers in the reports can be checked without re-running a 6-minute backtest.
