# Stage Execution Soak Verification Report — Phase 2 PASS, Phase 3 In Progress

Date: **2026-06-09** (verification run; updated through **~16:00 UTC** reconciliation)  
Repository: `microservices-trading-bot`  
Related: [`STAGE-EXECUTION-SOAK-OPERATOR-GUIDE-2026-06-04.md`](STAGE-EXECUTION-SOAK-OPERATOR-GUIDE-2026-06-04.md), [`STAGE-EXECUTION-SOAK-PHASE3-RECONCILIATION-2026-06-09.md`](STAGE-EXECUTION-SOAK-PHASE3-RECONCILIATION-2026-06-09.md), [`STAGE-SOAK-VERIFICATION-2026-06-07.md`](STAGE-SOAK-VERIFICATION-2026-06-07.md) (classification PASS), [`BAR-FIRST-INDICATORS-PRODUCTION-2026-06-04.md`](BAR-FIRST-INDICATORS-PRODUCTION-2026-06-04.md), [`STAGE-FINANCIAL-APPROACH-2026-06-04.md`](STAGE-FINANCIAL-APPROACH-2026-06-04.md)

---

## Timeline

| Milestone | Timestamp (UTC) |
|-----------|-----------------|
| Classification short soak PASS | 2026-06-07T22:45:08Z |
| Execution soak Phase 2 started | 2026-06-07T22:54:03Z |
| Phase 2 time gate (≥ 24 h) met | 2026-06-08T22:54:03Z |
| **Phase 2 PASS declared** | **2026-06-09T01:01:22Z** |
| **Phase 3 started** | **2026-06-09T01:06:52Z** |
| Stale `market-data` discovered (price vs Bitso dashboard) | 2026-06-09T03:05Z |
| `market-data` rollout restart (WS reconnect) | 2026-06-09T03:06:46Z |
| First live trade after restart (`LastPrice=1,096,860`) | 2026-06-09T03:08:36Z |
| 4× SELL signals rejected (0.1 BTC sizing bug) | 2026-06-09T03:08–03:11Z |
| **Sizing fix shipped** (`ResolvePositionSize`, image `638b776…`) | **2026-06-09T03:22Z** |
| Strategies re-registered; `mean_reversion_btc_mxn` restarted | 2026-06-09T03:22Z |
| First live BUY filled (`oVs9Fk5oEnzkfDqo`) | 2026-06-09T03:34:40Z |
| First live SELL placed (`VgdJKh6fKC2c1xrZ`) | 2026-06-09T03:41:24Z |
| SELL partial-fill burst (OM bug) | 2026-06-09T04:00:25Z |
| **OM partial-fill fix + reconciliation** | **2026-06-09T~16:00Z** — see reconciliation doc |

Operator session: Phase 3 started via `./scripts/run-stage-execution-soak-2026-06-04.sh phase3-start`.

Machine-readable window: `tmp/stage-execution-soak/execution-window.json`.

---

## Executive summary

| Gate | Status | Notes |
|------|--------|-------|
| Classification short soak (item 1) | ✅ **PASS** | 140/140 agreement — see 2026-06-07 report |
| Phase 2 — router lifecycle, engine dry | ✅ **PASS** | 26.2 h, 3,137 evaluations, 1 switch, zero errors |
| Phase 3 — engine live | ✅ | `trading_engine_dry_run 0` |
| Phase 3 — live price in cluster | ✅ **Fixed** | Was stale ~90 h; restart restored WS trades |
| Phase 3 — position sizing | ✅ **Fixed** | `parameters.position_size` no longer overridden by global `MaxPositionSize` |
| Phase 3 — first Stage round-trip | ✅ **Complete** | BUY + SELL filled after reconciliation (`7fae8b0`) |
| Phase 3 — fee honesty on fill | ✅ **Verified** | BUY `fee_rate` 0.57%; SELL `fee_rate` 0.57% on partial/final legs |
| Extended 7-day classification gate | 📋 **In progress** | Continue periodic sampling |

**Current priority:** Wait for next band-breakout signal at **0.001 BTC** (~1.1k MXN notional); confirm engine places order and OM records OID.

---

## Phase 2 — router lifecycle (PASS)

| Metric | Value |
|--------|-------|
| Elapsed at Phase 3 transition | **26.2 h** |
| Evaluations | **3,137** |
| Strategy switches | **1** (`<none>` → `mean_reversion_btc_mxn`) |
| Bitso orders | **None** (`trading_engine_dry_run 1` throughout) |

```text
elapsed_hours = 26.2 ≥ 24  ✅
evaluation_errors = 0      ✅
engine_orders = 0          ✅
```

---

## Phase 3 — incidents and fixes

### Incident 1 — Stale `market-data` (price ~4 days old)

**Symptom:** Bitso dashboard showed BTC/MXN ~**1,097,340** MXN; cluster snapshot reported **1,083,640** MXN (~1.3% low). No signals for hours after Phase 3 start.

**Evidence:**

| Source | Value |
|--------|-------|
| `strategy-executor` snapshot `current_price` | 1,083,640 |
| Last 1m bar timestamp | 2026-06-05T09:07:00Z |
| `market-data` `Last Trade` | **~90 h ago** (trade ID 195863374) |
| `GET /api/v1/trades?book=btc_mxn` | `count: 0` |
| Bitso REST `ticker` (prod) | ~1,097,500 |

**Root cause:** Bitso WebSocket on the `market-data` pod had stopped delivering trades while the pod stayed healthy. Indicators kept recomputing from **stale 1m bar closes**. `data_healthy: true` only reflects compute freshness (`data_age_sec`), **not** trade-stream freshness.

**Remediation:** `kubectl -n bitso-trading-dev rollout restart deploy/market-data`. After reconnect, live trades resumed within ~2 min (`LastPrice=1,096,860`).

**Operator check (run during Phase 3+):**

```bash
# Compare cluster vs Bitso
kubectl -n bitso-trading-dev exec deploy/strategy-executor -- \
  wget -qO- http://127.0.0.1:8081/api/v1/indicators/btc_mxn/snapshot | jq '{current_price,bars_used,computed_at}'

curl -s 'https://bitso.com/api/v3/ticker/?book=btc_mxn' | jq '.payload.last'

# market-data trade freshness
kubectl -n bitso-trading-dev logs deploy/market-data --tail=5 | grep 'Last Trade'
```

If `Last Trade` is more than a few minutes old while the book is active, restart `market-data` before trusting signals or soak metrics.

---

### Incident 2 — Position sizing override (0.1 BTC signals)

**Symptom:** After live price returned, `mean_reversion_btc_mxn` emitted **4× SELL** signals. All rejected by pre-trade validation:

```text
order value 109686.000000 exceeds maximum 100000.000000
```

**Root cause:** Strategy API showed `parameters.position_size: 0.001`, but at `Initialize()` the global `Sizing.MaxPositionSize` (**0.1 BTC**, from `DEFAULT_STRATEGY` / `MAX_TRADE_AMOUNT`) **replaced** the explicit parameter. Signals carried **0.1 BTC** × ~1.09M MXN ≈ **109k MXN** > `MAX_ORDER_VALUE` (100k MXN on order-management).

**Fix (code):** `ResolvePositionSize()` in `services/strategy-executor/internal/strategies/enhanced_strategy.go`:

- Explicit `parameters.position_size` wins when set.
- `Sizing.MaxPositionSize` is a **ceiling**, not an unconditional override.
- Applied to `mean_reversion`, `momentum`, and `limit_profit`.

**Deploy:** Image `326105557351.dkr.ecr.us-east-1.amazonaws.com/strategy-executor:638b776d5d0c853db9fa6c8fbd9ab757b618858f` (dev overlay `kustomization.yaml` updated).

**Post-fix verification:**

```bash
kubectl -n bitso-trading-dev exec deploy/strategy-executor -- \
  wget -qO- http://127.0.0.1:8081/api/v1/strategies/mean_reversion_btc_mxn | jq '.parameters.position_size'
# Expect: 0.001
```

Re-register strategies after executor rollout (`./scripts/run-stage-soak-2026-06-02.sh register`) because the registry is in-process memory.

---

### Phase 3 signal / order ledger (2026-06-09)

| Time (UTC) | Event | Amount | Result |
|------------|-------|--------|--------|
| 03:08:41 | SELL signal @ 1,096,860 | 0.1 BTC (bug) | Pre-trade **rejected** (>100k MXN) |
| 03:09:46 | SELL signal @ 1,096,390 | 0.1 BTC | **Rejected** |
| 03:10:51 | SELL signal @ 1,096,390 | 0.1 BTC | **Rejected** |
| 03:11:56 | SELL signal @ 1,097,200 | 0.1 BTC | **Rejected** |
| 03:22+ | After sizing fix + re-register | — | **0 new signals** (price inside bands) |

| Metric (trading-engine) | Value |
|-------------------------|-------|
| `signals_received_total` | 4 |
| `pretrade_validation_rejections_total` | 4 |
| `orders_failed_total{reason="pretrade_validation"}` | 4 |
| Bitso orders placed | **0** |

---

## Phase 3 — configuration (latest)

| Component | Expected | Actual (03:23 UTC) |
|-----------|----------|---------------------|
| `strategy-router` `DRY_RUN` | `true` | `true` |
| `trading-engine` live | `trading_engine_dry_run 0` | `0` |
| Running strategies | One | `mean_reversion_btc_mxn` |
| `position_size` | 0.001 | **0.001** (post-fix) |
| Cluster `current_price` | ≈ Bitso | **1,097,020** |
| Bollinger bands | — | upper 1,106,729 / lower 1,075,927 (inside → no entry) |

---

## What to validate (Phase 3 pass criteria)

1. **trading-engine** — `Successfully placed … order <oid>` (not `[DRY-RUN] Would place`)
2. **order-management** — `Recorded order placed` with `bitso_order_id`
3. **Bitso Stage dashboard** — OID matches engine log
4. **Fill sync** — OM status `submitted` → `filled`
5. **Fee honesty** — realized `fee_rate` on first complete round-trip

---

## Monitor commands

```bash
./scripts/run-stage-execution-soak-2026-06-04.sh phase3-status

kubectl -n bitso-trading-dev logs deploy/trading-engine -f | grep -iE 'signal|placed|validation'

kubectl -n bitso-trading-dev exec deploy/trading-engine -- \
  wget -qO- http://127.0.0.1:8080/metrics | grep -E 'signals_received|pretrade_validation|orders_'

./scripts/run-stage-soak-2026-06-02.sh sample   # classification drift
./scripts/run-stage-execution-soak-2026-06-04.sh rollback   # incident rollback
```

---

## Next steps

1. **Watch for band breakout** — next signal should be **0.001 BTC** and pass OM validation (~1.1k MXN notional).
2. **Confirm first Bitso OID** — engine log + OM + Stage dashboard.
3. **Fee honesty** — realized `fee_rate` on fill.
4. **Keep `market-data` fresh** — grep `Last Trade` in logs during soak.
5. **Phase 4** — after first round-trip spot-check, enable router + engine live per execution guide.

---

## Related documents

- [`STAGE-EXECUTION-SOAK-OPERATOR-GUIDE-2026-06-04.md`](STAGE-EXECUTION-SOAK-OPERATOR-GUIDE-2026-06-04.md) — § Phase 3 troubleshooting
- [`BAR-FIRST-INDICATORS-PRODUCTION-2026-06-04.md`](BAR-FIRST-INDICATORS-PRODUCTION-2026-06-04.md) — bar-first compute; `data_healthy` semantics
- [`STAGE-SOAK-MARKET-DATA-ATR-OBSERVATIONS-2026-06-03.md`](STAGE-SOAK-MARKET-DATA-ATR-OBSERVATIONS-2026-06-03.md) — market-data flow
- [`../ORDER-FLOW-AND-BITSO-TESTING.md`](../ORDER-FLOW-AND-BITSO-TESTING.md)
