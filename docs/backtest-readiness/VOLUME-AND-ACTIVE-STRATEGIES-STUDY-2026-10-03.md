# Volume and More-Active Strategies Study (2026-10-03)

**Question.** The frozen SMA50 rule sits in "hold" most days. Can a strategy that uses **trading
volume**, or that **acts every week**, beat Bitso's fees, and can anything deliver a small profit
**every week**?

**Short answer.**

1. **No strategy we tested makes money every week.** Even the best variants lose in about 40–60%
   of the weeks they are invested. They win through a few large weeks.
2. **Trading volume spikes on their own (buy after a high-volume day) loses money after fees** on
   both books. There is a weak and inconsistent tendency for prices to keep rising for a few days
   after a high-volume *up* day (3 of 4 windows). Fees eat most of it.
3. **Making the bot act more often lowers returns.** A daily 20/50/100/200 trend ensemble trades
   about 3× as often as SMA50. Its frictionless returns are similar, but costs drag the net result
   below plain SMA50 on every window. Weekly volatility targeting barely changes anything.
4. **One variant stands out, and it trades *less*, not more.** SMA50 where a new entry also needs
   the signal day's volume to be at least 1.5× its 20-day average. It beat SMA50 and buy-and-hold
   on **both books, in the development window and in the untouched 2-year holdout**, with lower
   drawdowns. A threshold sensitivity check, added after the fact, shows a broad plateau, not a
   single lucky value. It is a **candidate for a new pre-registered forward test**, not something
   to trade yet (see §6).

> [!IMPORTANT]
> **Correction to an earlier statement.** On 2026-10-03 the assistant said SMA50 "trades a few times
> a year". That is wrong. On `btc_mxn` it made 155 trades (about 11 round trips a year) over
> 2017-12 to 2024-09, and 53 trades in the last two years. It is in the market about half the time.
> Most days are still "hold", but the rule is not idle for months.

---

## 1. Setup

| Item | Value |
|---|---|
| Data | Bitso production daily candles (Mexico City days): `btc_mxn` 2017-05-31 → 2026-10-01 (3,411 bars), `btc_usd` 2020-04-24 → 2026-10-01 (2,352 bars). Snapshots and SHA-256 in [`evidence-2026-10-03/`](evidence-2026-10-03/). |
| Windows | **Development**: first warm bar (after 200 days) → 2024-09-30. **Holdout**: 2024-10-01 → 2026-09-30 (730 days), the last two years. |
| Discipline | All 16 variants and their parameters were written into the code **before** either window was run. Every variant is reported for both windows. Nothing was chosen on the holdout. The only thing added after seeing results is the sensitivity table in §4, which is labelled as post-hoc and does not change the chosen threshold. |
| Execution | Decision at day *t*'s close, filled at day *t+1*'s open; long or flat (spot); a position open at the end is sold at the last close. Same model as `cmd/daily-research` and the pre-registrations. |
| Costs per leg | Pre-registered levels: `btc_mxn` **70 bps** (maker 60 + slippage 10), `btc_usd` **40 bps** (taker 30 + slippage 10). Stress: `btc_mxn` 88 bps (taker 78 + 10), `btc_usd` 60 bps. |
| Tool | [`services/strategy-executor/cmd/weekly-research`](../../services/strategy-executor/cmd/weekly-research/main.go) (with unit tests). |

Reproduce:

```bash
cd services/strategy-executor
E=../../docs/backtest-readiness/evidence-2026-10-03
go run ./cmd/weekly-research -book btc_mxn -csv $E/btc_mxn_daily_2026-10-01.csv -leg-bps 70 -stress-leg-bps 88
go run ./cmd/weekly-research -book btc_usd -csv $E/btc_usd_daily_2026-10-01.csv -leg-bps 40 -stress-leg-bps 60
```

### Variants (fixed in advance)

| Group | Variant | Idea |
|---|---|---|
| Baselines | buy-and-hold; SMA50 (the frozen rule) | |
| Volume filter | SMA50, but a **new entry** needs volume ≥ 1.5× its 20-day mean | Your idea, used as confirmation |
| More activity | 20/50/100/200 trend ensemble (position in 25% steps), daily and weekly | Acts more often, in smaller steps |
| Weekly sizing | Volatility target 50%/yr on buy-and-hold, on SMA50, on the ensemble; re-sized on Sundays | Acts every week |
| Volume spikes | Buy after a day with volume ≥ 2× or 3× its 20-day mean, on up days or down days, hold 1 or 5 days | Your idea, used as the signal |

---

## 2. Results: holdout (2024-10-01 → 2026-09-30), base costs

| Strategy | `btc_mxn` return | max DD | trades | `btc_usd` return | max DD | trades |
|---|---:|---:|---:|---:|---:|---:|
| Buy-and-hold | +19.8% | 54.8% | 1 | +31.0% | 52.8% | 1 |
| SMA50 (frozen) | +22.6% | 42.6% | 53 | +60.9% | 30.0% | 43 |
| **SMA50 + volume ≥ 1.5× entry** | **+53.7%** | **25.2%** | 23 | **+64.8%** | **21.0%** | 19 |
| Ensemble, daily | +29.5% | 36.7% | 143 | +51.7% | 26.5% | 144 |
| Ensemble, weekly | +35.5% | 34.7% | 61 | +33.4% | 32.3% | 64 |
| Vol-target 50%, weekly | +17.0% | 55.1% | 8 | +36.6% | 52.5% | 9 |
| SMA50 × vol-target, weekly | +19.8% | 42.6% | 55 | +61.0% | 30.0% | 43 |
| Ensemble × vol-target, weekly | +33.7% | 34.0% | 63 | +34.4% | 32.3% | 64 |
| Best volume-spike rule (2× up day, hold 5d) | +6.1% | 18.3% | 40 | +9.9% | 27.1% | 44 |

## 3. Results: development window, base costs

`btc_mxn` 2017-12-17 → 2024-09-30; `btc_usd` 2020-11-10 → 2024-09-30.

| Strategy | `btc_mxn` return | max DD | trades | `btc_usd` return | max DD | trades |
|---|---:|---:|---:|---:|---:|---:|
| Buy-and-hold | +221% | 83.5% | 1 | +311% | 76.8% | 1 |
| SMA50 (frozen) | +122% | 63.7% | 155 | +393% | 60.3% | 69 |
| **SMA50 + volume ≥ 1.5× entry** | **+763%** | **48.9%** | 61 | **+539%** | **51.1%** | 37 |
| Ensemble, daily | +95% | 67.2% | 524 | +309% | 53.4% | 250 |
| Ensemble, weekly | +174% | 63.1% | 198 | +148% | 61.1% | 119 |
| Vol-target 50%, weekly | +278% | 76.7% | 78 | +214% | 74.1% | 45 |
| Ensemble × vol-target, weekly | +282% | 49.8% | 207 | +106% | 55.4% | 129 |
| Best volume-spike rule (2× up day, hold 5d) | +12% | 40.0% | 86 | −16% | 56.9% | 72 |

What the cost columns in the evidence show:
- **Fees are the main reason more activity loses.** The daily ensemble on `btc_mxn` would have made +481% with zero costs but kept +95%. Its fees came to 140% of starting equity.
- **The volume filter improves the signal, not just the fee bill.** SMA50 + volume ≥ 1.5× also has higher frictionless returns than SMA50 (+1,232% vs +563% on `btc_mxn` development). Filtering out low-volume entries removes many whipsaw trades.
- **Stress costs keep the same ordering.** At 88 bps/leg on `btc_mxn` and 60 bps/leg on `btc_usd`, the volume-confirmed variant stays first in every window.

## 4. Post-hoc sensitivity of the volume threshold

This was added after seeing §2–§3, so it is a robustness check, not a selection. Return / max DD at base costs:

| k (entry needs volume ≥ k × 20-day mean) | `btc_mxn` dev | `btc_mxn` holdout | `btc_usd` dev | `btc_usd` holdout |
|---|---:|---:|---:|---:|
| none (= SMA50) | +122% / 64% | +23% / 43% | +393% / 60% | +61% / 30% |
| 1.00 | +399% / 56% | +30% / 36% | +518% / 57% | +75% / 22% |
| 1.25 | +429% / 54% | +40% / 36% | +600% / 54% | +66% / 21% |
| **1.50 (pre-declared)** | **+763% / 49%** | **+54% / 25%** | **+539% / 51%** | **+65% / 21%** |
| 1.75 | +825% / 41% | +67% / 24% | +655% / 36% | +71% / 21% |
| 2.00 | +782% / 41% | +49% / 28% | +518% / 45% | +38% / 29% |
| 2.50 | +642% / 41% | +67% / 19% | +364% / 38% | +20% / 29% |

Every threshold from 1.0 to 1.75 beats plain SMA50 in all four windows. Above 2.0 the `btc_usd` holdout degrades because too few entries are left. That is a plateau, which is what a real effect looks like. A knife-edge optimum would look like overfitting.

## 5. Volume-spike event study (your "high volume = opportunity" idea)

This is the average return from the next open to the close *h* days after a high-volume day, against the same measurement after every day. Mean % / number of events:

| Condition, h = 5 days | `btc_mxn` dev | `btc_mxn` holdout | `btc_usd` dev | `btc_usd` holdout |
|---|---:|---:|---:|---:|
| All days (baseline) | +0.58 / 2475 | +0.26 / 725 | +0.77 / 1416 | +0.33 / 725 |
| Volume ≥ 2×, up day | **+2.30** / 56 | **+1.83** / 29 | +0.77 / 46 | **+1.18** / 37 |
| Volume ≥ 2×, down day | +1.11 / 66 | −0.26 / 31 | −0.33 / 48 | −0.43 / 36 |
| Volume ≥ 3×, up day | −2.68 / 7 | +0.03 / 8 | −4.62 / 8 | +5.57 / 10 |
| Volume ≥ 3×, down day | −5.05 / 12 | +0.95 / 8 | −1.23 / 15 | −2.76 / 11 |

Reading it:
- **High-volume up days are followed by slightly higher returns** in 3 of 4 windows (continuation). After a round trip's costs, that leaves about +0.4% to +0.9% per trade on `btc_mxn` and about 0 to +0.4% on `btc_usd`. The events are rare: about 8–18 a year.
- **High-volume down days do not reliably bounce.** A 1-day bounce showed up in the development windows (+1.1% to +1.3%) and vanished in the holdout.
- **3× spikes are too rare to say anything** (7–15 events per window), and their signs flip between windows.
- As trading rules, these events lose money or barely break even (§2–§3): too few trades, and the bad weeks are large.

## 6. Can anything make a profit every week?

Number of weeks with a gain, loss, or no position, holdout, base costs:

| Strategy | `btc_mxn` weeks up / down / flat | `btc_usd` weeks up / down / flat |
|---|---|---|
| Buy-and-hold | 48 / 57 / 0 | 55 / 50 / 0 |
| SMA50 | 30 / 42 / 33 | 37 / 34 / 34 |
| SMA50 + volume ≥ 1.5× | 23 / 32 / 50 | 30 / 26 / 49 |

The best performer on `btc_mxn` had **more losing weeks than winning weeks** and still returned +54%. These strategies make money by catching a few large up-moves and sitting out large down-moves, not by grinding out small weekly gains. A weekly-profit target is not compatible with what the data shows. A better target is positive returns over a quarter or a year, with a capped drawdown.

## 7. Caveats

- **Multiple testing.** 16 variants × 2 books × 2 windows were looked at. The volume-confirmed SMA50 result is consistent across all four windows and the post-hoc threshold plateau, which makes luck less likely, but this is still a backtest.
- **No random-entry baseline yet.** Earlier studies required beating ≥ 95% of random same-trade-count strategies (H3 in the pre-registrations). That must be computed before pre-registering the new variant.
- **Small samples.** The holdout has 19–23 trades per book. One or two big trades can swing the result.
- **Volume data quality.** Bitso daily volume is in BTC, per Mexico City day. Volume regimes shift over the years (exchange growth, promotions). The 20-day ratio adapts, but a structural change in how volume is reported would break the filter.
- **Same market, two books.** `btc_mxn` and `btc_usd` are both Bitcoin, so they are not independent confirmations.

---

## 8. State-of-the-art literature (2024–2026)

**Can the assistant use Google Scholar?** Yes, partly. Fetching Scholar result pages works and returns titles, authors, venues, citation counts and links (PDF links when open access). It does **not** return abstracts. One query took about 7 minutes, and Scholar may rate-limit or show a CAPTCHA if queried too much. For abstracts and full text, the arXiv API (`export.arxiv.org/api/query`), SSRN and open-access PDFs work. The list below was found through Scholar on 2026-10-03, filtered to 2024–2026. **Only titles and venues were read; the papers themselves have not been reviewed yet.**

| Topic | Paper (2024–2026) | Relevance |
|---|---|---|
| Trend following | *Systematic trend-following with adaptive portfolio construction: enhancing risk-adjusted alpha in cryptocurrency markets*, arXiv 2602.11708 (2026) | Multi-horizon trend plus position sizing, close to the ensemble tested here |
| Trend following | *Time-series momentum and market timing in Bitcoin*, Risk Management (Palgrave, 2026), link.springer.com/article/10.1057/s41283-026-00234-7 | Direct test of the family SMA50 belongs to |
| Trend following | *Time-series momentum in cryptocurrency: a published edge that has aged — regime-dependence and retail feasibility (2016–2023)*, SSRN 7405060 | Whether trend still pays at retail costs; relevant to the fee findings |
| Trend + risk | *Risk-managed time-series momentum in crypto majors: crash-state de-risking and drawdown control*, SSRN 7115459 | Drawdown control, which is where SMA50's edge has shown up so far |
| Volume | *Cryptocurrency volume-weighted time series momentum*, SSRN 4825389 | **Closest to the volume-confirmed result**; read first |
| Volume | *On the dynamic relationship between transaction volume and returns: evidence from the cryptocurrency market*, J. Economic and Administrative Sciences 41(2) | Volume–return causality |
| Volatility sizing | *Cryptocurrency market risk-managed momentum strategies*, Finance Research Letters (2025) | Volatility-scaled momentum |
| Volatility sizing | *Adaptive risk allocation in crypto markets: evaluating volatility-scaled portfolios*, SSRN 5090097 | Volatility targeting in crypto; here it changed little |
| Volatility sizing | *Cryptocurrency momentum has (not) its moments*, Financial Markets and Portfolio Management (2025) | Whether momentum survives risk scaling |
| Reversal | *The tail is the only signal: flush reversion in equity indices and crypto perpetual futures*, SSRN 7363482 | Down-day bounce; the holdout here did not support it on spot |
| Intraday | *Periodicity in cryptocurrency volatility and liquidity*, J. Financial Econometrics 22(1) (2024), arXiv 2109.12142 | Time-of-day volume/volatility patterns; relevant once the collector has months of trades |
| Predictability | *Variance decomposition and cryptocurrency return prediction*, JFQA (2024) | What part of returns is predictable at all |
| ML forecasting | *Predicting cryptocurrency returns with machine learning: evidence from high-dimensional factor modeling*, Pacific-Basin Finance Journal (2025) | Mostly cross-sectional (many coins); limited use for a single BTC book |
| ML trading | *A profitable trading algorithm for cryptocurrencies using a neural network model*, Expert Systems with Applications (2024) | Check whether it uses realistic fees and a true holdout before trusting it |
| Survey | *Cryptocurrency trading: a comprehensive survey*, Fang et al. (Financial Innovation 2022; listed by Scholar for its 2025 open-access book reprint, 768 citations) | Broad map of methods |

**Pattern across the literature (from titles and prior knowledge, to be confirmed by reading):**
trend and time-series momentum, with risk management, are the most robust published edges in
crypto. Volume helps mainly as a weight or a confirmation. Deep-learning papers rarely survive
realistic fees and a true out-of-sample test. This matches what the data here shows.

---

## 9. Plan

1. **Keep the frozen SMA50 forward tests unchanged.** Nothing here changes them; changing them would void the pre-registration.
2. **Compute the random same-trade-count baseline** for SMA50 + volume ≥ 1.5× on both books (as in `cmd/daily-research`). Continue only if it beats ≥ 95% of random runs over the full history.
3. **Pre-register "SMA50 with volume-confirmed entries (k = 1.5)" as a second forward test** on `btc_mxn` and `btc_usd`. Use the same structure as the existing pre-registrations: frozen rule, costs, start date, interim and evaluation dates, pass criteria. Keep k = 1.5 as declared; the sensitivity plateau supports it without re-tuning.
4. **Run it in `daily-executor` in dry-run mode alongside the frozen rule** (separate ledger), then on stage once the code is reviewed. That is a small change: one more frozen spec plus the volume ratio from the same candles.
5. **Do not build** the volume-spike rules, the daily ensemble or volatility targeting. They did not beat simpler rules after fees.
6. **Intraday volume research waits for data.** The collector has `btc_mxn` trades since 2026-08-19 and `btc_usd` since 2026-09-30. That is too short for anything intraday. Revisit with at least 6–12 months, starting from the time-of-day periodicity paper above.
7. **Fees are the biggest lever.** Prefer `btc_usd` for anything active, use maker orders, and check Bitso's volume-based fee tiers.
8. **Read the five most relevant papers in full**: volume-weighted TSMOM, aged-edge TSMOM, risk-managed TSMOM, Bitcoin TSMOM and market timing, and adaptive trend-following. Note anything that contradicts these findings.

## Evidence

- [`evidence-2026-10-03/weekly-research-btc-mxn.txt`](evidence-2026-10-03/weekly-research-btc-mxn.txt): full output, both windows, base and stress costs, post-hoc sensitivity
- [`evidence-2026-10-03/weekly-research-btc-usd.txt`](evidence-2026-10-03/weekly-research-btc-usd.txt): same for `btc_usd`
- [`evidence-2026-10-03/btc_mxn_daily_2026-10-01.csv`](evidence-2026-10-03/btc_mxn_daily_2026-10-01.csv), [`btc_usd_daily_2026-10-01.csv`](evidence-2026-10-03/btc_usd_daily_2026-10-01.csv): input snapshots, with [`SHA256SUMS`](evidence-2026-10-03/SHA256SUMS)
