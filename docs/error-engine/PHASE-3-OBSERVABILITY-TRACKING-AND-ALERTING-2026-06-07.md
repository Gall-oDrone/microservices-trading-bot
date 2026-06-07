# Phase 3 — Observability, Tracking, and Alerting

Date: 2026-06-07  
Repository: `microservices-trading-bot`

## 1) Objective

Plan **production observability** for the Error Engine using the existing Prometheus/Grafana/Alertmanager stack — no new infrastructure assumptions for v1.

Covers: metrics naming standards, inventory normalization, alert tiers, SLOs/error budgets, and dashboard requirements.

**Prerequisites:** [`PHASE-2-ERROR-MODEL-AND-CLASSIFICATION-2026-06-07.md`](PHASE-2-ERROR-MODEL-AND-CLASSIFICATION-2026-06-07.md)

**Next:** [`PHASE-4-FINANCIAL-VENUE-AND-RISK-ERRORS-2026-06-07.md`](PHASE-4-FINANCIAL-VENUE-AND-RISK-ERRORS-2026-06-07.md)

---

## 2) Metrics standard

### 2.1 Naming convention

Follow Prometheus naming best practices aligned with existing services:

```
{service_prefix}_errors_total{error_code, domain, severity, book}
```

Examples:

```
order_management_errors_total{error_code="OM_SYNC_BITSO_API_ERROR", domain="venue", severity="P1", book="btc_mxn"}
strategy_router_errors_total{error_code="SR_EVALUATION_ERROR", domain="strategy", severity="P2", book="btc_mxn"}
```

**Migration path:** Keep legacy counters (e.g. `bitso_sync_errors_total`) during transition; add normalized vec alongside; deprecate after Phase 6 pilot sign-off.

### 2.2 Required labels

| Label | Required when | Cardinality note |
|-------|---------------|------------------|
| `error_code` | Always | Bounded catalog (~50 codes v1) |
| `domain` | Always | 7 values |
| `severity` | Always | 4 values |
| `book` | Trading-path errors | Limit to configured books |
| `service` | Cross-service dashboards | Implicit from metric namespace |

**Avoid:** unbounded labels (`order_id`, `message`, `stack`).

### 2.3 RED / USE alignment

| Service type | Primary signals |
|--------------|-----------------|
| HTTP APIs (gateway, executor, router) | Rate, errors (5xx + structured codes), duration |
| Kafka consumers (engine, executor) | Lag, consumer errors, processing duration |
| Pollers (OM sync, user-trades) | Success timestamp gauges, error rate |
| WebSocket (market-data) | Connection state, message rate, WS errors |

Existing gauges to preserve:

- `bitso_sync_last_success_timestamp`
- `user_trades_last_poll_timestamp`
- Indicator snapshot health (`data_healthy`)

---

## 3) Inventory — existing error metrics

### 3.1 trading-engine (`services/trading-engine/internal/metrics/metrics.go`)

| Legacy metric | Proposed error_code | Severity |
|---------------|---------------------|----------|
| `bitso_balance_fetch_errors_total` | `TE_BALANCE_FETCH_ERROR` | P1 |
| `kafka_consumer_errors_total` | `TE_KAFKA_CONSUMER_ERROR` | P1 |
| `order_placed_events_publish_errors_total` | `TE_ORDER_PLACED_PUBLISH_ERROR` | P1 |

Also track pretrade outcomes via existing validation counters — map rejects to `TE_PRETRADE_RISK_VIOLATION`.

### 3.2 order-management (`services/order-management/internal/metrics/prometheus.go`)

| Legacy metric | Proposed error_code | Severity |
|---------------|---------------------|----------|
| `bitso_sync_errors_total` | `OM_SYNC_BITSO_API_ERROR` | P1 |
| `user_trades_poll_errors_total` | `OM_USER_TRADES_POLL_ERROR` | P1 |
| `session_risk_request_errors_total` | `OM_SESSION_RISK_REQUEST_ERROR` | P1 |
| `events_failed` (publisher) | `OM_KAFKA_PUBLISH_FAILED` | P1 |

Financial gauges (not errors but related): `daily_realized_pnl_*`, `drawdown_*`, `current_equity_*`.

### 3.3 strategy-router (`services/strategy-router/internal/metrics/metrics.go`)

| Legacy metric | Proposed error_code | Severity |
|---------------|---------------------|----------|
| `strategy_router_evaluation_errors_total{book}` | `SR_EVALUATION_ERROR` | P2 |

Also expose: `strategy_router_blocked_total{reason}` — map `has_position` stuck to alert (existing).

### 3.4 strategy-executor (`services/strategy-executor/internal/metrics/metrics.go`)

| Legacy pattern | Proposed error_code | Severity |
|----------------|---------------------|----------|
| `StrategyErrors{strategy, book, error_type}` | Map `error_type` → catalog | P1–P2 |
| `MarketDataErrors{topic, book, error_type}` | `SE_MARKET_DATA_PROCESS_ERROR` | P2 |

### 3.5 market-data (`services/market-data/internal/metrics/prometheus.go`)

| Legacy metric | Proposed error_code |
|---------------|---------------------|
| `market_data_websocket_errors_total` | `MD_WEBSOCKET_ERROR` |
| `market_data_websocket_subscribe_errors_total` | `MD_WEBSOCKET_SUBSCRIBE_ERROR` |
| `market_data_storage_errors_total` | `MD_STORAGE_ERROR` |
| `market_data_historical_errors_total` | `MD_HISTORICAL_FETCH_ERROR` |
| `market_data_api_errors_total` | `MD_API_ERROR` |

### 3.6 api-gateway (`services/api-gateway/internal/metrics/prometheus.go`)

| Legacy metric | Proposed error_code |
|---------------|---------------------|
| `api_gateway_backend_errors_total{service, endpoint, error_type}` | `AG_BACKEND_ERROR` / `AG_BACKEND_TIMEOUT` |

---

## 4) Alert tiers (extend `k8s/monitoring/prometheus-rules.yaml`)

### 4.1 Existing groups (preserve)

- `trading-alerts` — service down, 5xx, latency, restarts, memory
- `strategy-router-alerts` — blocked position, evaluation failures, not evaluating

### 4.2 Planned group: `error-engine-financial`

| Alert | Expr (sketch) | Severity | For |
|-------|---------------|----------|-----|
| `FinancialIntegrityError` | `increase(order_management_errors_total{severity="P0"}[5m]) > 0` | critical | 0m |
| `ReconciliationDeltaDetected` | Custom gauge `reconciliation_delta_quote > threshold` | critical | 5m |
| `FeeAssumptionDrift` | `abs(configured_fee - realized_fee) > 0.0001` histogram | warning | 15m |
| `RealizedPnLStaleAfterFill` | fill event timestamp vs P&L gauge update | warning | 10m |

### 4.3 Planned group: `error-engine-venue`

| Alert | Expr (sketch) | Severity | For |
|-------|---------------|----------|-----|
| `BitsoSyncErrorsSustained` | `increase(bitso_sync_errors_total[10m]) > 10` | warning | 5m |
| `UserTradesPollStalled` | `time() - user_trades_last_poll_timestamp > 300` | warning | 5m |
| `BalanceFetchErrorsBurst` | `increase(bitso_balance_fetch_errors_total[5m]) > 5` | warning | 2m |
| `MarketDataWebSocketDown` | WS connected gauge == 0 | critical | 2m |

### 4.4 Planned group: `error-engine-kafka`

| Alert | Expr (sketch) | Severity | For |
|-------|---------------|----------|-----|
| `KafkaConsumerErrorsBurst` | `increase(kafka_consumer_errors_total[5m]) > 5` | warning | 2m |
| `OrderFillPublishFailures` | `increase(events_failed{event="order_fill"}[5m]) > 0` | critical | 0m |

### 4.5 Alert annotation standards

Every Error Engine alert annotation MUST include:

- `error_code` (when applicable)
- Link to runbook section (Phase 5)
- One-line operator action ("port-forward OM, check sync logs for oid X")
- `summary` / `description` consistent with existing router alerts

Route P0 to critical channel; P1 to warning; integrate with `ops-agent` Alertmanager webhook (future).

---

## 5) SLOs and error budgets

### 5.1 Service SLOs (Stage targets)

| Service | SLI | SLO (30d) | Error budget |
|---------|-----|-----------|--------------|
| order-management sync | Successful sync cycle / attempts | 99.5% | 0.5% failed cycles |
| strategy-router | Evaluations without error / total | 99% | 1% evaluation errors |
| trading-engine | Signals processed without consumer error | 99.5% | 0.5% |
| market-data WS | Uptime of connected state | 99% | 1% disconnect time |

### 5.2 Financial integrity SLO

| SLI | SLO | Measurement |
|-----|-----|-------------|
| P&L reconciliation delta | 0 confirmed deltas per rolling 7d | Batch reconciliation job (Layer A — agentic strategy doc) |
| Fill price accuracy | 100% fills with VWAP from `/order_trades` | Audit sample + `OM_SYNC_AVG_PRICE_MISMATCH` count == 0 |

**Error budget policy:** Exhausting financial SLO → freeze Production promotion; Stage-only until post-mortem.

---

## 6) Dashboard requirements (Grafana)

### 6.1 Error Engine overview dashboard (new)

Panels:

1. **Error rate by `error_code`** — stacked area, 1h / 24h
2. **P0/P1 count** — stat panel with threshold coloring
3. **Top books by error rate**
4. **Venue health** — sync last success, trades poll last success, WS connected
5. **Router health** — evaluations, evaluation errors, blocked reasons
6. **Kafka error counters** — consumer + publish failures by service

### 6.2 Financial integrity row

- Realized P&L gauge vs last fill timestamp
- Fee assumption vs realized (when histogram exists)
- Reconciliation delta gauge (future)
- Drawdown gauges (existing OM metrics)

### 6.3 Operator drill-down

- Template variables: `book`, `service`, `error_code`, `severity`
- Link to Loki/log query placeholder (if logs centralized later)

Existing strategy-router dashboard referenced in soak docs — extend with normalized error_code panels.

---

## 7) Structured logging for tracking

Complement metrics with searchable logs:

| Field | Source |
|-------|--------|
| `error_code` | Phase 2 catalog |
| `correlation_id` | Context propagation |
| `severity` | Classification |
| `book`, `strategy`, `order_id`, `bitso_oid` | Context envelope |

**Format:** JSON in production (strategy-executor zerolog pattern); consistent across services.

**Retention:** Align with cluster log retention; P0/P1 optionally copied to audit store (Phase 5).

---

## 8) OpenTelemetry (stretch — not v1 required)

Future enhancement:

- Trace spans for HTTP router→executor and Kafka consume→publish
- `trace_id` in context envelope links logs + metrics + traces
- Export via OTel collector sidecar

**v1 decision:** Prometheus + structured logs sufficient for Stage/Production v1 Error Engine.

---

## 9) Exit criteria (Phase 3 complete)

- [x] Metrics naming and label standard defined
- [x] Legacy metric inventory mapped to error codes
- [x] Alert group sketches documented
- [x] SLO targets and dashboard panels specified
- [ ] Prometheus rules implemented in `k8s/monitoring/prometheus-rules.yaml` (Phase 6)
- [ ] Grafana dashboard JSON committed (Phase 6)
- [ ] Stage alert fire drill completed (Phase 6)
