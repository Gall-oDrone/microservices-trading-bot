# Bitso Trading Platform - Microservices Architecture

This document describes the new microservices architecture for the Bitso Trading Platform.

## Project Structure

```
bitso-trading-platform/
├── docker-compose.yml                 # Local development orchestration
├── k8s/                               # Kubernetes manifests
│   ├── base/                          # Base Kubernetes configurations
│   ├── overlays/                      # Environment-specific overlays
│   │   ├── development/
│   │   ├── staging/
│   │   └── production/
│   └── helmcharts/                    # Helm charts for EKS deployment
│
├── services/                          # Microservices
│   ├── trading-engine/               # Core trading engine (refactored from staging)
│   │   ├── cmd/main.go
│   │   ├── internal/
│   │   ├── Dockerfile
│   │   └── go.mod
│   │
│   ├── backtesting/                  # Backtesting service
│   │   ├── cmd/main.go
│   │   ├── internal/
│   │   │   ├── engine/              # Backtesting engine
│   │   │   ├── simulator/           # Market simulator
│   │   │   └── analyzer/            # Performance analysis
│   │   ├── Dockerfile
│   │   └── go.mod
│   │
│   ├── strategy-executor/            # Strategy execution service
│   │   ├── cmd/main.go
│   │   ├── internal/
│   │   │   ├── strategies/
│   │   │   ├── signals/
│   │   │   └── risk/
│   │   ├── Dockerfile
│   │   └── go.mod
│   │
│   ├── market-data/                  # Market data aggregation
│   │   ├── cmd/main.go
│   │   ├── internal/
│   │   │   ├── websocket/
│   │   │   ├── historical/
│   │   │   └── cache/
│   │   ├── Dockerfile
│   │   └── go.mod
│   │
│   ├── order-management/             # Order lifecycle management
│   │   ├── cmd/main.go
│   │   ├── internal/
│   │   ├── Dockerfile
│   │   └── go.mod
│   │
│   └── api-gateway/                  # REST/GraphQL API gateway
│       ├── cmd/main.go
│       ├── internal/
│       ├── Dockerfile
│       └── go.mod
│
├── shared/                            # Shared libraries
│   ├── pkg/
│   │   ├── bitso/                   # Bitso client library
│   │   ├── kafka/                   # Kafka utilities
│   │   ├── redis/                   # Redis utilities
│   │   └── models/                  # Shared domain models
│   └── go.mod
│
├── infrastructure/                    # Infrastructure as Code
│   ├── terraform/
│   │   ├── eks/                     # EKS cluster configuration
│   │   ├── rds/                     # RDS database configuration
│   │   ├── elasticache/             # ElastiCache Redis configuration
│   │   └── msk/                     # AWS Managed Kafka
│   └── ansible/
│
└── scripts/
    ├── deploy.sh                     # Deployment script
    └── local-dev.sh                  # Local development setup
```

## Services Overview

### Trading Engine
- **Purpose**: Core trading logic and execution
- **Port**: 8080
- **Dependencies**: Kafka, Redis
- **Refactored from**: `golang_server/staging/`

### Backtesting Service
- **Purpose**: Historical strategy testing and performance analysis
- **Port**: 8081
- **Dependencies**: Kafka, Redis
- **New Service**: Built from scratch

### Strategy Executor
- **Purpose**: Strategy execution and signal processing
- **Port**: 8082
- **Dependencies**: Kafka, Redis
- **New Service**: Built from scratch

### Market Data Service
- **Purpose**: Real-time and historical market data aggregation
- **Port**: 8083
- **Dependencies**: Kafka, Redis
- **New Service**: Built from scratch

### Order Management
- **Purpose**: Order lifecycle management and tracking
- **Port**: 8084
- **Dependencies**: Kafka, Redis
- **New Service**: Built from scratch

### API Gateway
- **Purpose**: REST/GraphQL API gateway for all services
- **Port**: 8085
- **Dependencies**: All other services
- **New Service**: Built from scratch

## Development

### Local Development
```bash
# Start all services
docker-compose up -d

# Start specific service
docker-compose up trading-engine

# View logs
docker-compose logs -f trading-engine
```

### Building Services
```bash
# Build all services
docker-compose build

# Build specific service
docker-compose build trading-engine
```

## Deployment

### Kubernetes
```bash
# Deploy to development
kubectl apply -k k8s/overlays/development

# Deploy to staging
kubectl apply -k k8s/overlays/staging

# Deploy to production
kubectl apply -k k8s/overlays/production
```

### AWS EKS
```bash
# Deploy infrastructure
cd infrastructure/terraform
terraform init
terraform plan
terraform apply

# Deploy application
./scripts/deploy.sh production
```

## Migration Plan

1. **Phase 1**: Create new structure (current)
2. **Phase 2**: Migrate trading-engine from staging
3. **Phase 3**: Implement new services
4. **Phase 4**: Migrate shared libraries
5. **Phase 5**: Update deployment configurations
6. **Phase 6**: Testing and validation
7. **Phase 7**: Production deployment

## Next Steps

- [ ] Migrate existing trading logic to trading-engine service
- [ ] Implement backtesting service
- [ ] Implement strategy executor service
- [ ] Implement market data service
- [ ] Implement order management service
- [ ] Implement API gateway
- [ ] Create shared libraries
- [ ] Update Kubernetes configurations
- [ ] Create deployment scripts
- [ ] Add monitoring and logging
- [ ] Add testing framework
