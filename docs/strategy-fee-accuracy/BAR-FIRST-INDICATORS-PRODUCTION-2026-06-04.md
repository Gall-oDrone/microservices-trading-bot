# Bar-First Indicators — Production Approach

Date: 2026-06-04  
Repository: `microservices-trading-bot`  
Related: [`STAGE-FINANCIAL-APPROACH-2026-06-04.md`](STAGE-FINANCIAL-APPROACH-2026-06-04.md), [`STAGE-SOAK-MARKET-DATA-ATR-OBSERVATIONS-2026-06-03.md`](STAGE-SOAK-MARKET-DATA-ATR-OBSERVATIONS-2026-06-03.md), [`../FINANCIAL-STRATEGY-IMPLEMENTATION-GUIDE.md`](../FINANCIAL-STRATEGY-IMPLEMENTATION-GUIDE.md)

## Purpose

This document describes the **production-ready indicator pipeline** shipped on 2026-06-04. It replaces the fragile “count ticks in a 5-minute Redis window” model with **1m bar closes** from `market-data`, bootstrap warm-up, explicit data-health metadata, and router **pause-on-stale** behavior.

**Not in scope (deferred):** Stage vs prod WebSocket URL alignment (`BITSO_WS_URL` patch) — see soak observations doc.

---

## Problem (2026-06-04 soak)

| Symptom | Root cause |
|---------|------------|
| `Insufficient trades for btc_mxn: got N, need 20` | Indicators gated on **hot-cache tick count** (`SMAPeriod=20`) |
| Empty `/api/v1/indicators/{book}/snapshot` after ~5 min quiet | Indicator Redis TTL expired while compute kept failing |
| Bash ↔ Go still “agreed” on `neutral` | Both fell back when `price=0` — **not** valid soak evidence |
| One `market-data` pod stale 50+ min | Two replicas, two WebSocket consumers, shared Redis |
| Kafka `Unknown Topic Or Partition` | `market-data.trades` topic missing (publisher noise; HTTP path unaffected) |

**Financial risk:** Trading (or soak scoring) on empty/stale snapshots treats **missing data as neutral**, which can route capital incorrectly.

---

## Architecture (after this change)

```text
Bitso WS → market-data
             ├─ hot cache (5m TTL)     … live ticks, /api/v1/trades
             ├─ historical storage     … 30d trades (Redis DB+1)
             └─ GET /api/v1/bars       … 1m OHLCV (storage → cache fallback)

strategy-executor
  1. Bootstrap: retry bar-based compute until warm or timeout
  2. Every INDICATORS_UPDATE_INTERVAL:
       GET /api/v1/bars?interval=1m&limit=N
       SMA / EMA / RSI / Bollinger ← bar **closes**
       ATR ← bar OHLCV
       VWAP ← recent trades (optional, when ticks exist)
  3. Snapshot exposes data_health fields
  4. Redis indicator TTL = INDICATORS_REDIS_TTL (default 15m)

strategy-router
  if !snapshot.data_healthy → blocked, preferred=none (pause)
  else → Classify → route as before
```

### Why bars, not tick count

| Approach | Production fit |
|----------|----------------|
| ≥20 ticks in 5 min | Breaks on sparse Stage/prod books; confuses soak with liquidity |
| 20 × **1m bar closes** | Standard quant semantics; matches backtest candles; survives quiet minutes |

Period **20** still means **20 minutes** of 1m data — not lowered for Stage convenience.

---

## Code changes

| Area | Files | Behavior |
|------|-------|----------|
| Bar-first compute | `services/strategy-executor/internal/indicators/service.go`, `bars_helpers.go` | Primary input = bars; trades only for VWAP |
| Bootstrap | `service.go` `bootstrap()` | Retries warm-up for `INDICATORS_BOOTSTRAP_WAIT` |
| Snapshot health | `Snapshot` struct | `data_healthy`, `computed_at`, `bars_used`, `data_age_sec`, `stale_reason`, `current_price` |
| Config | `internal/config/config.go`, dev overlay | `INDICATORS_BAR_*`, `INDICATORS_MAX_STALENESS`, `INDICATORS_REDIS_TTL`, bootstrap envs |
| Router gate | `services/strategy-router/internal/router/router.go` | Blocks routing when `data_healthy=false` |
| Client | `internal/clients/strategy_executor.go` | Maps new snapshot fields |
| Tests | `service_test.go`, `router_test.go` | Bar-first + stale-data cases |

---

## Configuration (development overlay)

`k8s/overlays/development/strategy-executor-indicators-dev.yaml`:

| Env | Value | Role |
|-----|-------|------|
| `INDICATORS_UPDATE_INTERVAL` | `5s` | Poll cadence (dev) |
| `INDICATORS_BAR_INTERVAL` | `1m` | Bar series |
| `INDICATORS_BAR_LIMIT_BUFFER` | `5` | Extra bars beyond min periods |
| `INDICATORS_MAX_STALENESS` | `15m` | Snapshot marked unhealthy after this |
| `INDICATORS_REDIS_TTL` | `15m` | Last-known-good retention |
| `INDICATORS_BOOTSTRAP_WAIT` | `2m` | Startup warm-up budget |
| `INDICATORS_BOOTSTRAP_EVERY` | `10s` | Bootstrap retry interval |

Production should use **`INDICATORS_UPDATE_INTERVAL=30s`** unless a strategy explicitly needs faster OnTick.

---

## Infrastructure (development overlay)

| Change | File | Why |
|--------|------|-----|
| `market-data` **replicas: 1** | `market-data-replicas-dev.yaml` | Single WS consumer per book |
| Kafka topics Job | `kafka-topics-init-job.yaml` | Creates `market-data.trades` (+ core topics) idempotently |

After deploy:

```bash
kubectl -n bitso-trading-dev apply -k k8s/overlays/development
kubectl -n bitso-trading-dev logs job/kafka-topics-init
kubectl -n bitso-trading-dev rollout restart deploy/strategy-executor deploy/strategy-router
```

---

## Operator verification

### 1. Bars warm-up

```bash
kubectl -n bitso-trading-dev exec deploy/strategy-executor -- \
  wget -qO- 'http://market-data:8083/api/v1/bars?book=btc_mxn&interval=1m&limit=30' | jq '{count: (.bars|length)}'
```

Expect **`count ≥ 21`** after ~20–25 minutes of any trade activity on the book (or sooner if historical storage has data).

### 2. Snapshot health

```bash
kubectl -n bitso-trading-dev exec deploy/strategy-executor -- \
  wget -qO- http://127.0.0.1:8081/api/v1/indicators/btc_mxn/snapshot | \
  jq '{data_healthy, current_price, bars_used, data_age_sec, stale_reason, atr: .atr.value, rsi: .rsi.value}'
```

Pass: `data_healthy=true`, non-zero `current_price`, core indicators present.

### 3. Router respects stale data

```bash
kubectl -n bitso-trading-dev exec deploy/strategy-router -- \
  wget -qO- http://127.0.0.1:8092/api/v1/router/state | \
  jq '.last_decisions[0] | {action, reason, regime, preferred}'
```

When snapshot unhealthy: `action=blocked`, `preferred=none`, reason contains `indicator data not ready`.

### 4. Soak script

```bash
./scripts/run-stage-soak-2026-06-02.sh check
./scripts/run-stage-soak-2026-06-02.sh sample
```

Regime agreement should use **real** indicator values, not `neutral` fallback from missing price.

---

## Warm-up timeline (typical)

| Time after deploy | Expected |
|-------------------|----------|
| 0–2 min | Bootstrap retries; snapshot may show `data_healthy=false` |
| ~20 min | ≥21 complete 1m bars → first stable SMA/RSI/Bollinger |
| Ongoing | Router blocked during gaps > `INDICATORS_MAX_STALENESS` |

---

## Financial rules (unchanged)

From [`STAGE-FINANCIAL-APPROACH-2026-06-04.md`](STAGE-FINANCIAL-APPROACH-2026-06-04.md):

1. **Engineering proof** (soak) ≠ economic proof (shadow P&L).
2. **Pause > guess** — stale indicators → no routing (this change enforces that).
3. **Fee floor** still dominates scalping; bar-first indicators do not create edge by themselves.

---

## Related documents

- [`STAGE-SOAK-OPERATOR-GUIDE-2026-06-02.md`](STAGE-SOAK-OPERATOR-GUIDE-2026-06-02.md) — classification soak
- [`STAGE-EXECUTION-SOAK-OPERATOR-GUIDE-2026-06-04.md`](STAGE-EXECUTION-SOAK-OPERATOR-GUIDE-2026-06-04.md) — execution soak (after classification pass)
- [`docs/ORGANIC-TRADING-STARTUP.md`](../ORGANIC-TRADING-STARTUP.md) — legacy “insufficient trades” note (superseded for indicators by this doc)
