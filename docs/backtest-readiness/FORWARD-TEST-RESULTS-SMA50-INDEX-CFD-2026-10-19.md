# Forward-Test Results — SMA50 Trend Rule on eToro Index CFDs — Execution Report, Week 1

Report date: **2026-10-19** (after the 09:35 New York run and the 15:00 UTC risk report)  
Covers: demo executor runs **2026-10-12 → 2026-10-19** (forward paper days 2026-10-13 → 2026-10-16, four bars)  
Branch: `feat/etoro` @ `0bd94ee` · Ledger `etoro-daily-data/demo/ledger.jsonl`, 13 records, sha256 prefix `2104c8bb54042af9`  
Pre-registration: [`FORWARD-TEST-PREREGISTRATION-SMA50-INDEX-CFD-2026-10-12.md`](FORWARD-TEST-PREREGISTRATION-SMA50-INDEX-CFD-2026-10-12.md) (§1–4 frozen, unchanged)  
Runbook: [`docs/etoro/ETORO-DAILY-EXECUTOR.md`](../etoro/ETORO-DAILY-EXECUTOR.md) · Risk report: `etoro-daily-data/demo/risk/2026-10-19/risk.md` (S3 copy under `daily-executor/etoro-demo/risk/2026-10-19/`)

> [!IMPORTANT]
> **This is not an evaluation.** The hypotheses H1–H3 are judged on the paper series at the primary
> date **2027-10-12**; the interim look is **2027-04-12** and takes no decision. This report is the
> pre-registration's §5 *execution note*: the demo executor ran, so its fills, the spreads paid and
> the overnight fees charged are reported **against the frozen costs**, from the ledger and eToro's
> account. Four forward days say nothing about the rule. They say a lot about the costs.

## 1. What the rule did

Both instruments were long at registration (§1). The window opened into a down week: NSDQ100 fell
5.5 % and SPX500 2.0 % from the registration close (2026-10-09) to the last bar in this report
(2026-10-16). The rule went flat on both within four forward days, as a trend rule does in a
pull-back.

| Bar (decision) | NSDQ100 close / SMA50 | Signal | SPX500 close / SMA50 | Signal |
|---|---|---|---|---|
| 2026-10-09 (registration) | 30,893.30 / 29,776.03 | long | 7,816.31 / 7,697.41 | long |
| 2026-10-12 | 30,649.50 / 29,824.42 | long | 7,806.65 / 7,703.74 | long |
| 2026-10-13 | 30,561.70 / 29,859.52 | long | 7,806.05 / 7,707.80 | long |
| 2026-10-14 | 29,917.20 / 29,863.59 | long | 7,715.66 / 7,707.17 | long |
| 2026-10-15 | 29,868.70 / 29,871.96 | **flat** (by 3.3 pts) | 7,738.57 / 7,707.34 | long |
| 2026-10-16 | 29,193.50 / 29,867.89 | flat | 7,657.86 / 7,706.39 | **flat** |

NSDQ100 crossed under its SMA50 by 0.01 % at the 2026-10-15 close. That is the kind of signal the
two-read check exists for; both reads agreed (§4).

## 2. Paper account (the judged series) — reporting only

From the executor's `paper` block at the 2026-10-19 run (marked at the 2026-10-16 close, before
the SPX500 paper sell fills at the 2026-10-19 open). Frozen costs: spread 15.5 / 14.8 bps round
trip, 2.3 bps per calendar night.

| Book | Forward days | Position | Round trips | Equity if closed | CFD hold | Max drawdown | Financing |
|---|---:|---|---:|---:|---:|---:|---:|
| NSDQ100 | 4 | flat since 2026-10-16 open | 1 | **0.9734** | 0.9506 | 2.66 % | 0.07 % |
| SPX500 | 4 | long, sell pending at 2026-10-19 open | 1 (closing) | **0.9792** | 0.9792 | 2.01 % | 0.07 % |

Both paper accounts opened at the 2026-10-13 open (the first in-window fill) and are down with the
market. The NSDQ100 exit saved 2.3 pp against the hold over two days. None of this is evidence for
or against H1–H3.

## 3. Demo account: every order against the frozen costs

Five orders in the week: four by the executor (`sma50-*`), one from the console (`ui-*`, §6). All
`executed`; none duplicated, none resumed from an intent. Quoted spreads are eToro's bid/ask in
the cash session at the moment of the check; "spread cost" is eToro's own cost preview for the
leg.

| Fill (NY date) | Book | Kind | Order | Position | Units | Price | Quoted spread | Fill vs mid | Fill vs decision close | Spread cost USD (bps/leg) | Latency |
|---|---|---|---|---|---:|---:|---:|---:|---:|---:|---:|
| 2026-10-12 | NSDQ100 | open | 388031762 | 3616683146 | 0.035799 | 30,726.90 | 0.23 bps | −5.22 bps | −53.9 bps | 0.04 (0.36) | 4.6 s |
| 2026-10-12 | SPX500 | open | 388031765 | 3616683295 | 0.140735 | 7,816.07 | 0.38 bps | −1.73 bps | −0.3 bps | 0.04 (0.36) | 3.7 s |
| 2026-10-16 | NSDQ100 | close | 389021477 | 3616683146 | 0.035799 | 29,770.50 | 0.64 bps | +5.96 bps | +32.9 bps | 0.00 | 1.3 s |
| 2026-10-19 | SPX500 | close | 389373343 | 3616683295 | 0.140735 | 7,699.85 | 0.39 bps | +0.19 bps | −54.8 bps | 0.00 | 3.3 s |
| 2026-10-19 | NSDQ100 | open (console) | 389430462 | 3622570112 | 0.036950 | 29,769.80 | 0.24 bps | −0.79 bps | +197.4 bps¹ | — | 2.0 s |

¹ The console line's reference close is the last recorded bar (2026-10-16, 29,193.50); the market
had gapped up by the time of the trade. Not an execution cost.

### 3.1 Frozen vs measured

| Item | Frozen (§3, decides pass/fail) | Measured, week 1 | Ratio |
|---|---|---|---|
| NSDQ100 spread per leg | **7.75 bps** | 0.18 bps mean (0.36 at the open, 0 at the close as eToro quotes it) | ×0.023 |
| SPX500 spread per leg | **7.40 bps** | 0.18 bps mean | ×0.025 |
| Quoted bid/ask width, cash session | 15.5 / 14.8 bps (weekend session capture) | 0.23–0.64 bps, mean 0.41 bps | ×0.03 |
| Overnight fee, per calendar night at x1 | **2.3 bps** (0.253 USD on 1,100) | **not visible in cash** — see §3.2 | — |
| ETF commission (benchmark) | 1.50 USD per order | not traded | — |

The frozen cost model is pessimistic by a factor of about 40 on spreads, as the pre-registration
already noted when it froze the weekend values by its §3 fallback. The frozen figures still
decide. The risk report's Monte Carlo (`risk.md` §3) now prices both scenarios: moving from
frozen to measured spreads raises the median one-year trend return by about 1.5 pp on each
instrument and the probability of a shallower drawdown than the hold by about 4 pp; it does not
change the picture against the ETF hold (≈20 % either way).

### 3.2 Financing: the demo account charged nothing visible

Cash moved exactly by the price P&L of the two round trips:

| | USD |
|---|---:|
| Cash before the opens (2026-10-12 13:35) | 99,993.93 |
| Cash after both opens | 97,793.95 (−2,199.98 = 2 × 1,099.99) |
| Cash after both closes (2026-10-19 13:35) | 99,943.33 |
| **Net cash change** | **−50.60** |
| Price P&L: NSDQ100 0.035799 × (29,770.50 − 30,726.90) | −34.24 |
| Price P&L: SPX500 0.140735 × (7,699.85 − 7,816.07) | −16.36 |
| **Sum of price P&L** | **−50.59** |
| Modelled financing at 2.3 bps/night: 4 + 7 nights (one weekend each) | 2.78 |

Over eleven position-nights including two weekends the account shows no overnight deduction, to
the cent. eToro's cost preview at the opens quoted an overnight fee of 0.26 / 0.25 USD per night,
in line with the frozen 0.253. Either the demo account does not charge financing, or it books it
outside the cash balance (the position's `unrealizedPnL` object, or the trading history's fees
field). **Pending:** read the two closed positions' fee fields from the trading history (the
`positions` view in progress in `ui-api` does this) and record the answer here. Until then the
paper account keeps charging the frozen 2.3 bps, as it must, and the demo-versus-paper tracking
(risk report §4: −55 bps NSDQ100, +43 bps SPX500) carries a 2.78 USD unknown.

### 3.3 Execution slippage versus the paper fill

The paper fills at the next eToro bar's open (00:00 UTC); the demo fills at 09:35 New York, 13.5
hours later, inside the cash session. The gap is reported (§5 bar-source note), not judged.

| Fill | Fill vs the bar's open |
|---|---:|
| 2026-10-12 NSDQ100 open | −55.7 bps (demo paid less) |
| 2026-10-12 SPX500 open | −2.5 bps |
| 2026-10-16 NSDQ100 close | +41.1 bps (demo received more) |
| 2026-10-19 SPX500 close | n/a (bar still open at report time) |

Note also that the demo was long one day before the paper window: the executor brought the account
to the rule's target on 2026-10-12 from the 2026-10-09 decision, while the paper account's first
fill is 2026-10-13. That one day explains most of the tracking difference in the risk report.

## 4. Data integrity (§5)

- **Registered snapshot vs the bars the executor saved on 2026-10-16:** for every bar before
  2026-10-10, **0 close differences** on NSDQ100 (955 bars compared) and **0** on SPX500 (959).
  The live route's history start slid forward by nine bars (its window rolls); the SMA50 needs 50.
- **Two-version reads within a run** (the executor reads twice): NSDQ100 up to 16 revised closes,
  max 1.26 bps; SPX500 up to 13, max 0.92 bps; several runs saw none. No read pair disagreed on a
  signal and no run was skipped. The 2026-10-15 NSDQ100 flip (3.3 points, 1.1 bps from the SMA)
  was agreed by both reads.
- Recomputing the SMA50 signal from the registered snapshot spliced with the new bars gives the
  same position as the saved series on every forward day. **The result stands** by the §5 rule.
- Candle files are saved per run with their sha256 prefix in the ledger (`candles.sha256_prefix`).

## 5. Operational incidents

Three of the five reconciles after an order needed more than one read. None was a drift in the
books; all were eToro's read-after-write lag, now waited out in code.

| Date | What | Resolution | Code |
|---|---|---|---|
| 2026-10-12 | `missing_position` ×2 at 13:35:14 UTC: the portfolio did not show either fill 5 s after the adapter had resolved their position ids; cash unchanged. Present 80 minutes later. | Re-check 14:56:22 UTC **clean** (`2026-10-12.recheck.json`). Reconcile taught to re-read the portfolio for up to 60 s after a recent order. | `efb5ac7` |
| 2026-10-16 | Portfolio settled after 3 reads (10 s); `close_not_in_history`: the closed NSDQ100 position was gone from the portfolio but not yet in the trading history. First `ui-alerts` e-mail of the soak, 13:45 UTC. | Re-check 16:43:43 UTC **clean**. Reconcile taught to wait for the history too. | `4db4c9d` |
| 2026-10-19 | Portfolio after 3 reads (10 s), history after 9 reads (40 s); `settle_waited_s: 50`. | **Clean inside the run**, two `warn` findings, no alert. First run on the binaries rebuilt 2026-10-16. | — |

P3's exit criterion (five consecutive agreeing trading days) was met on 2026-10-16 under the
re-check rule; the 2026-10-19 run is the first with both lags handled in-run. The kill-switch
drill passed 40/40 on 2026-10-08 and the ledger was never halted during the week. The watchdog,
cron installer and S3 copy went in on 2026-10-18 (P6); `upload=ok` on every run since.

## 6. Console trade (outside the paper account)

The first assisted trade from the operator console (runbook §8, protection phases A–B of
[`ETORO-ASSISTED-TRADE-PROTECTION-PLAN-2026-10-19.md`](../etoro/ETORO-ASSISTED-TRADE-PROTECTION-PLAN-2026-10-19.md))
was placed on 2026-10-19 at 15:46:34 UTC, after the day's run and inside the cash session.

| Item | Value |
|---|---|
| Ledger line | `source: "ui"`, `signal: manual`, `prev_signal: flat`, `client_ref ui-nsdq100-2026-10-19-open-13868e`, operator "Diego Gallo", reason "Testing Manual Order Placement" |
| Order | 389430462 → position 3622570112, 0.036950 units at 29,769.80, 1,099.99 USD x1 |
| Quote / fill | bid 29,771.80 / ask 29,772.50 (0.24 bps); fill −0.79 bps vs mid |
| Protection | profile **custom**: stop 29,177.05 (−2.0 %), target 30,367.95 (+2.0 %), `stop_type fixed`; ATR 509.6 (1.75 %); max loss at the stop 21.90 USD; policy `etoro-demo-2026-10-19` |
| Against the rule | Yes (rule flat). The executor brings it back to flat at the 2026-10-20 09:35 run unless the ledger is halted or the venue closes it first. |
| Attempts before it | 12:46 and 13:02 UTC prepare → `market_closed` (correct, pre-session); 14:19 prepare rejected `unknown field "profile"` (a `ui-api` built before phase B; restarted); 15:45 place refused `token expired` (120 s TTL). All in `ui-audit.jsonl`. |

Because protection phase C (venue-close detection in the executor) is not built, a stop-out or
take-profit overnight would leave the ledger long and the account flat, and the 2026-10-20 run
would stop for manual resolution (runbook §5). Whatever happens to this position is recorded in
the next report and does not touch the paper series.

## 7. What is pending for the next report

1. **Financing from the trading history** (§3.2) for positions 3616683146 and 3616683295: fee
   fields, and whether they explain the 2.78 USD the model expects.
2. **Resolution of the console position** 3622570112: closed by the rule on 2026-10-20, by the
   venue at the stop/target, or by hand.
3. The 2026-10-19 SPX500 paper sell at the bar open, closing the paper round trip.
4. A measured-costs line to accompany the frozen one in the operator UI's forward-test cards (the
   risk report already carries both).

## 8. Reproduce

```bash
cd services/strategy-executor
# fills, quotes, costs, candles hashes: every demo order in the ledger
python3 -c 'import json;[print(json.dumps({k:r[k] for k in ("book","recorded_at","decision","demo")})) for r in map(json.loads,open("etoro-daily-data/demo/ledger.jsonl")) if (r.get("demo") or {}).get("order")]'
# lag incidents
ls etoro-daily-data/demo/reconcile/
# fill quality and frozen-vs-measured Monte Carlo (section 3 and 5 of risk.md)
cat etoro-daily-data/demo/risk/2026-10-19/risk.md
# registered snapshot vs saved bars
sha256sum ../../docs/etoro/evidence-2026-10-10/daily_nsdq100.csv ../../docs/etoro/evidence-2026-10-10/daily_spx500.csv   # 09e8729c…, 0610258c…
```
