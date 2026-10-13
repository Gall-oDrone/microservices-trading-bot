# Model risk validation, addendum: eToro index-CFD executor (2026-10-13)

**Scope.** The risk and analytics models added for the eToro index-CFD executor in porting-plan
phase P4: trading-day VaR/ES, index stress, a CFD-priced Monte Carlo, P&L attribution with
financing, and fill quality. They live in `shared/pkg/cfdrisk` and are run by
`services/strategy-executor/cmd/etoro-risk-report`; the outcomes below are the first run on the
real demo tree (`etoro-daily-data/demo`, as of 2026-10-13 New York, two days into the P3 soak).
This addendum extends [MODEL-VALIDATION-2026-10-09.md](MODEL-VALIDATION-2026-10-09.md) (same
structure, numbering continued from M9) and does not change it.

**Status.** First validation, prepared by engineering. **Independent sign-off is pending** (§8).

**One rule overrides everything below.** Every model here is reporting only: none blocks an order
or feeds `etoro-daily-executor`. The frozen SMA50 rule, its pre-registered costs (15.5 / 14.8 bps
round trip, 2.3 bps per calendar night) and its evaluation dates
([pre-registration](../backtest-readiness/FORWARD-TEST-PREREGISTRATION-SMA50-INDEX-CFD-2026-10-12.md)
§1–4) are outside this document's authority.

---

## 1. Inventory

| ID | Model | Code | Output | Use | Tier |
|---|---|---|---|---|---|
| M10 | Trading-day VaR/ES: per book parametric VaR 99 % 1-day, vol = max(EWMA λ 0.94, 252-trading-day); historical VaR 99 % and ES 97.5 % on the last 252 trading-day returns; Basel backtest + Kupiec over 250 trading days. Portfolio: z·√(wᵀΣw) with Σ = D·R·D, R the EWMA correlation on aligned trading days; historical simulation on aligned days | `cfdrisk/calendar.go`, `cfdrisk/var.go` (on `varmodel.EstimateWith` / `LogReturnsOn`) | `risk.json` → `var` | Limit context for the demo book (no limit is wired yet) | 2 |
| M11 | Index stress: uniform shocks −5/−10/−20/−30 %, 9 equity-index episodes replayed on today's exposure (^NDX for NSDQ100, ^GSPC for SPX500), reverse stress vs capital, margin close-out (x1: −50 %) | `cfdrisk/stress.go`, `KnownIndexEpisodePaths` (generated from the committed Yahoo CSVs) | `risk.json` → `stress` | Tail context | 2 |
| M12 | CFD Monte Carlo: stationary block bootstrap of (gap, intraday) pairs (mean block 20), dates advancing by NYSE trading days, each path priced with `cfdsim` three ways (SMA50 rule, CFD hold with financing, ETF hold with a fixed fee), at the frozen costs and at the measured in-session costs | `cfdrisk/montecarlo.go` | P(beats CFD hold), P(beats ETF hold), P(shallower DD), quantiles, P(margin close-out) | Expectation setting | 3 |
| M13 | Attribution: per lot / book / total, price P&L, spread, markup, fees, financing modelled (2.3 bps × calendar nights × open notional) and charged (reconcile `financing_to_date`), realized / unrealized; paper-vs-demo tracking in bps | `cfdrisk/attribution.go` | `risk.json` → `attribution` | Books and records context; the cost-model check | 2 |
| M14 | Fill quality: per leg quoted spread, fill vs mid, fill vs decision close, fill vs day open, spread cost, latency, portfolio lag; measured spread per leg vs frozen 7.75 / 7.4 bps | `cfdrisk/fillquality.go` | `risk.json` → `fill_quality` | The pre-registration's "report frozen and actual costs" (§3, §6) | 2 |

Tier 1 means a wrong answer misstates a decision or the books. Tier 2 misstates a limit or a
sizing. Tier 3 misstates context.

## 2. Conceptual soundness and key assumptions

| ID | Assumption | Assessment |
|---|---|---|
| M10 | A return is formed only between consecutive NYSE trading days (Friday → Monday is one return, a holiday is skipped, a missing trading day breaks the chain); weekend and holiday CFD bars are dropped first. The long window is 252 returns. The two indices are distinct assets: the portfolio uses an EWMA correlation (same λ, seeded like the variance) with the per-book floored vols on the diagonal, so with ρ = 1 it reduces to the sum of the per-book VaRs. | The Bitso recursion unchanged (`varmodel` wrappers pin it bit for bit); only the chain and the windows differ. Using the floored vol on the diagonal keeps portfolio and book figures consistent and conservative. The Basel zone thresholds are those for 250 observations at 99 %, which the trading-day window still is. |
| M10 | The historical simulation sums each aligned day's book P&Ls, so the dependence is the observed one (tails included); at least 250 aligned scenarios are required. | Standard; the eToro history (≈950 trading days since 2022-12) covers it. |
| M11 | Episodes are measured on the official indices (Yahoo ^NDX / ^GSPC) from the last calm close to the trough; the position is held throughout; the loss is at the worst common day. eToro's CFD tracks the index (P2: close ratio ≈ 1.0000). At x1 the close-out is a −50 % move on an isolated position. | Conservative for a long-only, unlevered book. The demo account's equity (100,000 USD) is ≈45× the exposure, so in practice the account, not the position, decides a close-out; the flag is kept as the simulators keep it. The 2010 flash crash is measured on closes (−3 %), not the −9 % intraday print. |
| M12 | Same bootstrap as M5; synthetic dates are NYSE trading days, so a path has the real pattern of one- and three-night holds; financing is 2.3 bps per calendar night on the mark; the ETF hold pays 1.50 USD per order and no financing; the resampled history is ^NDX from 1985 / ^GSPC from 1970 spliced in front of eToro's own bars (one series whose last close is the traded instrument's). | Blocks of 20 days under-sample long trends, so P(beats hold) is a lower bound (as M5). The index closes are price returns: the ETF hold is understated by the dividend yield (QQQ ≈ 0.6 %, SPY ≈ 1.3 % a year), which makes P(beats ETF hold) an upper bound. |
| M13 | Lots are rebuilt from executed open/close orders in the ledger; open lots are marked at the last closed bar on or before the as-of date; nights are the calendar-day difference between fill dates (weekend = 3); financing modelled on the open notional. The paper equity is the ledger's `paper.equity_if_closed`. | A mechanical transcription of the ledger. The paper account fills at the next open and the demo fills in the cash session at 09:35 New York, so the tracking contains that timing gap by design (§6 of the pre-registration reports it). |
| M14 | The ledger's own `fill_vs_mid_bps` / `fill_vs_close_bps` are carried through; fill vs open uses eToro's UTC-day bar open (00:00 UTC, not the 09:30 cash open); portfolio lag is the reconcile `portfolio_lag` warning or, before the reconcile learned to wait, a `missing_position` drift within 10 minutes of the fill. | The "measured spread per leg" is eToro's own spread cost on the order; half the quoted spread and the fill vs mid are shown beside it because each answers a different question (the fill vs mid also contains ≈70 s of price drift between quote and fill). |

## 3. Outcomes analysis (first run, as of 2026-10-13, `etoro-daily-data/demo/risk/2026-10-13/`)

Positions: NSDQ100 long 0.035799 units (1,097 USD at the 2026-10-12 close 30,649.5), SPX500 long
0.140735 units (1,099 USD at 7,806.65); 2,196 USD net.

| ID | Test | Result | Evidence |
|---|---|---|---|
| M10 | Per-book VaR 99 % 1-day, vol, backtest | NSDQ100: vol 1.29 %/day (EWMA 0.93 %, 252-day 1.29 %: the long window binds), VaR 32.94 USD, historical 35.89, ES 97.5 % historical 35.09 / normal 33.10, backtest **green** (2 long / 2 short exceptions in 250, Kupiec p 0.74). SPX500: vol 0.84 % (EWMA 0.58 %, long 0.84 %), VaR 21.48, historical 23.28, ES 23.95 / 21.59, **green** (1 / 1, p 0.28). | `risk.md` §1; `cfdrisk/var_test.go` (2-asset VaR by hand, quantiles on a known ladder) |
| M10 | Portfolio | Parametric VaR 52.81 USD against 54.42 undiversified: implied NSDQ100/SPX500 correlation **0.878** (EWMA; 0.935 equal-weighted over 252 days; 952 aligned returns). Historical VaR 61.48 (252 scenarios), ES 57.84 historical / 53.07 normal; historical / parametric **1.16** (below the 1.5 divergence alert of M1/M2). Worst aligned day 2026-06-05, 86.72 USD. VaR is 0.05 % of the 100,000 USD demo capital. | `risk.md` §1 |
| M11 | Episode troughs pinned to the committed Yahoo CSVs | ^NDX: Lehman −40.6 %, flash crash −3.3 %, 2011 −15.2 %, 2015 −12.1 %, 2018-02 −10.2 %, 2018-12 −16.5 %, COVID −28.0 %, 2022 −34.6 %, 2025-04 −12.7 %. ^GSPC: −40.1 / −3.2 / −16.8 / −11.2 / −10.2 / −15.7 / −33.9 / −25.4 / −12.1 %. Every window complete (one close per trading day). | `cfdrisk/episode_paths_gen_test.go` |
| M11 | Scenarios on today's exposure | Shocks: 110 / 220 / 439 / 659 USD. Worst episode **2008-09 Lehman, 885.51 USD (0.89 % of capital)**; COVID 679, 2022 bear 659. No close-out in any scenario. Reverse stress: the capital exceeds the net exposure, so no uniform fall exhausts it; the position's own close-out (−50 %) would cost 1,098 USD. | `risk.md` §2 |
| M12 | Monte Carlo, 2,000 paths × 252 trading days, seed 1, frozen costs | NSDQ100 (resampled from 1985): trend median +0.7 % (p5 −22.9 %, p95 +41.8 %), CFD hold median +7.3 %, ETF hold median +16.1 %; financing median 5.6 % (trend) / 9.1 % (hold); **P(trend > CFD hold) 32.1 % [30.1, 34.2]**, **P(trend > ETF hold) 19.0 % [17.3, 20.8]**, P(shallower DD) 68.8 %, P(loss) 48.2 %, P(close-out) 0 % trend / 0.75 % hold. SPX500 (from 1970): trend median −2.6 %, CFD hold −0.1 %, ETF hold +8.6 %; **P(beats CFD hold) 39.5 % [37.3, 41.6]**, **P(beats ETF hold) 18.2 % [16.6, 20.0]**, P(shallower DD) 71.5 %, P(close-out) 0 % / 0.15 %. | `risk.md` §3 |
| M12 | Measured in-session costs (0.5 bps round trip, same 2.3 bps/night, same paths) | NSDQ100 P(beats CFD hold) 34.8 %, P(beats ETF hold) 20.3 %; SPX500 43.5 % / 20.2 %. The spread moves the answer by 3–4 pp; financing is the cost that matters (hold pays ≈9 %/yr, the rule ≈5.5 %). Consistent with the P2 study's conclusion that the rule is unlikely to beat the ETF. | `risk.md` §3 |
| M12 | Sanity | With zero costs every path's CFD-priced trend return equals the frozen `dailyrule` simulator's; same seed → same paths (the ETF hold is identical across cost scenarios); dates are consecutive NYSE trading days. | `cfdrisk/stress_mc_test.go`, `etoro-risk-report/report_test.go` |
| M13 | Attribution after one night | Price P&L −2.77 (NSDQ100) / −1.33 (SPX500) USD; spread 0.04 + 0.04; markup and fees 0; financing modelled 0.25 + 0.25 (one night at 2.3 bps of 1,099.99); **net −4.68 USD** (all unrealized). eToro's `financing_to_date` read 0.00 on 2026-10-13 13:35Z for both positions (reconcile report): either the Monday-night charge posts later than the morning run or the demo position's `totalFees` does not carry it; to be checked against the trade history (§4). Paper not started (forward window begins 2026-10-13): paper equity 1.0, demo equity 0.9972 / 0.9985, tracking **−27.9 / −14.7 bps**, which is the demo's one-day head start on the paper account, not a model error. | `risk.md` §4; `cfdrisk/attribution_test.go` on the two real opens |
| M14 | Fill quality, 2 opens of 2026-10-12 | Quoted spread 0.23 / 0.38 bps; fill vs mid −5.22 / −1.73 bps (better than mid: the market fell between 13:33:59Z and the 13:35 fills); fill vs decision close −53.86 / −0.31 bps; fill vs UTC-day open −55.73 / −2.49 bps; spread cost 0.04 USD = 0.36 bps per leg on both; latency 4.59 / 3.74 s; portfolio lag on both (the `missing_position` of 2026-10-12.json, 5 s after the fill). **Measured spread per leg 0.36 bps against the frozen 7.75 / 7.40 bps (×0.047 / ×0.049).** | `risk.md` §5; `cfdrisk/attribution_test.go` (`TestFillQualityOnRealOpens`) |
| all | Golden | `risk.json` on the committed fixture (the real 4-line ledger, the last 400 bars per book, the three reconcile reports, 200 paths) is byte-identical to `testdata/golden/risk.json`. | `etoro-risk-report/report_test.go` |

**Reading of the first run.** The two demo positions are small against the account (VaR 0.05 % of
capital, Lehman 0.9 %), the VaR backtests are green on both books, and the fat-tail ratio (1.16) is
inside the alert band carried over from the Bitso monitor. The cost-model check already says what
the P2 study expected: the in-session spread is ≈5 % of the frozen weekend figure, so the frozen
costs are conservative for the spread; the financing cost, which the frozen model charges at the
measured rate, is the one that decides the forward test.

## 4. Limitations and compensating controls

| Limitation | Effect | Control |
|---|---|---|
| eToro's own bars start 2022-12 (1000-bar window) | No eToro data for any episode before 2023; VaR/backtest rest on ≈950 trading days | Episodes on the official indices (^NDX / ^GSPC), pinned by test; the P2 overlap check (CFD vs index close ratio ≈ 1.0000, return correlation 0.96–0.97) is the bridge |
| Demo spreads and demo fills | Fill quality measures the demo book; the real account may differ | Reported as "demo"; the pre-registration already frozen the weekend figures as the conservative case |
| No dividend adjustment in the Monte Carlo paths or the CFD P&L | ETF hold understated by the dividend yield; eToro's dividend treatment on long index CFDs unverified | P(beats ETF hold) read as an upper bound; the pre-registration §3 lists it as not modelled |
| `financing_charged` comes from the reconcile's `financing_to_date`, which read 0 after the first night | The modelled-vs-charged comparison is empty until eToro's figure moves | Modelled financing is always reported; check the trade history after the first week (T11) |
| Two fills so far | Fill-quality aggregates are two points; the cost-model ratio is not yet a statistic | Re-run after each soak week; the `n` column is printed |
| Monte Carlo blocks shorter than trends (as M5) | P(beats hold) biased low | Read as a lower bound; the measured-cost scenario brackets the spread effect |
| Portfolio-lag detection before the reconcile waited (2026-10-12) relies on the 10-minute heuristic | A `missing_position` for another reason within 10 minutes of a fill would be read as lag | From 2026-10-13 the reconcile emits `portfolio_lag` explicitly; the heuristic only applies to the one historical day |

## 5. Ongoing monitoring

- **After each soak week (manual until P6 schedules it):**
  ```bash
  cd services/strategy-executor
  go run ./cmd/etoro-risk-report -ledger ./etoro-daily-data/demo/ledger.jsonl \
    -candles ./etoro-daily-data/demo/candles \
    -yahoo ../../docs/backtest-readiness/evidence-2026-10-10/data \
    -reconcile ./etoro-daily-data/demo/reconcile \
    -size 1100 -capital 100000 -paths 2000 -seed 1 \
    -out ./etoro-daily-data/demo/risk/$(TZ=America/New_York date +%F)
  ```
  Output lands in `etoro-daily-data/demo/risk/<NY date>/risk.{json,md}` (git-ignored, like the
  ledger). P6 adds it to the ops cron after the Friday run; P5 surfaces `risk.json` in ui-api.
- **What to look at:** the backtest zone per book (yellow/red → T1 of the 2026-10-09 document),
  historical / parametric above 1.5 (T2), the measured spread per leg against the frozen value
  (the pre-registration's §3 "report both"), modelled vs charged financing once eToro's figure is
  non-zero, and the tracking bps once the paper account has started.
- **Pinned tests:** `cd shared && go test ./pkg/cfdrisk ./pkg/varmodel`;
  `cd services/strategy-executor && go test ./cmd/etoro-risk-report`.

## 6. Review triggers (in addition to T1–T10 of the 2026-10-09 document)

| # | Trigger | First action |
|---|---|---|
| T11 | eToro's `financing_to_date` differs from the modelled financing by more than 20 % after ≥ 5 nights, or stays 0 after a week | Read the position's fees from the trade history; if eToro charges on a different base or calendar, report both figures (the frozen model still decides pass or fail) |
| T12 | Measured spread per leg above the frozen 7.75 / 7.4 bps on any in-session fill | Record it in the forward-test report under "fee changes during the window" |
| T13 | A new index episode (a daily ^NDX or ^GSPC close-to-close move beyond −7 %) | Add it to `cfdrisk.IndexEpisodes`, re-fetch the Yahoo CSVs and regenerate `episode_paths_gen.go` (`-update-episodes`) |
| T14 | The implied NSDQ100/SPX500 correlation below 0.5 for 5 days | Check the aligned-day chain for a data gap on one book before reading it as a regime change |

## 7. Re-running the evidence

```bash
cd shared && go test ./pkg/cfdrisk ./pkg/varmodel                       # M10–M14 pinned tests
cd services/strategy-executor && go test ./cmd/etoro-risk-report          # golden risk.json
go run ./cmd/etoro-risk-report -ledger ./etoro-daily-data/demo/ledger.jsonl \
  -candles ./etoro-daily-data/demo/candles -yahoo ../../docs/backtest-readiness/evidence-2026-10-10/data \
  -reconcile ./etoro-daily-data/demo/reconcile -size 1100 -capital 100000 -paths 2000 -seed 1 \
  -as-of 2026-10-13 -out ./etoro-daily-data/demo/risk/2026-10-13       # the run in §3
```

## 8. Sign-off

| Role | Name | Date | Decision |
|---|---|---|---|
| Model owner (engineering) | | 2026-10-13 | Submitted |
| Independent validator (risk) | | | Pending |
| Operator | | | Pending |
