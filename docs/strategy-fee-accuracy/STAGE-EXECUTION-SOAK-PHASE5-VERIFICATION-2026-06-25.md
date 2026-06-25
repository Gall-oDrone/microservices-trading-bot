# Stage Execution Soak Verification Report — Phase 5 (7-day extended) — TEMPLATE

> **STATUS: TEMPLATE — NOT YET VERIFIED.** This is the fill-in skeleton for the Phase 5 PASS
> report (Bucket A1 of [`RECOMMENDED-NEXT-STEPS-2026-06-25.md`](RECOMMENDED-NEXT-STEPS-2026-06-25.md)).
> Replace every `<…>` placeholder with measured values, delete this banner and the `TEMPLATE`
> suffix in the title, and set the final verdict before treating this as authoritative.
>
> **How to populate:**
> ```bash
> ./scripts/run-stage-execution-soak-2026-06-04.sh phase5-status
> # window file: tmp/stage-execution-soak/execution-window.json
> ./scripts/strategy-daily-report.sh --book btc_mxn          # per-day P&L / signals / fee drift
> ./scripts/kill-switch-status.sh                            # breaker state
> ./scripts/analyze-regime-stratified-agreement.sh           # D1 per-regime agreement
> ./scripts/validate-prod-ws-hardening.sh                    # D2 reconnect-rate drop
> ```

Date: **2026-06-25** (verification run **<HH:MM> UTC**)
Repository: `microservices-trading-bot`
Branch: `feat/k8s-deployment-manifests`
Related: [`STAGE-EXECUTION-SOAK-PHASE4-VERIFICATION-2026-06-11.md`](STAGE-EXECUTION-SOAK-PHASE4-VERIFICATION-2026-06-11.md), [`STAGE-EXECUTION-SOAK-POST-PHASE5-FINANCIAL-NEXT-STEPS-2026-06-12.md`](STAGE-EXECUTION-SOAK-POST-PHASE5-FINANCIAL-NEXT-STEPS-2026-06-12.md), [`STAGE-MARKET-DATA-FRESHNESS-HARDENING-2026-06-10.md`](STAGE-MARKET-DATA-FRESHNESS-HARDENING-2026-06-10.md), [`B-D-IMPLEMENTATION-2026-06-25.md`](B-D-IMPLEMENTATION-2026-06-25.md)

---

## Timeline

| Milestone | Timestamp (UTC) |
|-----------|-----------------|
| Phase 4 PASS declared | 2026-06-11T17:37:00Z |
| **Phase 5 started** | 2026-06-11T18:06:59Z |
| 7-day gate ETA | 2026-06-18T18:06:59Z |
| WS `inbox`-close fix deployed | `<commit / timestamp>` |
| Incidents / restarts (if any) | `<list or "none">` |
| Stale trade-stream windows (excluded) | `<list or "none">` |
| **This verification run** | `<2026-06-25THH:MM:SSZ>` |

Operator session: `./scripts/run-stage-execution-soak-2026-06-04.sh phase5-status`
Machine-readable window: `tmp/stage-execution-soak/execution-window.json`

---

## Executive summary

| Gate | Status | Notes |
|------|--------|-------|
| Phase 5 — router live (`DRY_RUN=false`) | `<✅ / ❌>` | `<strategy_router DRY_RUN=…>` |
| Phase 5 — engine live (`trading_engine_dry_run 0`) | `<✅ / ❌>` | `<value>` |
| Phase 5 — 7-day time gate | `<✅ / ❌>` | `<elapsed>` h elapsed since start (≥ 168 h) |
| Trades & prices aligned | `<✅ / ❌>` | `<gap %>` vs Bitso Stage |
| Router health | `<✅ / ❌>` | `<evaluations>` evaluations, `<errors>` evaluation errors |
| Safe switching | `<✅ / ❌>` | One strategy per book; `has_position` blocks handoff |
| Classification drift ≥ 99% | `<✅ / ❌>` | `<rate>` over `<N>` samples |
| Regime-stratified coverage (D1) | `<✅ / ⚠️>` | `<high_vol/trending_* sample counts>` |
| WS reconnect-rate drop post-fix (D2) | `<✅ / ❌>` | `<reconnect delta over window>` |
| No critical incidents | `<✅ / ❌>` | `<count + summary>` |

**Verdict:** `<PASS / EXTEND / FAIL>`. `<One-paragraph rationale: time gate met, router+engine live,
prices aligned under hardened market-data, classification agreement, incident summary. If EXTEND/FAIL,
state what is missing and the new gate date.>`

---

## Phase 5 runtime state (`<HH:MM>` UTC)

| Component | Value |
|-----------|-------|
| Elapsed since Phase 5 start | `<elapsed h>` |
| Current regime | `<regime>` |
| Router action | `<noop / switch …>` |
| Running strategy | `<strategy_btc_mxn>` |
| Registered (stopped) | `<list>` |
| `position_size` | `<0.001>` BTC |
| Strategy state | `has_position=<bool>`, `trade_count=<n>` |
| `market-data` image | `<image:tag>` (`<ready>`) |
| `strategy-executor` image | `<image:tag>` (`<ready>`) |

### Router metrics

| Metric | Value |
|--------|-------|
| `strategy_router_evaluations_total` | `<n>` |
| `strategy_router_evaluation_errors_total` | `<n (0 expected)>` |
| Strategy switches (cumulative) | `<n>` |

Switch breakdown:

| From | Regime | To | Count |
|------|--------|-----|-------|
| `<from>` | `<regime>` | `<to>` | `<n>` |

### Router blocks (cumulative)

| Reason | Count | Interpretation |
|--------|-------|----------------|
| `has_position` | `<n>` | Expected — blocks unsafe handoff while in position |
| `data_stale` | `<n>` | Router paused during stale snapshots |
| `cooldown` | `<n>` | Normal switch debounce |
| `not_registered` | `<n>` | `<explain any non-zero>` |

---

## Trades & price alignment

### Snapshot health

| Check | Value | Threshold | Result |
|-------|-------|-----------|--------|
| `data_healthy` | `<true/false>` | — | `<✅/❌>` |
| `last_bar_age_sec` | `<s>` | < 900 s (15 m) | `<✅/❌>` |
| `current_price` | `<MXN>` | non-zero | `<✅/❌>` |
| RSI (14) | `<present?>` | — | — |
| ATR (14) | `<present?>` | — | — |

### Trade stream

| Check | Value | Threshold | Result |
|-------|-------|-----------|--------|
| `market_data_last_trade_age_seconds` | `<s>` | < 300 s | `<✅/❌>` |
| `/health/ready` `trade_stream` | `<ready?>` | `ready` | `<✅/❌>` |
| Latest trade (API) | `<id @ price, ts>` | recent | `<✅/❌>` |

### Cross-check vs Bitso

| Source | Price (MXN) | Gap vs cluster |
|--------|-------------|----------------|
| Cluster snapshot | `<MXN>` | — |
| Latest trade (market-data API) | `<MXN>` | `<%>` |
| Bitso Stage ticker | `<MXN>` | `<%>` |

**Pass threshold:** cluster vs Bitso Stage gap < 0.5%.

### Hardening layer activity (cumulative over Phase 5)

| Metric | Value | Phase 4 baseline | Trend |
|--------|-------|------------------|-------|
| `market_data_trade_silence_reconnects_total` | `<n>` | 341 | `<↓/↑/→>` |
| `market_data_rest_fallback_fetches_total` | `<n>` | 1,058 | `<↓/↑/→>` |
| `market_data_rest_fallback_trades_ingested_total` | `<n>` | 2,166 | `<↓/↑/→>` |

**D2 check:** post-`inbox`-fix the silence/reconnect **rate** (per hour) should fall vs Phase 4.
Run `./scripts/validate-prod-ws-hardening.sh` and record the per-window delta: `<result>`.

---

## Classification drift

| Metric | Value |
|--------|-------|
| Total samples (`agreement-samples.jsonl`) | `<n>` |
| Matches | `<n>` |
| Agreement rate | `<rate>` |
| Latest sample (`<ts>`) | `go=<r>` `bash=<r>` `match=<bool>` |

### Regime-stratified agreement (D1)

Run `./scripts/analyze-regime-stratified-agreement.sh`. Per-regime, reference = Go:

| Regime | Samples | Matches | Rate | Coverage (≥ 30) |
|--------|---------|---------|------|-----------------|
| `low_vol_range` | `<n>` | `<n>` | `<rate>` | `<✅/⚠️>` |
| `neutral` | `<n>` | `<n>` | `<rate>` | `<✅/⚠️>` |
| `trending_up` | `<n>` | `<n>` | `<rate>` | `<✅/⚠️>` |
| `trending_down` | `<n>` | `<n>` | `<rate>` | `<✅/⚠️>` |
| `high_vol` | `<n>` | `<n>` | `<rate>` | `<✅/⚠️>` |

**Note:** treat under-sampled regimes (`high_vol`, `trending_*`) as *not yet verified*, not passed.

---

## Orders & engine activity (7-day cumulative)

| Item | Value |
|------|-------|
| `trading_engine_dry_run` | `<0 = live>` |
| Total orders placed | `<n>` |
| Total fills | `<n>` |
| Failed orders | `<n>` (reasons: `<…>`) |
| Round-trips completed | `<n>` |
| OM sync | `<clean? any drift?>` |

### Fee honesty (B1) — realized vs assumed

From `/metrics` (`strategy_executor_fee_drift_ratio`, `..._realized_fee_rate`, `..._assumed_fee_rate`):

| Book | Side | Liquidity | Realized | Assumed | Drift ratio | Alert? |
|------|------|-----------|----------|---------|-------------|--------|
| `btc_mxn` | buy | `<maker/taker>` | `<rate>` | `<rate>` | `<ratio>` | `<none / FeeRealized…>` |
| `btc_mxn` | sell | `<maker/taker>` | `<rate>` | `<rate>` | `<ratio>` | `<none / FeeRealized…>` |

**Pass:** no sustained `FeeRealizedAboveAssumed` (>1.15) or `FeeRealizedFarFromAssumed` (>1.30 / <0.70) firing.

---

## Economic observation (informational — NOT a Phase 5 gate)

Phase 5 proves **engineering durability**, not profitability. Record headline numbers here for
continuity into the Bucket C shadow run; do **not** use them to justify sizing up.

| Strategy | Net P&L (quote, after fees) | Trades | Win rate | Notes |
|----------|----------------------------|--------|----------|-------|
| `mean_reversion_btc_mxn` | `<MXN>` | `<n>` | `<%>` | — |
| `momentum_btc_mxn` | `<MXN>` | `<n>` | `<%>` | — |

Per-regime net P&L after realized fees is the **Bucket C** deliverable, not this report.

---

## Pass criteria scorecard

| Criterion | Target | Result |
|-----------|--------|--------|
| 7-day time gate | ≥ 168 h since Phase 5 start | `<✅/❌>` |
| Router health — zero evaluation errors | 0 | `<✅/❌>` |
| Safe switching — one strategy per book | always | `<✅/❌>` |
| OM sync — fills syncing cleanly | no drift | `<✅/❌>` |
| Classification drift ≥ 99% | ≥ 0.99 | `<✅/❌>` |
| Live price fresh | `last_trade_age` < 300 s sustained | `<✅/❌>` |
| WS reconnect rate dropped post-fix (D2) | < Phase 4 rate | `<✅/❌>` |
| Fee drift within bounds (B1) | no sustained alert | `<✅/❌>` |
| No critical incidents | 0 | `<✅/❌>` |

---

## Excluded windows (if any)

Per [`STAGE-MARKET-DATA-FRESHNESS-HARDENING-2026-06-10.md`](STAGE-MARKET-DATA-FRESHNESS-HARDENING-2026-06-10.md) §5,
list any stale periods excluded from PASS scoring:

| Window (UTC) | Duration | Symptom | Excluded? |
|--------------|----------|---------|-----------|
| `<start–end>` | `<h>` | `<…>` | `<yes/no>` |

---

## Watch items (non-blocking)

1. `<e.g. residual WS silence pattern, sample-loop continuity, any new anomaly>`
2. `<…>`

---

## Recommended next steps

1. **`<Declare Phase 5 PASS / Extend gate / Fix incidents>`** — `<rationale>`.
2. **Declare POST-POINT-10 engineering milestone complete (A5)** — only after this report is PASS and A1–A4 done.
3. **Enter Bucket C shadow trading (C1/C2)** — 2–4+ weeks, Stage only, `position_size=0.001`, one book; track per-regime net P&L after realized fees via `./scripts/strategy-daily-report.sh`.
4. **Validate prod WS hardening (C3)** — `./scripts/validate-prod-ws-hardening.sh --url <prod market-data>` before any prod overlay.
5. **Close D1 coverage** — keep sampling until `high_vol` / `trending_*` clear the minimum sample floor.

---

## Monitor commands

```bash
./scripts/run-stage-execution-soak-2026-06-04.sh phase5-status

# Trade stream + price alignment
kubectl -n bitso-trading-dev exec deploy/market-data -- \
  wget -qO- http://127.0.0.1:8083/metrics | grep -E 'last_trade_age|reconnects_total|rest_fallback'
kubectl -n bitso-trading-dev exec deploy/strategy-executor -- \
  wget -qO- http://127.0.0.1:8081/api/v1/indicators/btc_mxn/snapshot \
  | jq '{data_healthy, last_bar_age_sec, current_price}'

CLUSTER=$(kubectl -n bitso-trading-dev exec deploy/strategy-executor -- \
  wget -qO- http://127.0.0.1:8081/api/v1/indicators/btc_mxn/snapshot | jq -r .current_price)
BITSO=$(curl -s 'https://stage.bitso.com/api/v3/ticker?book=btc_mxn' | jq -r .payload.last)
echo "cluster=$CLUSTER bitso_stage=$BITSO"

# Phase 5 / Bucket B-D tooling
./scripts/strategy-daily-report.sh --book btc_mxn
./scripts/kill-switch-status.sh
./scripts/analyze-regime-stratified-agreement.sh
./scripts/validate-prod-ws-hardening.sh
./scripts/stage-soak-sample-loop.sh status
./scripts/run-stage-execution-soak-2026-06-04.sh rollback   # incident rollback
```

---

## Related documents

- [`STAGE-EXECUTION-SOAK-PHASE4-VERIFICATION-2026-06-11.md`](STAGE-EXECUTION-SOAK-PHASE4-VERIFICATION-2026-06-11.md) — Phase 4 PASS (basis for this template)
- [`STAGE-EXECUTION-SOAK-POST-PHASE5-FINANCIAL-NEXT-STEPS-2026-06-12.md`](STAGE-EXECUTION-SOAK-POST-PHASE5-FINANCIAL-NEXT-STEPS-2026-06-12.md) — post-Phase 5 financial path
- [`STAGE-MARKET-DATA-FRESHNESS-HARDENING-2026-06-10.md`](STAGE-MARKET-DATA-FRESHNESS-HARDENING-2026-06-10.md) — hardening layers + scoring rules
- [`B-D-IMPLEMENTATION-2026-06-25.md`](B-D-IMPLEMENTATION-2026-06-25.md) — B1–B4 + C/D tooling used to populate this report
- [`RECOMMENDED-NEXT-STEPS-2026-06-25.md`](RECOMMENDED-NEXT-STEPS-2026-06-25.md) — Bucket A/C/D context
