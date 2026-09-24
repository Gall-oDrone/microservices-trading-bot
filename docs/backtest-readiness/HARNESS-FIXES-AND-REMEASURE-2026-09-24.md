# Harness Fixes and Re-measure — R1, R2, R4 Implemented

Date: **2026-09-24**  
Repository: `microservices-trading-bot`  
Branch: `feat/k8s-deployment-manifests`  
Audience: Whoever decides what work happens next on the strategies, and whoever deploys `strategy-executor`  
Follows: [`PATH-TO-PROFITABILITY-2026-09-23.md`](PATH-TO-PROFITABILITY-2026-09-23.md)

The 2026-09-23 report ranked five recommendations. This document records the work done on them and
the numbers produced by the corrected harness.

| Rec | Scope | Status |
|---|---|---|
| **R1** | Fee gate on by default; finish `momentum` gating; a test proving the gate is active | ✅ Done |
| **R2** | Fix the ruler: two-leg commission, simulated clock, `buy_and_hold` baseline, `internal/processor` tests | ✅ Done |
| **R3** | Move horizon to 1h–4h | ⏭️ **Skipped deliberately.** It needs 6–12 months of archive to mean anything. |
| **R4** | Cost structure: model maker vs taker | ✅ Done, **in the backtest only**. Live order placement is untouched. |
| **R5** | Signal work | ⏸️ **Not started, by decision.** This round only re-measures; no strategy tuning. |

> [!WARNING]
> **Behaviour change for live deployments.** The fee gate is now **on by default in production code
> paths**, not just in backtests (§2.1). `mean_reversion` and `momentum` will refuse entries whose
> expected move does not clear the estimated round-trip cost, and `momentum` refuses **every** entry
> while ATR is unavailable. Opt out per strategy with `fallback_round_trip_bps: 0`, and only when no
> fee provider is configured.

---

## 1. Headline results

The window is **34 days of `btc_mxn`** (2026-08-19 → 2026-09-22, 70,708 trades). Slippage is 10 bps per leg,
and every strategy uses its **production** `min_signal_interval`, which the simulated clock now makes
meaningful (§2.2). The column headings name the fee scenario.

| Strategy | (a) taker/taker 78/78 | (b) maker buy 60 / taker sell 78 | (c) maker/maker 60/60 | (d) flat 65/65 |
|---|---:|---:|---:|---:|
| `mean_reversion` | 3 trades / **+23.27** | 4 / **+29.39** | 4 / **+27.43** | 4 / **+29.51** |
| `momentum` | 0 / 0.00 | 0 / 0.00 | 0 / 0.00 | 0 / 0.00 |
| `limit_profit` | 10 / **+81.18** | 10 / **+92.45** | 11 / **+110.53** | 11 / **+92.82** |
| `random_baseline` | 95 / −2,372.98 | 95 / −2,142.78 | 95 / −1,913.27 | 95 / −2,040.97 |
| **`buy_and_hold`** | 1 / **+312.66** | 1 / **+314.75** | 1 / **+317.45** | 1 / **+316.12** |

All P&L figures are **net MXN** and include both legs of commission plus slippage. Scenarios (a)–(c)
use fee rates **fetched live from the Bitso API** (§3). Evidence:
[`evidence-2026-09-24/`](evidence-2026-09-24/).

> [!CAUTION]
> **Nothing beats buy-and-hold.** Holding a single 0.001 BTC position for the window earned
> **~10× more** than the best active strategy under every fee scenario. The active strategies are now
> *non-negative*, but only because the gate stops them trading. `mean_reversion` made 4 trades in
> 34 days and `limit_profit` made 11. **No regime bucket reached the 30-sample bar in any scenario**,
> so none of these profits is a supported finding.

### 1.1 What the harness fixes changed, in isolation

These are the same strategies over the same window, with only one factor changing per step.

| Configuration | `mean_reversion` | `limit_profit` | `random_baseline` | Evidence |
|---|---:|---:|---:|---|
| 2026-09-22 harness (one-leg commission, wall-clock throttle disabled) | 700 / −7,291.40 | 38 / −113.45 | 95 / −1,209.71 | [prior](evidence-2026-09-22/backtest-run-fixed-no-throttle.txt) |
| **(f)** Fixed harness, **no gate at all** | **660 / −12,642.68** | 34 / −409.15 | **95 / −2,040.97** | [f](evidence-2026-09-24/backtest-f_legacy65_ungated.txt) |
| **(e)** Fixed harness, only the new **default** gate (130 bps fallback, no injected rates) | 4 / +21.04 | 34 / −409.15 | 95 / −2,040.97 | [e](evidence-2026-09-24/backtest-e_legacy65_feeblind.txt) |
| **(d)** Fixed harness, rates **injected** (commission + slippage per leg) | 4 / +29.51 | 11 / +92.82 | 95 / −2,040.97 | [d](evidence-2026-09-24/backtest-d_legacy65.txt) |

How to read this:

- **The two-leg commission fix roughly doubles the measured losses.** `random_baseline` makes the
  identical 95 trades, but its loss goes from −1,209.71 to −2,040.97 once the buy-leg fee is charged. The
  ungated `mean_reversion` loss goes from −7,291 to −12,643. Every number published before today was
  optimistic by about half a round trip.
- **The gate does all of the work.** Row (f) → row (e) takes `mean_reversion` from 660 trades and −12,643
  MXN to 4 trades and +21, purely by declining to trade.
- **`limit_profit` needs the injected rates** because it gates on `use_bitso_fees`, not on the fallback.
  With the rates injected it goes from 34 trades / −409 to 11 trades / +93.
- **`random_baseline` is identical across (d), (e) and (f)**, as it should be, since it ignores fees. That is a
  useful sanity check that the fee injection doesn't leak into the accounting.

### 1.2 `momentum` makes zero trades, and did before this work

`momentum` closed **0 trades in every scenario, including the fully ungated (f)**. It also closed 0 in
all three 2026-09-22 runs ([example](evidence-2026-09-22/backtest-run-fixed-no-throttle.txt)). So the
new gate did not cause this. Its entry conditions are simply never met on this data at these parameters.
Finding out why is signal work (R5), so it has deliberately not been investigated. It is recorded here so
nobody mistakes "0 trades, 0 loss" for a result.

---

## 2. Changes made

All changes are in `services/strategy-executor`.

### 2.1 R1: fee gate on by default

| Change | File |
|---|---|
| `DefaultFallbackRoundTripBPS = 130` (65 bps × 2 legs). The gate is now active even when no fee provider is configured. Opt out with `fallback_round_trip_bps: 0`. | [`fee_gate.go`](../../services/strategy-executor/internal/strategies/fee_gate.go) |
| `mean_reversion` defaults to that fallback. The stale "no-op by default" comment is corrected. | [`mean_reversion.go`](../../services/strategy-executor/internal/strategies/mean_reversion.go) |
| `momentum` gets full gating. New params: `min_net_profit_bps`, `fallback_round_trip_bps` (default 130) and `expected_move_atr_mult` (default 1.0). The expected move is ATR × mult, and both long and short entries are gated. **A missing ATR fails closed.** A voluntary `take_profit` exit refuses to book a net loss. Stop-loss, max-hold and circuit-breaker exits are unaffected. `momentum` now implements `SetFeeRatesProvider`, so the live registry injects Bitso rates into it the same way it does for `mean_reversion`. | [`momentum_strategy.go`](../../services/strategy-executor/internal/strategies/momentum_strategy.go) |
| The backtest engine injects per-leg rates (commission + slippage) into any strategy that accepts a provider. For `limit_profit` it defaults `use_bitso_fees=true` unless the caller sets it. `DisableFeeRates` switches injection off. | [`engine.go`](../../services/strategy-executor/internal/backtest/engine.go) |

> [!NOTE]
> The 130 bps default was chosen from the rates in the code fixtures (65 bps taker). §3 shows the Bitso
> **stage** account actually charges **78 bps** taker, which puts the real taker round trip at 156 bps
> plus slippage. The fallback only applies when no live fee provider is configured, which is the case
> when the service has no Bitso API key and secret (`config.Bitso.FeesEnabled` is false). It has been **left at 130** because retuning defaults is out of
> scope. Revisit it once production rates are confirmed.

### 2.2 R2: fixing the ruler

| Problem | Fix | File |
|---|---|---|
| Only the sell-leg commission was charged, so every trade looked better than reality by about half a round trip | Per-trade P&L is now `exitNet − entryFee`. The balance is still debited at entry and credited `exitNet` at exit, so the balance and the reported P&L agree. | [`runner.go`](../../services/strategy-executor/internal/backtest/runner.go) |
| `MinSignalInterval` and hold-time checks ran on the wall clock, so trade counts depended on CPU speed | Added an injectable `Clock` to `BaseEnhancedStrategy`. Every `time.Now()` / `time.Since()` in `mean_reversion`, `momentum` and `limit_profit` now reads it. The runner seeds the clock from the first trade's timestamp *before* `Start()`, advances it per tick, and restores the wall clock afterwards. | [`clock.go`](../../services/strategy-executor/internal/strategies/clock.go), [`enhanced_strategy.go`](../../services/strategy-executor/internal/strategies/enhanced_strategy.go), [`data_provider.go`](../../services/strategy-executor/internal/backtest/data_provider.go) |
| A position still open at the end of data was silently dropped, so buy-and-hold reported 0 trades | Any open position is closed at the last price, charged slippage and sell commission, and tagged `end_of_backtest`. This can be turned off with `DisableEndOfRunClose`. | [`runner.go`](../../services/strategy-executor/internal/backtest/runner.go) |
| No buy-and-hold baseline | `buy_and_hold` buys 0.001 BTC on the first tick and holds. The end-of-run close books it. | [`buy_and_hold.go`](../../services/strategy-executor/internal/backtest/buy_and_hold.go) |
| `internal/processor` tests did not compile on `HEAD` | The test literals referenced fields that `models.TradeEvent` no longer has (`MakerOrderID`, `TakerOrderID`, `Source`, `Metadata`, and `CreatedAt` which became `CreatedAtMillis`). They were updated to the current struct. The `event.Metadata` assertions refer to the processor's own `MarketEvent` and were correct as they stood. | [`data_processor_test.go`](../../services/strategy-executor/internal/processor/data_processor_test.go), [`filters_test.go`](../../services/strategy-executor/internal/processor/filters_test.go) |

### 2.3 R4: maker/taker in the backtest

`RunnerConfig` and `EngineConfig` gained `BuyCommissionBPS` and `SellCommissionBPS`. When either is set,
they replace the flat `CommissionBPS` for that leg. `cmd/backtest-archive` exposes these new flags:

| Flag | Purpose |
|---|---|
| `-buy-commission-bps`, `-sell-commission-bps` | Set each leg's commission explicitly |
| `-fees-from-bitso` | Fetch the account's real maker/taker rates for `-book`. Reads `BITSO_KEY` and `BITSO_SECRET` **from the environment only** and prints only the rates, never the credentials. |
| `-buy-liquidity`, `-sell-liquidity` | With `-fees-from-bitso`: whether each leg is `maker` or `taker` |
| `-bitso-api-base-url` | Defaults to `$BITSO_API_BASE_URL`, then `https://stage.bitso.com/api`, which matches the k8s manifests. The shared client's built-in default is production, where these keys return 401. |
| `-disable-fee-rates` | Don't inject rates into strategies. Use this for before/after comparisons. |

Live order placement is unchanged. The code still places the same order types as before.

### 2.4 Tests added

| Test file | What it proves |
|---|---|
| [`runner_test.go`](../../services/strategy-executor/internal/backtest/runner_test.go) (8 tests) | The throttle follows simulated time, not wall time. The clock is seeded before `Start()` and restored afterwards. Both commission legs are charged. The balance matches the sum of P&L. Per-leg overrides apply. The end-of-run close happens, is charged its costs, and can be turned off. |
| [`engine_fees_test.go`](../../services/strategy-executor/internal/backtest/engine_fees_test.go) | The engine injects buy = (50+10)/1e4 and sell = (65+10)/1e4 into the strategy, and `DisableFeeRates` suppresses the injection |
| [`momentum_fee_gate_test.go`](../../services/strategy-executor/internal/strategies/momentum_fee_gate_test.go) (7 tests) | The gate is on by default. There is a table of the ATR-scaled entry threshold and a check of the short-side arithmetic. A missing ATR blocks entry. The explicit opt-out works. `take_profit` refuses a net loss, while stop-loss still realises one. |
| [`mean_reversion_fee_gate_test.go`](../../services/strategy-executor/internal/strategies/mean_reversion_fee_gate_test.go) | `GateOnByDefault` replaces the old test asserting "ungated by default". `ExplicitOptOutDisablesGate` is new. |

**Mutation check:** with the runner's `SetClock` call removed, the throttle and clock tests **fail**. They
really do detect the wall-clock bug they were written for, rather than passing by accident.

Existing signal-logic tests in `mean_reversion_test.go` and `enhanced_registry_test.go` now pass
`fallback_round_trip_bps: 0`. They test signal generation, not cost gating, and their synthetic price
moves (for example ~127 bps) sit just below the new 130 bps default.

`go vet ./...` and `go test ./...` are **green across the whole `strategy-executor` module**, including
`internal/processor` for the first time on this branch.

---

## 3. Real Bitso fee rates: higher than the code assumed

`-fees-from-bitso` against the **stage** API returned this for `btc_mxn`:

| Role | Rate the code assumed ([`fee_compute_test.go`](../../shared/pkg/bitso/fee_compute_test.go#L26-L29)) | **Stage API, 2026-09-24** |
|---|---:|---:|
| Maker | 50 bps | **60 bps** |
| Taker | 65 bps | **78 bps** |

The real taker round trip is **156 bps + 20 bps slippage = 176 bps**, against the 150 bps used in every
earlier report. The 2026-09-23 conclusion, that the cost is ~50× the median one-minute move, therefore
**understated** the problem.

> [!IMPORTANT]
> These are the rates for the **stage** account the keys belong to. Production rates depend on the
> production account's volume tier and may differ. Before relying on the 60/78 figures, run
> `-fees-from-bitso -bitso-api-base-url https://bitso.com/api` with **production** keys.

Across the fee scenarios, going maker-only (c vs a) improves `limit_profit` by +29 MXN and
`random_baseline` by +460 MXN over 95 trades. That is real, but it doesn't change any conclusion. At
60 bps a leg this venue is still roughly 6× the cost of a major exchange.

---

## 4. Caveats

- **Small samples.** 3–11 trades per active strategy. No per-regime bucket reached 30 samples, except
  `random_baseline` in `low_vol_range`. Every active-strategy profit in §1 is **directional only**.
- **One window, one trend.** The 34 days were a ~+29% uptrend, which flatters buy-and-hold and punishes
  mean reversion. A falling market would reverse that ordering.
- **Tiny position size.** Strategies trade ~0.001 BTC against a 100,000 MXN balance. The absolute MXN
  figures are only meaningful relative to each other.
- **Slippage is still assumed** (10 bps per leg), not measured. We have no order-book data.
- **Gated ≠ profitable.** A strategy that trades 4 times in a month and wins is mostly a strategy that
  has learned not to trade. Whether its 4 entries carry real edge cannot be told from 4 samples.

---

## 5. Where this leaves the recommendations

| Rec | State after this round |
|---|---|
| R1 | Done. The gate is on everywhere by default and covered by tests. |
| R2 | Done. The ruler is now trustworthy for what it measures. |
| R3 | Still the most promising strategic lever. It is blocked on archive length (6–12 months), as agreed. |
| R4 | Modelled. The real stage rates are *worse* than assumed. Confirming production rates and pursuing a volume tier or another venue remain commercial actions. |
| R5 | Not started, by decision. The first item when it opens is **why `momentum` never enters** (§1.2). |

**Bottom line:** the harness now tells the truth. Two things it says are uncomfortable. First, fixing
the commission bug made every ungated loss about twice as large. Second, even with the gate turned on, the best
active result is about a tenth of simply holding. The gate has turned catastrophic losers into
near-zero-activity break-even strategies. It has not produced an edge.

---

## 6. Reproducing

```bash
cd services/strategy-executor
go build -o /tmp/bta ./cmd/backtest-archive

COMMON="-archive ./archive -book btc_mxn -from 2026-08-19 -to 2026-09-22T23:59:59Z \
  -labels ./regime_labels.csv \
  -strategies mean_reversion,momentum,limit_profit,random_baseline,buy_and_hold \
  -slippage-bps 10"

# (a)-(c): live stage rates -- export BITSO_KEY / BITSO_SECRET in your shell first; never commit them
/tmp/bta $COMMON -fees-from-bitso -buy-liquidity taker -sell-liquidity taker   # (a)
/tmp/bta $COMMON -fees-from-bitso -buy-liquidity maker -sell-liquidity taker   # (b)
/tmp/bta $COMMON -fees-from-bitso -buy-liquidity maker -sell-liquidity maker   # (c)

# (d)-(f): flat 65 bps, comparable with earlier reports
/tmp/bta $COMMON -commission-bps 65                                            # (d)
/tmp/bta $COMMON -commission-bps 65 -disable-fee-rates                         # (e)
/tmp/bta $COMMON -commission-bps 65 -disable-fee-rates \
  -params '{"fallback_round_trip_bps":0}'                                      # (f)
```

The six scenarios run in parallel in about 10 minutes.
