# Forward-Test Pre-Registration — SMA50 Trend Rule on the ETFs QQQ and SPY at x1 (real asset)

Date registered and costs frozen: **2026-10-21** (in-session capture 15:32–15:34 UTC, inside the NYSE cash session)
Repository: `microservices-trading-bot` · Branch: `feat/etoro` · Next-steps plan, cell **#1**
Audience: Whoever evaluates this in 2027
Context: [`INDEX-CFD-SMA50-STUDY-2026-10-10.md`](INDEX-CFD-SMA50-STUDY-2026-10-10.md) §4.2 and §6.3 ·
base rates: [`evidence-2026-10-21/etf-report.txt`](evidence-2026-10-21/etf-report.txt) ·
eToro facts: [`docs/etoro/evidence-2026-10-21/`](../etoro/evidence-2026-10-21/) ·
sibling test: [`FORWARD-TEST-PREREGISTRATION-SMA50-INDEX-CFD-2026-10-12.md`](FORWARD-TEST-PREREGISTRATION-SMA50-INDEX-CFD-2026-10-12.md)

This test is judged **on paper**, from eToro's own daily bars for the ETFs. The demo-account executor
(`cmd/etoro-daily-executor`, instruments `QQQ,SPY`, its own ledger) places the same trades on the eToro
**demo** account; its fills are reported next to the paper result and **do not decide pass or fail**
(§5). No real money is involved: the code hard-blocks `ETORO_ENV=real`.

Why this cell exists. The index-CFD study named "hold QQQ / SPY x1" the honest alternative to the CFD
rule and showed that most of the rule's shortfall is bull-market return it misses, not the CFD's carry
(§4.2: the rule on the ETF itself still trails holding it in 76–84 % of years). This cell runs **the same
frozen rule on the ETF itself**, so the forward year has an apples-to-apples pair — rule-on-ETF against
hold-ETF, no financing on either side, one fee model — and exercises the demo pipeline on a product that
**settles as the real asset**.

> [!IMPORTANT]
> **Do not edit sections 1–4 after 2026-10-21.** Any change restarts the test from the date of the
> change, under a new dated file. Results go in a separate report that links here.

---

## 1. Rule (frozen)

| Item | Value |
|---|---|
| Instruments | eToro **QQQ** (instrument id 3006) and **SPY** (id 3000), ETFs at **x1**, settlement **real** ([`eligibility.json`](../etoro/evidence-2026-10-21/eligibility.json): `real long [1]`, min exposure 10 USD, stop loss allowed 0–100 %, trailing allowed), tested separately |
| Bars | eToro live `OneDay` candles for the ETF itself, read with [`etorodaily`](../../shared/pkg/etorodaily/etorodaily.go): UTC calendar days, **NYSE trading days only**, today's bucket excluded. The route serves 1,000 buckets back to 2022-09-22 with **28 trading days missing per ETF** (mostly Fridays, listed in [`daily_qqq_report.json`](../etoro/evidence-2026-10-21/daily_qqq_report.json)); every gap is shorter than `dailyrule.MaxGapDays`, so the SMA never restarts |
| Signal | **Long while the close is above the simple average of the last 50 closes (today included); otherwise flat.** Engine: `dailyrule.Trend(bars, 50)` on the ETF's **own raw closes** (unadjusted, as the route serves them) |
| Timing | Decide at bar *t*'s close, fill at bar *t+1*'s **open** |
| Side / size | Long or flat, x1, all-in / all-out, **1,100 USD** per instrument. When flat the capital sits as USD cash |
| Simulator | [`shared/pkg/cfdsim`](../../shared/pkg/cfdsim/cfdsim.go) with `FeePerLeg` and no `NightlyRate`, through the executor's `paper` block (`instSpec.Kind == etf`) and [`cmd/etf-trend-research -forward`](../../services/strategy-executor/cmd/etf-trend-research/forward.go), as of the commit that adds this file |
| Warm-up | the 995 eToro bars of the capture; the 50-bar SMA needs no earlier data |

**State at registration**, from the capture of 2026-10-21 (last closed bar 2026-10-20;
[`daily_qqq.csv`](../etoro/evidence-2026-10-21/daily_qqq.csv) sha256 prefix `470aee7a745e1200`,
[`daily_spy.csv`](../etoro/evidence-2026-10-21/daily_spy.csv) sha256 prefix `f79a9390ae099f78`):

| ETF | Close 2026-10-20 | SMA50 | Rule | Previous day |
|---|---|---|---|---|
| QQQ | 722.31 | 726.66 | **flat** | flat |
| SPY | 771.60 | 768.27 | **long** (crossed up on this bar) | flat |

The executor's first demo run (2026-10-22, 09:35 New York) therefore opens SPY and leaves QQQ flat,
unless the 2026-10-21 close says otherwise — that decision is the first one inside the window.

## 2. Window and evaluation dates (frozen)

- **Forward window: 2026-10-22 → 2027-10-21** (NYSE trading days only). The first decision inside the
  window is taken at the 2026-10-21 close and filled at the 2026-10-22 open. This is after the cost freeze.
- **Interim look: 2027-04-21.** Report only. **No decision is taken on it**, in either direction.
- **Primary evaluation: 2027-10-21.**
- No early stopping on good results. Stopping on bad results only with a written reason. A stop of the
  demo executor does **not** stop this paper test.

## 3. Costs (frozen on 2026-10-21)

| Item | Primary | Secondary (pessimistic) |
|---|---|---|
| Commission per order | **15 bps of notional** (eToro's cost preview: 1.65 USD on 1,100 USD; 1.50 USD on 1,000 USD in the 2026-10-10 capture — the fee is proportional, not fixed) | ×2 |
| QQQ bid/ask, round trip | **0.28 bps** (725.22 / 725.24 at 15:32 UTC) | ×2 |
| SPY bid/ask, round trip | **0.26 bps** (772.17 / 772.19) | ×2 |
| Overnight financing | **none** (x1 real asset; preview `overnightFee 0`) | same |
| Dividends | **not in either leg** of the judged comparison: rule and hold use the same eToro bars, which are not adjusted. The Yahoo total-return hold over the same dates is reported next to them (§4, `yahoo_total_return_hold_pct` in `forward.json`). How eToro credits an ETF dividend on demo is recorded when the first one happens |

- **Source:** [`costs.json`](../etoro/evidence-2026-10-21/costs.json), [`rates.json`](../etoro/evidence-2026-10-21/rates.json), [`roundtrip.json`](../etoro/evidence-2026-10-21/roundtrip.json) (one 1,100 USD QQQ open and close through the broker adapter: 1.516906 units at 725.14, settlement `REAL`, idempotent re-open resolved to the same position, close executed, re-close refused with code 741).
- **Measured on demo, reported, does not decide:** the demo account charged **neither** the 1.65 USD commission nor any fee on that round trip (history `fees 0.00`, net +0.09 USD; [`fees.md`](../etoro/evidence-2026-10-21/fees.md)), just as it charged no overnight financing on the CFD positions over 11 nights. The frozen costs are what a real account would pay per eToro's own preview; the paper account charges them.
- **Fee changes during the window:** if eToro changes the commission or the spreads, report both the frozen and the actual costs. **The frozen costs decide pass or fail.**

## 4. Hypotheses and pass criteria (frozen)

Each ETF is judged separately, on eToro bars, with primary costs, from equity 1.0.

| # | Hypothesis | Pass if, at 2027-10-21 | Base rate: calendar years (QQQ 2000–2026 · SPY 1994–2026) | Base rate: rolling 252-day windows |
|---|---|---|---|---|
| **E-H1** | The rule reduces risk | Trend max drawdown **<** ETF hold max drawdown | 17 / 27 · 17 / 33 | 65.2 % · 56.3 % |
| **E-H2** | The rule beats holding the ETF | Trend return **>** ETF hold return (same bars, after commission) | 6 / 27 · 4 / 33 | 23.1 % · 14.1 % |
| **E-H3** | The timing is not luck | Trend beats **≥ 95 %** of 2,000 random strategies with the same number of trips (seed 1) | 1 / 27 · 1 / 33 | — |
| **E-H4** (reported, not judged) | Signal source does not matter | Own-close signal and index-proxy signal agree on ≥ 97 % of forward days | 96–100 % every year | — |

Base rates: [`evidence-2026-10-21/etf-report.txt`](evidence-2026-10-21/etf-report.txt) (signal on own
closes, P&L on dividend-adjusted Yahoo bars, 15 bps per order). Median one-year shortfall of the rule
against holding: **−11.3 pp (QQQ), −9.5 pp (SPY)**; median drawdown saved: 2.4 pp / 1.1 pp.

A dry run of this exact procedure over the **already seen** year 2025-10-21 → 2026-10-20, on eToro's
own ETF bars with the frozen costs ([`evidence-2026-10-21/forward-dryrun.txt`](evidence-2026-10-21/forward-dryrun.txt)):
- QQQ: trend +1.8 % vs hold +17.8 % (Yahoo TR hold +23.0 %); drawdown 12.9 % vs 12.2 %; beats 41 % of random. **E-H1, E-H2, E-H3 fail.**
- SPY: trend +3.8 % vs hold +14.7 % (Yahoo TR +16.9 %); drawdown 6.1 % vs 9.2 %; beats 54 % of random. **E-H1 passes; E-H2, E-H3 fail.**

That dry run checks the procedure; it is not evidence.

How to read the outcome:
- **E-H1 and E-H2 pass on an ETF:** the rule earned its keep on a real asset; worth designing a small, capped real-account version. Separate decision; real trading stays blocked in code.
- **E-H1 passes, E-H2 fails:** the expected case (prior ≈ 60 % · 15–25 %). A drawdown tool, not a return tool.
- **E-H1 fails:** close the ETF variant of this rule.
- **One ETF passes and the other fails:** one pass out of two tries; the passing ETF proceeds only if its pessimistic-cost E-H2 also passes.
- The pair **(this cell, the CFD cell)** answers one more question than either alone: if the CFD rule's H2b fails and this cell's E-H2 fails by a similar margin, the CFD's financing was not the reason; if this cell does markedly better, it was.

---

## 5. Procedure

```bash
cd services/strategy-executor
set -a; . ../../.env.etoro.local; set +a                 # demo keys; git-ignored, never committed
E=../../docs/backtest-readiness/evidence-<date>
go run ./cmd/etoro-spike -out $E/etoro -symbols QQQ,SPY -benchmarks "" -amount 1100
go run ./cmd/etf-trend-research -data ../../docs/backtest-readiness/evidence-2026-10-10/data \
  -etoro $E/etoro -out $E -forward 2026-10-22:2027-10-21 \
  -etf-fee-bps 15 -spread-bps-qqq 0.28 -spread-bps-spy 0.26 -sims 2000 -seed 1 \
  -costs-status "FROZEN 2026-10-21" > $E/forward.txt
```

- `forward.json` holds both scenarios: primary decides; pessimistic (commission ×2, spread ×2) is reported.
- For the interim look use `-forward 2026-10-22:2027-04-21`.
- Commit the capture, `forward.txt`, `forward.json` and a `SHA256SUMS`. Run `scripts/check-no-etoro-secrets.sh` before committing.

**Demo executor.** Same binary and wrapper as the CFD cell, separate ledger, second cron line:

```
CRON_TZ=America/New_York
35 9 * * 1-5  ETORO_EXECUTOR_LEDGER=./etoro-daily-data/demo-etf/ledger.jsonl /path/to/repo/scripts/etoro-daily-executor-run.sh -demo -instruments QQQ,SPY
```

- The ETF ledger directory is never written by the CFD run and vice versa; `LegsOn`, the halt file and the reconcile reports are per ledger.
- **Protection is not part of this cell.** The executor skips the disaster stop and the ratchet for `Kind == etf` (demo.go) and runs ETF-only runs under `risk.EtoroDemoETFPolicy` (`etoro-demo-etf-2026-10-21`: books `qqq` / `spy`, 1,500 USD per leg, one leg per book per day, no stop requirement; added the same day after a dry run showed the CFD policy refusing `qqq` outright). Eligibility allows a stop on x1 real positions; adding one later is a demo-side overlay like the CFD addendum and does not touch §1–4.
- **Data integrity:** compare the bars before 2026-10-21 with the registered snapshot (sha256 prefixes in §1). The executor's two-read check and the recorded-close check apply unchanged. The result stands only if differences do not change the rule's position on any forward day.
- **Bar-source note:** an ETF bucket on eToro is a UTC day; the bar's open is the 09:30 New York open, which is about when the demo executor fills (09:35). Report the measured gap as for the CFD cell.
- **Execution note:** report the demo fills, commissions and any dividend credit against the frozen costs, from eToro's trade history, with `cmd/etoro-fees-check`.
