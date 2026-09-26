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
| [`DAILY-HORIZON-NEWS-STUDY-2026-09-26.md`](DAILY-HORIZON-NEWS-STUDY-2026-09-26.md) | 2026-09-26 | Tests R3's 24h horizon on a year of Yahoo BTC-USD daily bars + LLM-scored news, with Bitso costs, next-open fills and a random same-trade-count baseline. **Verdict: no rule clearly beats holding.** SMA50 trend: −3.96% vs −8.94% hold, but only beats 84.5% of random (+14.6% frictionless; costs ate 18 pp). The news-sentiment rule lost 49% and is anti-informative at 1–3 days. |

## Evidence

Raw command output backing each assessment is stored in a dated `evidence-YYYY-MM-DD/` subfolder
next to the document that cites it:

- [`evidence-2026-09-22/`](evidence-2026-09-22/) — regime distribution, daily ATR coverage, `ws_gaps` audit, backtest runs
- [`evidence-2026-09-23/`](evidence-2026-09-23/) — edge analysis (move distribution + oracle), fee-gate-enabled backtest
- [`evidence-2026-09-24/`](evidence-2026-09-24/) — six-scenario re-measure on the fixed harness (Bitso maker/taker rates, flat 65 bps, fee-blind, fully ungated)
- [`evidence-2026-09-25/`](evidence-2026-09-25/) — `momentum` entry-condition diagnostic, scenario (d) re-run on the incremental engine (identical numbers, with timing)
- [`evidence-2026-09-26/`](evidence-2026-09-26/) — 24h-horizon study on Yahoo BTC-USD daily + news (Bitso costs and frictionless reference)

Evidence files are committed verbatim (only ANSI colour codes and per-tick log spam are stripped) so
the numbers in the reports can be checked without re-running a 6-minute backtest.
