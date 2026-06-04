# Stage Soak, Market Data, and ATR — Observations

Date: 2026-06-03  
Repository: `microservices-trading-bot`  
Related: [`STAGE-SOAK-OPERATOR-GUIDE-2026-06-02.md`](STAGE-SOAK-OPERATOR-GUIDE-2026-06-02.md), [`POST-POINT-10-IMPLEMENTATION-STATUS-2026-05-25.md`](POST-POINT-10-IMPLEMENTATION-STATUS-2026-05-25.md)

This document captures operator and engineering observations from the POST-POINT-10 Stage soak work: what the soak measures, how market data flows, why ATR was missing, `DRY_RUN` semantics, and follow-up actions.

---

## 1. What the Stage soak is for

The soak (POST-POINT-10 **item 1**) is **not** a trading, P&L, or fee-honesty test.

| Phase | Duration | `DRY_RUN` | Goal |
|-------|----------|-----------|------|
| Short soak | 24–48 h | `true` (bash + Go router) | ≥ 99% **regime agreement**; no evaluation errors |
| Extended gate | ≥ 7 days | `true` | Milestone completion |
| Go live | After pass | `false` on Go router only | Router may start/stop strategies |

**Primary metric:** bash `strategy-regime-router.sh` and in-cluster `strategy-router` assign the same `regime` label on the same indicator snapshot logic.

**Secondary:** router health (`strategy_router_evaluations_total`, flat `evaluation_errors_total`, alerts quiet).

Helper script: `./scripts/run-stage-soak-2026-06-02.sh` (`register`, `start-bash`, `check`, `status`, `stop-bash`).

Development overlay: `k8s/overlays/development/strategy-router-soak.yaml` (`DRY_RUN=true`, canonical `ROUTE_*`).

---

## 2. Does the soak use live market data?

**Yes — indirectly.** Routers never call Bitso. They poll strategy-executor every `ROUTER_INTERVAL_SEC` (default 30s):

```
strategy-router / bash router
  → GET /api/v1/indicators/{book}/snapshot
  → GET /api/v1/strategies
```

**strategy-executor** builds indicators from:

1. **market-data** (WebSocket trades → Redis/Kafka; HTTP `GET /api/v1/trades`)
2. Background refresh (`INDICATOR_UPDATE_INTERVAL`, e.g. 5s in dev overlay)

So RSI, EMA, Bollinger, SMA, and price **do move** with live trades. The soak is not a frozen snapshot.

**What soak does not do:**

- Run strategy `OnTick` or emit signals (unless a strategy was manually started)
- Place Bitso orders
- Validate realized fees (after `DRY_RUN=false` on router + live round-trip)

---

## 3. Why run 24–48 hours if `DRY_RUN=true`?

A 5-minute check proves one matching cycle. **24–48 hours** proves:

- Bash vs Go stay aligned across hundreds of polls and varying RSI/EMA/Bollinger
- Router loop survives pod restarts, port-forward drops, overnight liquidity
- `strategy_router_evaluation_errors_total` stays flat
- Operational habit: audit logs (`/tmp/strategy-regime-router.log`, bash log) are usable for agreement math

`DRY_RUN=true` means **no start/stop** — you are validating **classification and orchestration**, not execution.

---

## 4. `DRY_RUN` is not one global switch

| Location | `DRY_RUN=true` | `DRY_RUN=false` |
|----------|----------------|-----------------|
| **strategy-router** | Classify only; no `start`/`stop` | May switch strategies (guardrails apply) |
| **trading-engine** | Log “Would place…”; **no** Bitso `PlaceOrder` | May place orders on `BITSO_API_BASE_URL` |
| **start-organic-trading.sh** | Sets strategy param `dry_run` on signals | Normal signal metadata |
| **bash router** | Log would START/STOP only | Calls lifecycle APIs |

**`DRY_RUN=false` on the router ≠ “bot trading on Stage.”** Live Stage orders also need: a **running** strategy, **trading-engine** without `DRY_RUN`, stage API keys, and `BITSO_API_BASE_URL=https://stage.bitso.com/api`.

---

## 5. Stage price consistency (WebSocket vs REST)

| Component | Config | K8s base default |
|-----------|--------|------------------|
| **market-data** (prices for indicators) | `BITSO_WS_URL` ← `bitso-ws-url` | `wss://ws.bitso.com` (production WS) |
| **trading-engine / strategy-executor** (orders, fees) | `BITSO_API_BASE_URL` | `https://stage.bitso.com/api` |

**Observation:** Dev cluster can feed indicators from **prod** WebSocket while orders use **stage** REST. For a true “Stage soak” aligned with stage trading, patch the **development overlay** ConfigMap to `bitso-ws-url: wss://ws.stage.bitso.com` (docker-compose already does this).

This does **not** block regime agreement measurement; it affects **interpretation** when comparing regimes to stage execution.

---

## 6. ATR gap (root cause and fix)

**Symptom:** `GET /api/v1/indicators/btc_mxn/snapshot` had `atr: null`, `atr_pct: 0`, regimes skewed to `low_vol_range`.

**Cause:** `strategy-executor` calls `GET /api/v1/bars?book=…&interval=1m&limit=30` on market-data (`HTTPDataProvider.GetRecentBars`). **market-data did not implement `/api/v1/bars`** — only trades, orderbook, ticker, stats.

ATR needs OHLCV (`ComputeFromBars`); without bars, ATR is never stored.

**Fix (2026-06-03):** market-data exposes `GET /api/v1/bars`, aggregating OHLCV from Redis recent trades / historical storage (`services/market-data/internal/bars`, `internal/api/bars.go`). strategy-executor picks up ATR on the next indicator refresh **without** router soak restart.

**Warm-up:** Need enough trades spanning ≥ `ATRPeriod+1` complete 1m buckets (default 14+1 minutes of activity on the book). Quiet markets may still return few bars until volume picks up.

---

## 7. Bash vs Go regime mismatch (snapshot JSON)

**Symptom:** Bash `neutral`, Go `low_vol_range` on the same wall time.

**Cause:** strategy-executor snapshot JSON uses PascalCase for some fields (`Value`, `Upper`) when struct tags were missing; bash jq expected `snake_case` (`value`, `lower_band`).

**Fixes on branch:**

- `scripts/strategy-regime-router.sh` — jq fallbacks for both shapes
- `services/strategy-router/internal/clients/strategy_executor.go` — Bollinger unmarshaling
- `services/strategy-executor/internal/indicators/interface.go` — JSON tags on `IndicatorValue` / `BollingerBands`

---

## 8. Soak operations lessons

### Bash leg depends on port-forward

If **strategy-executor** pod restarts, local `kubectl port-forward … 8084:8081` dies. Bash router logs `snapshot fetch failed` while Go router (in-cluster) stays healthy.

**Recovery:**

```bash
./scripts/run-stage-soak-2026-06-02.sh stop-bash
./scripts/run-stage-soak-2026-06-02.sh start-bash
```

### Keep strategies stopped during soak

If `mean_reversion_btc_mxn` (or any strategy) is **running**, soak still classifies but `current` ≠ `<none>` and dry-run logs show “would switch”. Stop running strategies for a clean soak:

```bash
kubectl -n bitso-trading-dev exec deploy/strategy-executor -- \
  wget -qO- --post-data='' http://127.0.0.1:8081/api/v1/strategies/mean_reversion_btc_mxn/stop
```

### Canonical strategy names

`ROUTER_MANAGED=true` with `ROUTER_CANONICAL_NAMES=true` registers `mean_reversion_btc_mxn`, `momentum_btc_mxn`, `limit_profit_btc_mxn` (matches default `ROUTE_*` in `strategy-router-soak.yaml`). Organic `organic_*_<timestamp>` names require manual `ROUTE_*` alignment.

---

## 9. ATR implementation (shipped 2026-06-03)

| Component | Path / behavior |
|-----------|-----------------|
| **API** | `GET /api/v1/bars?book=btc_mxn&interval=1m&limit=30` on market-data (`:8083`) |
| **Aggregation** | `services/market-data/internal/bars/aggregate.go` — OHLCV from Redis recent trades |
| **Handler** | `services/market-data/internal/api/bars.go` |
| **Consumer** | `strategy-executor` `HTTPDataProvider.GetRecentBars` → ATR in `ComputeAndStore` |

**Deploy:** Roll **`market-data`** only (CI image or manual). **Do not** restart `strategy-router` or bash soak for bars to appear; executor picks up ATR on the next indicator cycle.

**Optional:** Development overlay `bitso-ws-url: wss://ws.stage.bitso.com` for Stage-aligned trade prices (see §5).

**Extended soak:** After warm-up, a second 24–48h window can validate agreement when `high_vol` / `low_vol_range` branches are active — not required for the minimum item 1 gate if bash and Go already agree on trade-based indicators.

---

## 10. POST-POINT-10 status snapshot

| Item | Status |
|------|--------|
| Items 2–10 (K8s, CI, Grafana, POINT-11, etc.) | ✅ Implemented on `feat/k8s-deployment-manifests` |
| Item 1 Stage soak | 📋 Operator — bash + Go, 24–48h + 7d gate |
| ATR / `/api/v1/bars` | ✅ Implemented 2026-06-03 (this repo) |
| Stage WS alignment | 📋 Optional dev overlay patch |
| Fee honesty after soak | After `DRY_RUN=false` + first round-trip |
| Merge PR | After soak passes |

---

## 11. Related documents

- [`STAGE-EXECUTION-SOAK-OPERATOR-GUIDE-2026-06-04.md`](STAGE-EXECUTION-SOAK-OPERATOR-GUIDE-2026-06-04.md) — router lifecycle + Stage orders after classification soak
- [`STAGE-SOAK-OPERATOR-GUIDE-2026-06-02.md`](STAGE-SOAK-OPERATOR-GUIDE-2026-06-02.md)
- [`POST-POINT-10-IMPLEMENTATION-STATUS-2026-05-25.md`](POST-POINT-10-IMPLEMENTATION-STATUS-2026-05-25.md)
- [`README.md`](README.md)
- [`../ORDER-FLOW-AND-BITSO-TESTING.md`](../ORDER-FLOW-AND-BITSO-TESTING.md)
- [`scripts/run-stage-soak-2026-06-02.sh`](../../scripts/run-stage-soak-2026-06-02.sh)

---

## 12. Verifying ATR after deploy

```bash
# From cluster (market-data bars)
kubectl -n bitso-trading-dev exec deploy/strategy-executor -- \
  wget -qO- 'http://market-data:8083/api/v1/bars?book=btc_mxn&interval=1m&limit=30'

# Snapshot should include atr after one indicator refresh cycle
kubectl -n bitso-trading-dev exec deploy/strategy-executor -- \
  wget -qO- http://127.0.0.1:8081/api/v1/indicators/btc_mxn/snapshot | jq '.atr'

./scripts/run-stage-soak-2026-06-02.sh check
```

Do **not** set `strategy-router` `DRY_RUN=false` until soak agreement gate passes.
