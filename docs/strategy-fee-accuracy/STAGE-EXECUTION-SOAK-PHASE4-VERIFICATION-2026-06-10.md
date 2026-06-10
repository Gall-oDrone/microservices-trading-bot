# Stage Execution Soak Verification Report — Phase 4 Status (29.8 h)

Date: **2026-06-10** (verification run **~23:01 UTC**)  
Repository: `microservices-trading-bot`  
Related: [`STAGE-EXECUTION-SOAK-PHASE4-VERIFICATION-2026-06-09.md`](STAGE-EXECUTION-SOAK-PHASE4-VERIFICATION-2026-06-09.md), [`STAGE-MARKET-DATA-FRESHNESS-HARDENING-2026-06-10.md`](STAGE-MARKET-DATA-FRESHNESS-HARDENING-2026-06-10.md), [`STAGE-EXECUTION-SOAK-OPERATOR-GUIDE-2026-06-04.md`](STAGE-EXECUTION-SOAK-OPERATOR-GUIDE-2026-06-04.md), [`STAGE-SOAK-VERIFICATION-2026-06-07.md`](STAGE-SOAK-VERIFICATION-2026-06-07.md)

---

## Timeline

| Milestone | Timestamp (UTC) |
|-----------|-----------------|
| Phase 4 started | 2026-06-09T17:11:11Z |
| Stale trade-stream incidents (excluded from PASS) | 2026-06-09 17:36–19:21; 2026-06-10 02:27–15:46 |
| **market-data hardening deployed** (`bfdffac`) | **2026-06-10T16:56:31Z** |
| strategy-executor restart; momentum + limit_profit re-registered | 2026-06-10T16:51:42Z / 17:14:20Z |
| **Post-hardening 24 h observation started** | **2026-06-10T16:56:31Z** |
| **This verification run** | **2026-06-10T23:01:00Z** |

Operator session: `./scripts/run-stage-execution-soak-2026-06-04.sh phase4-status`

Machine-readable window: `tmp/stage-execution-soak/execution-window.json`

---

## Executive summary

| Gate | Status | Notes |
|------|--------|-------|
| Classification short soak | ✅ **PASS** | 144/144 samples, rate=1.0 |
| Phase 4 — router live | ✅ | `strategy-router DRY_RUN=false` |
| Phase 4 — engine live | ✅ | `trading_engine_dry_run 0` |
| Phase 4 — original 24 h gate | ✅ **PASS** | **29.8 h** elapsed since start |
| Phase 4 — post-hardening 24 h gate | 📋 **Pending** | **6.1 h** elapsed; ~18 h remaining |
| Trades & prices aligned | ✅ **PASS** | Cluster price = Bitso Stage; trade stream fresh |
| Router health | ✅ | 3,582 evaluations, **0** evaluation errors |
| Safe switching | ✅ | One strategy running; `has_position` blocks handoff |

**Verdict:** Phase 4 is **operationally healthy** at verification time. Trades and prices are aligned and fetching correctly under the hardened `market-data` stack. **Full Phase 4 PASS cannot be declared** until the post-hardening 24 h live-price observation completes (~**2026-06-11T16:56 UTC**). Stale windows before hardening remain excluded per [`STAGE-MARKET-DATA-FRESHNESS-HARDENING-2026-06-10.md`](STAGE-MARKET-DATA-FRESHNESS-HARDENING-2026-06-10.md) §5.

---

## Phase 4 runtime state (23:01 UTC)

| Component | Value |
|-----------|-------|
| Elapsed since Phase 4 start | **29.8 h** |
| Post-hardening observation elapsed | **6.1 h** |
| Current regime | `low_vol_range` |
| Router action | `noop` — preferred strategy already running |
| Running strategy | `mean_reversion_btc_mxn` |
| Registered (stopped) | `momentum_btc_mxn`, `limit_profit_btc_mxn` |
| `position_size` | **0.001** BTC on all canonical strategies |
| Strategy state | `has_position=false`, `trade_count=1` |
| `market-data` image | `market-data:bfdffac` (1/1 Ready) |

### Router metrics

| Metric | Value |
|--------|-------|
| `strategy_router_evaluations_total` | 3,582 |
| `strategy_router_evaluation_errors_total` | **0** |
| Strategy switches (cumulative) | **44** total |

Switch breakdown:

| From | Regime | To | Count |
|------|--------|-----|-------|
| `<none>` | `low_vol_range` | `mean_reversion_btc_mxn` | 2 |
| `mean_reversion_btc_mxn` | `trending_up` | `momentum_btc_mxn` | 14 |
| `mean_reversion_btc_mxn` | `trending_down` | `momentum_btc_mxn` | 7 |
| `momentum_btc_mxn` | `low_vol_range` | `mean_reversion_btc_mxn` | 19 |
| `momentum_btc_mxn` | `neutral` | `mean_reversion_btc_mxn` | 2 |

### Router blocks (cumulative)

| Reason | Count | Interpretation |
|--------|-------|----------------|
| `has_position` | 1,728 | Expected — blocks unsafe handoff while in position |
| `data_stale` | 37 | Pre-hardening stale windows; router correctly paused |
| `cooldown` | 29 | Normal switch debounce |
| `not_registered` | 13 | Brief gap after strategy-executor restart (2026-06-10 ~16:51) |

---

## Trades & price alignment

### Snapshot health

| Check | Value | Threshold | Result |
|-------|-------|-----------|--------|
| `data_healthy` | `true` | — | ✅ |
| `last_bar_age_sec` | 122.5 s | < 900 s (15 m) | ✅ |
| `current_price` | 1,069,590 MXN | non-zero | ✅ |
| `strategy_executor_indicators_healthy` | 1 | — | ✅ |
| RSI (14) | 53.36 | — | — |
| ATR (14) | 522.79 | — | — |

### Trade stream

| Check | Value | Threshold | Result |
|-------|-------|-----------|--------|
| `market_data_last_trade_age_seconds` | **67.1 s** | < 300 s | ✅ |
| `/health/ready` `trade_stream` | `ready` | `ready` | ✅ |
| Latest trade (API) | ID 686241024 @ 1,069,590 MXN, 22:59:26Z | recent | ✅ |

### Cross-check vs Bitso

| Source | Price (MXN) | Gap vs cluster |
|--------|-------------|----------------|
| Cluster snapshot | 1,069,590 | — |
| Bitso Stage ticker | 1,069,590 | **0.0000%** |
| Bitso prod ticker | 1,069,580 | 0.0009% |

**Pass:** cluster price matches Bitso Stage exactly; gap < 0.5% vs prod.

### Hardening layer activity (cumulative)

| Metric | Value |
|--------|-------|
| `market_data_trade_silence_reconnects_total` | 68 |
| `market_data_rest_fallback_fetches_total` | 228 |
| `market_data_rest_fallback_trades_ingested_total` | 780 |

**Observation (last 2 h):** WS still goes quiet periodically (~5 m). Trade-silence watchdog forces reconnect; REST fallback ingests trades during gaps. Example at 22:59:33 UTC: last trade 5m8s → watchdog reconnect → trades resumed within ~30 s (`Last Trade: 37s ago` at 23:00:03).

---

## Classification drift

| Metric | Value |
|--------|-------|
| Total samples (`agreement-samples.jsonl`) | 144 |
| Matches | 144 |
| Agreement rate | **1.0000** |
| Latest sample (2026-06-10T23:00:56Z) | `go=low_vol_range` `bash=low_vol_range` `match=true` |

---

## Orders & engine activity

| Item | Status |
|------|--------|
| `trading_engine_dry_run` | 0 (live) |
| Open Bitso order | OID `s3ENQcRZnrVcrRpn` — `accepted`, limit ~1,071,810 MXN, unfilled |
| OM sync | Polling `SyncOrderFromBitso` every 10 s — normal |
| Last engine signal | ~14 m before verification (no new signals; Kafka consumer idle) |

No `orders_executed_total` or `orders_failed_total` counters were present in the scraped metrics at verification time (only `trading_engine_dry_run` exposed). Engine logs show routine Kafka consumer timeouts with no order errors.

---

## Pass criteria scorecard

| Criterion | Result |
|-----------|--------|
| Router health — zero evaluation errors | ✅ |
| Safe switching — one strategy per book | ✅ |
| OM sync — open order syncing, no stuck partial-fill burst | ✅ (monitor) |
| Classification drift ≥ 99% | ✅ 100% |
| Live price fresh post-hardening ≥ 24 h | ⏳ 6.1 / 24 h |

---

## Excluded windows (pre-hardening)

Per hardening doc, these stale periods are **excluded** from Phase 4 PASS scoring:

| Window (UTC) | Duration | Symptom |
|--------------|----------|---------|
| 2026-06-09 17:36–19:21 | ~1.8 h | WS silent; stale `current_price` |
| 2026-06-10 02:27–15:46 | ~13.3 h | WS silent; manual restart required |

Post-hardening observation clock starts at **2026-06-10T16:56:31Z**.

---

## Monitor commands

```bash
./scripts/run-stage-execution-soak-2026-06-04.sh phase4-status

# Trade stream + price alignment
kubectl -n bitso-trading-dev logs deploy/market-data --tail=5 | grep 'Last Trade'
kubectl -n bitso-trading-dev exec deploy/market-data -- \
  wget -qO- http://127.0.0.1:8083/metrics | grep last_trade_age
kubectl -n bitso-trading-dev exec deploy/strategy-executor -- \
  wget -qO- http://127.0.0.1:8081/api/v1/indicators/btc_mxn/snapshot \
  | jq '{data_healthy, last_bar_age_sec, current_price}'

CLUSTER=$(kubectl -n bitso-trading-dev exec deploy/strategy-executor -- \
  wget -qO- http://127.0.0.1:8081/api/v1/indicators/btc_mxn/snapshot | jq -r .current_price)
BITSO=$(curl -s 'https://stage.bitso.com/api/v3/ticker?book=btc_mxn' | jq -r .payload.last)
echo "cluster=$CLUSTER bitso_stage=$BITSO"

./scripts/run-stage-soak-2026-06-02.sh sample   # classification drift
./scripts/run-stage-execution-soak-2026-06-04.sh rollback   # incident rollback
```

---

## Next steps

1. **Wait for post-hardening gate** — re-run verification after **2026-06-11T16:56 UTC** (~18 h from this run).
2. **Monitor open order** — `s3ENQcRZnrVcrRpn`; confirm fill or cancel path is clean.
3. **Continue periodic sampling** — `./scripts/run-stage-soak-2026-06-02.sh sample` every few hours.
4. **Declare Phase 4 PASS** — if post-hardening 24 h holds with fresh prices and no order/OM incidents, proceed to Phase 5 (7-day extended soak).

---

## Related documents

- [`STAGE-EXECUTION-SOAK-PHASE4-VERIFICATION-2026-06-09.md`](STAGE-EXECUTION-SOAK-PHASE4-VERIFICATION-2026-06-09.md) — Phase 4 start report
- [`STAGE-MARKET-DATA-FRESHNESS-HARDENING-2026-06-10.md`](STAGE-MARKET-DATA-FRESHNESS-HARDENING-2026-06-10.md) — hardening layers + scoring rules
- [`STAGE-EXECUTION-SOAK-OPERATOR-GUIDE-2026-06-04.md`](STAGE-EXECUTION-SOAK-OPERATOR-GUIDE-2026-06-04.md) — § Phase 4, § Phase 5
- [`scripts/run-stage-execution-soak-2026-06-04.sh`](../../scripts/run-stage-execution-soak-2026-06-04.sh) — `phase4-status`
