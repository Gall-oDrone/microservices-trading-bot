# Forward-Test Pre-Registration — SMA50 Trend Rule on eToro Index CFDs NSDQ100 and SPX500

Date registered: **2026-10-10** (rule, window, hypotheses) · cost block frozen **2026-10-12** (see §3)  
Repository: `microservices-trading-bot`  
Branch: `feat/etoro` (porting plan phase **P2**)  
Audience: Whoever evaluates this in 2027  
Context: [`INDEX-CFD-SMA50-STUDY-2026-10-10.md`](INDEX-CFD-SMA50-STUDY-2026-10-10.md) ·
eToro facts: [`docs/etoro/evidence-2026-10-10/README.md`](../etoro/evidence-2026-10-10/README.md) ·
earlier tests: [`FORWARD-TEST-PREREGISTRATION-SMA50-2026-09-27.md`](FORWARD-TEST-PREREGISTRATION-SMA50-2026-09-27.md),
[`FORWARD-TEST-PREREGISTRATION-SMA50-BTCUSD-2026-09-29.md`](FORWARD-TEST-PREREGISTRATION-SMA50-BTCUSD-2026-09-29.md)

This test is judged **on paper**, from eToro's own daily bars. A demo-account executor (plan phase P3)
may place the same trades on the eToro **demo** account. Its fills are reported next to the paper
result, but they **do not decide pass or fail** (§5). No real money is involved: the code
hard-blocks `ETORO_ENV=real`.

The rule was not chosen because it worked on these instruments. It is the frozen SMA50 rule from the
Bitso tests, reused unchanged (decision D3). The study it is registered against says it is
**unlikely** to beat holding QQQ/SPY. This test records whether data nobody has seen yet agrees.

> [!IMPORTANT]
> **Do not edit sections 1–4 after 2026-10-12.** The one allowed change is in §3: the cells marked
> **PROVISIONAL** are replaced **once** by the weekday re-measure on 2026-10-12. That replacement is
> committed before the 2026-10-12 eToro bar closes (00:00 UTC on 2026-10-13). Any other change
> restarts the test from the date of the change, under a new dated file. Results go in a separate
> report that links here.

---

## 1. Rule (frozen)

| Item | Value |
|---|---|
| Instruments | eToro **NSDQ100** (instrument id 28) and **SPX500** (id 27), index CFDs, tested separately |
| Bars | eToro live `OneDay` candles, read with [`etorodaily`](../../shared/pkg/etorodaily/etorodaily.go). Each bar is a UTC calendar day, and only **NYSE trading days** are kept ([`mktcal`](../../shared/pkg/mktcal)). Weekend and holiday quotes are dropped. Today's in-progress bucket is excluded. |
| Signal | **Long while the close is above the simple average of the last 50 closes (today included); otherwise flat.** Engine: `dailyrule.Trend(bars, 50)` |
| Timing | Decide at bar *t*'s close, fill at bar *t+1*'s **open** |
| Side / size | Long or flat, **leverage x1**, all-in / all-out. **1,100 USD** per instrument. When flat, the capital sits as account cash in USD |
| Simulator | [`shared/pkg/cfdsim`](../../shared/pkg/cfdsim/cfdsim.go) through [`cmd/index-research -forward`](../../services/strategy-executor/cmd/index-research/forward.go), as of the commit that adds this file |
| Warm-up | eToro bars from 2022-12 (the start of the live route's history). The 50-bar SMA needs no earlier data |

**State at registration**, from the 2026-10-09 capture
([`daily_nsdq100.csv`](../etoro/evidence-2026-10-10/daily_nsdq100.csv),
[`daily_spx500.csv`](../etoro/evidence-2026-10-10/daily_spx500.csv)):

| Instrument | Close 2026-10-09 | SMA50 | Rule |
|---|---|---|---|
| NSDQ100 | 30,893.30 | 29,776.03 | **long** |
| SPX500 | 7,816.31 | 7,697.41 | **long** |

Both instruments are long going into the window. Whether they stay long at the first forward
decision follows mechanically from the 2026-10-12 close.

## 2. Window and evaluation dates (frozen)

- **Forward window: 2026-10-13 → 2027-10-12** (NYSE trading days only). The first decision inside
  the window is taken at the 2026-10-12 close and filled at the 2026-10-13 open. This is after the
  cost freeze.
- **Interim look: 2027-04-12.** Report only. **No decision is taken on it**, in either direction.
- **Primary evaluation: 2027-10-12.**
- No early stopping on good results. Stopping on bad results is allowed only with a written reason.
  A stop of the demo executor (P3) does **not** stop this paper test.

## 3. Costs (frozen on 2026-10-12)

| Item | Primary | Secondary (pessimistic) |
|---|---|---|
| NSDQ100 spread, round trip, half charged on each leg | **15.5 bps** — PROVISIONAL | ×2 |
| SPX500 spread, round trip, half charged on each leg | **14.8 bps** — PROVISIONAL | ×2 |
| CFD overnight fee at x1, per **calendar night** held (a weekend counts three nights) | **2.3 bps** of the position — PROVISIONAL | ×1.5 |
| Benchmark ETF (QQQ / SPY): commission per order on 1,100 USD | **1.50 USD** (13.6 bps per leg) | same |
| Benchmark ETF spread, round trip | QQQ 0.13 bps, SPY 0.26 bps | same |

- **Provisional source:** the demo capture on Saturday 2026-10-10 ([`costs.json`](../etoro/evidence-2026-10-10/costs.json)), taken while the CFDs were quoting outside the cash session.
- **Freeze:** a demo round trip and cost preview at 1,100 USD inside the NYSE cash session on Monday 2026-10-12, saved to `docs/etoro/evidence-2026-10-12/`. Its values replace the PROVISIONAL cells; if it cannot be taken that day, the provisional values become final.
- **Fee changes during the window:** if eToro changes its spreads or overnight fees, report both the frozen and the actual costs. **The frozen costs decide pass or fail.**
- **Not modelled:** dividend adjustments on long index-CFD positions, because eToro's treatment is unverified. If eToro is found to credit them, report an adjusted figure as well; the frozen model still decides.

## 4. Hypotheses and pass criteria (frozen)

Each instrument is judged separately, on eToro bars, using **primary costs**. Returns are in USD,
starting from equity 1.0. The ETF benchmark is the Yahoo **dividend-adjusted** QQQ / SPY series,
held over the same dates. This is what an eToro user could buy instead at x1 (it settles as the real
ETF and pays no financing).

| # | Hypothesis | Pass if, at 2027-10-12 | Base rate: calendar years (NSDQ100 2000–2026 · SPX500 1994–2026) | Base rate: rolling 252-day windows |
|---|---|---|---|---|
| **H1** | The rule reduces risk | Trend max drawdown **<** CFD hold max drawdown | 18 / 27 · 21 / 33 | 70.3 % · 69.9 % |
| **H2** | The rule beats holding the same CFD | Trend return **>** CFD hold return (both after spread and financing) | 8 / 27 · 7 / 33 | 30.7 % · 23.1 % |
| **H2b** | The rule beats the honest alternative | Trend return **>** QQQ / SPY hold return (dividends, commissions) | 5 / 27 · 3 / 33 | 20.9 % · 11.6 % |
| **H3** | The timing is not luck | Trend beats **≥ 95 %** of 2,000 random strategies with the same number of trips (seed 1) | 0 / 27 · 1 / 33 | — |

Base rates are from [`evidence-2026-10-10/report.txt`](evidence-2026-10-10/report.txt) (index-proxy
bars, rate-linked financing). On eToro's own bars the rule earned 4.2–4.6 pp less than on the proxy
over the same dates, so these rates are, if anything, generous. A dry run of this exact procedure
over the **already seen** year 2025-10-10 → 2026-10-09 (provisional costs) gave:
- NSDQ100: H1 passed (drawdown 13.9 % vs 15.6 %); H2, H2b and H3 failed (trend +1.1 %, CFD hold +13.8 %, QQQ +23.1 %).
- SPX500: all four failed (trend −5.2 %, CFD hold +7.0 %, SPY +16.8 %).

That dry run only checks the procedure; it is not evidence.

How to read the outcome:
- **H1 and H2b pass on an instrument:** worth designing a small, capped real-account version for
  that instrument. That needs a separate decision: real trading stays blocked in code until then.
- **H1 passes and H2b fails:** a drawdown tool, not a return tool. The operator UI shows the
  trade-off ("drawdown saved" against "return given up versus QQQ/SPY"). No real-account version.
- **H1 fails:** close the index-CFD variant of this rule.
- **One instrument passes and the other fails:** that counts as one pass out of two tries. The
  passing instrument may proceed only if its pessimistic-cost H2b also passes.
- H2 and H3 are reported; they are not expected to pass in a single year.

---

## 5. Procedure

```bash
cd services/strategy-executor
set -a; . ../../.env.etoro.local; set +a      # demo keys; git-ignored, never committed
E=../../docs/backtest-readiness/evidence-<date>
go run ./cmd/etoro-spike -out $E/etoro -amount 1100
go run ./cmd/index-research -fetch -data $E/data -etoro $E/etoro -out $E \
  -forward 2026-10-13:2027-10-12 \
  -spread-bps-nsdq100 <frozen> -spread-bps-spx500 <frozen> -overnight-bps <frozen> \
  -size 1100 -etf-fee 1.5 -sims 2000 -seed 1 -costs-status "FROZEN 2026-10-12" > $E/forward.txt
```

- `forward.json` holds both scenarios: primary decides; pessimistic (spread ×2, fee ×1.5) is reported.
- For the interim look use `-forward 2026-10-13:2027-04-12`.
- Commit the capture, the Yahoo CSVs, `forward.txt`, `forward.json` and a `SHA256SUMS` under `docs/backtest-readiness/evidence-<date>/`.
- Run `scripts/check-no-etoro-secrets.sh` before committing.

**Data integrity:** compare eToro's bars before 2026-10-10 with the registered snapshot:
- [`daily_nsdq100.csv`](../etoro/evidence-2026-10-10/daily_nsdq100.csv), sha256 prefix `09e8729ca926d90e`;
- [`daily_spx500.csv`](../etoro/evidence-2026-10-10/daily_spx500.csv), sha256 prefix `0610258c4f1daaa4`.

Record any differences in the report. The result stands only if they do not change the rule's
position on any forward day.

Expect small differences. On 2026-10-10, eToro's live daily route answered with two versions of
recent history about an hour apart:
- 16 NSDQ100 closes and 13 SPX500 closes between 2026-08-26 and 2026-09-25 differed;
- the largest difference was 1.3 bps, opens, highs and lows were identical, and no SMA50 position changed;
- the SMA50 in §1 (29,776.03) is from one version; the other gives 29,776.38.

The executor therefore reads the bars twice per run. It does not act if the two reads disagree on the
signal or differ by more than 5 bps, and it records the size of the difference in its ledger
(`candles.revised_closes`, `candles.max_revision_bps`).

**Bar-source note:** an eToro daily bar closes at 00:00 UTC, about four hours after the NYSE
close. The paper fill is at the next bar's open, which the CFD quotes continuously. A demo executor
that trades inside the cash session fills at a different time and price. That gap is measured and
reported as execution slippage. It is not part of the pass criteria.

**Execution note:** if the P3 demo executor runs, report its fills, spreads paid and overnight
fees charged. Report them against the frozen costs, from eToro's trade history. That turns the
assumed costs into measured ones.
