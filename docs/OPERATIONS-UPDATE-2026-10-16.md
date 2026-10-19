# Operations update (2026-10-16)

A summary of the operational work done 2026-10-12 → 2026-10-18 on `feat/etoro`: what changed, what
runs on its own now, what an operator still has to do, and what comes next. Details: the eToro
runbook [etoro/ETORO-DAILY-EXECUTOR.md](etoro/ETORO-DAILY-EXECUTOR.md), the frontend plan
[FRONTEND-UI-PLAN-2026-10-03.md](frontend/FRONTEND-UI-PLAN-2026-10-03.md) §8.15–8.16, and the
porting plan's phases P3–P6.

**Unchanged throughout:** the frozen SMA50 rule, the pre-registered costs of the index-CFD forward
test (`FORWARD-TEST-PREREGISTRATION-SMA50-INDEX-CFD-2026-10-12.md` §1–4; the provisional weekend
values became final on 2026-10-12 by the §3 fallback) and its evaluation dates (interim 2027-04-12,
primary 2027-10-12). The Bitso stage executor, its alerts and its watchdog are unchanged. Every eToro
path is demo only; `ETORO_ENV=real` is refused in code.

## 1. What was built

| Phase | Commit | What |
|---|---|---|
| P3 soak | `efb5ac7`, `4db4c9d` | `etoro-reconcile` waits for eToro's portfolio (2026-10-12: a fill absent 5 s later) and trading history (2026-10-16: a close absent 10 s later) to settle after a fresh order, 60 s budget, `portfolio_lag` / `history_lag` warnings; runbook §3 re-check rule; **P3 exit criterion met 2026-10-16** (five trading days, two with a clean re-check for a lag the reconcile now waits out) |
| P4 | `05830c9` | `shared/pkg/cfdrisk` + `etoro-risk-report`: trading-day VaR/ES with the NDX/SPX correlation, nine index stress episodes, CFD-priced Monte Carlo vs CFD hold and ETF hold, attribution, fill quality vs the frozen costs; `MODEL-VALIDATION-ADDENDUM-ETORO-2026-10-13.md` |
| Alerts | `86549d8` | `ui-alerts` checks the eToro ledger directory every 15 min (`internal/etorohealth`): missed or failed 09:35 New York run, drift, unrecorded order, halt; first real alert and its resolution on 2026-10-16 |
| P5a | `353e4d5` | eToro as a second venue in ui-api (`/api/ui/etoro/*`); halt-all and halt/resume reach the eToro halt file |
| P5b | `cc93693` | The console's eToro views: forward tests, detail, risk with the P4 report, data health with reconcile, kill switch across venues |
| P5c | `1e3817f` | Live quotes: a read-only rates poller (12 reads/min) behind `/api/ui/etoro/live` and `/stream`; Market page with the provisional SMA50 flip level |
| P5d | `2ce8c66` | Assisted trades from the console (prepare → place, HMAC token 120 s, intent first, audited), recorded as `source: "ui"` lines in the executor's ledger; **off unless `-etoro-trading`**; executor and reconcile binaries rebuilt 2026-10-16 19:20 (ledger key for console lines, costs label `FROZEN`) |
| P6 | this commit | `install-ops-cron.sh` owns the eToro cron entry and the weekly risk report and builds the eToro binaries; the wrapper copies the ledger directory to S3; the watchdog Lambda learns the NYSE calendar (`:nyse`); `ui-alerts` `etoro.upload`; env-vars doc |

## 2. What runs on its own (local cron, UTC; managed by `scripts/install-ops-cron.sh`)

| When | Job | Does |
|---|---|---|
| 13:35 / 14:35 Mon–Fri, guard = 09:xx New York | `ops-run.sh etoro` | The demo run, the read-only reconcile, the copy to `s3://…/daily-executor/etoro-demo/` (run log last) |
| 15:00 Monday | `ops-run.sh etoro-risk` | The P4 risk report as of the week (newest candle file ends on Friday), copied to `…/etoro-demo/risk/<date>/` |
| every 15 min | `ops-run.sh alerts` | ui-alerts over the Bitso stage ledger **and** the eToro ledger directory (7 eToro checks) |
| hourly, from AWS | `mtb-ledger-watchdog` | `stage` (daily, 30 h) and `etoro-demo` (`:nyse`: no run log within 6 h of the last trading day's 09:35 New York run pages; weekends and holidays quiet) |

The other jobs (Bitso executor 06:15, compaction, sampler, research, prereg) are as in the
2026-10-09 update.

## 3. Operator checklist

- **Done on 2026-10-18 01:50 UTC**: installer run on the ops host (crontab diff: the eToro entry
  moved into the managed block unchanged, the Monday job added, the legacy block removed), first S3
  copy of the ledger directory, watchdog redeployed (dry run quiet: newest run Friday 13:35, due
  Friday), `ui-alerts` rebuilt (0 open), weekly risk report run once by hand (as of 2026-10-17,
  uploaded).
- **Monday 2026-10-19, after 13:35 UTC**: read the run log. First run on the rebuilt executor; if
  Friday's close held, the rule closes SPX500 (it closed NSDQ100 on 2026-10-16) and the account is
  flat in both books. Expect `reconcile=clean` with at most a `portfolio_lag` / `history_lag`
  warning, and `upload=ok`.
- **After Friday 2026-10-23's run**, if the week was clean: delete the `.prev` binaries next to
  `etoro-daily-data/etoro-daily-executor` and `etoro-reconcile`.
- **Not done, deliberately**: no assisted trade has been placed from the console. To try it:
  `ui-api -operator-token-file … -etoro-ledger … -etoro-trading` (runbook §8), on a day and at a time
  the executor would not act, or after halting the ledger; the executor undoes a position against
  the rule at its next run.
- **Model-validation addendum sign-off** (`docs/risk/MODEL-VALIDATION-ADDENDUM-ETORO-2026-10-13.md`
  §8) is the operator's.

## 4. What comes next

1. ~~Forward-test results report~~ **Done 2026-10-19:**
   [`FORWARD-TEST-RESULTS-SMA50-INDEX-CFD-2026-10-19.md`](backtest-readiness/FORWARD-TEST-RESULTS-SMA50-INDEX-CFD-2026-10-19.md)
   (week-1 execution report: 0.18 bps measured per leg vs 7.4–7.75 frozen, the three lag incidents,
   the first console trade). Left pending there: the overnight fee from the trading history (none
   is visible in the demo cash over 11 position-nights) and the fate of the console position.
2. P7 is running on paper since 2026-10-13 (weekly `prereg` job). Interim look 2027-04-12.
3. Housekeeping: `go fmt` on the pre-existing unformatted files under `shared/pkg/{config,indicators,
   metrics,models}` and `strategy-executor/internal/*` (not touched by this work); the vitest suite
   needs Node 22.
