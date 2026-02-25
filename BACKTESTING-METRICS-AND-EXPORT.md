# Backtesting Metrics, Parameters & Export

This document describes how the backtesting service documents strategy parameters and metrics, evaluates optional success criteria, and supports export to logs (e.g. CloudWatch Logs) and downstream systems (e.g. S3).

**Related:** [INTRADAY-STRATEGY-IMPLEMENTATION-PLAN.md](INTRADAY-STRATEGY-IMPLEMENTATION-PLAN.md) Phase 6 (Backtesting workflow). [services/backtesting/README.md](services/backtesting/README.md) for API and configuration.

---

## Overview

When a backtest runs (with strategy X and parameters), the service now:

1. **Persists a config snapshot** with every result (Redis/file), so strategy name, `strategy_params`, book, date range, and execution settings are stored and available in reports and exports.
2. **Optionally evaluates success criteria** (e.g. min Sharpe, max drawdown %, min trades) and records whether the run met thresholds (`met_thresholds`, `failure_reason`).
3. **Logs one structured event per completion** (`backtest_completion`) with config + key metrics (and outcome), so log aggregators (CloudWatch Logs, Fluent Bit → S3) can capture and query runs without changing the service.

---

## Config Snapshot

Every stored `BacktestResult` includes an optional **`config`** (ConfigSnapshot) with:

| Field | Description |
|-------|-------------|
| `name` | Backtest name |
| `book` | Trading pair (e.g. `btc_mxn`) |
| `strategy` | Strategy name (e.g. `basic`) |
| `strategy_params` | Strategy-specific parameters (e.g. `rsi_period`, `rsi_oversold`) |
| `start_date`, `end_date` | Backtest window |
| `initial_balance` | Starting balance |
| `slippage_model`, `slippage_value` | Slippage settings |
| `commission_rate` | Commission |
| `data_source`, `data_granularity` | Data settings |

- **Where it’s set:** Engine runner calls `result.SetConfigSnapshot(r.config)` when creating the result; failed runs get the snapshot in the manager before save.
- **Where it appears:** Stored in Redis/file/S3 with the result; included in **text**, **JSON**, and **HTML** reports (see Reports below).

---

## Result Storage: Redis, File, or S3

The backtesting service can store results in three backends (selected by **`STORAGE_TYPE`**):

| Type   | Use case | Required env |
|--------|----------|----------------|
| `redis` | Default; fast; TTL support | `REDIS_HOST`, `REDIS_PORT` (and optional `REDIS_PASSWORD`, `REDIS_DB`) |
| `file`  | Local or NFS; no Redis | `STORAGE_PATH` |
| `s3`    | Long-term retention; archival | `S3_BUCKET`; optional `S3_PREFIX`, `AWS_REGION` (or `S3_REGION`) |

- **S3:** Results are stored as JSON objects at `s3://<bucket>/<prefix><backtestID>.json`. The service uses the default AWS credential chain (env vars, IAM role, etc.). List/Get/Delete are supported; filters (status, strategy, book, date range) work by listing under the prefix and filtering in memory.

---

## Success Criteria (Optional)

You can define when a backtest “worked” by passing **`success_criteria`** in the create request. All fields are optional; zero means “not set” and the check is skipped.

| Field | Meaning | Example |
|-------|---------|--------|
| `min_sharpe_ratio` | Require Sharpe ≥ value | `1.0` |
| `max_drawdown_percent` | Require drawdown ≥ -value% (use positive number) | `10` → allow down to -10% |
| `min_total_trades` | Require at least N trades | `10` |
| `min_win_rate` | Require win rate ≥ value (0–1) | `0.5` → 50% |
| `min_total_return_percent` | Require total return % ≥ value | `5` |

After the run completes, the service sets on the result:

- **`met_thresholds`** — `true` if all set criteria passed, `false` otherwise.
- **`failure_reason`** — If not met, a short explanation (e.g. `sharpe_ratio 0.80 < min 1.00`).

**Example request (excerpt):**

```json
{
  "name": "Basic RSI test",
  "start_date": "2024-01-01T00:00:00Z",
  "end_date": "2024-06-30T23:59:59Z",
  "book": "btc_mxn",
  "initial_balance": 100000,
  "strategy": "basic",
  "strategy_params": { "rsi_period": 14, "rsi_oversold": 30, "rsi_overbought": 70 },
  "success_criteria": {
    "min_sharpe_ratio": 1.0,
    "max_drawdown_percent": 10,
    "min_total_trades": 5
  }
}
```

---

## Structured Completion Logging

On every completion (success or failure), the manager logs **one** structured log line with message **`backtest_completion`** and consistent fields so you can filter and export (e.g. to CloudWatch Logs or S3).

**Common fields:**

- `event`: `"backtest_completion"`
- `outcome`: `"completed"` or `"failed"`
- `backtest_id`, `strategy`, `book`, `name`
- `start_date`, `end_date` (RFC3339)
- `strategy_params_json`: JSON string of strategy params (when non-empty)

**When outcome = completed:**

- `total_return_percent`, `sharpe_ratio`, `max_drawdown_percent`, `win_rate`, `total_trades`
- `met_thresholds` (bool)
- `failure_reason` (if success criteria were set and not met)

**When outcome = failed:**

- `error`: error message

Logs go to **stdout** (or the configured `LOG_OUTPUT` file). With JSON format (`LOG_FORMAT=json`), each line is a JSON object; your log collector (Fluent Bit, CloudWatch agent, etc.) can ship these to CloudWatch Logs or to S3 (e.g. via subscription filter + Lambda, or Kinesis).

---

## Completion Notifiers (Webhook, Kafka, S3 Export)

In addition to structured logging, the service can send each completion event to external systems. All are optional; set the corresponding env vars to enable.

| Notifier    | Env vars | Behavior |
|-------------|----------|----------|
| **Webhook** | `BACKTEST_WEBHOOK_URL` | HTTP POST the event as JSON to the URL (timeout 10s). |
| **Kafka**   | `KAFKA_BROKERS`, `KAFKA_TOPIC_BACKTEST_COMPLETED` (default topic: `backtesting.completions`) | Produce one message per completion; key = `backtest_id`, value = JSON event. |
| **S3 export** | `BACKTEST_EXPORT_S3_BUCKET`; optional `BACKTEST_EXPORT_S3_PREFIX` (default `backtests/completions/`), `AWS_REGION` or `BACKTEST_EXPORT_S3_REGION` | Write one JSON file per completion at `s3://<bucket>/<prefix>YYYY/MM/DD/backtest-<id>-summary.json`. |

The payload is the same for all: **BacktestCompletionEvent** (event, outcome, backtest_id, strategy, book, name, start_date, end_date, strategy_params_json, and for completed: total_return_percent, sharpe_ratio, max_drawdown_percent, win_rate, total_trades, met_thresholds, failure_reason; for failed: error). Notifiers are invoked asynchronously (fire-and-forget with 15s timeout) so they do not block the manager.

---

## Reports Include Config and Success Result

- **Text report** (`GET /api/v1/backtests/{id}/report?format=text`): Adds a **STRATEGY & PARAMETERS** section (name, book, strategy, dates, balance, slippage, commission, strategy_params) and a **SUCCESS CRITERIA** section (met_thresholds, failure_reason) when present.
- **HTML report** (`format=html`): Adds **Strategy & Parameters** and **Success Criteria** sections.
- **JSON report** (`format=json`): The response is the full `BacktestResult`, which now includes `config`, `met_thresholds`, and `failure_reason`.

---

## Export to CloudWatch Logs or S3

1. **CloudWatch Logs:** Run the backtesting service so stdout (or the log file) is collected by your log agent (e.g. Fluent Bit or CloudWatch Logs agent). Each `backtest_completion` line is a single structured event. Use log group metric filters or subscription filters to react (e.g. alert when `outcome=failed` or `met_thresholds=false`).
2. **S3 from logs (Lambda):** A **Lambda function** is provided to subscribe to the backtesting log group and write each `backtest_completion` event to S3. See **[infrastructure/lambda/backtest-log-to-s3/README.md](infrastructure/lambda/backtest-log-to-s3/README.md)** for setup (filter pattern, env vars `EXPORT_BUCKET`, `EXPORT_PREFIX`, IAM, and optional Terraform). No change required in the backtesting service.
3. **S3 from API:** You can periodically call `GET /api/v1/backtests/{id}/report?format=json` for completed backtests and upload the response to S3 (e.g. via a cron job or pipeline). The JSON includes config and metrics.

---

## Files Touched (Implementation)

| Area | Location |
|------|----------|
| Config snapshot & success criteria types | `services/backtesting/internal/models/config.go`, `result.go` |
| Set config snapshot, evaluate criteria | `services/backtesting/internal/engine/runner.go` |
| Failed result config snapshot, completion logging + notifiers | `services/backtesting/internal/manager/backtest_manager.go` |
| API: accept `success_criteria` | `services/backtesting/internal/api/backtest_handlers.go` |
| Reports: config + success in text/HTML | `services/backtesting/internal/analyzer/report.go` |
| S3 result storage | `services/backtesting/internal/storage/s3_storage.go` |
| Export config & loader | `services/backtesting/internal/config/config.go`, `loader.go` |
| Completion notifiers (webhook, Kafka, S3 export) | `services/backtesting/internal/export/` |
| Wire storage + notifiers in main | `services/backtesting/cmd/main.go` |
| CloudWatch Logs → S3 Lambda | `infrastructure/lambda/backtest-log-to-s3/` |
| Documentation | This file, Lambda README |

---

**Document version:** 1.1  
**Status:** Implemented. Config snapshot and success criteria are optional; completion logging is always emitted. Optional: S3 result storage, webhook/Kafka/S3 export notifiers, and Lambda for log-to-S3.
