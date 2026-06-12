# Stage Execution Soak — Post-Phase 5 Financial Next Steps

Date: **2026-06-12**  
Repository: `microservices-trading-bot`  
Related: [`STAGE-FINANCIAL-APPROACH-2026-06-04.md`](STAGE-FINANCIAL-APPROACH-2026-06-04.md), [`STAGE-EXECUTION-SOAK-OPERATOR-GUIDE-2026-06-04.md`](STAGE-EXECUTION-SOAK-OPERATOR-GUIDE-2026-06-04.md), [`STAGE-EXECUTION-SOAK-PHASE4-VERIFICATION-2026-06-11.md`](STAGE-EXECUTION-SOAK-PHASE4-VERIFICATION-2026-06-11.md), [`POST-POINT-10-IMPLEMENTATION-STATUS-2026-05-25.md`](POST-POINT-10-IMPLEMENTATION-STATUS-2026-05-25.md), [`../FINANCIAL-STRATEGY-IMPLEMENTATION-GUIDE.md`](../FINANCIAL-STRATEGY-IMPLEMENTATION-GUIDE.md)

> **Status (2026-06-12):** Phase 5 **in progress** — started 2026-06-11T18:06:59Z; ~1.2 days elapsed of 7-day gate. Classification, trade stream, and price alignment **healthy**. This document records operator observations and the **production-ready financial path** after Phase 5 completes.

---

## 1. Two jobs — do not conflate them

| Job | Question | Phase 5 role |
|-----|----------|--------------|
| **Engineering proof** | Does the machine classify, route, place orders, and count fees **correctly**? | Phase 5 is the **final engineering gate** (7-day extended execution soak). |
| **Economic proof** | After **real** fees and spread, does each strategy in each regime have **positive expectancy**? | **Not** validated by Phase 5. Requires **weeks of shadow** on Stage. |

**A passing Phase 5 soak does not mean you should size up or move to production for profit.** See [`STAGE-FINANCIAL-APPROACH-2026-06-04.md`](STAGE-FINANCIAL-APPROACH-2026-06-04.md) §1.

---

## 2. Phase 5 status snapshot (2026-06-12)

Observed via `./scripts/run-stage-execution-soak-2026-06-04.sh phase5-status` and live cross-checks.

### Window

| Field | Value |
|-------|-------|
| Phase | 5 |
| Started | 2026-06-11T18:06:59Z |
| Elapsed | ~1.2 days (~29 h) |
| 7-day gate | **Pending** — ETA **2026-06-18T18:06:59Z** |
| Phase 4 PASS ref | [`STAGE-EXECUTION-SOAK-PHASE4-VERIFICATION-2026-06-11.md`](STAGE-EXECUTION-SOAK-PHASE4-VERIFICATION-2026-06-11.md) |

### Live configuration

| Check | Status |
|-------|--------|
| `strategy-router` `DRY_RUN` | `false` (live routing) |
| `trading-engine` `DRY_RUN` | unset — `trading_engine_dry_run 0` |
| `BITSO_API_BASE_URL` | `https://stage.bitso.com/api` |
| Active strategy | `mean_reversion_btc_mxn` (running, **has position**) |
| Router regime | `low_vol_range` → noop (preferred already running) |
| `position_size` | 0.001 |

### Execution activity (cumulative)

| Metric | Value |
|--------|-------|
| Router evaluations | 9,472 |
| Strategy switches | 96 (mean_reversion ↔ momentum) |
| Router blocks — `has_position` | 2,479 (expected while position open) |
| Router blocks — `data_stale` | 532 |
| Engine orders executed | 28 |
| Engine signals received | 52 (24 failed pretrade validation) |
| Strategy `trade_count` | 2 (`mean_reversion_btc_mxn`) |

### Classification agreement

| Metric | Value |
|--------|-------|
| Sample loop | **Running** (PID 8200, every 30 min) |
| Samples | 203 |
| Matches | 202 |
| Agreement rate | **99.51%** (≥ 99% pass) |
| Last sample | 2026-06-12T23:24:59Z — `low_vol_range` match=True |
| Single mismatch | 2026-06-12T06:57:19Z — go=`high_vol`, bash=`neutral` |

### Trades & price alignment

| Source | Price (MXN) | Result |
|--------|-------------|--------|
| Latest cluster trade | 1,096,880 | Age ~70s |
| Strategy snapshot | 1,096,880 | `data_healthy=true`, `last_bar_age_sec` ~92s |
| Router snapshot `Price` | 1,096,880 | Exact match |
| Latest 1m bar close | 1,096,880 | Exact match |
| Bitso Stage ticker | 1,094,920 | Gap **0.18%** (under 0.5% threshold) |

**Verdict:** trades and prices **aligned and fetching correctly**.

### Market-data hardening (active)

| Metric | Value | Meaning |
|--------|-------|---------|
| `market_data_last_trade_age_seconds` | ~18–58s | Trade stream fresh (< 300s threshold) |
| `market_data_trade_silence_reconnects_total` | 129 | WS went quiet; watchdog forced reconnect |
| `market_data_rest_fallback_fetches_total` | 433 | REST poller activated during WS silence |
| `market_data_rest_fallback_trades_ingested_total` | 1,692 | Trades recovered via HTTP fallback |
| `/health/ready` `trade_stream` | `ready` | Pod healthy |

**Interpretation:** Hardening is **working as designed**. Bitso Stage WebSocket still goes quiet periodically; the watchdog + REST fallback keep prices fresh without manual `rollout restart`. High counters document WS flakiness, not a current failure — as long as `last_trade_age` stays low and prices align.

---

## 3. What Phase 5 completion unlocks

Phase 5 validates **7 days** of:

- Router + engine live under router-managed strategies
- Regime switching with `has_position` / cooldown guardrails
- Bitso Stage order placement, OM sync, realized fees on fills
- Classification agreement ≥ 99% (sample loop every 30 min)
- Trade stream fresh under hardened `market-data`
- No critical incidents (unsafe switching, sustained stale data, fee drift)

This closes the **POST-POINT-10 engineering milestone** (items 1 + execution soak phases 2–5).

---

## 4. What Phase 5 does **not** validate

Per [`STAGE-EXECUTION-SOAK-OPERATOR-GUIDE-2026-06-04.md`](STAGE-EXECUTION-SOAK-OPERATOR-GUIDE-2026-06-04.md) § "What this soak does not validate":

| Gap | Implication |
|-----|-------------|
| Production Bitso API / WebSocket | Stage WS behavior ≠ prod; re-verify hardening on `wss://ws.bitso.com` before prod |
| P&L profitability | One lucky round-trip or week of plumbing ≠ edge |
| `limit_profit` at scale | May 2026 loss (−17 MXN on ~1340 MXN) — fee floor kills scalping on Stage |
| Strategy parameter optimality | Shadow period required per regime |
| `agent-coordinator` threshold tuner | Deferred in POST-POINT-10 |
| In-process `meta_router` | Deferred until ≥ 4 weeks clean classification |

---

## 5. Recommended next steps after Phase 5 PASS

### Step 1 — Close the engineering gate (~1 day)

1. Run final `phase5-status` after **2026-06-18T18:06:59Z**.
2. Write **Phase 5 PASS verification report** (mirror Phase 4 report format) recording:
   - 7-day elapsed, classification final rate, router/order metrics
   - Regime distribution, switch count, failed orders
   - Fee honesty on fills (realized `fee_rate` vs config)
   - Market-data stale windows (if any) and hardening counter trends
   - Incidents and rollbacks
3. Test rollback path:

   ```bash
   ./scripts/run-stage-execution-soak-2026-06-04.sh rollback
   # verify DRY_RUN=true on router + engine, then re-enable live for shadow
   ```

4. Declare **POST-POINT-10 engineering milestone complete**.

### Step 2 — Enter Shadow Trading (2–4+ weeks minimum)

Transition from **Paper/Soak** (engineering) to **Shadow** (economics) per lifecycle in [`../FINANCIAL-STRATEGY-IMPLEMENTATION-GUIDE.md`](../FINANCIAL-STRATEGY-IMPLEMENTATION-GUIDE.md):

```text
Research → Backtest → Paper (soak) → Shadow (weeks) → Production
```

| Constraint | Value |
|------------|-------|
| Environment | Bitso **Stage** only |
| Size | **Tiny** — keep `position_size=0.001` |
| Books | **One** — `btc_mxn` until per-regime economics proven |
| Financial question | *Per-regime net P&L after realized fees* — not headline P&L |

**Daily / weekly tracking:**

| Metric | Financial why |
|--------|---------------|
| Net P&L **per regime** | Router allocates capital; losses hide in wrong-regime trades |
| Realized fees on every fill | POINT-9/11 — config assumptions ≠ Bitso charges |
| Fee floor vs actual move | `minimum_favorable_move ≈ round_trip_fee% + spread% + slippage` |
| Win rate, drawdown, trade frequency | Gate 2 drift vs backtest (±30% Sharpe, +50% max DD, etc.) |
| Router pause in `high_vol` | Sitting out is financially correct when fees swamp edge |

Use the **Daily Stage Observation Report** template in [`../FINANCIAL-STRATEGY-IMPLEMENTATION-GUIDE.md`](../FINANCIAL-STRATEGY-IMPLEMENTATION-GUIDE.md) § Stage Observation Protocol.

### Step 3 — Fee-floor strategy decisions (during shadow)

| Strategy | Post-soak stance |
|----------|------------------|
| `mean_reversion` | Default for `low_vol_range` / `neutral` — continue shadow |
| `momentum` | Only in `trending_*` — verify moves clear buy-side fees |
| `limit_profit` | **Defer as default** until fee floor proven on Stage |
| `high_vol` | Keep routed to `none` — pause > trade |

Do **not** size up, add books, or enable production capital until per-regime expectancy is positive after realized fees.

### Step 4 — Hardening & observability carry-forward

| Action | When |
|--------|------|
| Keep classification sample loop running (30 min) | Entire shadow period |
| Monitor `market_data_last_trade_age_seconds` | Alert if > 300s sustained |
| Watch `trade_silence_reconnects_total` rate | > 3/hour sustained → investigate |
| Validate hardening on **production WebSocket** | Before prod overlay |
| Grafana `strategy-router` dashboard | Regime, switches, blocks, evaluation errors |
| Fee assumption vs realized histogram alert | POINT-9 §7 (deferred — implement before prod) |

### Step 5 — Infrastructure prep (staging, not production flip)

| Action | Financial why |
|--------|---------------|
| Merge `feat/k8s-deployment-manifests` → `main` | CI publishes `strategy-router:<sha>` on every push |
| Deploy **staging overlay** with prod-like config | Test prod URLs, secrets, `STORAGE_TYPE=redis` |
| Document + test kill switches / rollback | Gate 3 requirement |
| Confirm session risk + daily loss limits | Economics capped by controls |

**Do not** switch `BITSO_API_BASE_URL` to production or deploy real capital until Gate 3 passes.

### Step 6 — Production consideration (Gate 3 only)

From [`../FINANCIAL-STRATEGY-IMPLEMENTATION-GUIDE.md`](../FINANCIAL-STRATEGY-IMPLEMENTATION-GUIDE.md) § Validation Gates:

- [ ] Minimum **2 weeks** on Stage with acceptable metrics
- [ ] No circuit breaker / daily loss triggers
- [ ] Manual review of edge cases (partial fills, stale OID, WS gaps, router blocks)
- [ ] Documented rollback procedure tested
- [ ] Monitoring and alerting verified on target environment
- [ ] Capital budget + risk limits defined

Only then: production credentials, production overlay, **smallest** live size, one book, one strategy.

---

## 6. Explicitly defer (POST-POINT-10)

| Item | Defer until |
|------|-------------|
| `agent-coordinator` threshold tuner | Approval workflow + ≥ 1 week clean audit logs |
| In-process `meta_router` | ≥ 4 weeks clean Stage classification |
| `limit_profit` organic LP at scale | Dedicated LP soak + fee floor proof |
| Parameter optimization / sizing up | Positive per-regime shadow P&L |
| Production Bitso capital | Gate 3 checklist complete |

---

## 7. Decision tree

```text
Phase 5 PASS (≥ 7d, no critical incidents)?
  NO  → Extend Phase 5; fix incidents; do not advance
  YES → Write Phase 5 PASS report; declare engineering milestone done
      → Enter Shadow (2–4+ weeks, tiny size, per-regime P&L)
      → Fee-floor review; keep limit_profit deferred
      → Staging overlay + prod WS hardening validation

Shadow: per-regime net P&L positive after realized fees?
  NO  → Adjust params, pause strategies, or reject — not a plumbing problem
  YES → Gate 3 checklist → production with minimal capital
```

---

## 8. One-sentence optimal approach

**Minimize structural loss (fees + wrong regime + wrong strategy), prove realized-fee P&L on tiny Stage size, use the router to pause more than to trade, and only after weeks of shadow treat Stage P&L as evidence for production capital.**

---

## 9. Monitor commands

```bash
# Phase 5 status (daily until 7d gate)
./scripts/run-stage-execution-soak-2026-06-04.sh phase5-status

# Classification drift
./scripts/stage-soak-sample-loop.sh status
./scripts/run-stage-soak-2026-06-02.sh report

# Trade stream + price alignment
kubectl -n bitso-trading-dev exec deploy/market-data -- \
  wget -qO- http://127.0.0.1:8083/metrics | grep last_trade_age
kubectl -n bitso-trading-dev exec deploy/strategy-executor -- \
  wget -qO- http://127.0.0.1:8081/api/v1/indicators/btc_mxn/snapshot \
  | jq '{data_healthy, last_bar_age_sec, current_price}'

CLUSTER=$(kubectl -n bitso-trading-dev exec deploy/strategy-executor -- \
  wget -qO- http://127.0.0.1:8081/api/v1/indicators/btc_mxn/snapshot | jq -r .current_price)
BITSO=$(curl -s 'https://stage.bitso.com/api/v3/ticker?book=btc_mxn' | jq -r .payload.last)
echo "cluster=$CLUSTER bitso_stage=$BITSO"

# Incident rollback
./scripts/run-stage-execution-soak-2026-06-04.sh rollback
```

---

## 10. Related documents

| Topic | Document |
|-------|----------|
| Financial single reference | [`STAGE-FINANCIAL-APPROACH-2026-06-04.md`](STAGE-FINANCIAL-APPROACH-2026-06-04.md) |
| Execution soak phases | [`STAGE-EXECUTION-SOAK-OPERATOR-GUIDE-2026-06-04.md`](STAGE-EXECUTION-SOAK-OPERATOR-GUIDE-2026-06-04.md) |
| Phase 4 PASS | [`STAGE-EXECUTION-SOAK-PHASE4-VERIFICATION-2026-06-11.md`](STAGE-EXECUTION-SOAK-PHASE4-VERIFICATION-2026-06-11.md) |
| Market-data hardening | [`STAGE-MARKET-DATA-FRESHNESS-HARDENING-2026-06-10.md`](STAGE-MARKET-DATA-FRESHNESS-HARDENING-2026-06-10.md) |
| POST-POINT-10 status | [`POST-POINT-10-IMPLEMENTATION-STATUS-2026-05-25.md`](POST-POINT-10-IMPLEMENTATION-STATUS-2026-05-25.md) |
| Strategy lifecycle + Gate 3 | [`../FINANCIAL-STRATEGY-IMPLEMENTATION-GUIDE.md`](../FINANCIAL-STRATEGY-IMPLEMENTATION-GUIDE.md) |
| Realized fees | [`POINT-9-REALIZED-FEES.md`](POINT-9-REALIZED-FEES.md) |
| MR/momentum fee honesty | [`POINT-11-FEE-HONESTY-MEAN-REVERSION-MOMENTUM-2026-05-22.md`](POINT-11-FEE-HONESTY-MEAN-REVERSION-MOMENTUM-2026-05-22.md) |
