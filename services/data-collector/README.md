# data-collector

Standalone Bitso WebSocket trade archiver for intraday backtesting data.
This is a **data-collection** service only — it never places orders and does
not depend on Kafka, Redis, or the trading microservices.

## What it does

1. Connects to Bitso's public WebSocket (`wss://ws.bitso.com` by default).
2. Subscribes to the `trades` channel for a configurable book (default
   `btc_mxn`).
3. Persists every trade to two sinks:
   - **S3** — durable Parquet archive, partitioned
     `trades/book=<book>/year=YYYY/month=MM/day=DD/*.parquet`
   - **Postgres** — hot store of the last N days (default 7) for live
     queries and health checks
4. Reconnects with exponential backoff on disconnect and writes structured
   gap records (`ws_gaps`) so outages are queryable.
5. Exposes a dead-man's-switch `/healthz` and Prometheus `/metrics`.

## Book configuration

| Env | Default | Notes |
|-----|---------|-------|
| `BITSO_BOOK` | `btc_mxn` | Single book for now; change without code changes |
| `BITSO_WS_URL` | `wss://ws.bitso.com` | Public feed; no API keys required |

## Sinks and retention

| Sink | Role | Retention |
|------|------|-----------|
| S3 Parquet | Forever / cheap archive | Lifecycle → STANDARD_IA after 90 days (Terraform) |
| Postgres | Hot queries | `HOT_RETENTION_DAYS` (default 7); rows older than that are pruned |

Flush to S3 happens on `FLUSH_INTERVAL` (default 60s) **or**
`FLUSH_MAX_ROWS` (default 500), whichever comes first.

## HTTP contracts

### `GET /healthz`

Returns `200` + `{"status":"healthy",...}` when a trade was received within
`HEALTH_STALE_AFTER` (default `5m`). Returns `503` +
`{"status":"unhealthy",...}` otherwise (including no trades since start
after the same window). This exists so a silent stall cannot go unnoticed
for days.

### `GET /metrics`

Prometheus text exposition. Key series:

- `data_collector_trades_received_total`
- `data_collector_ws_reconnects_total`
- `data_collector_s3_flush_failures_total`
- `data_collector_postgres_write_failures_total`
- `data_collector_seconds_since_last_trade`

## Trade fields

Per trade: `book`, `tid`, `price`, `amount`, `maker_side` (`buy`/`sell`),
`exchange_ts` (Bitso creation time), `received_at` (collector wall clock).

## Local run (public feed, no AWS)

```bash
cd services/data-collector

# Capture only (in-memory S3 sink; no Postgres)
export ENABLE_S3=false
export ENABLE_POSTGRES=false
export HTTP_PORT=8085
export BITSO_BOOK=btc_mxn

go run ./cmd
```

Then:

```bash
curl -s localhost:8085/healthz
curl -s localhost:8085/metrics | head
```

### Against real S3 + Postgres

```bash
export ENABLE_S3=true
export S3_BUCKET=your-bucket
export AWS_REGION=us-east-1
export ENABLE_POSTGRES=true
export POSTGRES_DSN='postgres://user:pass@localhost:5432/trades?sslmode=disable'
go run ./cmd
```

## Layout

```
services/data-collector/
  cmd/main.go
  internal/
    collector/   # orchestration
    config/
    gap/         # disconnect/reconnect gap records
    health/      # /healthz dead-man's switch
    metrics/     # Prometheus
    models/
    sink/        # Parquet batcher, S3, Postgres
    websocket/   # reconnect wrapper around shared/pkg/bitso
```

WebSocket reconnect/backoff mirrors `services/market-data/internal/websocket`
and uses `bitso.NewWebSocketConnWithURL` from `shared/pkg/bitso`.

## Non-goals

No order placement, no strategy logic, no Kafka/Redis, no trading API keys.
