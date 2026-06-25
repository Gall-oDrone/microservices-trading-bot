# Recommended Next Steps

Date: **2026-06-25**  
Repository: `microservices-trading-bot`  
Branch: `feat/k8s-deployment-manifests`  
Audience: Operators and developers deciding what to do after the POST-POINT-10 engineering work and the Stage execution soak

This document consolidates the recommended next steps from a review of the strategy / fee-accuracy program: [`FINANCIAL-STRATEGY-IMPLEMENTATION-GUIDE.md`](../FINANCIAL-STRATEGY-IMPLEMENTATION-GUIDE.md), [`LIMIT-PROFIT-STRATEGY.md`](../LIMIT-PROFIT-STRATEGY.md), [`LIMIT-PROFIT-ROBUSTNESS.md`](../LIMIT-PROFIT-ROBUSTNESS.md), [`MOMENTUM-STRATEGY.md`](../MOMENTUM-STRATEGY.md), and the `strategy-fee-accuracy/` set (POST-POINT-10 status/roadmap, classification + execution soak reports, market-data hardening, and the financial approach).

It supersedes the "operator next steps" sections of the dated soak reports by rolling them into one prioritized list, and adds engineering gaps that were verified against the codebase on 2026-06-25.

> **Update 2026-06-25 — Buckets B, C (tooling) and D are now implemented.** The code/tooling items
> below (B1 fee-drift metric+alert, B2 backtest API, B3 promotion scripts, B4 momentum counters, the
> C/D operator scripts) shipped in this branch. See
> [`B-D-IMPLEMENTATION-2026-06-25.md`](B-D-IMPLEMENTATION-2026-06-25.md). What remains is **operator-led**:
> the Bucket A Phase-5 verification and the Bucket C multi-week Stage shadow run that produces the
> economic proof.

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
| Bucket B engineering (B1–B4) | ✅ **Complete** (2026-06-25) | [`B-D-IMPLEMENTATION-2026-06-25.md`](B-D-IMPLEMENTATION-2026-06-25.md) |
| Bucket C/D tooling (scripts) | ✅ **Complete** (2026-06-25) | [`B-D-IMPLEMENTATION-2026-06-25.md`](B-D-IMPLEMENTATION-2026-06-25.md) §6 |
| Bucket C shadow run (economic proof) | ⏳ **Not started** | Operator-led; use `strategy-daily-report.sh` |

**Key observation:** Phase 5 started 2026-06-11T18:06:59Z with a 7-day gate **ETA of 2026-06-18T18:06:59Z**. That window has now elapsed, but **no Phase 5 PASS verification report exists in this folder.** Closing that gap is the next operator step (A1).

**Framing (do not conflate):** the program has strong **engineering proof** (classify → route → order → count fees correctly) but **no economic proof** (positive per-regime expectancy after real fees). A passing soak is not a signal to size up or go to production. See [`STAGE-FINANCIAL-APPROACH-2026-06-04.md`](STAGE-FINANCIAL-APPROACH-2026-06-04.md) §1.

---

## 2. Bucket A — Close the engineering gate (now, operator-led)

These are already scripted; they need to run to completion and be recorded.

| # | Action | Detail |
|---|--------|--------|
| A1 | **Verify Phase 5 and write the PASS report** | Run `./scripts/run-stage-execution-soak-2026-06-04.sh phase5-status` after the elapsed gate. **A fill-in template exists:** [`STAGE-EXECUTION-SOAK-PHASE5-VERIFICATION-2026-06-25.md`](STAGE-EXECUTION-SOAK-PHASE5-VERIFICATION-2026-06-25.md) — populate the `<…>` placeholders (7-day elapsed, classification rate + regime-stratified coverage, router/order metrics, switch count, failed orders, realized `fee_rate` vs config / B1 drift, WS reconnect-rate drop, stale windows, incidents), then remove the TEMPLATE banner and set the verdict. |
| A2 | **Confirm the WebSocket `inbox`-close fix is deployed** | The fix is in code (`shared/pkg/bitso/websocket.go` closes `inbox` on read-loop exit), but the Phase 4 PASS doc still lists rollout + verification as a step. After deploy, confirm `market_data_trade_silence_reconnects_total` **rate drops** — proof the watchdog (Layer 2), not just REST fallback (Layer 3), is functional. |
| A3 | **Keep classification sampling alive** | `./scripts/stage-soak-sample-loop.sh status` must show an active loop (it silently stalled ~48 h during Phase 4). Sample every 30 min through the shadow period. |
| A4 | **Test the rollback path** | `./scripts/run-stage-execution-soak-2026-06-04.sh rollback`; confirm `DRY_RUN=true` on router + engine, then re-enable live for shadow. Gate 3 requires a tested rollback. |
| A5 | **Declare POST-POINT-10 engineering milestone complete** | Only after A1–A4. |

---

## 3. Bucket B — Engineering components ✅ Complete (2026-06-25)

These gaps were verified **missing** on 2026-06-25 and **implemented** the same day. Full detail:
[`B-D-IMPLEMENTATION-2026-06-25.md`](B-D-IMPLEMENTATION-2026-06-25.md).

| # | Item | Status | Deliverable |
|---|------|--------|-------------|
| B1 | **Fee assumption vs realized drift + alert** | ✅ Done | Metrics `strategy_executor_realized_fee_rate`, `..._assumed_fee_rate`, `..._fee_drift_ratio` on every fill; alerts `FeeRealizedAboveAssumed` / `FeeRealizedFarFromAssumed` in `monitoring/prometheus/rules/trading-alerts.yml` |
| B2 | **Backtest HTTP API (`/api/v1/backtests`)** | ✅ Done | `internal/backtest/engine.go` + `replay_provider.go` (look-ahead-free indicators); `internal/server/backtest_handler.go` (create/list/results/report) |
| B3 | **Phase 4 promotion scripts** | ✅ Done | `backtest-strategy.sh`, `validate-backtest.sh`, `promote-strategy-to-stage.sh`, `compare-stage-vs-backtest.sh`, `strategy-daily-report.sh`, `kill-switch-status.sh` (plus existing `validate-limit-profit-backtest.sh`) |
| B4 | **`momentum_*` dedicated Prometheus counters** | ✅ Done | `momentum_entry/exit_signals_total`, `momentum_position_hold_duration_seconds`, `momentum_daily_realized_pnl_quote`, `momentum_circuit_breaker_active` |

**Use now:** `./scripts/backtest-strategy.sh <type> <book>` → `./scripts/validate-backtest.sh <id>` → `./scripts/promote-strategy-to-stage.sh <id>`.

---

## 4. Bucket C — Economic validation (the real remaining work)

Engineering and tooling are done; **profitability is unproven.** After Phase 5 PASS (A1):

| # | Action | Status | Constraint / tooling |
|---|--------|--------|----------------------|
| C1 | **Enter Shadow Trading** | ⏳ Pending | 2–4+ weeks, Bitso **Stage only**, `position_size=0.001`, **one book** (`btc_mxn`). Track **net P&L per regime after realized fees**. Daily: `./scripts/strategy-daily-report.sh --book btc_mxn`; breakers: `./scripts/kill-switch-status.sh`. |
| C2 | **Fee-floor strategy decisions during shadow** | ⏳ Pending | `mean_reversion` default for `low_vol_range` / `neutral`; `momentum` only in `trending_*` (verify moves clear buy-side fees); keep **`limit_profit` deferred as default** until the fee floor is proven on Stage; keep `high_vol` routed to `none`. Watch B1 fee-drift metrics during shadow. |
| C3 | **Validate hardening on the production WebSocket** | ⏳ Pending | Proven only against `stage.bitso.com`. Run `./scripts/validate-prod-ws-hardening.sh --url <prod market-data>` before any prod overlay. |

---

## 5. Bucket D — Soak quality follow-ups

| # | Action | Status | Rationale / tooling |
|---|--------|--------|---------------------|
| D1 | **Regime-stratified classification agreement** | ✅ Tooling done; ⏳ coverage pending | The 06-07 PASS (100%) was dominated by `low_vol_range` (129/140); `high_vol` had **0** samples, `trending_*` only 8. Run `./scripts/analyze-regime-stratified-agreement.sh` on accumulated `agreement-samples.jsonl` (from `analyze-stage-soak-agreement.sh sample`) until `high_vol` / `trending_*` clear the minimum sample floor. |
| D2 | **Confirm reconnect-rate drop post-fix** | ✅ Tooling done; ⏳ verification pending | Tie to A2. Run `./scripts/validate-prod-ws-hardening.sh` over a 2-minute window and confirm websocket + silence reconnect deltas stay within budget post-`inbox`-fix. |

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
✅ DONE (2026-06-25)
   B1  Fee-drift metric + Prometheus alert
   B2  /api/v1/backtests HTTP API (look-ahead-free engine)
   B3  Promotion scripts (backtest / validate / promote / compare / daily-report / kill-switch)
   B4  momentum_* Prometheus counters
   D   Tooling: analyze-regime-stratified-agreement.sh, validate-prod-ws-hardening.sh

NEXT (operator-led)
1. (now)     A1  Verify Phase 5 → write PHASE5 PASS verification report
2. (now)     A2  Confirm WS inbox-close fix deployed; run validate-prod-ws-hardening.sh (D2)
3. (now)     A3/A4  Keep sampling alive; test rollback; run stratified agreement as samples accrue (D1)
4. (now)     A5  Declare POST-POINT-10 engineering milestone complete (after A1–A4)
5. (then)    C1/C2  Shadow: 2–4 wks, tiny size, one book, per-regime net P&L after fees
6. (then)    C3  Validate hardening on prod WebSocket before prod overlay
7. (later)   Merge feat/k8s-deployment-manifests → main; defer tuner / meta_router
8. (gate 3)  Only after weeks of positive shadow economics → minimal production capital
```

---

## 8. Decision gate

```text
Phase 5 verified PASS (≥ 7d, no critical incidents)?
  NO  → Extend Phase 5; fix incidents; do not advance
  YES → Write PASS report (A1); declare engineering milestone done (A5)
      → B1 fee-drift alert is live — monitor during shadow
      → Enter Shadow (C1/C2): per-regime net P&L after realized fees
      → Validate prod WS hardening (C3); staging overlay

Shadow: per-regime net P&L positive after realized fees?
  NO  → Adjust params, pause strategies, or reject — not a plumbing problem
  YES → Gate 3 checklist → production with minimal capital
```

---

## 9. One-sentence summary

**Bucket B engineering and C/D tooling are complete (2026-06-25); one Phase 5 verification report (A1) closes the engineering milestone; the genuinely missing validation is weeks of shadow trading to prove per-regime expectancy after real fees — until that exists the system is provably correct but not provably profitable.**

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
| Bucket B–D implementation | [`B-D-IMPLEMENTATION-2026-06-25.md`](B-D-IMPLEMENTATION-2026-06-25.md) |
| Folder index | [`README.md`](README.md) |
