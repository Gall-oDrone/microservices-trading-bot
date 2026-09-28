# Forward-Test Pre-Registration — SMA50 Trend Rule on Bitso `btc_mxn`

Date registered: **2026-09-27** (the git commit that adds this file is the timestamp)  
Repository: `microservices-trading-bot`  
Branch: `feat/k8s-deployment-manifests`  
Audience: Whoever evaluates this in 2027  
Context: [`BTC-MXN-TREND-CHECK-2026-09-27.md`](BTC-MXN-TREND-CHECK-2026-09-27.md)

This file fixes, **before any forward data exists**, exactly what will be tested, how, and what
counts as success. It is a paper test: no orders, no capital, no changes to the live bot.

> [!IMPORTANT]
> **Do not edit sections 1–4 after 2026-09-27.** If anything in them changes, the test restarts from
> the date of the change under a new, dated file. Results go in a separate report that links here.

---

## 1. Rule (frozen)

| Item | Value |
|---|---|
| Instrument | Bitso `btc_mxn`, daily candles from `/api/v3/ohlc` (`time_bucket=86400`), via [`cmd/bitso-daily`](../../services/strategy-executor/cmd/bitso-daily/main.go) |
| Bar | Mexico City calendar day; today's in-progress bucket excluded |
| Signal | **Long while close > simple average of the last 50 closes (including today); otherwise flat** |
| Timing | Decide at day *t*'s close, fill at day *t+1*'s **open** |
| Side / size | Long-only, all-in / all-out |
| Engine | `cmd/daily-research` `trend_sma50`, as of the commit that adds this file |
| Warm-up | Full history from 2017-05-31; no restart unless a > 7-day data gap occurs |

State at registration: the 2026-09-26 close of 1,493,130 MXN is above the SMA50 of 1,294,875, so
the rule is **long**, and the forward window opens long at 2026-09-27's open.

## 2. Window and evaluation dates (frozen)

- **Forward window starts 2026-09-27.** Everything before it is backtest and is not evaluated here.
- **Interim look: 2027-03-26** (6 months). Report only. **No decision is taken on it**, in either
  direction.
- **Primary evaluation: 2027-09-26** (12 months).
- No early stopping on good results. Stopping on bad results is allowed only with a written reason.

## 3. Costs (frozen)

| Scenario | Per leg | Round trip | Role |
|---|---|---:|---|
| **Maker + slippage** | 60 bps + 10 bps | **140 bps** | **Primary** |
| Taker + slippage | 78 bps + 10 bps | 176 bps | Secondary (pessimistic) |
| Frictionless | 0 | 0 | Diagnostic only (does the signal time at all?) |

## 4. Hypotheses and pass criteria (frozen)

The benchmark is buy-and-hold `btc_mxn` over the same window, paying one round trip at the same
costs.

| # | Hypothesis | Pass if, at 2027-09-26, primary costs | Backtest base rate at primary costs (calendar years 2018–2026 it held) |
|---|---|---|---|
| **H1** | The rule reduces risk | Trend max drawdown **<** hold max drawdown | 6 / 9 |
| **H2** | The rule adds return | Trend return **>** hold return | 5 / 9: won the flat and down years, lost every strong bull year (2019, 2020, 2023, 2024) |
| **H3** | The timing is not luck | Trend beats **≥ 95%** of 2,000 random same-trade-count strategies (seed 1) | 1 / 9 (2020, exactly 95.0%) |

How to read the outcome:
- **All three pass:** worth designing a live strategy and a small, capped Stage soak.
- **H1 only:** a drawdown tool. Only useful if the product objective is changed to risk reduction.
- **None pass, or only H3:** close the line of work.

From the backtest, H2 is roughly a coin flip that depends on the market (a bull year means it will
probably fail), and H3 will most likely **fail** over a single year. Registering them anyway is the
point: the forward result must be able to disagree with the backtest. Base rates are from
[`btc-mxn-maker-plus-slippage.txt`](evidence-2026-09-27/btc-mxn-maker-plus-slippage.txt).

---

## 5. Procedure

```bash
cd services/strategy-executor
go run ./cmd/bitso-daily -book btc_mxn -from 2017-06-01 -out ../../bitso/btc_mxn_daily.csv
go run ./cmd/daily-research -prices ../../bitso -windows 2026-09-27:2027-09-26 \
  -buy-bps 60 -sell-bps 60 -slippage-bps 10          # primary
go run ./cmd/daily-research -prices ../../bitso -windows 2026-09-27:2027-09-26    # taker
go run ./cmd/daily-research -prices ../../bitso -windows 2026-09-27:2027-09-26 \
  -buy-bps 0 -sell-bps 0 -slippage-bps 0            # diagnostic
```

(For the interim look, use `2026-09-27:2027-03-26`.)

Commit the raw output and the fetched CSV under `docs/backtest-readiness/evidence-<date>/`, and write
a short results report linking here.

**Data integrity.** If Bitso's history for dates before 2026-09-27 differs from the registered
snapshot ([`btc_mxn_daily_bitso.csv`](evidence-2026-09-27/btc_mxn_daily_bitso.csv), sha256 prefix
`b85ee055a653bf87`), record the differences in the report. The forward result stands only if they do
not change the rule's position on any forward day.

**Proposing variants.** Any other rule (a different SMA length, a band, a slower filter) needs its own
dated pre-registration file before its forward window starts. It can't be evaluated on the forward
data of this test, because that data will already have been looked at.
