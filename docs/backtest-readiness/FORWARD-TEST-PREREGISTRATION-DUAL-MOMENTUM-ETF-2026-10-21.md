# Forward-Test Pre-Registration — Dual Momentum (12-1) between QQQ and SPY at x1, cash otherwise

Date registered and costs frozen: **2026-10-21**
Repository: `microservices-trading-bot` · Branch: `feat/etoro` · Next-steps plan, cell **#2**
Audience: Whoever evaluates this in 2027
Context: [`DUAL-MOMENTUM-INDEX-STUDY-2026-10-21.md`](DUAL-MOMENTUM-INDEX-STUDY-2026-10-21.md) ·
eToro facts: [`docs/etoro/evidence-2026-10-21/`](../etoro/evidence-2026-10-21/) ·
sibling tests: [`FORWARD-TEST-PREREGISTRATION-SMA50-ETF-2026-10-21.md`](FORWARD-TEST-PREREGISTRATION-SMA50-ETF-2026-10-21.md) (same ETFs, same costs, the daily rule),
[`FORWARD-TEST-PREREGISTRATION-SMA50-INDEX-CFD-2026-10-12.md`](FORWARD-TEST-PREREGISTRATION-SMA50-INDEX-CFD-2026-10-12.md)

This test is judged **on paper**, from eToro's own daily bars for the ETFs, by
[`cmd/rotation-research -forward`](../../services/strategy-executor/cmd/rotation-research/forward.go).
The demo-account executor (`etoro-daily-executor -strategy dm_12_1`, built later the same day, runbook
§11; its own ledger `etoro-daily-data/demo-rotation/`) places the same trades on the eToro **demo**
account from the window's first fill; it holds cash before that. Its fills are reported next to the
paper result and **do not decide pass or fail**. No real money is involved: the code
hard-blocks `ETORO_ENV=real`.

Why this cell exists. The study found that on the ETF product this rule is the first in this
repository to beat holding SPY over the long run after costs, that it beats the frozen SMA50 rule on
the same ETF in 19 of 26 years with six times fewer trades, and that the CFD product is dominated by
financing. It also found the edge is concentrated in three crashes and the one-year odds are modest.
This test records whether a year nobody has seen agrees. The **variant was fixed in code before the
study's first run** and is not the best variant on the historical data; it stays.

> [!IMPORTANT]
> **Do not edit sections 1–4 after 2026-10-21.** Any change restarts the test from the date of the
> change, under a new dated file. Results go in a separate report that links here. From this date
> the arithmetic of [`shared/pkg/rotation`](../../shared/pkg/rotation/rotation.go) and
> [`cfdsim.SimulateRotation`](../../shared/pkg/cfdsim/rotation.go) is frozen like `dailyrule`'s.

---

## 1. Rule (frozen)

| Item | Value |
|---|---|
| Assets | eToro **QQQ** (instrument id 3006) and **SPY** (id 3000), ETFs at **x1**, settlement **real**; and **cash** (USD) |
| Bars | eToro live `OneDay` candles for each ETF, read with [`etorodaily`](../../shared/pkg/etorodaily/etorodaily.go) (UTC days, NYSE trading days only, today's bucket excluded), **aligned on common dates** (`rotation.Align`). Raw closes |
| Decision days | the **last NYSE trading day of each calendar month** (`rotation.IsMonthEnd`: the next bar, or for the latest bar the next `mktcal` trading day, is in another month). No decision on any other day |
| Signal | **Relative momentum:** each asset's return over the **252 bars ending 21 bars ago** (the "12-1" lookback: `Lookback 252, Skip 21`). Pick the higher. **Absolute momentum:** hold it only if that return is **> 0** (`Hurdle nil`); otherwise cash. Engine: `rotation.Targets(bars, rotation.Params{Lookback: 252, Skip: 21})`, variant `dm_12_1` |
| Timing | Decide at the month-end bar's close, fill at the **next bar's open**; the target holds until the next month end |
| Side / size | One position at a time, long or cash, x1, all-in / all-out, **1,100 USD**. A switch QQQ → SPY (or back) at one open closes one and opens the other from the proceeds |
| Simulator | [`cfdsim.SimulateRotation`](../../shared/pkg/cfdsim/rotation.go) with `FeePerLeg` and no `NightlyRate`, through `cmd/rotation-research -forward`, as of the commit that adds this file |
| Warm-up | 273 aligned bars; the capture has 995 |

**State at registration** (`cmd/rotation-research -state` on the capture of 2026-10-21, last closed bar
2026-10-20; registered snapshot [`daily_qqq.csv`](../etoro/evidence-2026-10-21/daily_qqq.csv) sha256
prefix `470aee7a745e1200`, [`daily_spy.csv`](../etoro/evidence-2026-10-21/daily_spy.csv) `f79a9390ae099f78`):

| Last decision bar | QQQ 12-1 momentum | SPY 12-1 momentum | Hurdle | Target in force | Next decision bar |
|---|---|---|---|---|---|
| 2026-09-30 | **+25.5 %** | +19.1 % | 0 % | **QQQ** | 2026-10-30 |

## 2. Window and evaluation dates (frozen)

- **Forward window: 2026-11-02 → 2027-10-29** (NYSE trading days only; twelve month-end decisions). The
  first decision inside the window is taken at the **2026-10-30** close and filled at the 2026-11-02
  open; the paper account is **in cash before that fill** (it does not inherit the QQQ target in force
  at registration, so the window's first fill is the rule's own first decision on unseen data).
- **Interim look: 2027-04-30.** Report only. **No decision is taken on it**, in either direction.
- **Primary evaluation: 2027-10-29.**
- No early stopping on good results. Stopping on bad results only with a written reason. A stop of any
  demo executor does **not** stop this paper test.

## 3. Costs (frozen on 2026-10-21)

Identical to the ETF cell's §3, from the same in-session capture:

| Item | Primary | Secondary (pessimistic) |
|---|---|---|
| Commission per order | **15 bps of notional** (1.65 USD on 1,100 USD; proportional) | ×2 |
| QQQ bid/ask, round trip | **0.28 bps** | ×2 |
| SPY bid/ask, round trip | **0.26 bps** | ×2 |
| Overnight financing | **none** (x1 real asset) | same |
| Dividends | **not in any leg** of the judged comparison (rule, QQQ hold, SPY hold and SMA50-on-QQQ all use the same eToro bars). Reported separately by the ETF cell |

- **Source:** [`costs.json`](../etoro/evidence-2026-10-21/costs.json), [`rates.json`](../etoro/evidence-2026-10-21/rates.json), [`roundtrip.json`](../etoro/evidence-2026-10-21/roundtrip.json).
- **Measured on demo, reported, does not decide:** the demo account charged no commission on the capture's round trip ([`fees.md`](../etoro/evidence-2026-10-21/fees.md)). The frozen costs are what a real account would pay per eToro's preview.
- **Fee changes during the window:** report both; **the frozen costs decide pass or fail.**

## 4. Hypotheses and pass criteria (frozen)

Judged on eToro bars with primary costs, from equity 1.0, over the window.

| # | Hypothesis | Pass if, at 2027-10-29 | Base rate: calendar years 2001–2026 | Base rate: rolling 252-day windows (305) |
|---|---|---|---|---|
| **H1** | The rule reduces risk | Rotation max drawdown **<** SPY hold max drawdown | 10 / 26 | 33.8 % |
| **H2** | The rule beats the broad benchmark | Rotation return **>** SPY hold return | **15 / 26** | **56.4 %** |
| **H2b** | The rule beats the honest alternative | Rotation return **>** QQQ hold return | 6 / 26 | 27.2 % |
| **H3** | The timing is not luck | Rotation beats **≥ 95 %** of 2,000 permutations of its own monthly targets (seed 1); **n/a** if one target held all window | 0 / 17 applicable | — |
| **H4** | The rule beats the one already under test | Rotation return **>** SMA50 on QQQ, same bars and costs | **19 / 26** | **66.9 %** |

Base rates: [`evidence-2026-10-21/rotation-report.txt`](evidence-2026-10-21/rotation-report.txt)
(ETF product, dividend-adjusted Yahoo bars, 15 bps per order). Median one-year difference: **+1.4 pp
vs SPY, 0.0 pp vs QQQ** (the rule is QQQ in most calm years).

A dry run of this exact procedure over the **already seen** year 2025-10-21 → 2026-10-20, on eToro's
own ETF bars with the frozen costs ([`evidence-2026-10-21/rotation-forward-dryrun.txt`](evidence-2026-10-21/rotation-forward-dryrun.txt)):
QQQ held throughout (no switch); rotation +17.8 % = QQQ +17.8 %, SPY +14.7 %, SMA50-on-QQQ +1.8 %;
drawdown 12.2 % vs SPY 9.2 %. **H2 and H4 pass; H1 and H2b fail; H3 n/a.** That dry run checks the
procedure; it is not evidence.

How to read the outcome:
- **H2 and H2b pass:** the rule earned its keep against both benchmarks; worth designing a small, capped real-account version. Separate decision; real trading stays blocked in code.
- **H2 passes, H2b fails:** the expected case (prior ≈ 56 % · 27 %): beats the broad market, not the growth index it mostly holds. Report; a second year before any further step.
- **H2 fails:** close this variant.
- H1, H3 and H4 are reported. H3 is informative only over several years; H4 is the comparison with the SMA50 ETF cell running on the same bars.

---

## 5. Procedure

```bash
cd services/strategy-executor
set -a; . ../../.env.etoro.local; set +a                 # demo keys; git-ignored
E=../../docs/backtest-readiness/evidence-<date>
go run ./cmd/etoro-spike -out $E/etoro -symbols QQQ,SPY -benchmarks "" -amount 1100
go run ./cmd/rotation-research -data ../../docs/backtest-readiness/evidence-2026-10-10/data \
  -etoro-etf $E/etoro -out $E -forward 2026-11-02:2027-10-29 -primary dm_12_1 \
  -etf-fee-bps 15 -spread-bps-qqq 0.28 -spread-bps-spy 0.26 -sims 2000 -seed 1 \
  -costs-status "FROZEN 2026-10-21" > $E/rotation-forward.txt
```

- `forward.json` holds both scenarios: primary decides; pessimistic (commission ×2, spread ×2) is reported. `targets_in_force` lists every switch.
- For the interim look use `-forward 2026-11-02:2027-04-30`.
- `-state` prints the rule's state at the latest closed bar (what the next month end will decide from).
- Commit the capture, `rotation-forward.txt`, `forward.json` and a `SHA256SUMS`. Run `scripts/check-no-etoro-secrets.sh` before committing.

**Data integrity:** compare the bars before 2026-10-21 with the registered snapshot (sha256 prefixes in §1). The result stands only if differences do not change the rule's target on any decision day.

**Demo executor.** `etoro-daily-executor -strategy dm_12_1` takes the two books together: at 09:41 New York it reads both, aligns them, applies the rule once and runs the book the rule leaves before the one it enters, from its own ledger directory (`etoro-daily-data/demo-rotation/`), with the same idempotent-order, reconcile, risk (`EtoroDemoETFPolicy`) and halt machinery as the daily executors. On a day without a change it records the day and does nothing; before 2026-11-02 it holds cash. Runbook §11.

## 6. Execution incidents (demo executor; appended as they happen, never edited)

The paper account in §1–§4 decides pass or fail and is unaffected by anything below. The window
opens on 2026-11-02; the runs below were pre-window runs that record the day and hold cash.

### 6.1 2026-10-22: run refused by the ETF cell's SPY position (recorded 2026-10-25)

`[spy] FAILED: ledger says flat but the account holds position(s) 3623764792 on SPY: resolve by
hand, nothing sent`. The position was the **SMA50 ETF cell's** SPY, opened that morning on the
same demo account; this cell's eligibility rule ("the executor is the only actor on its
instruments") treated it as foreign and recorded nothing for 2026-10-22. Fixed 2026-10-25: each
cell names its siblings' ledgers (`ETORO_SIBLING_LEDGERS`); a position a sibling's ledger holds is
left alone and noted on the day's line; a position no ledger holds is still refused.

### 6.2 2026-10-23: run stopped by the bar-revision guard (recorded 2026-10-25)

`[dm_12_1] FAILED: QQQ: two reads of eToro's bars differ by 6.4 bps on some close (tolerance 5):
not acting on it`; nothing recorded for 2026-10-23. The 5 bps figure was the CFD cell's; eToro's
two versions of ETF history differ by more (SPY 4.68 bps the same days). Since 2026-10-25 the
guard is 15 bps for ETF bars; the two reads must still agree on the rule's target and a genuine
revision still stops the run (SMA50 ETF pre-registration §6.1 has the detail).

Both days were outside the window and hold-cash days: no decision was missed and the paper
account is unaffected. The two ledger lines are permanently absent; this note stands in for them.

