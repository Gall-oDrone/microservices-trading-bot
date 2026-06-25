# Bucket B–D Implementation

Date: **2026-06-25**
Repository: `microservices-trading-bot`
Branch: `feat/k8s-deployment-manifests`
Scope: Implements the code/tooling items from [`RECOMMENDED-NEXT-STEPS-2026-06-25.md`](RECOMMENDED-NEXT-STEPS-2026-06-25.md) Buckets **B**, **C** (tooling), and **D**.

This is an engineering-completeness pass. It does **not** produce economic proof — that still
requires the multi-week Stage shadow run (Bucket C operational work). What changed is that the
program now has the missing safeguards, the backtest API, and the operator tooling to run that
shadow phase properly.

---

## 1. Summary of what shipped

| Item | Status | Type |
|------|--------|------|
| **B1** Fee assumption vs realized drift metric + alert | ✅ Implemented | Go + PrometheusRule |
| **B2** Backtest HTTP API (`/api/v1/backtests`) | ✅ Implemented | Go (engine + handler) |
| **B3** Phase-4 promotion scripts (6) | ✅ Implemented | bash |
| **B4** `momentum_*` Prometheus counters | ✅ Implemented | Go |
| **C** Shadow-trading + prod-WS tooling | ✅ Implemented (operational run still pending) | bash |
| **D1** Regime-stratified agreement report | ✅ Implemented | bash + python |
| **D2** Reconnect-rate-drop verification | ✅ Tooling (folded into prod-WS validator) | bash |

Unit tests added and passing: `internal/backtest` (engine + replay provider), `internal/metrics`
(fee-drift + momentum export). Pre-existing unrelated failure in `internal/processor` tests
(`models.TradeEvent` field drift) was left untouched.

---

## 2. B1 — Fee assumption vs realized drift (POINT-9 §7)

The direct defense against the May 2026 loss, where the configured maker/taker assumption diverged
from what Bitso actually charged.

**Metrics** (`services/strategy-executor/internal/metrics/prometheus.go`, exported on `/metrics`):

| Metric | Labels | Meaning |
|--------|--------|---------|
| `strategy_executor_realized_fee_rate` | `book,side,liquidity` | Last realized fee rate (decimal fraction of notional) from a fill |
| `strategy_executor_realized_fee_rate_observations_{sum,count}` | `book,side,liquidity` | Summary for averaging |
| `strategy_executor_assumed_fee_rate` | `book,liquidity` | Configured/assumed maker or taker rate from the Bitso fee provider |
| `strategy_executor_fee_drift_ratio` | `book,side,liquidity` | `realized / assumed` (1.0 = match, >1 = paying more than assumed) |

**Wiring:** `EnhancedRegistry.recordFeeDrift(fill)` runs on every order fill
(`NotifyOrderFilled`). It records the realized rate from `OrderFill.FeeRate`/`Liquidity` and, when a
`MakerTakerFeeProvider` is configured, computes the drift vs the assumed maker/taker rate for that
book. No new dependencies; uses the existing fee provider.

**Alerts** (`monitoring/prometheus/rules/trading-alerts.yml`, group `fee-honesty-alerts`):

- `FeeRealizedAboveAssumed` — `fee_drift_ratio > 1.15` for 15m (warning)
- `FeeRealizedFarFromAssumed` — ratio `> 1.30` or `< 0.70` for 30m (critical; likely inverted
  liquidity role — the POINT-9 failure mode)

---

## 3. B2 — Backtest HTTP API

Wires the existing `backtest.Runner` behind an HTTP API and, importantly, makes it run with
**correct, look-ahead-free indicators**.

**New code:**
- `internal/backtest/replay_provider.go` — `ReplayProvider` implements `indicators.DataProvider`
  over a growing window of past trades, with incremental 1-minute OHLCV aggregation (O(1) per tick).
- `internal/backtest/engine.go` — `RunHistorical(...)` builds a dedicated in-memory indicator
  service fed by the replay provider, recomputes indicators **per tick from past data only**, and
  runs the strategy through the existing runner. Indicator values are written to a private store so
  a backtest **never perturbs live `/metrics` gauges**.
- `internal/backtest/runner.go` — added a `BeforeTick` hook so indicators advance before each
  `OnTick`.
- `internal/server/backtest_handler.go` — the API.

**Endpoints** (enabled when a market-data trade source is configured, which `main.go` now does):

| Method | Path | Purpose |
|--------|------|---------|
| `POST` | `/api/v1/backtests` | Create + run a backtest (synchronous; returns `id`, `status`) |
| `GET` | `/api/v1/backtests` | List backtests |
| `GET` | `/api/v1/backtests/{id}` | Job status |
| `GET` | `/api/v1/backtests/{id}/results` | `BacktestResult` JSON (`sharpe_ratio`, `max_drawdown_pct`, `win_rate`, `profit_factor`, `total_trades`, `total_pnl`, …) |
| `GET` | `/api/v1/backtests/{id}/report?format=text` | Human-readable report |

The results schema matches what `scripts/validate-limit-profit-backtest.sh` already expected.
Supported strategy types: `mean_reversion`, `momentum`, `limit_profit`.

**Request body:** `book`, `strategy`, optional `strategy_name`, `parameters`, `start_date`/`end_date`
(RFC3339 filter), `initial_balance`, `slippage_bps`, `commission_bps`, `limit` (max recent trades to
fetch; default 2000).

**Known limitation:** backtests replay the most-recent `limit` trades from market-data (optionally
date-filtered). Data depth is bounded by what market-data retains; on sparse books the available
window may be too short to warm indicators — the result reports `ticks_processed` and `trades_used`
so this is visible. A dedicated historical trade archive is a future enhancement.

---

## 4. B3 — Promotion scripts

All under `scripts/`, aligned to the B2 API and `/metrics`:

| Script | Purpose |
|--------|---------|
| `backtest-strategy.sh` | POST a backtest, poll, print the text report |
| `validate-backtest.sh` | Validate any backtest result against Gate 1 thresholds |
| `promote-strategy-to-stage.sh` | Validate Gate 1 (read-only), then print the exact Stage shadow steps; never starts live trading |
| `compare-stage-vs-backtest.sh` | Compare live Stage gauges to the backtest baseline (directional sanity + fee drift) |
| `strategy-daily-report.sh` | Daily shadow observation report from `/metrics` (P&L, win rate, signals, breakers, fee drift) |
| `kill-switch-status.sh` | Show circuit-breaker state; non-zero exit if any breaker tripped |

---

## 5. B4 — `momentum_*` counters

Mirrors the rich `limit_profit_*` metrics for momentum
(`internal/strategies/momentum_strategy.go` + registry wiring + `prometheus.go`):

`momentum_entry_signals_total{strategy,book,side}`,
`momentum_exit_signals_total{strategy,book,reason}`,
`momentum_position_hold_duration_seconds{_sum,_count}`,
`momentum_daily_realized_pnl_quote`,
`momentum_circuit_breaker_active`.

---

## 6. C / D — Operational tooling

- `strategy-daily-report.sh`, `compare-stage-vs-backtest.sh`, `kill-switch-status.sh` support the
  **C1/C2** shadow run (per-regime net P&L after realized fees, fee-floor decisions, breaker watch).
- `validate-prod-ws-hardening.sh` covers **C3 + D2**: scrapes market-data `/metrics` twice over a
  window and checks `market_data_last_trade_age_seconds`, the websocket + silence reconnect deltas,
  and REST-fallback usage. Confirms the `inbox`-close fix held (reconnect rate within budget).
- `analyze-regime-stratified-agreement.sh` covers **D1**: takes the existing
  `agreement-samples.jsonl` and produces per-regime agreement + a go-vs-bash confusion matrix,
  flagging under-sampled regimes (`high_vol`, `trending_*`) as *not yet verified* rather than passed.

---

## 7. What is still pending (operator-led)

These are unchanged from `RECOMMENDED-NEXT-STEPS-2026-06-25.md` and are **not** code:

1. **A1** — verify Phase 5 and write its PASS report.
2. **C1/C2** — run the 2–4+ week Stage shadow (tiny size, one book) and record per-regime net P&L
   after realized fees. This is the economic proof; the tooling above exists to run it.
3. **C3/D2** — run `validate-prod-ws-hardening.sh` against `wss://ws.bitso.com` before any prod
   overlay.
4. **D1** — accumulate enough `high_vol` / `trending_*` samples for the stratified report to clear
   coverage.

The system remains **provably correct, not yet provably profitable.**
