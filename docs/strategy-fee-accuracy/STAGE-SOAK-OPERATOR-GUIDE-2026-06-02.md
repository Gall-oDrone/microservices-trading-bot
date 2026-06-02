# Stage Soak — Operator Guide

Date: 2026-06-02  
Repository: `microservices-trading-bot`  
Related: POST-POINT-10 roadmap **item 1** (operator-owned milestone gate)

## Purpose

The **Stage soak** validates that the in-cluster Go `strategy-router` and the reference bash router (`scripts/strategy-regime-router.sh`) classify the **same live market** the same way, and that the router loop stays healthy on Bitso Stage — **without placing orders or switching strategies**.

This is **not** a trading, P&L, or fee-honesty test. Those are separate checks after the soak passes.

| Phase | Duration | `DRY_RUN` | Goal |
|-------|----------|-----------|------|
| Short soak | 24–48 h | `true` (both routers) | ≥ 99% regime agreement; no evaluation errors |
| Extended log | ≥ 7 days | `true` | Milestone gate for post-POINT-10 completion |
| Go live | After pass | `false` on Go router only | Router may start/stop strategies |

See also: [`POST-POINT-10-IMPLEMENTATION-STATUS-2026-05-25.md`](POST-POINT-10-IMPLEMENTATION-STATUS-2026-05-25.md), [`POST-POINT-10-ROADMAP-2026-05-22.md`](POST-POINT-10-ROADMAP-2026-05-22.md) item 1.

---

## How the router behaves during the soak

### Does the router continuously check Bitso Stage market data?

**Indirectly, yes — on a fixed poll interval.** The router **never calls the Bitso API**. It only calls **strategy-executor**:

```
strategy-router  ──every ROUTER_INTERVAL_SEC (default 30s)──►
  GET /api/v1/indicators/{book}/snapshot
  GET /api/v1/strategies
  (optional) GET /api/v1/strategies/{name}/state
```

**strategy-executor** maintains indicators (ATR, EMA, RSI, Bollinger, etc.) by:

1. Consuming **market-data** (Kafka trades/tickers and/or HTTP `GET` to the market-data service), and/or  
2. Refreshing from recent trades on a background interval (`INDICATOR_UPDATE_INTERVAL`, default **30s** in the indicator service).

**market-data** on Stage connects to the **Bitso Stage WebSocket / REST** for the configured book (e.g. `btc_mxn`). So as long as `market-data` and `strategy-executor` are running in the cluster, each router poll sees **indicators derived from live Stage prices** — not a frozen snapshot.

### What happens each poll cycle

| Step | Bash router | Go `strategy-router` |
|------|-------------|----------------------|
| 1 | `GET …/indicators/{book}/snapshot` | Same |
| 2 | Compute `regime` (5 labels) | `classifier.Classify()` — same rules as bash |
| 3 | Map `regime` → `preferred` strategy name | `Routes.Resolve(regime)` |
| 4 | Compare to `current` running strategy | Same |
| 5 | Apply guardrails (position, cooldown, registered) | Same |
| 6 | **`DRY_RUN=true`:** log only, no `start`/`stop` | `action=dry_run`, `blocked_total{reason="dry_run"}` |

With **`ROUTER_MANAGED=true`** organic registration, strategies are **registered but stopped**. The soak still runs the full classify + resolve path; you will see `preferred` vs `current=<none>` and dry-run “would start” reasons in logs/metrics.

### What the soak does **not** do

- Does not run strategy `OnTick` or emit trade signals (unless you manually started a strategy).
- Does not place Bitso orders.
- Does not validate realized fees (do that after `DRY_RUN=false` — see POST-POINT-10 status doc § operator step 4).

---

## What is being measured

### Primary: regime label agreement

**Target: ≥ 99%** of comparable poll cycles where bash and Go produce the same `regime`:

`low_vol_range` | `trending_up` | `trending_down` | `high_vol` | `neutral`

Compare:

- Bash stdout: `regime=…` and `regime inputs: price=… atr_pct=…`
- Go: `GET /api/v1/router/state` → `last_decisions[].regime` and `.snapshot` (atr_pct, ema_dist_pct, bollinger_pb, rsi)

### Secondary: router health (Go only)

| Signal | Healthy |
|--------|---------|
| `rate(strategy_router_evaluations_total[5m])` | ≈ `1 / ROUTER_INTERVAL_SEC` (~0.033/s at 30s) |
| `increase(strategy_router_evaluation_errors_total[5m])` | 0 |
| Alerts `RouterNotEvaluating`, `RouterEvaluationsFailing` | Not firing |
| Regime stuck on `neutral` + “missing price” | Investigate cold indicators |

### Usually **not** scored during soak

| Item | Why |
|------|-----|
| `action` (switched vs blocked) | Separate cooldown clocks per process |
| `preferred` strategy actually started | `DRY_RUN=true` blocks lifecycle |
| Switch audit lines in bash log | Dry-run may not append switch lines every cycle; prefer Go JSONL audit |

---

## Classifier inputs and thresholds

Both routers use the same decision tree (see `services/strategy-router/internal/classifier/classifier.go` and `scripts/strategy-regime-router.sh`):

| Derived value | Formula |
|---------------|---------|
| `atr_pct` | `(atr / price) * 100` |
| `bollinger_pb` | `(price - lower) / (upper - lower)` |
| `ema_dist_pct` | `((price - ema) / price) * 100` |

| Regime | Condition (defaults) |
|--------|----------------------|
| `high_vol` | `atr_pct > 1.5` (`ATR_HIGH_VOL_PCT`) |
| `low_vol_range` | `atr_pct < 0.30` and `0.15 < %B < 0.85` |
| `trending_up` | `ema_dist_pct > 0.10` and `rsi < 70` |
| `trending_down` | `ema_dist_pct < -0.10` and `rsi > 30` |
| `neutral` | else (incl. missing price) |

**Align env on bash and Go** — mismatched thresholds are the most common cause of false “disagreement.”

| Variable | Default |
|----------|---------|
| `ATR_HIGH_VOL_PCT` | `1.5` |
| `ATR_LOW_VOL_PCT` | `0.30` |
| `RSI_OVERBOUGHT` | `70` |
| `RSI_OVERSOLD` | `30` |
| `BB_PB_UPPER` / `BB_PB_LOWER` | `0.85` / `0.15` |
| `EMA_DIST_ENTRY_PCT` | `0.10` (bash hardcodes `0.10`; set explicitly on Go) |

---

## Routing table and strategy naming

Default routes (book = `btc_mxn`):

| Regime | Default `preferred` |
|--------|---------------------|
| `low_vol_range`, `neutral` | `mean_reversion_btc_mxn` |
| `trending_up`, `trending_down` | `momentum_btc_mxn` |
| `high_vol` | `none` (pause) |

Override with `ROUTE_LOW_VOL`, `ROUTE_TRENDING_UP`, `ROUTE_TRENDING_DOWN`, `ROUTE_HIGH_VOL`, `ROUTE_NEUTRAL`.

**Naming trap:** `ROUTER_MANAGED=true ./scripts/start-organic-trading.sh` registers `organic_mean_reversion_<timestamp>`, not `mean_reversion_btc_mxn`. Regime comparison is unaffected, but you will see `not_registered` blocks until `ROUTE_*` matches the names you registered:

```bash
# After registration, set routes to actual names (example)
export ROUTE_LOW_VOL=organic_mean_reversion_1717334400
export ROUTE_NEUTRAL=organic_mean_reversion_1717334400
export ROUTE_TRENDING_UP=organic_momentum_1717334400
export ROUTE_TRENDING_DOWN=organic_momentum_1717334400
```

---

## Environment parameters (full list)

### Shared (bash + Go)

| Variable | Default | Purpose |
|----------|---------|---------|
| `BOOK` / `STRATEGY_ROUTER_BOOK` | `btc_mxn` | Book |
| `ROUTER_INTERVAL_SEC` | `30` | Seconds between evaluation cycles |
| `COOLDOWN_SECS` | `120` | Min seconds between real switches (live only) |
| `DRY_RUN` | `false` → **`true` for soak** | No start/stop |
| `ATR_HIGH_VOL_PCT` … `EMA_DIST_ENTRY_PCT` | see above | Classifier |
| `ROUTE_*` | see routing table | Regime → strategy name |

### Go service only

| Variable | Default | Purpose |
|----------|---------|---------|
| `STRATEGY_EXECUTOR_URL` | `http://strategy-executor:8081` | In-cluster executor |
| `ROUTER_AUDIT_LOG_PATH` | `/tmp/strategy-regime-router.log` | JSONL + text audit |
| `ROUTER_AUTOSTART` | `true` | Set `false` to evaluate only via `POST /api/v1/router/run` |
| `STRATEGY_ROUTER_PORT` | `8092` | HTTP / metrics |

### Bash only

| Variable | Default | Purpose |
|----------|---------|---------|
| `NAMESPACE` | `bitso-trading-dev` | For kubectl port-forward |
| `STRATEGY_EXECUTOR_URL` | (port-forward) | Must reach same executor as Go router |
| `ROUTER_DURATION_SEC` | `0` | `0` = run until stopped |

### Organic registration (before soak)

| Variable | Purpose |
|----------|---------|
| `ROUTER_MANAGED=true` | Register strategies, do not `start` |
| `STRATEGY_TYPES` | Default `mean_reversion,limit_profit,momentum` when unset |

---

## Step-by-step process

### Prerequisites

- [ ] `market-data` and `strategy-executor` running on Stage (`bitso-trading-dev` or your namespace).
- [ ] `GET /api/v1/indicators/btc_mxn/snapshot` returns non-zero ATR, RSI, EMA, Bollinger.
- [ ] Strategies registered (`ROUTER_MANAGED=true`); none required to be running during soak.

### 1. Register strategies

```bash
# Canonical names (default when ROUTER_MANAGED=true): mean_reversion_btc_mxn, momentum_btc_mxn, limit_profit_btc_mxn
./scripts/run-stage-soak-2026-06-02.sh register

# Or manually:
NAMESPACE=bitso-trading-dev BOOK=btc_mxn \
  ROUTER_MANAGED=true ROUTER_CANONICAL_NAMES=true \
  STRATEGY_TYPES=mean_reversion,momentum,limit_profit \
  ./scripts/start-organic-trading.sh
```

Development overlay `strategy-router-soak.yaml` sets `DRY_RUN=true` and canonical `ROUTE_*` names.

### 2. Deploy Go router with `DRY_RUN=true`

```bash
kubectl -n bitso-trading-dev apply -k k8s/overlays/development
# Patch or overlay: DRY_RUN=true on strategy-router Deployment
kubectl -n bitso-trading-dev rollout status deploy/strategy-router
```

### 3. Run bash router in parallel (dry-run)

```bash
./scripts/run-stage-soak-2026-06-02.sh start-bash
# Or manually:
DRY_RUN=true BOOK=btc_mxn ROUTER_INTERVAL_SEC=30 \
  NAMESPACE=bitso-trading-dev \
  ./scripts/strategy-regime-router.sh
```

Use the **same** classifier env as the Deployment.

### 4. Compare regimes

```bash
kubectl -n bitso-trading-dev port-forward svc/strategy-router 8092:8092
curl -s http://127.0.0.1:8092/api/v1/router/state | jq '.last_decisions[] | {regime, snapshot: .snapshot.regime, atr_pct: .snapshot.atr_pct, rsi: .snapshot.rsi}'
```

Tail Go audit inside the pod:

```bash
kubectl -n bitso-trading-dev exec deploy/strategy-router -- tail -f /tmp/strategy-regime-router.log
```

Each JSON line includes `"regime"` and `"snapshot"` for offline agreement math.

### 5. Prometheus / Grafana

```bash
GRAFANA_URL=http://localhost:3001 ./scripts/grafana-import-dashboard.sh strategy-router
```

Watch `strategy_router_regime`, `strategy_router_evaluations_total`, `strategy_router_evaluation_errors_total`.

### 6. Agreement formula

```text
agreement_rate = matching_regimes / comparable_cycles

comparable_cycles = cycles where both routers fetched a valid snapshot
  (exclude Go cycles with Reason containing "snapshot fetch failed")
```

**Pass:** `agreement_rate ≥ 0.99` over 24–48 h short soak, then maintain logging for **≥ 7 days** for the milestone gate.

### 7. After pass

1. Set `DRY_RUN=false` on `strategy-router` only; stop bash router.
2. Fee honesty spot-check on first live round-trip (see POST-POINT-10 status doc).
3. Import Grafana dashboard; confirm alerts quiet.

---

## Pass / fail checklist

| Criterion | Pass | Fail → action |
|-----------|------|----------------|
| Regime agreement | ≥ 99% | Diff thresholds; dump both `snapshot` payloads |
| Evaluation errors | Flat | Fix executor URL / network |
| Evaluations rate | ~1/interval | Check `ROUTER_AUTOSTART`, pod restarts |
| Indicators warm | Non-zero snapshot | Fix market-data WS / Kafka |
| Constant `not_registered` | — | Align `ROUTE_*` with registered names |

---

## Offline classifier replay (optional)

Replay snapshots through the Go classifier only (does not compare bash side-by-side):

```bash
go run ./services/strategy-router/cmd/classifier-backtest/main.go \
  -url http://127.0.0.1:8084 -book btc_mxn -samples 100 -interval 30s
```

For apples-to-apples bash vs Go on **one** snapshot, fetch once and pipe the JSON through both paths manually.

---

## Related documents

- [`POINT-10-STRATEGY-REGIME-ROUTER.md`](POINT-10-STRATEGY-REGIME-ROUTER.md) — Phase 1 design
- [`STRATEGY-REGIME-ROUTER-SERVICE-2026-05-22.md`](STRATEGY-REGIME-ROUTER-SERVICE-2026-05-22.md) — Phase 2 service
- [`POST-POINT-10-ROADMAP-2026-05-22.md`](POST-POINT-10-ROADMAP-2026-05-22.md) — item 1 acceptance
- [`POST-POINT-10-IMPLEMENTATION-STATUS-2026-05-25.md`](POST-POINT-10-IMPLEMENTATION-STATUS-2026-05-25.md)
- [`../ORGANIC-TRADING-STARTUP.md`](../ORGANIC-TRADING-STARTUP.md)
- [`../MOMENTUM-STRATEGY.md`](../MOMENTUM-STRATEGY.md)
