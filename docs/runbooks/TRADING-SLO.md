# Trading operations SLOs: runbook

Spec: [`monitoring/prometheus/slo/trading-slos.yaml`](../../monitoring/prometheus/slo/trading-slos.yaml)
(the source). Generated from it by `scripts/gen-slo.py`: the rules
[`monitoring/prometheus/rules/trading-slo-rules.yml`](../../monitoring/prometheus/rules/trading-slo-rules.yml)
(k8s copy by `scripts/sync-prometheus-rules.sh`) and the dashboard **Trading Operations SLOs**
(`monitoring/grafana/dashboards/domain/trading-slo.json`, uid `trading-slo`). Unit tests:
`monitoring/prometheus/tests/trading-slo-rules.test.yml`. Design decision: frontend plan §11 q4
([`FRONTEND-UI-PLAN-2026-10-03.md`](../frontend/FRONTEND-UI-PLAN-2026-10-03.md)), "follow production
standards, as hedge funds and private banks run trading operations"; built 2026-10-20.

**What an SLO adds to the alerts you already have.** The trading-risk alerts fire on a single bad
event or a stale gauge. An SLO says how much of that is tolerable over 30 days, measures how fast the
tolerance is being used up (the *burn rate*) and pages when it is being used faster than the budget
allows. The burn-rate alerts are a second opinion on the same metrics, not new ones.

**Severity:** `critical` = page now (the fast windows: budget gone in a day or less at this rate);
`warning` = review the same trading day (the slow windows and the exhausted budget). **Team** is the
SLO's: `trading` (order path, freshness) or `risk` (gate, kill switch).

> [!IMPORTANT]
> **This platform trades a few times a day.** An event SLI (orders, decisions) over an hour may
> hold two or three events, so one slow order would be a 50 % error ratio. Every event-SLI burn
> alert is therefore gated on a minimum number of events in its long window (`slo:min_events:*`:
> 10 in 6 h, 20 in 1 d, 30 in 3 d), and the 1 h / 5 m page is **not** defined for event SLIs. At
> today's volume the event SLOs will mostly show an SLI and a budget on the dashboard and stay
> silent; the per-event alerts in
> [`TRADING-RISK-ALERTS.md`](TRADING-RISK-ALERTS.md) remain the guard. The time SLIs (freshness,
> readiness) sample every minute and page normally.

---

## The SLOs (30-day rolling)

| SLO | Kind | Team | Target | Objective | SLI source |
|---|---|---|---|---|---|
| `tick_to_trade` | event | trading | 99 % | signal-to-order decisions acknowledged by the exchange within 5 s | `signal_to_order_latency_seconds` (trading-engine) |
| `signal_freshness` | event | trading | 99 % | decisions act on a signal younger than 10 s | `signal_age_at_decision_seconds` |
| `risk_gate_availability` | event | risk | 99.9 % | pre-trade checks evaluate (allowed or blocked); an `error` fails closed and counts | `pretrade_policy_checks_total{result}` |
| `order_acceptance` | event | trading | 95 % | orders not rejected for validation / exchange reasons (`risk_check_failed` and `manual` excluded: the gate working is not an error) | `orders_created_total`, `orders_rejected_total{reason}` (order-management) |
| `fill_quality` | event | trading | 90 % | executed orders fill within 25 bps of the decision price (arrival slippage, before fees) | `order_arrival_slippage_bps` |
| `reconciliation_freshness` | time | trading | 99.5 % | the exchange order sync **and** the fill poll each succeeded within the last 120 s | `bitso_sync_last_success_timestamp_seconds`, `user_trades_last_poll_timestamp_seconds` |
| `market_data_freshness` | time | trading | 99.5 % | the newest exchange trade is younger than 120 s | `market_data_last_trade_age_seconds` |
| `kill_switch_readiness` | time | risk | 99.9 % | every live trading-engine watches ≥ 1 halt file and evaluated them within 30 s (dry-run engines count as ready) | `trading_halt_last_check_timestamp_seconds`, `trading_halt_files_configured`, `trading_engine_dry_run` |

**Series you can query.** `slo:sli:30d{slo}`, `slo:budget_remaining:30d{slo}` (1 = untouched, 0 =
spent, negative = over), `slo:burn_rate:<W>{slo}` for W in 5m, 30m, 1h, 2h, 6h, 1d, 3d, 30d (1 =
exactly on budget), `slo:error_ratio:<W>{slo}`, `slo:events:<W>{slo}` (event SLIs), `slo:good:bool{slo}`
(time SLIs, the condition now), `slo:target{slo}`, `slo:min_events:<W>{slo}`.

**Burn-rate windows** (Google SRE workbook, 30-day SLO):

| Long / short | Factor | Means | Severity | Kinds |
|---|---|---|---|---|
| 1 h / 5 m | 14.4× | 2 % of the budget in an hour | critical | time |
| 6 h / 30 m | 6× | 5 % in six hours | critical (time) / warning (event) | time, event |
| 1 d / 2 h | 3× | 10 % in a day | warning | time, event |
| 3 d / 6 h | 1× | 10 % in three days | warning | time, event |

---

## Alerts

### TradingSLOBurnFast1H
*critical, time SLIs.* The condition has been failing for most of the last hour **and** the last
five minutes: `market_data_freshness`, `reconciliation_freshness` or `kill_switch_readiness`.
1. The `slo` label says which. Open **Trading Operations SLOs** → the SLO's row: *Condition held*
   shows when it dropped to 0.
2. Act on the underlying signal, not the SLO:
   - `market_data_freshness` → [`MarketDataTradeStreamStale`](TRADING-RISK-ALERTS.md): the WebSocket
     is silent; check market-data's log and reconnects. Strategies are deciding on stale prices.
   - `reconciliation_freshness` → [`OrderExchangeSyncStale` /
     `OrderFillDiscoveryStale`](TRADING-RISK-ALERTS.md#orderexchangesyncstale): the OMS cannot see
     Bitso; positions and session limits may be wrong. Consider halting.
   - `kill_switch_readiness` → [`TradingKillSwitchUnreachable` /
     `TradingHaltWatcherStale`](TRADING-RISK-ALERTS.md#tradingkillswitchunreachable): a live engine
     that HALT ALL cannot stop. Fix `TRADING_HALT_FILES` or stop the engine.
3. Resolves on its own once the condition holds again for a few minutes.

### TradingSLOBurnFast6H
*critical, time SLIs.* Same as above over six hours and thirty minutes: a condition that has
failed for a sizeable part of the session, or keeps flapping. Same steps; also look for the pattern
(a reconnect loop, a poller that dies at the same time every day).

### TradingSLOBurnSlow6HEvents
*warning, event SLIs, only with ≥ 10 events in 6 h.* One of `tick_to_trade`, `signal_freshness`,
`risk_gate_availability`, `order_acceptance`, `fill_quality` is burning at 6× over six hours.
1. The SLO's row shows the error ratio by window against the budget and the event count.
2. Read the per-event alert for the same signal
   ([`TradingTickToTradeSlow`](TRADING-RISK-ALERTS.md#tradingticktotradeslow),
   [`TradingSignalStale`](TRADING-RISK-ALERTS.md#tradingsignalstale),
   [`TradingRiskStateUnknown`](TRADING-RISK-ALERTS.md#tradingriskstateunknown),
   [`OrderRejectRateHigh`](TRADING-RISK-ALERTS.md#orderrejectratehigh),
   [`OrderSlippageHigh`](TRADING-RISK-ALERTS.md#orderslippagehigh)); it probably fired first and has
   the diagnosis.
3. `risk_gate_availability` burning means orders are being blocked fail-closed: the engine cannot
   read exposure or the halt state. Treat as urgent even though the SLO alert is a warning.

### TradingSLOBurnSlow1D / TradingSLOBurnSlow1DEvents
*warning.* 10 % of the month's budget gone in a day. Review the same trading day: which hours, which
book, which rule or reason. For event SLIs the guard is 20 events in the day.

### TradingSLOBurnSlow3D / TradingSLOBurnSlow3DEvents
*warning.* A slow leak: 10 % of the budget over three days at a steady rate. Usually a configuration
or a venue change (a slower exchange endpoint, a new reject reason, a wider spread) rather than an
incident. Guard: 30 events in 3 d.

### TradingSLOBudgetExhausted
*warning, after 1 h.* `slo:budget_remaining:30d <= 0`: the 30-day SLI is below its target. The SLO is
missed for this window whatever happens next.
1. Decide, and write down, whether the target was wrong (set at design time on 2026-10-20 without
   history) or the service was. Both are legitimate; silently living under a missed SLO is not.
2. Do not raise the target during an incident. Target changes are edits to the spec, reviewed, with
   the reason in the commit.
3. While exhausted, no new risk: no new books, no larger sizes, no strategy enabled, until the
   30-day SLI is back above target.

---

## Operations KPIs on the same dashboard

Below the SLO rows the dashboard shows the §11 q4 items that are limits or states rather than
objectives, each read from series the trading-risk alerts already use: session limits used (daily
loss, drawdown) and per-order limit utilisation p95; gross exposure and VaR against its limit;
orders per hour, reject rate by reason, fill ratio by book, pre-trade blocks by rule; the sync,
fill-poll, market-data and halt-watcher ages with the halt state; alerts firing by severity and the
age of the longest-firing one (`ALERTS_FOR_STATE`).

## What is not measured, and why

- **Kill-switch time-to-halt.** How long from HALT ALL to every engine blocking. The engines do not
  export when the halt was issued (the file's `at`), only when they last checked, so the
  interval cannot be computed from metrics. The kill-switch drill
  (`scripts/kill-switch-drill.sh`, §8.14) measures it by hand. Instrumentation gap: a gauge
  `trading_halt_issued_timestamp_seconds` read from the halt file would close it.
- **Alert acknowledgement time.** Acknowledgement is an on-call-tool concept (PagerDuty, Opsgenie);
  this repo routes to SNS e-mail and keeps no ack. The dashboard shows time **firing** instead.
- **Ledger freshness of the daily executors** (Bitso stage, eToro demo). They are cron jobs that
  export no metrics; `ui-alerts` and the off-machine watchdog (plan §8.9, §8.10, P6) cover them
  from the files. Out of Prometheus' sight by design.
- **Intraday P&L and drawdown against limits** are shown as the session-limit utilisation gauges
  the engine already exports (plan §6.4.4), not as a separate P&L series.

## Changing an SLO

Edit the spec, then `scripts/gen-slo.py && scripts/sync-prometheus-rules.sh`, run
`promtool test rules monitoring/prometheus/tests/trading-slo-rules.test.yml`, and commit all four
files with the reason. CI refuses a rules file or dashboard that does not match the spec. Targets
were chosen on 2026-10-20 from the thresholds of the existing per-event alerts and without a history
of the SLIs; revisit them after the first 30 days with the numbers on the dashboard.
