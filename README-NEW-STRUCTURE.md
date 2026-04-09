# New Structure Notes

This document describes the intended microservices architecture and the current implementation reality.

## Scope

- Intended architecture: decoupled trading platform with domain-focused services.
- Current implementation: partially migrated; not all target components are production-ready.

Use `README.md` for day-to-day setup and runnable local commands.

## Current Service Maturity

| Service | Status | Notes |
| --- | --- | --- |
| `services/trading-engine` | Implemented | Loads config, connects Bitso/Redis/Kafka, processes signals |
| `services/market-data` | Implemented | Streams Bitso market data and publishes to Kafka |
| `services/backtesting` | Scaffold | Placeholder main, exits immediately |
| `services/strategy-executor` | Scaffold | Placeholder main, exits immediately |
| `services/order-management` | Scaffold | Placeholder main, exits immediately |
| `services/api-gateway` | Scaffold | Placeholder main, exits immediately |

## Implemented Directory Shape

```text
microservices-trading-bot/
├── services/
│   ├── trading-engine/
│   ├── market-data/
│   ├── backtesting/
│   ├── strategy-executor/
│   ├── order-management/
│   └── api-gateway/
├── shared/pkg/
│   ├── bitso/
│   ├── config/
│   ├── database/
│   ├── kafka/
│   ├── models/
│   ├── redis/
│   └── utils/
├── golang_server/                 # Legacy services still present
├── infrastructure/
│   ├── terraform/
│   └── cloudformation/
├── k8s/
│   ├── base/
│   └── overlays/
└── docker-compose.yml
```

## Deployment Readiness

- `docker-compose.yml`: usable for local development and integration testing.
- `k8s/`: structure exists, but manifests/patches are incomplete in current repository state.
- Terraform: environment stacks exist under `infrastructure/terraform/envs/*` and are the primary IaC path.

## Migration Checklist

- [x] Create service and shared-library directory model
- [x] Implement initial `trading-engine` service
- [x] Implement initial `market-data` service
- [ ] Implement `api-gateway`
- [ ] Implement `backtesting`
- [ ] Implement `strategy-executor`
- [ ] Implement `order-management`
- [ ] Complete Kubernetes manifests referenced by overlays
- [ ] Consolidate or retire `golang_server/` legacy services
