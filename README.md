# Microservices Trading Bot

Hybrid trading platform repository with:
- a new microservices layout in `services/` and `shared/`
- legacy Go services in `golang_server/` that are still present
- infrastructure as code for local, Kubernetes, and AWS environments

## Current Status

This repo is in active migration. Two services have meaningful implementations:
- `services/trading-engine`
- `services/market-data`

The remaining services are scaffolds with startup placeholders:
- `services/api-gateway`
- `services/backtesting`
- `services/order-management`
- `services/strategy-executor`

## Repository Layout

- `services/`: new microservices (Go, one module per service)
- `shared/`: shared Go packages (`bitso`, `config`, `database`, `kafka`, `models`, `redis`, `utils`)
- `golang_server/`: legacy services and workers
- `infrastructure/terraform/`: AWS infrastructure by environment
- `infrastructure/cloudformation/`: CloudFormation templates/scripts for IDE and support infra
- `k8s/`: Kustomize base and environment overlays (partial manifests)
- `monitoring/`, `security/`, `config/`, `ci-cd/`: platform configuration and automation
- `testing/`: test assets and integration test compose definitions

## Prerequisites

- Docker + Docker Compose
- Go 1.21+ for service-local execution
- (Optional) Terraform 1.6+ and AWS credentials for cloud deployments

## Quick Start (Local)

1) Start the local stack:

```bash
docker compose up -d --build
```

2) Check service logs:

```bash
docker compose logs -f trading-engine
```

3) Stop the stack:

```bash
docker compose down
```

## Run Services Without Compose

From the repository root:

```bash
go run ./services/trading-engine/cmd/main.go
go run ./services/market-data/cmd/main.go
```

Scaffold services can also be run with `go run`, but they currently exit immediately after logging a TODO message.

## Service Ports (Compose)

- `trading-engine`: `8080`
- `backtesting`: `8081`
- `strategy-executor`: `8082`
- `market-data`: `8083`
- `order-management`: `8084`
- `api-gateway`: `8085`
- `grafana`: `3000`
- `prometheus`: `9090`
- `alertmanager`: `9093`
- `kibana`: `5601`
- `jaeger`: `16686`
- `kafka`: `9092`
- `redis`: `6379`
- `postgres`: `5432`
- `cassandra`: `9042`
- `vault`: `8200`

## Infrastructure

- Terraform environments:
  - `infrastructure/terraform/envs/development`
  - `infrastructure/terraform/envs/staging`
  - `infrastructure/terraform/envs/production`
- Kubernetes overlays:
  - `k8s/overlays/development`
  - `k8s/overlays/staging`
  - `k8s/overlays/production`

See each environment README for exact `terraform init/plan/apply` flow.

## Known Gaps

- Kubernetes overlay references are incomplete in the current repository state.
- Some Compose-mounted config paths are expected but may be missing depending on branch history.
- `trading-engine` Compose healthcheck targets `/health`, but the service currently does not expose that HTTP endpoint.
- Legacy and new implementations coexist; migration is still in progress.
