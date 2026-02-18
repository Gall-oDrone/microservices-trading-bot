# Prometheus Alert Rules

Alert rule files in this directory are loaded by Prometheus via `rule_files: - "rules/*.yml"` in `../prometheus.yml`.

## trading-alerts.yml

- **ServiceDown** – Any target `up == 0` for 1m (critical).
- **BalanceLastSuccessTimestampStale** – Bitso balance not refreshed in 5m (critical, trading-engine).
- **TradingEngineOrdersFailedRateHigh** – Order failure rate > 0.5/s over 5m (critical).
- **TradingEngineNotRunning** – Trading-engine up but `engine_state != 2` for 5m (warning).
- **TradingEngineHealthCheckFailuresHigh** – Health check failures > 5 in 15m (warning).
- **TradingEngineHighErrorRate** – HTTP 5xx rate > 0.1/s (critical).
- **APIRateLimitExceeded** – Bitso API rate limit metric > 0 (warning).
- **HighOrderExecutionLatency** – p95 order execution latency > 5s (warning).
- **LowAvailableBalance** – `bitso_available_balance` < 1000 (critical).

Severity and routing (e.g. to Alertmanager and on-call) should be configured in Alertmanager.
