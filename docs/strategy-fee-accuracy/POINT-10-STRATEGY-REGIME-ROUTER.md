# Point 10 — Regime-Driven Strategy Routing

Date: 2026-05-21  
Repository: `microservices-trading-bot`

> **Status update (2026-05-22):** Phase 1 (this document, external bash script) shipped on 2026-05-21. Phase 2 — the in-cluster Go service with Prometheus metrics and an HTTP control surface — landed on 2026-05-22 and is the recommended way to run the router 24/7 on Stage and Production. See [`STRATEGY-REGIME-ROUTER-SERVICE-2026-05-22.md`](STRATEGY-REGIME-ROUTER-SERVICE-2026-05-22.md). The bash script remains the reference implementation and is still useful for local sessions and threshold tuning.

## 1) Problem

`limit_profit` is essentially a **scalping** framework. To earn its keep it needs:

- Low fees (or maker rebates on both legs).
- High signal frequency.
- Tight spreads.

On the May 2026 Bitso Stage run, the round-trip fee was ≈ 1.31% (0.741% taker BUY + 0.570% maker SELL) and the typical spread was ≈ 0.05%. On a 1 340 MXN notional that mathematically requires a ≥ 17 MXN price move just to break even — `limit_profit` is a **structural loser** in that regime no matter how well it is tuned.

We have two better-suited strategies already registered in `strategy-executor`:

- `mean_reversion` (Bollinger Bands) — wider entry/exit moves, better fee/profit ratio in range-bound markets.
- `momentum` (RSI + EMA) — fewer signals, larger expected moves, suitable for trending markets.

There is no mechanism today to **automatically** pick the right one. Operators decide at deploy time.

## 2) Goal

Let the bot pick — and switch — its active strategy based on **live financial indicators** with the following constraints:

1. Zero changes to the existing strategy framework (start small; we can graduate to an in-process meta-router later).
2. No risk to open positions (a switch can only happen when the current strategy is flat).
3. Tight cooldown to avoid thrash on indicator noise.
4. Trivial to operate and audit (logs, dry-run, single env-var to disable).

## 3) Design — External Router Script

`scripts/strategy-regime-router.sh` is a small, dependency-light loop that uses the **existing** strategy-executor HTTP surface:

| Endpoint | Used for |
|----------|---------|
| `GET /api/v1/indicators/{book}/snapshot` | Pull current ATR, EMA, RSI, Bollinger %B for the regime classifier. |
| `GET /api/v1/strategies` | Discover which strategies are registered and which one is running. |
| `GET /api/v1/strategies/{name}/state` | Check `has_position` before any switch. |
| `POST /api/v1/strategies/{name}/start` | Activate the preferred strategy. |
| `POST /api/v1/strategies/{name}/stop` | Deactivate the current strategy or pause trading. |

The script does **not** create strategies — operators pre-register them once via `scripts/start-organic-trading.sh` (e.g. start `mean_reversion`, `momentum`, and optionally `limit_profit`, all with the same `STRATEGY_TYPES=mean_reversion,momentum,limit_profit` run), then the router decides which one is *running* at any moment.

### 3.1 Regime classifier

Inputs come straight from the snapshot:

| Indicator | Used for |
|-----------|---------|
| `atr.value` | Volatility — normalized as `atr/price * 100` (percentage). |
| `bollinger.upper_band` / `lower_band` / `current_price` | Bollinger %B = `(price - lower) / (upper - lower)` — 0..1 across the band. |
| `ema.value` + `bollinger.current_price` | EMA distance — `(price - ema)/price * 100`. Acts as a trend proxy. |
| `rsi.value` | Overbought / oversold gating (avoids buying into exhaustion). |

The classifier returns one of `low_vol_range | trending_up | trending_down | high_vol | neutral`. Thresholds are environment-tunable; defaults are documented in the script header.

```
high_vol:        atr_pct > ATR_HIGH_VOL_PCT
low_vol_range:   atr_pct < ATR_LOW_VOL_PCT AND BB_PB_LOWER < %B < BB_PB_UPPER
trending_up:     ema_dist_pct > 0.10 AND rsi < RSI_OVERBOUGHT
trending_down:   ema_dist_pct < -0.10 AND rsi > RSI_OVERSOLD
neutral:         anything else
```

### 3.2 Routing table (defaults)

| Regime | Default route | Why |
|--------|---------------|-----|
| `low_vol_range` | `mean_reversion_{BOOK}` | Tight bands favor reversion plays; spreads are usually narrowest in calm regimes. |
| `trending_up` | `momentum_{BOOK}` | Larger expected moves are required to clear the fee floor on the buy side. |
| `trending_down` | `momentum_{BOOK}` | Same logic; short side handled by momentum's signal model. |
| `high_vol` | `none` (pause) | Round-trip fee + slippage swamps expected edge; safer to sit out. |
| `neutral` | `mean_reversion_{BOOK}` | Default to the less risky model when nothing is clear. |

The table is fully env-overridable (`ROUTE_LOW_VOL`, `ROUTE_TRENDING_UP`, etc.). Set any route to `none` to pause trading in that regime.

### 3.3 Safety guardrails

The router will **refuse** to switch when:

1. The currently running strategy has `has_position == true` (we never hand a live position to another model).
2. Less than `COOLDOWN_SECS` (default 120s) have passed since the last switch.
3. The preferred strategy is not registered (the operator must register it first).

When `DRY_RUN=true` is set, the script only logs decisions — useful for testing the classifier on live indicators before letting it touch the lifecycle endpoints.

### 3.4 Audit trail

Every switch is appended to `/tmp/strategy-regime-router.log`:

```
2026-05-21T14:32:01Z regime=low_vol_range stop=momentum_btc_mxn start=mean_reversion_btc_mxn
2026-05-21T14:48:12Z regime=high_vol → pause stop=mean_reversion_btc_mxn
```

## 4) Usage

```bash
# Step 1 — register the candidate strategies (one-time per deploy):
NAMESPACE=bitso-trading-dev BOOK=btc_mxn \
  STRATEGY_TYPES=mean_reversion,momentum,limit_profit \
  ./scripts/start-organic-trading.sh

# Step 2 — let the router decide which one is active:
NAMESPACE=bitso-trading-dev BOOK=btc_mxn \
  ROUTER_INTERVAL_SEC=30 COOLDOWN_SECS=120 \
  ./scripts/strategy-regime-router.sh

# Dry-run for the first hour to sanity-check the classifier:
DRY_RUN=true ROUTER_DURATION_SEC=3600 ./scripts/strategy-regime-router.sh
```

Tail the audit log:

```bash
tail -f /tmp/strategy-regime-router.log
```

## 5) Why External, Not In-Process

Both options were considered:

| Option | Pros | Cons |
|--------|------|------|
| **External script** (chosen) | Zero changes to running services; trivial rollback; auditable in shell logs; classifier thresholds tunable per env. | Coarser switch granularity (script poll interval); two moving parts (executor + router). |
| **In-process `meta_router` strategy** | Single binary, sub-second switch granularity, can short-circuit signals from inner strategies. | Much larger blast radius — must handle position handoff, fill correlation across inner strategies, P&L attribution, and Redis persistence of nested state. Treats every existing strategy as a building block, which is a refactor not a feature. |

We deliberately ship Phase 1 (external) and document the in-process design as a future evolution in `services/strategy-executor/FEATURES-ROADMAP.md` ("Strategy Composition Framework / Ensemble Methods").

## 6) Future Work

- ✅ **(Phase 2, 2026-05-22)** Bash classifier ported to a Go service in `services/strategy-router/`. Same env-var surface as the bash script, plus Prometheus metrics (`strategy_router_regime`, `strategy_router_switches_total{from,to,regime}`, `strategy_router_blocked_total{reason}`, `strategy_router_evaluation_latency_ms`) and an HTTP control surface (`GET /api/v1/router/state`, `POST /api/v1/router/run`). See [`STRATEGY-REGIME-ROUTER-SERVICE-2026-05-22.md`](STRATEGY-REGIME-ROUTER-SERVICE-2026-05-22.md).
- Introduce a `meta_router` strategy type in `strategy-executor` once the external router has demonstrated stable regime classification for several weeks (see `FEATURES-ROADMAP.md` "Strategy Composition Framework").
- Add a Grafana panel showing the regime label over time, plotted against the strategy currently running and the per-strategy P&L. With Phase 2 deployed this can be built on top of `strategy_router_regime` and `strategy_router_active_strategy` instead of grepping the audit log.
- Hook into `services/agent-coordinator` so an LLM agent can *recommend* regime threshold changes off-line, while the router itself remains deterministic.

## 7) Related Files

- `scripts/strategy-regime-router.sh` — the Phase 1 router (reference + local sessions).
- `services/strategy-router/` — the Phase 2 in-cluster Go service.
- `docs/strategy-fee-accuracy/STRATEGY-REGIME-ROUTER-SERVICE-2026-05-22.md` — Phase 2 design, metrics, and operations notes.
- `scripts/start-organic-trading.sh` — pre-registers strategies the router can choose from.
- `services/strategy-executor/internal/server/http_server.go` — endpoints consumed.
- `services/strategy-executor/internal/indicators/service.go` — `Snapshot` schema (`atr`, `ema`, `rsi`, `bollinger`). ATR requires `GET /api/v1/bars` on market-data (implemented 2026-06-03; see [`STAGE-SOAK-MARKET-DATA-ATR-OBSERVATIONS-2026-06-03.md`](STAGE-SOAK-MARKET-DATA-ATR-OBSERVATIONS-2026-06-03.md)).
- `services/strategy-executor/FEATURES-ROADMAP.md` — long-term "Strategy Composition Framework".
- `k8s/base/strategy-router.yaml` — Kubernetes manifest for the Phase 2 service.
