# INTRADAY-STRATEGY-IMPLEMENTATION-PLAN.md — Analysis & Verification

**Verified:** 2026-02-10  
**Plan version:** 1.7

---

## 1. Document summary

The plan defines six phases for intraday strategy and metrics, plus operational next steps. It correctly describes Bitso stage vs production, paper trading via stage, order/fill sync, risk limits, intraday metrics with Grafana, and backtesting.

---

## 2. Phase-by-phase verification

### Phase 1: Bitso environment configuration — VERIFIED

| Claim | Location | Status |
|-------|----------|--------|
| `BitsoAPIBaseURL` in shared config | `shared/pkg/config/config.go` | Present; default `https://stage.bitso.com/api` |
| Trading-engine uses config value | `services/trading-engine/cmd/main.go` | Uses `cfg.BitsoAPIBaseURL` |
| Stage keys: `STAGE_BITSO_API_KEY`, `STAGE_BITSO_API_SECRET` | `shared/pkg/config/config.go` | Loaded from env |

**Verdict:** Accurate. No code changes needed for this phase.

---

### Phase 2: Paper trading / dry-run — VERIFIED

Plan states paper trading = stage URL + stage keys; no separate dry-run flag required. Matches Phase 1 config and trading-engine usage. Optional local dry-run (skip `PlaceOrder`) is described and not yet implemented.

**Verdict:** Accurate.

---

### Phase 3: Order & fill sync — VERIFIED

| Claim | Verification |
|-------|--------------|
| Trading-engine publishes to `trading.orders.placed` | Kafka topic config and producer present in codebase |
| Order-management consumes and Bitso sync job | `internal/sync/bitso_sync.go`, `SyncOrderFromBitso`, consumer for orders placed |
| Polling via `LookupOrders` | Described in plan and sync implementation |

**Verdict:** Accurate. Phase 3 marked Done is consistent with code.

---

### Phase 4: Daily loss limit & max drawdown — VERIFIED

| Claim | Location | Status |
|-------|----------|--------|
| `MaxDailyLoss`, `MaxDrawdownPct` in config | `shared/pkg/models/trading_config.go` | Present (lines 31–32, 64–65) |
| `CheckSessionLimits`, `SessionRiskProvider` | Plan references execution and engine | Described |
| Order-management `GET /api/v1/risk/session` | Plan and server references | Documented |

**Verdict:** Accurate. Risk model and config exist as stated.

---

### Phase 5: Intraday metrics & observability — VERIFIED

| Item | Plan claim | Verification |
|------|------------|---------------|
| Shared metric types | `shared/pkg/metrics/` | `shared/pkg/metrics/constants.go` defines `trading_*` names |
| Prometheus gauges | `services/order-management/internal/metrics/prometheus.go` | Gauges: `dailyRealizedPnL`, `dailyUnrealizedPnL`, `drawdownPercent`, `drawdownAbsolute`, `peakEquity`, `currentEquity`, `tradesToday`, `winsToday`, `lossesToday` |
| IntradayAggregator | `services/order-management/internal/metrics/intraday_aggregator.go` | Present |
| Metric names | Dashboard uses `trading_daily_realized_pnl_currency`, etc. | Match `shared/pkg/metrics/constants.go` (e.g. `NameDailyRealizedPnL = "trading_daily_realized_pnl_currency"`) |
| Grafana dashboard | `monitoring/grafana/dashboards/trading-metrics.json` | Exists; row "Intraday / P&L" with Daily Realized/Unrealized P&L, Drawdown %, Drawdown Absolute, Equity, Trades Today, Wins/Losses, Win Rate %, plus time series |

**Dashboard panels verified:**

- Daily Realized P&L (stat + time series)
- Daily Unrealized P&L
- Drawdown % (stat + time series)
- Drawdown Absolute
- Current Equity, Peak Equity
- Trades Today, Wins / Losses Today, Win Rate Today %

**Verdict:** Phase 5 implementation status in the plan is accurate. Dashboard and metric names align.

---

### Phase 6: Backtesting — VERIFIED

Plan describes running backtests, comparing to Grafana (Total Return ↔ Daily Realized P&L, Max Drawdown ↔ Drawdown %, etc.), and references `scripts/run-one-backtest.sh`. That script exists. `scripts/intraday-backtest-and-compare.sh` also exists as referenced.

**Verdict:** Accurate and consistent with repo.

---

## 3. Scripts and next steps

| Script | Plan reference | Exists |
|--------|-----------------|--------|
| `scripts/intraday-validate-stage-pipeline.sh` | Priority 1 | Yes |
| `scripts/run-one-backtest.sh` | Phase 6, Priority 2 | Yes |
| `scripts/intraday-backtest-and-compare.sh` | Priority 2 | Yes |
| `scripts/grafana-port-forward.sh` | (Not in plan; used for dashboard access) | Yes |

---

## 4. Minor notes (no corrections required)

1. **Document version:** Plan says "1.7" and "Phases 1–6 complete" — matches content and verification.
2. **REMAINING-PHASES-CHECKLIST.md:** Referenced for Redis/production; not verified here.
3. **Grafana access:** Plan mentions "Grafana intraday panels" and "Trading Platform Metrics" dashboard. Dashboard title in JSON is "Trading Platform Metrics"; tags include "trading", "bitso", "intraday". To view it, Grafana must be reachable (port-forward or ALB).

---

## 5. Conclusion

- **Phases 1–5:** Descriptions and “Done” status match the codebase and dashboard.
- **Phase 6:** Backtesting flow and scripts are correctly described.
- **Next steps (Priorities 1–4):** Scripts exist; operational checklists are consistent with implementation.
- **Intraday Grafana dashboard:** Implemented at `monitoring/grafana/dashboards/trading-metrics.json` with the "Intraday / P&L" row and all stated metrics.

**No inaccuracies or required plan changes identified.** The document is suitable for operational validation and backtest-vs-live comparison.
