# Stage Market-Data Freshness Hardening

Date: **2026-06-10**  
Repository: `microservices-trading-bot`  
Related: [`STAGE-SOAK-MARKET-DATA-ATR-OBSERVATIONS-2026-06-03.md`](STAGE-SOAK-MARKET-DATA-ATR-OBSERVATIONS-2026-06-03.md), [`BAR-FIRST-INDICATORS-PRODUCTION-2026-06-04.md`](BAR-FIRST-INDICATORS-PRODUCTION-2026-06-04.md), [`STAGE-EXECUTION-SOAK-OPERATOR-GUIDE-2026-06-04.md`](STAGE-EXECUTION-SOAK-OPERATOR-GUIDE-2026-06-04.md)

---

## Problem

During Stage execution soak **Phase 4**, the Bitso WebSocket on `market-data` repeatedly stopped delivering trades while pods stayed `1/1` Ready. Symptoms:

| Symptom | Impact |
|---------|--------|
| `Last Trade: N hours ago` in logs | Stale `current_price` vs Bitso |
| `data_healthy: true` on snapshot | Misleading — compute fresh, bars stale |
| Manual `rollout restart deploy/market-data` | Only remediation |

Incidents: ~90 h (Phase 3), ~7 h + ~13 h (Phase 4, 2026-06-09/10).

---

## Shipped mitigations (2026-06-10)

### Layer 1 — Bar-age `data_healthy` (`strategy-executor`)

| Change | Detail |
|--------|--------|
| `LastBarAt` tracking | Newest 1m bar timestamp stored per book |
| Health gate | `data_healthy=false` when `last_bar_age > INDICATORS_MAX_STALENESS` (default **15m**) |
| Snapshot field | `last_bar_age_sec` exposed on `GET /api/v1/indicators/{book}/snapshot` |
| Router | Existing pause-on-stale blocks routing when `data_healthy=false` |

### Layer 2 — Trade-silence watchdog (`market-data`)

| Change | Detail |
|--------|--------|
| Watchdog | Every 30s, if `Last Trade` age > `TRADE_SILENCE_THRESHOLD` (**5m**), force WS reconnect |
| Cooldown | `TRADE_SILENCE_RECONNECT_COOLDOWN` (**2m**) between watchdog reconnects |
| Metrics | `market_data_last_trade_age_seconds`, `market_data_trade_silence_reconnects_total` |

### Layer 3 — REST fallback (`market-data`)

| Change | Detail |
|--------|--------|
| Poller | When WS silent > `TRADE_REST_FALLBACK_THRESHOLD` (**3m**), poll Bitso `GET /v3/trades` every **60s** |
| Dedup | Ingest only trades with ID > last seen per book |
| Metrics | `market_data_rest_fallback_fetches_total`, `market_data_rest_fallback_trades_ingested_total` |
| Config | `BITSO_API_BASE_URL=https://stage.bitso.com/api` on dev |

### Layer 4 — Readiness probe (`market-data`)

| Check | Behavior |
|-------|----------|
| `GET /health/ready` | Fails if trade stream stale (> `READINESS_MAX_TRADE_AGE`, default **10m**) |
| Startup grace | `READINESS_STARTUP_GRACE` (**5m**) before failing on zero trades |
| K8s | HTTP probes on `/health/live` and `/health/ready` (port **8083**) |

### Layer 5 — Prometheus alerts

Rules in `monitoring/prometheus/rules/trading-alerts.yml`:

| Alert | Condition |
|-------|-----------|
| `MarketDataTradeStreamStale` | `market_data_last_trade_age_seconds > 300` for 2m |
| `MarketDataTradeSilenceReconnectsHigh` | >3 watchdog reconnects/hour |
| `StrategyExecutorIndicatorsUnhealthy` | `strategy_executor_indicators_healthy == 0` for 5m |

---

## Configuration (development overlay)

File: `k8s/overlays/development/market-data-freshness-dev.yaml`

| Env | Value | Role |
|-----|-------|------|
| `BITSO_API_BASE_URL` | `https://stage.bitso.com/api` | REST fallback + stage alignment |
| `TRADE_SILENCE_THRESHOLD` | `5m` | WS reconnect watchdog |
| `TRADE_REST_FALLBACK_THRESHOLD` | `3m` | Start REST polling |
| `TRADE_REST_FALLBACK_INTERVAL` | `60s` | REST poll cadence |
| `READINESS_MAX_TRADE_AGE` | `10m` | Readiness fail threshold |
| `READINESS_STARTUP_GRACE` | `5m` | Allow warm-up before ready |

`strategy-executor` (existing overlay `strategy-executor-indicators-dev.yaml`):

| Env | Value | Role |
|-----|-------|------|
| `INDICATORS_MAX_STALENESS` | `15m` | Bar-age health gate |

---

## Operator verification

### 1. Trade stream fresh

```bash
kubectl -n bitso-trading-dev logs deploy/market-data --tail=5 | grep 'Last Trade'
kubectl -n bitso-trading-dev exec deploy/market-data -- \
  wget -qO- http://127.0.0.1:8083/metrics | grep last_trade_age
```

Expect `Last Trade` < few minutes; `market_data_last_trade_age_seconds` < 300.

### 2. Readiness reflects trade stream

```bash
kubectl -n bitso-trading-dev exec deploy/market-data -- \
  wget -qO- http://127.0.0.1:8083/health/ready | jq .
```

Expect `checks.trade_stream: "ready"` when live.

### 3. Indicator bar-age health

```bash
kubectl -n bitso-trading-dev exec deploy/strategy-executor -- \
  wget -qO- http://127.0.0.1:8081/api/v1/indicators/btc_mxn/snapshot \
  | jq '{data_healthy, last_bar_age_sec, stale_reason, current_price}'
```

Pass: `data_healthy=true`, `last_bar_age_sec` < 900, non-zero `current_price`.

### 4. Cross-check vs Bitso Stage

```bash
CLUSTER=$(kubectl -n bitso-trading-dev exec deploy/strategy-executor -- \
  wget -qO- http://127.0.0.1:8081/api/v1/indicators/btc_mxn/snapshot | jq -r .current_price)
BITSO=$(curl -s 'https://stage.bitso.com/api/v3/ticker?book=btc_mxn' | jq -r .payload.last)
echo "cluster=$CLUSTER bitso=$BITSO"
```

Gap should be < 0.5% under normal conditions.

### 5. Soak scoring

Exclude stale windows from Phase 4 PASS (documented gaps: 17:36–19:21 and 02:27–15:46 UTC on 2026-06-09/10). After hardening deploy, require **≥24 h** live-price observation before declaring Phase 4 PASS.

---

## Incident runbook (if stale recurs)

1. Check `market_data_last_trade_age_seconds` and `market_data_trade_silence_reconnects_total`.
2. Check logs for `Trade silence watchdog` and `REST fallback ingested`.
3. If watchdog + REST both fail: `kubectl -n bitso-trading-dev rollout restart deploy/market-data`.
4. Verify readiness returns `trade_stream: ready` within ~2 min of trades resuming.
5. Confirm `data_healthy=true` and router not blocked on stale snapshot.

---

## Related documents

- [`STAGE-EXECUTION-SOAK-PHASE4-VERIFICATION-2026-06-09.md`](STAGE-EXECUTION-SOAK-PHASE4-VERIFICATION-2026-06-09.md)
- [`STAGE-FINANCIAL-APPROACH-2026-06-04.md`](STAGE-FINANCIAL-APPROACH-2026-06-04.md)
- [`monitoring/prometheus/rules/trading-alerts.yml`](../../monitoring/prometheus/rules/trading-alerts.yml)
