# eToro demo evidence: 2026-10-21 (ETF cell capture, financing check)

Captured **2026-10-21 15:32–15:40 UTC** (11:32–11:40 New York, inside the NYSE cash session) on the
**demo** account with `cmd/etoro-spike -symbols QQQ,SPY -benchmarks "" -amount 1100 [-roundtrip
-roundtrip-symbol QQQ]` and `cmd/etoro-fees-check`. No credentials or account ids are in these files;
`SHA256SUMS` covers everything. This capture freezes §3 of
[`FORWARD-TEST-PREREGISTRATION-SMA50-ETF-2026-10-21.md`](../../backtest-readiness/FORWARD-TEST-PREREGISTRATION-SMA50-ETF-2026-10-21.md)
and closes §3.2 of [`FORWARD-TEST-RESULTS-SMA50-INDEX-CFD-2026-10-19.md`](../../backtest-readiness/FORWARD-TEST-RESULTS-SMA50-INDEX-CFD-2026-10-19.md).

## 1. What was learned

| Question | Answer | File |
|---|---|---|
| Does the demo account charge overnight financing on the index CFDs? | **No.** 11 position-nights across the two week-1 positions: history `fees 0.00`, model 2.78 USD. Verdict `not_charged`. The frozen 2.3 bps/night stays in the paper account as pre-registered. | `fees.md`, `fees.json` |
| Does the live daily route serve the ETFs? | **Yes.** 1,000 UTC-day buckets each, 2022-09-22 → 2026-10-20, 995 NYSE trading days kept, **28 trading days missing per ETF** (mostly Fridays; all gaps < 7 days, so the SMA never restarts). Fresh at capture. | `daily_qqq.csv`, `daily_spy.csv`, `daily_*_report.json` |
| What does an ETF order cost? | **Commission 1.65 USD on 1,100 USD = 15 bps of notional, proportional** (1.50 on 1,000 in the 2026-10-10 capture). Spread cost 0.05 / 0.04 USD. Overnight fee **0**. Quoted bid/ask 0.28 / 0.26 bps. | `costs.json`, `rates.json` |
| Is x1 real settlement available, with stops? | **Yes.** `real long [1]`, min exposure 10 USD, stop loss 0–100 %, trailing allowed, `requiresW8Ben true` (did not block the demo round trip). | `eligibility.json` |
| Does a round trip work through the broker adapter? | **Yes.** Open 389818357 → position 3623460232, 1.516906 QQQ at 725.14, settlement `REAL`; the idempotent re-open with the same `x-request-id` resolved to the same position; close 389832145 executed; re-close refused with code 741 "already closed". | `roundtrip.json` |
| Was the commission actually charged on demo? | **No.** History row for 3623460232: `fees 0.00`, net profit +0.09 USD. The demo account charges neither the previewed commission nor financing. **The frozen costs (what a real account pays per eToro's preview) decide; this is reported.** | `fees.md` |
| DataPlatform history route for ETFs | Decode error (`volume` is a string): recorded, not fatal; research-only route, never a live input. | `history_*_coverage.json` |

## 2. State at registration (last closed bar 2026-10-20)

| ETF | Close | SMA50 | Rule |
|---|---|---|---|
| QQQ | 722.31 | 726.66 | flat |
| SPY | 771.60 | 768.27 | long (crossed up on this bar) |

From the executor's dry run (`-allow-draft -instruments QQQ,SPY`, 15:36 UTC). Registered snapshot:
`daily_qqq.csv` sha256 prefix `470aee7a745e1200`, `daily_spy.csv` `f79a9390ae099f78`.

## 3. Account

`account.json`: one open position at capture (not the ETF cell's), equity 99,938.78 USD, credit 98,836.29 USD.
