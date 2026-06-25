# Recommended Next Steps

Date: **2026-06-25**  
Repository: `microservices-trading-bot`  
Branch: `feat/k8s-deployment-manifests`  
Audience: Operators and developers deciding what to do after the POST-POINT-10 engineering work and the Stage execution soak

This document consolidates the recommended next steps from a review of the strategy / fee-accuracy program: [`FINANCIAL-STRATEGY-IMPLEMENTATION-GUIDE.md`](../FINANCIAL-STRATEGY-IMPLEMENTATION-GUIDE.md), [`LIMIT-PROFIT-STRATEGY.md`](../LIMIT-PROFIT-STRATEGY.md), [`LIMIT-PROFIT-ROBUSTNESS.md`](../LIMIT-PROFIT-ROBUSTNESS.md), [`MOMENTUM-STRATEGY.md`](../MOMENTUM-STRATEGY.md), and the `strategy-fee-accuracy/` set (POST-POINT-10 status/roadmap, classification + execution soak reports, market-data hardening, and the financial approach).

It supersedes the "operator next steps" sections of the dated soak reports by rolling them into one prioritized list, and adds engineering gaps that were verified against the codebase on 2026-06-25.

---

## 1. Where things actually stand (2026-06-25)

The most recent status report ([`POST-POINT-10-IMPLEMENTATION-STATUS-2026-05-25.md`](POST-POINT-10-IMPLEMENTATION-STATUS-2026-05-25.md)) is **stale** — six newer docs supersede it. Reading the folder chronologically, the real state is:

| Gate | Status | Source |
|------|--------|--------|
| POST-POINT-10 roadmap items 2–10 | ✅ Complete | [`POST-POINT-10-ROADMAP-2026-05-22.md`](POST-POINT-10-ROADMAP-2026-05-22.md) |
| Classification soak (bash ↔ Go ≥ 99%) | ✅ **PASS** (140/140, ~69.8 h) | [`STAGE-SOAK-VERIFICATION-2026-06-07.md`](STAGE-SOAK-VERIFICATION-2026-06-07.md) |
| Execution soak Phase 2 (router lifecycle) | ✅ PASS (26.2 h) | [`STAGE-EXECUTION-SOAK-VERIFICATION-2026-06-09.md`](STAGE-EXECUTION-SOAK-VERIFICATION-2026-06-09.md) |
| Execution soak Phase 3 (first round-trip) | ✅ PASS | [`STAGE-EXECUTION-SOAK-PHASE3-RECONCILIATION-2026-06-09.md`](STAGE-EXECUTION-SOAK-PHASE3-RECONCILIATION-2026-06-09.md) |
| Execution soak Phase 4 (router + engine live) | ✅ **PASS** (48.4 h, 100% agreement, 0 eval errors) | [`STAGE-EXECUTION-SOAK-PHASE4-VERIFICATION-2026-06-11.md`](STAGE-EXECUTION-SOAK-PHASE4-VERIFICATION-2026-06-11.md) |
| Market-data freshness hardening | ✅ Shipped + root-cause fix | [`STAGE-MARKET-DATA-FRESHNESS-HARDENING-2026-06-10.md`](STAGE-MARKET-DATA-FRESHNESS-HARDENING-2026-06-10.md) |
| Execution soak Phase 5 (7-day extended) | ⏳ **Gate elapsed, verification pending** | [`STAGE-EXECUTION-SOAK-POST-PHASE5-FINANCIAL-NEXT-STEPS-2026-06-12.md`](STAGE-EXECUTION-SOAK-POST-PHASE5-FINANCIAL-NEXT-STEPS-2026-06-12.md) |

**Key observation:** Phase 5 started 2026-06-11T18:06:59Z with a 7-day gate **ETA of 2026-06-18T18:06:59Z**. That window has now elapsed, but **no Phase 5 PASS verification report exists in this folder.** Closing that gap is step 1.

**Framing (do not conflate):** the program has strong **engineering proof** (classify → route → order → count fees correctly) but **no economic proof** (positive per-regime expectancy after real fees). A passing soak is not a signal to size up or go to production. See [`STAGE-FINANCIAL-APPROACH-2026-06-04.md`](STAGE-FINANCIAL-APPROACH-2026-06-04.md) §1.

---

## 2. Bucket A — Close the engineering gate (now, operator-led)

These are already scripted; they need to run to completion and be recorded.

| # | Action | Detail |
|---|--------|--------|
| A1 | **Verify Phase 5 and write the PASS report** | Run `./scripts/run-stage-execution-soak-2026-06-04.sh phase5-status` after the elapsed gate. Write `STAGE-EXECUTION-SOAK-PHASE5-VERIFICATION-2026-06-25.md` mirroring the Phase 4 report: 7-day elapsed, final classification rate, router/order metrics, regime distribution, switch count, failed orders, realized `fee_rate` vs config on fills, stale windows, incidents/rollbacks. |
| A2 | **Confirm the WebSocket `inbox`-close fix is deployed** | The fix is in code (`shared/pkg/bitso/websocket.go` closes `inbox` on read-loop exit), but the Phase 4 PASS doc still lists rollout + verification as a step. After deploy, confirm `market_data_trade_silence_reconnects_total` **rate drops** — proof the watchdog (Layer 2), not just REST fallback (Layer 3), is functional. |
| A3 | **Keep classification sampling alive** | `./scripts/stage-soak-sample-loop.sh status` must show an active loop (it silently stalled ~48 h during Phase 4). Sample every 30 min through the shadow period. |
| A4 | **Test the rollback path** | `./scripts/run-stage-execution-soak-2026-06-04.sh rollback`; confirm `DRY_RUN=true` on router + engine, then re-enable live for shadow. Gate 3 requires a tested rollback. |
| A5 | **Declare POST-POINT-10 engineering milestone complete** | Only after A1–A4. |

---

## 3. Bucket B — Missing engineering components (verified against code 2026-06-25)

These are referenced in the guides but confirmed **not implemented** in the tree.

| # | Gap | Evidence | Priority | Why it matters |
|---|-----|----------|----------|----------------|
| B1 | **Fee assumption vs realized histogram + alert** | No matching code/PrometheusRule; deferred in POINT-9 §7 and the POST-POINT-10 status "Deferred" table | **High** | Direct defense against the May 2026 loss: alert when configured liquidity / fee drifts from what Bitso actually charges. Operationalizes the fee-honesty thesis instead of trusting it passively. **Do before production capital.** |
| B2 | **Backtest HTTP API (`/api/v1/backtests`)** | Backtest engine exists (`services/strategy-executor/internal/backtest/runner.go` computes Sharpe/Sortino/profit factor/max DD) but there is **no HTTP endpoint** wiring it; `FINANCIAL-…GUIDE.md` Phase 4 is still "🔄 Pending" | Medium-High | Without it, Gate 1 (backtest approval) cannot be run repeatably and the promotion workflow has no data source. |
| B3 | **Phase 4 promotion scripts** | Only `scripts/validate-limit-profit-backtest.sh` exists; `backtest-strategy.sh`, `promote-strategy-to-stage.sh`, `validate-backtest.sh`, `compare-stage-vs-backtest.sh`, `strategy-daily-report.sh`, `kill-switch-status.sh` are **absent** (referenced in guide §Scripts Reference) | Medium | These enforce the gate between backtest → stage. Depends on B2 for the API-backed ones. |
| B4 | **`momentum_*` dedicated Prometheus counters** | No matches in code; momentum relies on generic `strategy_executor_signals_generated_total`; deferred in `MOMENTUM-STRATEGY.md` §7 | Low | `limit_profit` has rich per-exit-reason / circuit-breaker metrics; momentum lacks equivalent visibility. |

Recommended order within this bucket: **B1 → B2 → B3 → B4.** B1 is the highest-value economic safeguard; B2/B3 unlock repeatable validation; B4 is polish.

---

## 4. Bucket C — Economic validation (the real remaining work)

Engineering is essentially done; **profitability is unproven.** After Phase 5 PASS:

| # | Action | Constraint |
|---|--------|-----------|
| C1 | **Enter Shadow Trading** | 2–4+ weeks, Bitso **Stage only**, `position_size=0.001`, **one book** (`btc_mxn`). Track **net P&L per regime after realized fees** — not headline P&L. Use the Daily Stage Observation Report template in [`../FINANCIAL-STRATEGY-IMPLEMENTATION-GUIDE.md`](../FINANCIAL-STRATEGY-IMPLEMENTATION-GUIDE.md). |
| C2 | **Fee-floor strategy decisions during shadow** | `mean_reversion` default for `low_vol_range` / `neutral`; `momentum` only in `trending_*` (verify moves clear buy-side fees); keep **`limit_profit` deferred as default** until the fee floor (`min_favorable_move ≈ round_trip_fee% + spread% + slippage`) is proven on Stage; keep `high_vol` routed to `none`. |
| C3 | **Validate hardening on the production WebSocket** | All freshness thresholds (5m/3m/60s/10m) are proven only against `stage.bitso.com`. Re-verify on `wss://ws.bitso.com` before any prod overlay — this is the single biggest carry-forward risk. |

---

## 5. Bucket D — Soak quality follow-ups (from report review)

Two methodological gaps in the existing PASS reports are worth closing before treating them as definitive.

| # | Action | Rationale |
|---|--------|-----------|
| D1 | **Regime-stratified classification agreement** | The 06-07 PASS (100%) was dominated by `low_vol_range` (129/140 live samples); `high_vol` had **0** live samples, `trending_*` only 8. Re-run/report agreement **bucketed by regime** with a minimum sample floor for `high_vol` and `trending_*` — the financially consequential routes (`high_vol`→pause, `trending_*`→momentum) are currently under-tested. |
| D2 | **Confirm reconnect-rate drop post-fix** | Tie to A2. High `trade_silence_reconnects_total` (341) and `rest_fallback_trades_ingested_total` (2,166) were normalized as "working as designed," but they document chronic Stage WS flakiness and heavy reliance on REST fallback. Post-`inbox`-fix the watchdog reconnect rate should fall; if it does not, the system is still WS-blind and the fix did not take. |

---

## 6. Explicitly deferred (do not start yet)

| Item | Defer until |
|------|-------------|
| `agent-coordinator` threshold tuner | Approval workflow + ≥ 1 week clean Stage audit logs |
| In-process `meta_router` strategy type | ≥ 4 weeks clean Stage classification |
| `limit_profit` organic LP at scale | Dedicated LP soak + fee-floor proof |
| Per-strategy RSI/EMA periods (momentum) | Indicator service is global-only today (`INDICATOR_RSI_PERIOD` / `INDICATOR_EMA_PERIOD`) |
| Per-strategy ATR-scaled sizing (momentum) | Port from `limit_profit` only if needed |
| Parameter optimization / sizing up / production capital | Positive per-regime shadow P&L + Gate 3 checklist |

---

## 7. Prioritized sequence

```text
1. (now)     A1  Verify Phase 5 → write PHASE5 PASS verification report
2. (now)     A2  Roll out WS inbox-close fix; confirm reconnect rate drops (D2)
3. (now)     A3/A4  Keep sampling alive; test rollback
4. (this wk) B1  Fee-assumption-vs-realized histogram + Prometheus alert   ← highest-value code item
5. (this wk) B2  /api/v1/backtests HTTP API over existing runner.go
6. (this wk) B3  Promotion scripts (backtest-strategy / promote-strategy-to-stage / compare)
7. (this wk) D1  Regime-stratified agreement report
8. (then)    C1/C2  Shadow: 2–4 wks, tiny size, one book, per-regime net P&L after fees
9. (then)    C3  Validate hardening on prod WebSocket; deploy staging overlay (no real $)
10. (later)  Merge feat/k8s-deployment-manifests → main; defer tuner / meta_router
11. (gate 3) Only after weeks of positive shadow economics → minimal production capital
```

---

## 8. Decision gate

```text
Phase 5 verified PASS (≥ 7d, no critical incidents)?
  NO  → Extend Phase 5; fix incidents; do not advance
  YES → Write PASS report (A1); declare engineering milestone done
      → Ship B1 (fee-drift alert) before any capital discussion
      → Enter Shadow (C1/C2): per-regime net P&L after realized fees
      → Validate prod WS hardening (C3); staging overlay

Shadow: per-regime net P&L positive after realized fees?
  NO  → Adjust params, pause strategies, or reject — not a plumbing problem
  YES → Gate 3 checklist → production with minimal capital
```

---

## 9. One-sentence summary

**The engineering is essentially done and one verification report away from a closed milestone; the genuinely missing code is a fee-drift alert plus the backtest API and promotion scripts, and the genuinely missing validation is weeks of shadow trading to prove per-regime expectancy after real fees — until that exists the system is provably correct but not provably profitable.**

---

## 10. Related documents

| Topic | Document |
|-------|----------|
| Financial single reference | [`STAGE-FINANCIAL-APPROACH-2026-06-04.md`](STAGE-FINANCIAL-APPROACH-2026-06-04.md) |
| Post-Phase 5 financial path | [`STAGE-EXECUTION-SOAK-POST-PHASE5-FINANCIAL-NEXT-STEPS-2026-06-12.md`](STAGE-EXECUTION-SOAK-POST-PHASE5-FINANCIAL-NEXT-STEPS-2026-06-12.md) |
| Phase 4 PASS | [`STAGE-EXECUTION-SOAK-PHASE4-VERIFICATION-2026-06-11.md`](STAGE-EXECUTION-SOAK-PHASE4-VERIFICATION-2026-06-11.md) |
| Classification PASS | [`STAGE-SOAK-VERIFICATION-2026-06-07.md`](STAGE-SOAK-VERIFICATION-2026-06-07.md) |
| Market-data hardening | [`STAGE-MARKET-DATA-FRESHNESS-HARDENING-2026-06-10.md`](STAGE-MARKET-DATA-FRESHNESS-HARDENING-2026-06-10.md) |
| POST-POINT-10 status (stale) | [`POST-POINT-10-IMPLEMENTATION-STATUS-2026-05-25.md`](POST-POINT-10-IMPLEMENTATION-STATUS-2026-05-25.md) |
| Roadmap | [`POST-POINT-10-ROADMAP-2026-05-22.md`](POST-POINT-10-ROADMAP-2026-05-22.md) |
| Realized fees | [`POINT-9-REALIZED-FEES.md`](POINT-9-REALIZED-FEES.md) |
| MR/momentum fee honesty | [`POINT-11-FEE-HONESTY-MEAN-REVERSION-MOMENTUM-2026-05-22.md`](POINT-11-FEE-HONESTY-MEAN-REVERSION-MOMENTUM-2026-05-22.md) |
| Strategy lifecycle + Gate 3 | [`../FINANCIAL-STRATEGY-IMPLEMENTATION-GUIDE.md`](../FINANCIAL-STRATEGY-IMPLEMENTATION-GUIDE.md) |
| Folder index | [`README.md`](README.md) |
