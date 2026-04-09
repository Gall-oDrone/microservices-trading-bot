# Repository Structure Reference

This file documents the **current** layout and operating model of the microservices trading platform.

## Top-Level Structure

```text
microservices-trading-bot/
├── README.md
├── docker-compose.yml
├── services/
│   ├── trading-engine/
│   ├── backtesting/
│   ├── strategy-executor/
│   ├── market-data/
│   ├── order-management/
│   └── api-gateway/
├── shared/
│   └── pkg/
├── infrastructure/
│   └── terraform/
├── k8s/
├── monitoring/
├── scripts/
├── testing/
└── tests/
```

## Service Matrix

| Service | Main Responsibility | Default Internal Port | Docker Compose Host Port |
|---------|---------------------|------------------------|---------------------------|
| `trading-engine` | Trading execution and Bitso integration | `8080` | `8080` |
| `backtesting` | Historical strategy backtests and reports | `8084` | `8084` |
| `strategy-executor` | Strategy processing and signal generation | `8080` | `8082` |
| `market-data` | Real-time market ingestion and market APIs | `8083` | `8083` |
| `order-management` | Orders, positions, and risk controls | `8080` | `8086` |
| `api-gateway` | Public API entrypoint and aggregation | `8080` | `8085` |

Notes:
- Several services default to `SERVICE_PORT=8080` in code, while Docker Compose maps them to unique host ports.
- See `docker-compose.yml` for the exact runtime mapping used in local environments.

## Shared Modules

The `shared` module provides reusable packages used by multiple services:

- `shared/pkg/bitso` - Bitso API client integration
- `shared/pkg/kafka` - Kafka producer/consumer helpers
- `shared/pkg/redis` - Redis utilities
- `shared/pkg/models` - Domain model definitions
- `shared/pkg/indicators` - Trading indicator implementations
- `shared/pkg/health`, `shared/pkg/logger`, `shared/pkg/config` - operational utilities

## Infrastructure and Operations

- `k8s/` includes Kubernetes base/overlay resources.
- `infrastructure/terraform/envs/` contains environment-specific Terraform configs (`development`, `staging`, `production`).
- `monitoring/` contains Prometheus/Grafana/Alertmanager assets.
- `scripts/` contains local development and backtesting helper scripts.

## Local Workflow

### Start full local stack

```bash
docker compose up -d
```

### Build or run a single service

```bash
docker compose build strategy-executor
docker compose up -d strategy-executor
```

### Run a service directly with Go

```bash
cd services/market-data
go mod download
go run ./cmd/main.go
```

## Kubernetes and Terraform

```bash
# K8s overlays
kubectl apply -k k8s/overlays/development
kubectl apply -k k8s/overlays/staging
kubectl apply -k k8s/overlays/production
```

```bash
# Terraform (example: development env)
cd infrastructure/terraform/envs/development
terraform init
terraform plan
terraform apply
```

## Additional Documentation

- Root docs: architecture plans, deployment checklists, implementation plans
- Service docs: `services/*/README.md` where available
- Integration docs: `testing/integration/**/README.md`
