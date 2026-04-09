# Microservices Trading Bot

Microservices-based trading platform integrated with [Bitso](https://bitso.com). The system ingests market data, executes strategies, manages orders/positions, and supports historical backtesting.

## Current Architecture

### Core Services

- `market-data` (`8083`) - Ingests Bitso market streams, stores short-term data, and exposes market APIs.
- `strategy-executor` (`8082`) - Consumes market events and generates strategy signals.
- `trading-engine` (`8080`) - Executes trading logic and Bitso order flow.
- `order-management` (`8086` in docker-compose) - Handles order lifecycle, position tracking, and risk checks.
- `api-gateway` (`8085` in docker-compose) - Single HTTP entrypoint for service APIs and aggregated endpoints.
- `backtesting` (`8084`) - Runs historical strategy backtests and optimization workflows.

### Shared Infrastructure

- **Kafka** for event streaming across services.
- **Redis** for fast state/cache storage.
- **Prometheus + Grafana + Alertmanager** for monitoring.
- Optional observability/logging stack components via Docker Compose (Jaeger, Elasticsearch, Kibana, Fluentd).

## Repository Layout

```text
.
├── services/                   # Main microservices
├── shared/                     # Shared Go packages (config, clients, models, indicators)
├── infrastructure/             # Terraform and deployment infra
├── k8s/                        # Kubernetes manifests/overlays
├── monitoring/                 # Prometheus/Grafana/Alertmanager assets
├── testing/ and tests/         # Integration and service-level tests
└── scripts/                    # Build/run/dev helper scripts
```

## Local Development

### Prerequisites

- Go 1.21+
- Docker + Docker Compose
- Bitso stage credentials for non-production testing

### Start with Docker Compose

```bash
docker compose up -d
```

Useful endpoints once started:

- API Gateway: `http://localhost:8085`
- Trading Engine: `http://localhost:8080`
- Backtesting: `http://localhost:8084`
- Strategy Executor: `http://localhost:8082`
- Market Data: `http://localhost:8083`
- Order Management: `http://localhost:8086`
- Prometheus: `http://localhost:9090`
- Grafana: `http://localhost:3000`

If `docker compose build` fails due to Buildx version requirements, use:

```bash
export DOCKER_BUILDKIT=0
docker compose build
```

### Run Services Manually

Each service can also run independently:

```bash
cd services/<service-name>
go mod download
go run ./cmd/main.go
```

## Bitso Environments

- Stage API base URL: `https://stage.bitso.com/api`
- Production API base URL: `https://bitso.com/api`

The project is designed to work with Bitso stage for validation before production. Keep stage credentials and production credentials isolated.

## Key Documentation

| Document | Description |
|----------|-------------|
| [README-NEW-STRUCTURE.md](./README-NEW-STRUCTURE.md) | Expanded repository and service structure reference. |
| [DEVELOPMENT-ROADMAP.md](./DEVELOPMENT-ROADMAP.md) | Feature and capability roadmap. |
| [INTRADAY-STRATEGY-IMPLEMENTATION-PLAN.md](./INTRADAY-STRATEGY-IMPLEMENTATION-PLAN.md) | Intraday rollout phases and controls. |
| [REMAINING-PHASES-CHECKLIST.md](./REMAINING-PHASES-CHECKLIST.md) | Deployment and hardening checklist. |
| [docs/ORDER-FLOW-AND-BITSO-TESTING.md](./docs/ORDER-FLOW-AND-BITSO-TESTING.md) | Order flow validation in Bitso testing. |
| [docs/OPERATIONS-ENV-VARS.md](./docs/OPERATIONS-ENV-VARS.md) | Operational environment variable reference. |
