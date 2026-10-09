# Pre-registration: re-pegging the maker order every 5 minutes (2026-10-09)

**Status: frozen when committed.** The forward window opens at the 2026-10-10 Bitso day (06:00
UTC). Nothing in this file may change after the commit that adds it. A different design needs a
new dated file.

**What is tested.** An execution schedule only. The SMA50 forward tests, their rule, their frozen
costs (70/88 bps btc_mxn, 40/46 bps btc_usd) and their evaluation dates are **not affected**,
whatever this test finds. Their paper accounts never used execution at all.

## 1. Background and why the forward data must decide

[EXECUTION-RESEARCH-2026-10-09.md](EXECUTION-RESEARCH-2026-10-09.md) replayed execution schedules
over the archived production trades (2026-08-19 → 2026-10-08). Two schedules matter here:

- **today**, the daily executor's schedule (`internal/dailyexec`): one post-only order at the best
  price for 60 minutes, then a market order for the rest;
- **re-peg**, the same, but the resting price is moved to the current best price every 5 minutes.

The paired, same-legs comparison on that data (06:15 UTC, Through fill model):

| Book | Size | Legs | today mean | re-peg mean | diff | one-sided p | today p90 | re-peg p90 |
|---|---|---:|---:|---:|---:|---:|---:|---:|
| btc_mxn | 0.001 BTC | 98 | 66.1 | 64.2 | −1.84 | 0.069 | 89.6 | 77.5 |
| btc_mxn | 0.01 BTC | 98 | 72.2 | 69.1 | −3.14 | 0.006 | 101.6 | 92.1 |
| btc_usd | 0.001 BTC | 16 | 32.4 | 32.6 | +0.24 | 0.552 | 38.2 | 40.8 |

Evidence: [`exec-repeg-in-sample.md`](evidence-2026-10-09/exec-repeg-in-sample.md) and
[`exec-repeg-in-sample.json`](evidence-2026-10-09/exec-repeg-in-sample.json).

These data suggested the hypothesis, so they cannot confirm it. At stage size the difference was
not significant even in-sample (p = 0.069). The forward window below has not been looked at.

## 2. Design (frozen)

| Item | Value |
|---|---|
| Data | Bitso production public trades in the collector archive, `trades_compacted/` (read-only) |
| Forward window | Bitso days **2026-10-10 → 2027-01-09** (92 days), each day's leg starting at **06:15 UTC** (the executor's start) |
| Legs | A buy and a sell every day, sizes **0.001 BTC** (stage size) and **0.01 BTC** (the policy's max order) |
| Schedules | `today` = `maker 1h`; `re-peg` = `maker 1h repriced 5m`; `market` (reference only) |
| Model | `internal/execsim` and `cmd/exec-research` as of the commit that adds this file: best quotes from maker-side prints (≤ 10 min old, else last trade ± 1.5 / 1.3 bps); square-root impact Y = 1; registered fees (maker 60 / 30, taker 78 / 36 bps) |
| Primary fill model | **Through** (conservative). Touch is reported. |
| Day exclusion | Any archive gap over 30 minutes between the day's open and 07:15 UTC (the tool's `-max-gap` rule) |
| Minimum data | **≥ 60 used btc_mxn days.** If fewer, the window extends once, to 2027-02-08, and that is final. |

The frozen command (run on or after 2027-01-10):

```bash
cd services/strategy-executor
go run ./cmd/exec-research -bucket mtb-development-data-archive-326105557351 \
  -from 2026-10-10 -to 2027-01-09 -books btc_mxn,btc_usd -starts 06:15 -sizes 0.001,0.01 \
  -schedules "market,maker 1h,maker 1h repriced 5m" \
  -compare-base "maker 1h" -compare-alt "maker 1h repriced 5m" \
  -out-json repeg-forward.json -out-md repeg-forward.md
```

Commit both outputs under `docs/backtest-readiness/evidence-<run date>/`. Write a short results
note linking here. As a sensitivity check (reported, not deciding), run it again with
`-book-samples ./daily-executor-data/book-samples`, so the measured spread replaces the fallback
half-spread.

## 3. Hypotheses and pass criteria (frozen)

All are measured on btc_mxn, 0.001 BTC, the Through model, from the paired comparison in the
report.

| # | Hypothesis | Pass if |
|---|---|---|
| **E1** | Re-pegging lowers the mean cost per leg | Mean paired difference (re-peg − today) **< 0** with one-sided **p < 0.05** |
| **E2** | Re-pegging lowers the cost tail | re-peg p90 **<** today p90 |
| **E3** | It does not hurt the larger leg | At 0.01 BTC, mean difference **≤ 0** |

btc_usd is reported but does not decide: the in-sample data had only 8 days, and its maker fills
are already high.

How to read the outcome:

- **E1, E2 and E3 pass.**
  1. Add a re-peg option to the executor (`dailyexec.Config`), off by default, with tests.
  2. Turn it on for stage with a dated note.
  3. Watch the cost budget on the stage ledger (plan §6.4.10). The SMA50 tests keep their frozen
     costs.
- **E2 only.** A tail-risk improvement without a mean gain. Report it; no change unless a cost
  review (validation trigger T5) asks for it.
- **E1 fails.** Keep today's schedule and close this line.

## 4. Integrity

- The tool's commit is recorded in the results note. A later change to `execsim` or
  `exec-research` must reproduce this file's in-sample numbers above before it can be used for the
  forward run.
- If the archive loses data for the window (the compactor never deletes; `-cutover` stays manual),
  report the days lost. The minimum-data rule applies.
- No early stopping, and the window stays blind until 2027-01-10. The monthly execution study
  (`scripts/ops-run.sh research`) leaves out every repriced schedule and the paired comparison
  until then.
