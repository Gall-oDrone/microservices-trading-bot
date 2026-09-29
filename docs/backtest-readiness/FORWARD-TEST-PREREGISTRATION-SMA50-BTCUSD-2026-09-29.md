# Forward-Test Pre-Registration — SMA50 Trend Rule on Bitso `btc_usd`, Judged in Pesos

Date registered: **2026-09-29** (the git commit that adds this file is the timestamp)  
Repository: `microservices-trading-bot`  
Branch: `feat/k8s-deployment-manifests`  
Audience: Whoever evaluates this in 2027  
Context: [`BTC-USD-BOOK-CHECK-2026-09-29.md`](BTC-USD-BOOK-CHECK-2026-09-29.md) ·
companion test: [`FORWARD-TEST-PREREGISTRATION-SMA50-2026-09-27.md`](FORWARD-TEST-PREREGISTRATION-SMA50-2026-09-27.md) (`btc_mxn`)

This is a paper test: no orders, no capital, no changes to the live bot. It exists because the
`btc_usd` result was found **after** the rule was seen to work on Yahoo BTC-USD and to fail on
`btc_mxn`. Picking the venue that works is a form of selection. Only data that does not exist yet
can tell whether that selection found something real.

> [!IMPORTANT]
> **Do not edit sections 1–4 after 2026-09-29.** Any change restarts the test from the date of the
> change, under a new dated file. Results go in a separate report that links here.

---

## 1. Rule (frozen)

Identical to the `btc_mxn` test except for the instrument:

| Item | Value |
|---|---|
| Instrument | Bitso **`btc_usd`**, daily candles from `/api/v3/ohlc` (`time_bucket=86400`), via [`cmd/bitso-daily`](../../services/strategy-executor/cmd/bitso-daily/main.go) |
| Bar | Mexico City calendar day; today's in-progress bucket excluded |
| Signal | **Long while close > simple average of the last 50 closes (including today); otherwise flat** |
| Timing | Decide at day *t*'s close, fill at day *t+1*'s **open** |
| Side / size | Long-only, all-in / all-out. When flat, capital sits in USD on Bitso |
| Engine | `cmd/daily-research` `trend_sma50`, as of the commit that adds this file |
| Warm-up | Full history from 2020-04-24; no restart unless a > 7-day data gap occurs |

State at registration: the 2026-09-27 close of 83,167 USD is above the SMA50 of 76,148, so the rule
is **long**. The 2026-09-28 bar had not closed at registration. The first forward fill is at
2026-09-29's open and follows mechanically from that close.

## 2. Window and evaluation dates (frozen)

- **Forward window: 2026-09-29 → 2027-09-26.** The end date matches the `btc_mxn` test.
- **Interim look: 2027-03-26.** Report only. **No decision is taken on it**, in either direction.
- **Primary evaluation: 2027-09-26.**
- No early stopping on good results. Stopping on bad results is allowed only with a written reason.

## 3. Costs (frozen)

| Item | Primary | Secondary (pessimistic) |
|---|---|---|
| `btc_usd` trade, per leg | maker 30 bps + 10 slippage = **40 bps** | taker 36 + 10 = 46 bps |
| MXN → USD at the start, USD → MXN at the end (`usd_mxn`) | **60 bps each** (maker) | 78 bps each (taker) |
| Benchmark: hold `btc_mxn` | one round trip at 60 + 10 bps per leg | 78 + 10 bps per leg |

The fees are Bitso's published lowest-volume tier, checked on 2026-09-28. If Bitso changes them
during the window, report both the frozen and the actual fees; **the frozen fees decide pass or
fail**.

## 4. Hypotheses and pass criteria (frozen)

Returns are measured **in MXN**, because that is what an investor starting and ending in pesos
receives. USD/MXN is implied from the two Bitso series on the same day
(`btc_mxn close / btc_usd close`), using
[`scripts/research/mxn-terms.py`](../../scripts/research/mxn-terms.py).

| # | Hypothesis | Pass if, at 2027-09-26, primary costs | Backtest base rate (calendar years 2021–2026) |
|---|---|---|---|
| **H1** | The rule reduces risk | Trend max drawdown (USD) **<** hold `btc_usd` max drawdown | 5 / 6 (failed 2024: 28.4% vs 27.5%) |
| **H2** | The rule beats what a peso investor would otherwise do | Trend return **in MXN, after conversions** **>** hold `btc_mxn` return | 4 / 6 (lost the strong bull years 2023, 2024) |
| **H3** | The timing is not luck | Trend beats **≥ 95%** of 2,000 random same-trade-count strategies on `btc_usd` (seed 1) | 0 / 6 single years; 99.2% over 2020-06 → 2026-09 |

How to read the outcome:
- **H1 and H2 pass:** worth designing a live `btc_usd` strategy and a small, capped Stage soak,
  whatever H3 says. H3 is informative, but it is not expected to pass on a single year.
- **H1 only:** a drawdown tool. Only useful if the objective changes to risk reduction.
- **H2 fails:** close the `btc_usd` variant.

Base rates are from [`btc-usd-maker-plus-slippage.txt`](evidence-2026-09-28/btc-usd-maker-plus-slippage.txt)
and [`btc-usd-in-mxn-terms.txt`](evidence-2026-09-28/btc-usd-in-mxn-terms.txt).

---

## 5. Procedure

```bash
cd services/strategy-executor
go run ./cmd/bitso-daily -book btc_usd -from 2020-04-01 -out ../../fwd/usd/btc_usd_daily.csv
go run ./cmd/bitso-daily -book btc_mxn -from 2017-06-01 -out ../../fwd/mxn/btc_mxn_daily.csv
W=2026-09-29:2027-09-26
go run ./cmd/daily-research -prices ../../fwd/usd -windows $W -buy-bps 30 -sell-bps 30 -slippage-bps 10 > ../../fwd/usd-primary.txt
go run ./cmd/daily-research -prices ../../fwd/mxn -windows $W -buy-bps 60 -sell-bps 60 -slippage-bps 10 > ../../fwd/mxn-primary.txt
cd ../..
python3 scripts/research/mxn-terms.py fwd/usd/btc_usd_daily.csv fwd/mxn/btc_mxn_daily.csv \
  fwd/usd-primary.txt fwd/mxn-primary.txt 60
```

Repeat with the secondary costs (`-buy-bps 36 -sell-bps 36`, benchmark `78`, conversion `78`). For
the interim look use `W=2026-09-29:2027-03-26`. Commit the raw output and both fetched CSVs under
`docs/backtest-readiness/evidence-<date>/`.

**Data integrity:** if Bitso's `btc_usd` history before 2026-09-29 differs from the registered
snapshot ([`btc_usd_daily_bitso.csv`](evidence-2026-09-28/btc_usd_daily_bitso.csv), sha256 prefix
`86450fa73d42d066`), record the differences in the report. The result stands only if they do not
change the rule's position on any forward day.

**Execution note:** the trade archive only records `btc_mxn`, so this test's execution costs are
assumed, not measured (see the check document, §3). Adding `btc_usd` to the data collector before
the interim look would let the report measure them.
