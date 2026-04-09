# Order Management Service

Tracks order lifecycle, position state, and risk checks for the trading platform.

## Responsibilities

- Consume signals and order-placement events from Kafka.
- Maintain order and position state (in-memory or Redis-backed storage).
- Provide risk/session APIs used by trading-engine.
- Validate orders against risk limits.
- Publish order and event topics for other services.

## HTTP Endpoints

- `GET /health`
- `GET /health/live`
- `GET /health/ready`
- `GET /api/v1/status`
- `GET /api/v1/risk/session`
- `POST /api/v1/orders/validate`

## Key Environment Variables

- `SERVICE_PORT` (defaults to `8080`; compose maps host `8086`)
- `KAFKA_BROKERS`
- `KAFKA_TOPIC_SIGNALS`
- `KAFKA_TOPIC_ORDERS_PLACED`
- `KAFKA_TOPIC_ORDERS`
- `KAFKA_TOPIC_EVENTS`
- `STORAGE_TYPE` (`memory` or `redis`)
- `REDIS_HOST`
- `REDIS_PORT`
- `TRADING_ENGINE_BASE_URL`
- `BITSO_API_BASE_URL`
- `BITSO_API_KEY` / `BITSO_API_SECRET` (or stage equivalents)

## Local Run

```bash
cd services/order-management
go mod download
go run ./cmd/main.go
```

## Docker Compose

From repo root:

```bash
docker compose up -d order-management
```

In `docker-compose.yml`, this service is exposed on host port `8086`.
