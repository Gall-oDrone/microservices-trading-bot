# Model risk validation: risk, P&L and execution models (2026-10-09)

**Scope.** These are the quantitative models behind the operator UI, order-management's risk
metrics and the forward-test reports (plan §6.4.7–§6.4.13). The document follows the usual
structure of a model-risk review (in the spirit of SR 11-7):

- inventory;
- conceptual soundness;
- outcomes analysis;
- limitations;
- ongoing monitoring;
- review triggers.

**Status.** First validation, prepared by engineering. **Independent sign-off is pending** (§8).

**One rule overrides everything below.** Every model here is reporting only: none blocks an order
or feeds the executor. The frozen SMA50 rule, its pre-registered costs (70/88 bps btc_mxn, 40/46
bps btc_usd) and its evaluation dates (interim 2027-03-26, evaluation 2027-09-26) are outside this
document's authority. No finding or trigger below may change them. A changed rule or execution
needs its own dated pre-registration.

---

## 1. Inventory

| ID | Model | Code | Output | Use | Tier |
|---|---|---|---|---|---|
| M1 | Parametric VaR, 99 %, 1 day; vol = max(EWMA λ 0.94, 365-day) | `shared/pkg/varmodel`, order-management `internal/risk/portfolio.go` | `portfolio_var_quote`, against `RISK_VAR_LIMITS` | Limit monitoring (alerts only) | 2 |
| M2 | Historical-simulation VaR, 99 % | same | `portfolio_var_historical_quote` | Challenger to M1 | 3 |
| M3 | Expected shortfall, 97.5 % (historical and normal) | same | `portfolio_es_quote{method}` | Tail reporting | 3 |
| M4 | Stress: hypothetical shocks, 8 historical episodes, reverse stress | `varmodel/stress.go`, `KnownEpisodePaths` | `portfolio_stress_*`, `portfolio_reverse_stress_move_ratio` | Against `RISK_STRESS_LIMITS`, `RISK_CAPITAL` | 2 |
| M5 | Monte Carlo: stationary block bootstrap of the rule against hold | `shared/pkg/montecarlo` | P(H1), P(H2), quantiles | Expectation setting | 3 |
| M6 | Expected profit per trade (closed round trips) | `montecarlo/history.go` | Distribution, calendar base rates | Expectation setting | 3 |
| M7 | Execution cost: cost budget (TCA), capacity (book walk, square-root law), replay simulator | `dailyledger/costs.go`, `shared/pkg/execcost`, `strategy-executor/internal/execsim` | Budget used, capacity, cost per schedule | Sizing and execution research | 2 |
| M8 | Pre-registered verdicts (H1/H2/H3, btc_usd in MXN terms) | `strategy-executor/cmd/prereg-report`, `scripts/prereg-evaluation.sh` | `report.md` / `report.json` | The 2027 evaluations | 1 |
| C1 | Daily stage reconciliation (a control, not a model) | `strategy-executor/internal/reconcile` | Breaks, exit 3 | Books and records | 1 |

Tier 1 means a wrong answer misstates a decision or the books. Tier 2 misstates a limit or a
sizing. Tier 3 misstates context.

## 2. Conceptual soundness and key assumptions

| ID | Assumption | Assessment |
|---|---|---|
| M1 | Daily log returns are conditionally normal. Books in one currency share BTC, so correlation is 1. The vol floor is the 365-day vol. | Standard (RiskMetrics with a long-run floor). Correlation 1 is exact for one base asset. Normality understates tails; M2 and M3 measure by how much. |
| M2 | The last ≤ 365 aligned days represent tomorrow; today's exposures are revalued; at least 250 days and every book must have fresh history. | Standard. An incomplete history leaves the series absent rather than reporting a partial number. |
| M3 | Historical ES is the mean of the worst ceil(2.5 % n) losses; normal ES = 2.338σ. | The FRTB measure. With n ≈ 365, the historical ES rests on about 10 observations, so it is noisy. |
| M4 | Positions are held through each episode with no hedging; the loss is at the worst common day; a missing book is proxied by a book with the same base asset; the reverse stress is a uniform spot move. | Conservative for a long-only, unlevered book. Liquidity and fee costs in a crash are not modelled; at stage size they are immaterial. |
| M5 | Days are resampled in blocks (mean length 20) as (overnight gap, intraday move) pairs; the rule and its simulator are unchanged; there is a 50-bar warm-up. | Preserves fat tails and short-range dependence. Longer trends than the block length are under-sampled, so P(H2) is a lower bound (§3). |
| M6 | Closed round trips at the primary cost; calendar windows as registered. | Exact replay. The skew (one trade dominates the total) is stated on the page. |
| M7 | Walk: the visible top 20 levels. Square-root law: Y = 0.5–1. Replay: best quotes inferred from maker-side prints; the Through/Touch fill models bracket queue position. | Each view is partial, so three are shown. They agree within their ranges (§3). |
| M8 | Takes daily-research's registered output; MXN terms use `mxn-terms.py`'s formula; the criteria are strict inequalities. | A mechanical transcription of the pre-registrations. Cross-checked against the registered script in every run. |
| C1 | Read-only client; trades fetched again by client id; position = Σ legs; the account covers the books. | Complete for the stage ledger's scope (two books, one account). |

## 3. Outcomes analysis

| ID | Test | Result | Evidence |
|---|---|---|---|
| M1 | Basel traffic light and Kupiec POF over 250 days, long and short unit positions | Committed history to 2026-09-26: btc_mxn **yellow** (5 long-side exceptions, Kupiec p = 0.16, not rejected); others green. Live 2026-10-08: both books green (vol 2.06 % / 2.19 %). | `varmodel/real_data_test.go`; plan §6.4.8 |
| M1/M2 | Divergence | Live 2026-10-09: historical VaR 1,266 MXN against parametric 1,080 (ratio 1.17, below the 1.5 alert) | §6.4.9 |
| M3 | Historical ES above normal ES on the same data | Yes: 1,249 against 1,085 MXN. The fat tail is visible and modest at 97.5 %. | §6.4.9 |
| M4 | Episode troughs pinned to the committed CSVs | btc_mxn −62.0 % (2018-01) … −12.0 % (2024-08); btc_usd −41.1 % … −17.5 % (from 2021) | `varmodel/stress_real_data_test.go` |
| M5 | Calibration against the observed calendar years at the same cost | P(H1) 61–71 % against 6/9 observed: reproduced. P(H2) 29–42 % against 5/9 observed: within the uncertainty of 9 years, a lower bound. | §6.4.11 |
| M6 | Reproduces the pre-registrations' base rates | btc_mxn H2 5/9, H1 6/9; btc_usd H1 5/6: exact | `montecarlo` tests on the committed CSVs |
| M7 | Realized stage cost against the budget | btc_mxn 118 bps (169 % of budget), btc_usd 41 bps (103 %): one leg each | §6.4.10 |
| M7 | Replay on production prints | Today's schedule: 66.5 / 32.7 bps per leg at stage size, inside 70 / 40. **So the 118 bps stage leg is attributed to the stage book.** | [EXECUTION-RESEARCH-2026-10-09.md](../backtest-readiness/EXECUTION-RESEARCH-2026-10-09.md) |
| M7 | Capacity, three views | Walk 0.07–0.15 BTC; square-root law 0.02–0.09 BTC; replay: today's schedule passes the pessimistic 88 bps between 0.01 and 0.1 BTC. They agree on an order of magnitude. | §6.4.12; execution research §3 |
| M8 | Go formula against the registered `mxn-terms.py` | Run as of 2026-10-08: rule −2.47 % against hold −1.88 %; the script gives −2.5 / −1.9: equal | [prereg-as-of/](../backtest-readiness/evidence-2026-10-09/prereg-as-of/) |
| C1 | First stage run | 0 breaks: both legs matched, 8 leg-less days clean, positions = Σ legs | §6.4.10 |

**Finding from this validation, fixed:** the UI understated the btc_usd H2 gap.

- The forward-test page compared btc_usd in MXN terms using the paper account's mark-to-market
  equity, which leaves out the exit leg. Hold btc_mxn, its benchmark, paid both legs.
- The registered evaluation closes an open position at the window's last close and pays the exit.
  So the page showed the gap as −0.20 pp when the registered measure gave −0.59 pp.
- ui-api now values the rule as if closed (`equity_if_closed`), and a test pins it. The page now
  matches the registered evaluation.
- Reporting only. No decision used the old figure.

## 4. Limitations and compensating controls

| Limitation | Effect | Control |
|---|---|---|
| Normal VaR in a fat-tailed asset | Understates tail losses | Historical VaR and ES beside it; the `PortfolioVaRModelDivergence` alert; stress limits set at 50 % of capital, ten times the VaR limit |
| Short history for btc_usd (from 2020-04) | Pre-2020 episodes proxied | `portfolio_stress_proxied` flags them; a scenario with no proxy is left absent |
| Monte Carlo blocks shorter than trends | P(H2) biased low | Reported as a lower bound; 5, 20 and 60-day blocks shown side by side |
| Execution replay: about 40 days, production prints, inferred book | Means move a few bps; stage book is thinner | Monthly re-run; the hourly book sampler measures the real spread; the stage ledger stays the ground truth (cost budget) |
| One stage leg per book | Cost budget not yet meaningful | The `cost_over_pessimistic` finding waits for ≥ 3 legs |
| order-management needs egress to api.bitso.com for vol and episodes | Falls back to a 4 % vol | `VaRVolEstimateFallback` alert; episodes embedded |

## 5. Ongoing monitoring (in place)

- **Alerts** (`team: risk` unless noted):
  - VaR: `VaRBacktestYellow`, `VaRBacktestRed`, `VaRVolEstimateFallback`,
    `PortfolioVaRModelDivergence`, `PortfolioVaRLimitWarning`, `PortfolioVaRLimitBreached`.
  - Stress and capital: `PortfolioStressLimitBreached`, `PortfolioStressScenarioIncomplete`,
    `PortfolioStressExceedsCapital`.
  - Data and execution: `PortfolioRiskStale`, `PositionUnpriced`, `OrderSlippageHigh` (trading).
  - Receivers are still a template (an operator step, §7 of
    [OPERATIONS-UPDATE-2026-10-09.md](../OPERATIONS-UPDATE-2026-10-09.md)).
- **ui-alerts emails** (every 15 minutes): reconcile breaks or errors, the cost-budget finding, and
  stale data.
- **Nightly:** stage run, then reconciliation (06:15 UTC).
- **Hourly:** order-book sample (:07).
- **Monthly (manual):** `cmd/exec-research` on the growing archive.
- **Any time:** `scripts/prereg-evaluation.sh` for a progress report.

## 6. Review triggers (operational; none touches the rule)

Any of these opens a model review. It is recorded as a dated note in `docs/risk/` with the cause,
the evidence and the action. An action may change a reporting model, a limit or a monitoring
threshold. It may never change the SMA50 rule, its costs or its evaluation dates.

| # | Trigger | First action |
|---|---|---|
| T1 | VaR backtest red on any book, or yellow for 20 consecutive days | Re-estimate on the latest data; consider a fatter-tailed VaR (Student-t or filtered historical simulation) as the limit measure |
| T2 | Historical VaR over 1.5 × parametric for 5 days | As T1 |
| T3 | Vol estimate on fallback for over 24 h | Fix egress or data; the limit is running on 4 % |
| T4 | Any reconcile break or error | Stop and investigate before the next run; books and records first |
| T5 | Realized weighted cost above the pessimistic scenario after ≥ 3 legs | Re-run the execution study; check stage book depth; decide whether to pre-register an execution change |
| T6 | Book-sampler p10 walk capacity below 2 × the stage leg size, or p90 spread above 10 bps, over 7 days | Revisit the sizing in the capital overlay |
| T7 | A change in stage order size, the policy's `max_order_notional`, the Bitso fee tier, or a new book | Recompute capital and limits (`stage_capital_test.go`), the capacity study and the cost budget |
| T8 | A data gap over 7 days, or a Bitso history revision before a forward start | Follow the pre-registration's data-integrity clause |
| T9 | A new stress episode (a daily BTC move beyond −20 %) | Add it to `varmodel.Episodes` and regenerate the embedded paths |
| T10 | 12 months since the last validation | Full re-validation |

## 7. Re-running the evidence

```bash
cd shared && go test ./pkg/varmodel ./pkg/montecarlo ./pkg/execcost         # M1–M7 pinned tests
cd services/strategy-executor
go run ./cmd/exec-research -bucket mtb-development-data-archive-326105557351  # M7 replay
cd ../.. && scripts/prereg-evaluation.sh                                      # M8 progress report
```

## 8. Sign-off

| Role | Name | Date | Decision |
|---|---|---|---|
| Model owner (engineering) | | 2026-10-09 | Submitted |
| Independent validator (risk) | | | Pending |
| Operator | | | Pending |
