# Operations update (2026-10-09)

A summary of the risk, P&L and execution work done on 2026-10-08/09 on `feat/operator-ui`. It
covers what changed, what runs on its own now, what an operator still has to do, and what comes
next. Details are in the plan, [FRONTEND-UI-PLAN-2026-10-03.md](frontend/FRONTEND-UI-PLAN-2026-10-03.md)
§6.4.9–§6.4.13.

**Unchanged throughout:** the frozen SMA50 rule, its pre-registered costs and its evaluation dates
(interim 2027-03-26, evaluation 2027-09-26). Everything below is additive and reporting only. No
cluster was changed and no AWS resource was created. The Bitso stage keys are used read-only, for
reconciliation.

## 1. What was built

| Step | Commit | What | Plan |
|---|---|---|---|
| R6d | `79a763c` | 97.5 % expected shortfall (historical and normal), hypothetical and 8 historical stress scenarios, stress limits | §6.4.9 |
| R6e | `c2ad29b`, `69c2ca8` | Cost budget against the pre-registration; `RISK_CAPITAL` and reverse stress; embedded crash paths; read-only daily reconciliation, run nightly after the stage run; stage capital and limits overlay | §6.4.10 |
| R6f | `3221990` | Stage P&L attribution against paper; forward-test ratios; expected profit per trade; block-bootstrap Monte Carlo on the forward-test page | §6.4.11 |
| R6g | `34871c3` | Capacity and market impact (live book walk, square-root law); daily P&L and NAV; btc_usd in MXN terms | §6.4.12 |
| R6h | `fd44903` | Execution research on the trade archive; hourly order-book sampler and the capacity distribution; automated pre-registered evaluation report; model-risk validation; MXN-terms alignment fix | §6.4.13 |
| **R6i** | `13915df` and the next commit | Re-peg pre-registration (frozen before its window); monthly execution study and weekly verdicts on cron; latest verdicts and FIFO tax lots on the forward-test page | §6.4.14 |

### R6h in detail

- **Execution research.** Code: `strategy-executor/cmd/exec-research` and `internal/execsim`.
  Write-up: [EXECUTION-RESEARCH-2026-10-09.md](backtest-readiness/EXECUTION-RESEARCH-2026-10-09.md).
  - Replays market, maker-window, repriced and TWAP schedules over about 40 days of production
    trades. It is read-only on the archive.
  - Today's schedule costs 66.5 / 32.7 bps per leg at stage size, inside the registered 70 / 40 bps,
    so the 118 bps stage leg reflects stage's thin book.
  - Re-pegging every 5 minutes within the same hour lowers both the mean and the tail. It needs its
    own pre-registration before any use.
  - Capacity under today's schedule is about 0.01 BTC per btc_mxn leg.
- **Order-book sampler.** `strategy-executor/cmd/book-sampler` reads the public REST book and needs
  no keys.
  - Every hour at :07 (`scripts/ops-run.sh sampler`) it appends spread, top-20 depth, walk cost at
    0.001–1 BTC and capacity at 10 bps to `daily-executor-data/book-samples/<book>.jsonl`.
  - ui-api (`-book-samples`) adds `history` (30-day p10/p50/p90) to `/forward-tests/{book}/capacity`.
  - The forward-test page shows it under the capacity table.
  - First samples at 21:26 UTC: btc_mxn spread 2.2–3.1 bps, capacity 0.064–0.068 BTC; btc_usd
    spread 1.2–1.6 bps, capacity 0.55–0.74 BTC.
- **Pre-registered evaluation, automated.** `scripts/prereg-evaluation.sh [--phase as-of|interim|final]`.
  - Runs each pre-registration's frozen procedure: fetch, `daily-research` at primary and
    secondary costs, and the `mxn-terms.py` cross-check.
  - `cmd/prereg-report` then writes the H1/H2/H3 verdicts, with the registered reading of the
    outcome.
  - It decides nothing before 2027-09-26, and nothing at the interim look.
  - First progress report:
    [prereg-as-of/report.md](backtest-readiness/evidence-2026-10-09/prereg-as-of/report.md).
    Through 2026-10-08, both rules are still long from the start, so they tie with hold on H1;
    btc_usd is 0.59 pp behind holding btc_mxn in pesos.
- **Model-risk validation.** [risk/MODEL-VALIDATION-2026-10-09.md](risk/MODEL-VALIDATION-2026-10-09.md)
  covers VaR, ES, stress, Monte Carlo, execution cost, the verdict report and reconciliation. It
  gives evidence and limitations, plus ten review triggers that never touch the rule.
  Independent sign-off is pending.
- **Fix: btc_usd MXN terms now pay the exit leg.**
  - The forward-test page valued the rule at mark-to-market and hold btc_mxn with both legs paid,
    which showed the H2 gap as −0.20 pp. The registered measure gives −0.59 pp.
  - It now uses `equity_if_closed`, as the registered evaluation does.
- **CI.** The Go job covers the new packages (gofmt, vet, race tests) and checks the syntax of the
  ops scripts.

### R6i in detail (later on 2026-10-09)

- **Re-peg pre-registration.** [EXECUTION-PREREGISTRATION-REPEG-2026-10-09.md](backtest-readiness/EXECUTION-PREREGISTRATION-REPEG-2026-10-09.md),
  committed before its window opened.
  - It compares today's schedule against "maker 1h, re-peg every 5 minutes" on production trades
    for 2026-10-10 → 2027-01-09.
  - It passes only if the mean btc_mxn stage-leg cost is lower (paired, p < 0.05), the p90 is
    lower, and 0.01 BTC is no worse.
  - In-sample it was −1.84 bps with p = 0.069, not significant, so the unseen forward data
    decide.
  - The run command is frozen in the file. The executor is untouched unless all three pass.
- **Scheduled research and verdicts.** These are local only and read only public or archive data.
  - `ops-run.sh research`, monthly: the execution study with the sampler's spreads. It leaves out
    the repriced schedules until 2027-01-10, so the test stays blind.
  - `ops-run.sh prereg`, weekly: the verdict report. On 2027-03-26 and 2027-09-26 it runs the
    interim and the final evaluation by itself.
  - Both ran once on 2026-10-09.
- **On the forward-test page:**
  - **Pre-registered verdicts so far.** H1/H2/H3 per cost scenario, the days left to each date,
    and "decides? no".
  - **Tax lots (FIFO).** Open lots, matched sales and realized gains per year, in the quote
    currency and MXN. Not tax advice: Mexican ISR uses INPC-adjusted cost and Banxico FIX, which
    this does not apply.
- **Not done:** a second asset. It needs your decision and its own pre-registration.

## 2. What runs on its own (local cron, UTC)

| When | Job | Output |
|---|---|---|
| 06:15 daily | `ops-run.sh executor`: stage run, then reconciliation | Ledger, `reconcile.json`, `reconcile=` line, S3 copy |
| :07 hourly | `ops-run.sh sampler` (new) | `book-samples/<book>.jsonl` |
| 08:00 on the 1st | `ops-run.sh research` (R6i) | `exec-studies/<date>/exec-research.{md,json}` (repriced schedules blind until 2027-01-10) |
| 07:30 Mondays | `ops-run.sh prereg` (R6i) | `prereg/<date>-<phase>/report.{md,json}`; the interim and final runs happen on their dates |
| every 15 min | `ops-run.sh alerts` (ui-alerts) | Email on findings, reconcile breaks, stale data |
| 02:30 daily | `ops-run.sh compact` | `trades_compacted/` (non-destructive) |

The sampler, research and prereg lines were added to the installed crontab block in the same form
`scripts/install-ops-cron.sh` now writes. A re-install keeps them; `--no-sampler` and
`--no-research` leave them out.

## 3. Operator rollout checklist (not done; needs the operator)

1. **Order-management image with R6b–R6e risk metrics, then the dev overlay.**
   - Build and push order-management from this branch, and set the tag in
     `k8s/overlays/development/kustomization.yaml` (`images:` `order-management`).
   - Check before applying:

     ```bash
     kustomize build k8s/overlays/development | grep -A3 RISK_CAPITAL
     ```

     It should show `RISK_CAPITAL=MXN=25000,USD=1500`, `RISK_VAR_LIMITS=MXN=1250,USD=75` and
     `RISK_STRESS_LIMITS=MXN=12500,USD=750`.
   - Apply: `kubectl apply -k k8s/overlays/development` (namespace `bitso-trading-dev`).
   - Confirm the order-management pod can reach `api.bitso.com`. Without it `VaRVolEstimateFallback`
     fires and VaR runs on the 4 % fallback.
   - Check `portfolio_var_quote`, `portfolio_es_quote`, `portfolio_stress_worst_loss_quote` and
     `portfolio_risk_capital_ratio` on the Trading Risk Operations dashboard.
2. **Alertmanager receivers.** `monitoring/alertmanager/alertmanager.yml` is still a template; its
   header shows PagerDuty, Slack and SNS.
   - Add integrations with secrets read from files; never commit a key.
   - Run `scripts/tests/check-alertmanager-routes.sh`. It refuses a committed integration, so keep
     the secrets in the cluster.
   - Send a test alert through each receiver.
3. **First nightly reconciliation: 2026-10-10 at 06:15 UTC.**
   - Check `~/.local/state/mtb-ops/executor.log` for `reconcile=ok`.
   - Check `services/strategy-executor/daily-executor-data/stage/reconcile.json` shows 0 breaks.
   - Check the Data health page's Reconcile column.
   - `reconcile=breaks` means stop and investigate before the next run (trigger T4).
4. **Book sampler.** After a day, the capacity section of `/forward-tests/btc_mxn` should show
   about 24 samples. Check `~/.local/state/mtb-ops/sampler.log` for errors.
5. **Model validation sign-off.** An independent reviewer fills in §8 of the validation document.

## 4. Recommended next steps (updated after R6i)

1. **2027-01-10: re-peg forward run.** Run the frozen command in the pre-registration, commit the
   output and write the results note. Only a full pass (E1–E3) leads to an executor option.
2. **2027-03-26: interim look.** The weekly job writes `prereg/<date>-interim/`. Commit it under
   `docs/backtest-readiness/evidence-<date>/` with a short note. It is report only.
3. **2027-09-26: primary evaluation.** The weekly job writes the final verdicts; the pre-registered
   readings apply.
4. **Operator items from §3:** the overlay and order-management image, the Alertmanager receivers,
   the first nightly reconcile, and the validation sign-off.
5. **Your decision:** a second asset (needs its own pre-registration). Also an accountant's review
   of the tax-lot method before any filing.

Main UI: http://127.0.0.1:5173/. Drill UI: http://127.0.0.1:5174/strategies.
