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

## Evidence

Raw command output backing each assessment is stored in a dated `evidence-YYYY-MM-DD/` subfolder
next to the document that cites it:

- [`evidence-2026-09-22/`](evidence-2026-09-22/) — regime distribution, daily ATR coverage, `ws_gaps` audit, backtest runs
- [`evidence-2026-09-23/`](evidence-2026-09-23/) — edge analysis (move distribution + oracle), fee-gate-enabled backtest

Evidence files are committed verbatim (only ANSI colour codes and per-tick log spam are stripped) so
the numbers in the reports can be checked without re-running a 6-minute backtest.
