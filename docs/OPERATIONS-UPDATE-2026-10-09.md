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
| **R6h** | this update | Execution research on the trade archive; hourly order-book sampler and the capacity distribution; automated pre-registered evaluation report; model-risk validation; MXN-terms alignment fix | §6.4.13 |

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

## 2. What runs on its own (local cron, UTC)

| When | Job | Output |
|---|---|---|
| 06:15 daily | `ops-run.sh executor`: stage run, then reconciliation | Ledger, `reconcile.json`, `reconcile=` line, S3 copy |
| :07 hourly | `ops-run.sh sampler` (new) | `book-samples/<book>.jsonl` |
| every 15 min | `ops-run.sh alerts` (ui-alerts) | Email on findings, reconcile breaks, stale data |
| 02:30 daily | `ops-run.sh compact` | `trades_compacted/` (non-destructive) |

The sampler line was added to the installed crontab block in the same form
`scripts/install-ops-cron.sh` now writes. A re-install keeps it, and `--no-sampler` leaves it out.

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

## 4. Recommended next steps

1. **Pre-register the re-peg** ("maker 60 minutes, re-peg every 5 minutes, then market"), if the
   cost tail is worth attacking.
   - Hypothesis: mean cost per leg not above today's schedule on the stage ledger.
   - Only after it is committed should the executor gain the option. The SMA50 tests keep their
     costs.
2. **Re-run the execution study monthly.** Feed it the sampler's measured spreads instead of the
   assumed half-spread.
3. **Interim look on 2027-03-26:** `scripts/prereg-evaluation.sh --phase interim`, committed with
   a short results note. It is report only.
4. **Later:**
   - tax lots (FIFO cost basis per lot for Mexican tax reporting);
   - a second asset (multi-asset correlation stress);
   - a fatter-tailed limit VaR if trigger T1 or T2 fires.

Main UI: http://127.0.0.1:5173/. Drill UI: http://127.0.0.1:5174/strategies.
