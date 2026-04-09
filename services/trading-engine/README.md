# Trading Engine Service

Executes trading signals against Bitso and publishes order-placement events for downstream services.

## Responsibilities

- Consume strategy signals from Kafka (`trading.signals` by default).
- Execute buy/sell logic with Bitso API credentials.
- Persist operational state using Redis.
- Expose Prometheus metrics and health endpoints.
- Optionally call order-management for:
  - pre-trade validation (`POST /api/v1/orders/validate`)
  - session risk checks (`GET /api/v1/risk/session`)

## Endpoints

- `GET /health`
- `GET /metrics`

## Key Environment Variables

- `STAGE_BITSO_API_KEY`
- `STAGE_BITSO_API_SECRET`
- `BITSO_API_BASE_URL` (defaults to Bitso stage in shared config)
- `KAFKA_BROKERS`
- `KAFKA_TOPIC_SIGNALS`
- `KAFKA_TOPIC_ORDERS_PLACED`
- `REDIS_HOST`
- `REDIS_PORT`
- `REDIS_PASSWORD`
- `REDIS_DB`
- `ORDER_MANAGEMENT_URL` (optional, enables risk and validation HTTP calls)

## Local Run

```bash
cd services/trading-engine
go mod download
go run ./cmd/main.go
```

## Docker Compose

From repo root:

```bash
docker compose up -d trading-engine
```

In `docker-compose.yml`, this service is exposed on host port `8080`.
