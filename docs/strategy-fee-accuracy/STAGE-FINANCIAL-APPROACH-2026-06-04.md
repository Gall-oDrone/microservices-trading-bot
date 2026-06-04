# Stage Financial Approach — Single Reference

Date: 2026-06-04  
Repository: `microservices-trading-bot`  
Audience: Operators and developers who feel lost between **soaks**, **routing**, and **making money on Stage**

Use this document when you need one place to re-read **what matters financially**, **what order to do things in**, and **which detailed doc to open next**. It does not replace the soak operator guides — it situates them.

---

## 1. Two jobs (do not mix them)

| Job | Question | Where it lives | Success looks like |
|-----|----------|----------------|-------------------|
| **Engineering proof** | Does the machine classify, route, place orders, and count fees **correctly**? | Classification soak, execution soak, `DRY_RUN`, metrics | ≥ 99% regime agreement; real Bitso OID on Stage; realized `fee_rate` on fills |
| **Economic proof** | After **real** fees and spread, does this strategy in this regime have **positive expectancy**? | Backtest, shadow weeks, production capital | Net P&L per regime **after realized fees**; not one lucky round-trip |

**Stage soak docs are almost entirely engineering proof.** A passing soak does **not** mean you should size up or go to production for profit.

---

## 2. The lesson from the May 2026 Stage loss

Post-mortem: `organic_lp_pnl_1779300471` — about **−17 MXN on ~1340 MXN** round-trip.

| Factor | Order of magnitude | Implication |
|--------|-------------------|-------------|
| Round-trip fees | ~1.3% (taker buy + maker sell) | Dominates small scalps |
| Spread | ~0.05% | Small vs fees |
| Strategy | `limit_profit` (scalping) | Needs tiny fees + tight spread + high frequency |

**Structural conclusion:** In that regime, **`limit_profit` was a loser regardless of tuning** — edge did not clear the fee floor.

The repo’s financial response (shipped or in soak):

| Work | Financial meaning |
|------|-------------------|
| **POINT-9** — realized fees | P&L and exit thresholds use **what Bitso charged**, not config assumptions |
| **POINT-10** — regime router | **Do not run the wrong strategy for the market**; pause in `high_vol` |
| **POINT-11** — fee honesty on MR/momentum | Same truth for non-LP strategies |

**Optimal financial stance:** Prove plumbing first; only deploy strategies where **expected move > round-trip fee % + spread + slippage buffer**.

---

## 3. Fee floor (Layer A — before sizing)

For any strategy, answer:

```text
minimum_favorable_move ≈ round_trip_fee_% + spread_% + slippage_buffer
```

| Strategy type | Fee sensitivity | Default router bias |
|---------------|-----------------|---------------------|
| `limit_profit` | **Very high** — scalping | **Not** default in high-fee Stage; route only when fee floor math works |
| `mean_reversion` | Medium — fewer, wider moves | `low_vol_range`, `neutral` |
| `momentum` | Medium — fewer, larger moves | `trending_up`, `trending_down` |
| **Pause** | N/A | `high_vol` → `none` — sit out when vol/fees swamp edge |

Until **one** complete Stage round-trip shows realized fees on fills and strategy logs match, **treat all P&L as unreliable**.

---

## 4. Regime routing = capital allocation (Layer B)

The router does **not** create alpha. It decides **which business line may run**.

Default policy (book `btc_mxn`; overridable via `ROUTE_*`):

| Regime | Default route | Financial intent |
|--------|---------------|------------------|
| `low_vol_range` | `mean_reversion_{book}` | Range-bound; reversion fits calmer bands |
| `trending_up` / `trending_down` | `momentum_{book}` | Need larger moves to clear buy-side fees |
| `high_vol` | `none` | Fee + slippage often kill edge — **pause** |
| `neutral` | `mean_reversion_{book}` | Conservative when signal unclear |

Wrong regime + correct code → **predictable loss, faster**. That is why **classification soak** (bash ↔ Go agreement) comes before live routing.

---

## 5. Risk before return (Layer C)

Economics are capped by controls already in the stack:

| Control | Layer | Financial role |
|---------|-------|----------------|
| `position_size`, stop-loss, max hold, daily loss | Strategy params | Cap loss per trade/day |
| Session risk | order-management + trading-engine | Reject before place |
| `has_position` | strategy-router | No switch while exposed |
| `COOLDOWN_SECS` | strategy-router | Limit churn / fee bleed |
| `DRY_RUN` (three switches) | router, engine, strategy param | See §7 |

**On Stage:** smallest size that still completes buy → sell → fill; **one** strategy, **one** book until fee honesty passes.

---

## 6. Lifecycle — where profit is allowed to matter (Layer D)

From [`../FINANCIAL-STRATEGY-IMPLEMENTATION-GUIDE.md`](../FINANCIAL-STRATEGY-IMPLEMENTATION-GUIDE.md):

```text
Research → Backtest → Paper (Stage) → Shadow (Stage, weeks) → Production (real $)
```

| Stage | Real money? | Financial question |
|-------|-------------|-------------------|
| Backtest | No | Does the hypothesis hold on history? |
| Paper / soak (engineering) | No (Stage credits) | Are labels, orders, fills, fees **honest**? |
| Shadow (extended Stage) | No | Per-regime expectancy **after realized fees**? |
| Production | Yes | Capital budget + kill switches |

**Bitso Stage is for mechanics and fee math, not for optimizing returns.**

---

## 7. `DRY_RUN` — three switches (common confusion)

| Layer | Component | `true` | `false` |
|-------|-----------|--------|---------|
| Routing | `strategy-router` | Classify only; no start/stop | May switch strategies |
| Orders | `trading-engine` | Log only; no `PlaceOrder` | Bitso Stage REST |
| Signals | strategy `dry_run` param | Engine should skip | Normal path |

**Router live ≠ trading.** Live Stage orders need: running strategy + engine **not** dry + stage API keys + `BITSO_API_BASE_URL=https://stage.bitso.com/api`.

Detail: [`STAGE-SOAK-MARKET-DATA-ATR-OBSERVATIONS-2026-06-03.md`](STAGE-SOAK-MARKET-DATA-ATR-OBSERVATIONS-2026-06-03.md) §4.

---

## 8. Soak map — what each gate proves

| Gate | Doc | Proves (engineering) | Does **not** prove |
|------|-----|----------------------|---------------------|
| **Classification soak** | [`STAGE-SOAK-OPERATOR-GUIDE-2026-06-02.md`](STAGE-SOAK-OPERATOR-GUIDE-2026-06-02.md) | ≥ 99% bash ↔ Go `regime`; router loop healthy | P&L, fees on trades, routing safety under load |
| **Verification status** | [`STAGE-SOAK-VERIFICATION-2026-06-03.md`](STAGE-SOAK-VERIFICATION-2026-06-03.md) | Where item 1 stood on 2026-06-03 | — |
| **Execution soak** | [`STAGE-EXECUTION-SOAK-OPERATOR-GUIDE-2026-06-04.md`](STAGE-EXECUTION-SOAK-OPERATOR-GUIDE-2026-06-04.md) | Start/stop, orders, OM sync, fee honesty | Profitability, prod readiness |
| **ATR / market-data** | [`STAGE-SOAK-MARKET-DATA-ATR-OBSERVATIONS-2026-06-03.md`](STAGE-SOAK-MARKET-DATA-ATR-OBSERVATIONS-2026-06-03.md) | Indicators + `high_vol` / `low_vol` inputs | — |

**Order:** Classification PASS → execution phases (router dry → one strategy orders → full stack small) → extended shadow for economics.

---

## 9. Decision tree (when lost, ask in order)

```text
1. Bash ↔ Go regime agreement ≥ 99%?
   NO  → Classification soak only (run-stage-soak-2026-06-02.sh sample/report)
   YES → 2

2. One tiny Stage round-trip with trading_engine_dry_run = 0?
   NO  → Execution soak Phase 3 (one strategy, manual start)
   YES → 3

3. Net P&L matches logs using REALIZED fees on fills?
   NO  → Fix POINT-9/11 path; do not scale or enable router orders
   YES → 4

4. Router live: MR/momentum in matching regimes, pause in high_vol?
   Monitor fee-adjusted P&L per regime — not headline P&L
   YES → 5

5. Production real money?
   Only after weeks shadow + capital/risk budget (POST-POINT-10 defers meta_router/tuner)
```

---

## 10. What to do right now (priority stack)

As of the 2026-06-03 verification report, **classification short soak was not PASS** (Go audit lost on restart; need live samples).

| Priority | Action | Financial why |
|----------|--------|---------------|
| **1** | `./scripts/run-stage-soak-2026-06-02.sh sample` + `report` until `live_agreement_rate ≥ 0.99` | Wrong label → wrong strategy |
| **2** | Optional: `bitso-ws-url: wss://ws.stage.bitso.com` in dev overlay | Indicators align with Stage fills |
| **3** | One manual `mean_reversion` round-trip, minimal `position_size` | Realized fees or fiction |
| **4** | Execution guide Phase 2 → 4, small size | Prove allocation + plumbing together |
| **5** | Defer `limit_profit` as default until fee floor proven on Stage | Same failure mode as May loss |

**Do not** set `strategy-router` `DRY_RUN=false` for profit-seeking before priority **1** passes.

---

## 11. One-sentence optimal approach

**Minimize structural loss (fees + wrong regime + wrong strategy), prove realized-fee P&L on tiny Stage size, use the router to pause more than to trade, and only after weeks of shadow treat Stage P&L as evidence for production capital.**

---

## 12. Deep-dive index

| Topic | Document |
|-------|----------|
| Folder TL;DR + May loss | [`README.md`](README.md) |
| Realized fees | [`POINT-9-REALIZED-FEES.md`](POINT-9-REALIZED-FEES.md) |
| Regime routing design | [`POINT-10-STRATEGY-REGIME-ROUTER.md`](POINT-10-STRATEGY-REGIME-ROUTER.md) |
| Go router service | [`STRATEGY-REGIME-ROUTER-SERVICE-2026-05-22.md`](STRATEGY-REGIME-ROUTER-SERVICE-2026-05-22.md) |
| MR/momentum fee honesty | [`POINT-11-FEE-HONESTY-MEAN-REVERSION-MOMENTUM-2026-05-22.md`](POINT-11-FEE-HONESTY-MEAN-REVERSION-MOMENTUM-2026-05-22.md) |
| POST-POINT-10 status | [`POST-POINT-10-IMPLEMENTATION-STATUS-2026-05-25.md`](POST-POINT-10-IMPLEMENTATION-STATUS-2026-05-25.md) |
| Classification soak steps | [`STAGE-SOAK-OPERATOR-GUIDE-2026-06-02.md`](STAGE-SOAK-OPERATOR-GUIDE-2026-06-02.md) |
| Execution soak phases | [`STAGE-EXECUTION-SOAK-OPERATOR-GUIDE-2026-06-04.md`](STAGE-EXECUTION-SOAK-OPERATOR-GUIDE-2026-06-04.md) |
| Order flow on Stage | [`../ORDER-FLOW-AND-BITSO-TESTING.md`](../ORDER-FLOW-AND-BITSO-TESTING.md) |
| Strategy lifecycle | [`../FINANCIAL-STRATEGY-IMPLEMENTATION-GUIDE.md`](../FINANCIAL-STRATEGY-IMPLEMENTATION-GUIDE.md) |
| Organic startup | [`../ORGANIC-TRADING-STARTUP.md`](../ORGANIC-TRADING-STARTUP.md) |
| Helper scripts | [`../../scripts/run-stage-soak-2026-06-02.sh`](../../scripts/run-stage-soak-2026-06-02.sh), [`../../scripts/e2e-trading-flow-test.sh`](../../scripts/e2e-trading-flow-test.sh) |

---

## 13. Quick commands

```bash
# Classification gate
./scripts/run-stage-soak-2026-06-02.sh check
./scripts/run-stage-soak-2026-06-02.sh sample
./scripts/run-stage-soak-2026-06-02.sh report

# Engine dry-run check (in cluster)
kubectl -n bitso-trading-dev exec deploy/trading-engine -- \
  wget -qO- http://127.0.0.1:8080/metrics | grep '^trading_engine_dry_run'

# Pull Go audit before router pod restart
./scripts/analyze-stage-soak-agreement.sh pull-go
```
