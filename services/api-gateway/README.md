# API Gateway Service

A production-ready API Gateway that serves as the single entry point for the trading bot platform, routing requests to backend microservices (market-data, order-management, and strategy-executor).

## Status

🚧 **Status**: Planning Complete - Ready for Implementation  
📅 **Last Updated**: October 27, 2025  
📝 **Version**: 1.0.0

## Overview

The API Gateway provides:
- **Unified API**: Single entry point for all clients
- **Request Routing**: Route requests to appropriate backend services
- **API Aggregation**: Combine data from multiple services
- **Cross-Cutting Concerns**: Authentication, rate limiting, circuit breaking, logging
- **Observability**: Comprehensive metrics and health checks
- **Resilience**: Circuit breakers, retries, timeouts

## Architecture

```
┌─────────────┐
│   Clients   │
└──────┬──────┘
       │
       ▼
┌─────────────────────────────────────┐
│         API Gateway                  │
│  ┌────────┐  ┌────────┐  ┌────────┐│
│  │ Router │─▶│Middlew.│─▶│Handler ││
│  └────────┘  └────────┘  └────────┘│
│       │           │           │     │
│  ┌────▼────┐ ┌───▼────┐ ┌───▼────┐│
│  │ MD      │ │ OM     │ │ SE     ││
│  │ Client  │ │ Client │ │ Client ││
│  └────┬────┘ └───┬────┘ └───┬────┘│
└───────┼──────────┼──────────┼─────┘
        │          │          │
        ▼          ▼          ▼
   ┌────────┐ ┌────────┐ ┌────────┐
   │Market  │ │ Order  │ │Strategy│
   │ Data   │ │  Mgmt  │ │  Exec  │
   └────────┘ └────────┘ └────────┘
```

## Features

### Core Features
- ✅ **Request Routing** - Route to backend services
- ✅ **API Aggregation** - Combine multiple service responses
- ✅ **Rate Limiting** - Token bucket rate limiter
- ✅ **Circuit Breaking** - Per-service circuit breakers
- ✅ **Health Checks** - Liveness, readiness, detailed health
- ✅ **Metrics** - Prometheus metrics collection
- ✅ **Logging** - Structured JSON logging
- ✅ **Error Handling** - Consistent error responses
- ✅ **CORS** - CORS handling
- ✅ **Recovery** - Panic recovery

### Planned Features
- 🔲 JWT Authentication
- 🔲 API Key Management
- 🔲 Redis Caching
- 🔲 WebSocket Support
- 🔲 GraphQL Endpoint
- 🔲 Distributed Tracing

## API Endpoints

### Health & Status
```
GET  /health                    # Detailed health check
GET  /health/live               # Liveness probe
GET  /health/ready              # Readiness probe
GET  /api/v1/status             # Service status
```

### Market Data (Proxy to market-data:8083)
```
GET  /api/v1/market-data/trades
GET  /api/v1/market-data/trades/:id
GET  /api/v1/market-data/orderbook
GET  /api/v1/market-data/ticker
GET  /api/v1/market-data/stats/trades
GET  /api/v1/market-data/summary
```

### Orders (Proxy to order-management:8081)
```
GET  /api/v1/orders
GET  /api/v1/orders/:id
POST /api/v1/orders/:id/cancel
GET  /api/v1/orders/active
GET  /api/v1/orders/history
```

### Positions (Proxy to order-management:8081)
```
GET  /api/v1/positions
GET  /api/v1/positions/:book
GET  /api/v1/positions/summary
```

### Strategies (Proxy to strategy-executor:8082)
```
GET  /api/v1/strategies
GET  /api/v1/strategies/:name
POST /api/v1/strategies/:name/start
POST /api/v1/strategies/:name/stop
PUT  /api/v1/strategies/:name/config
```

### Aggregated Endpoints (New functionality)
```
GET  /api/v1/dashboard          # Aggregated dashboard data
GET  /api/v1/portfolio          # Portfolio overview
GET  /api/v1/trading/overview   # Trading overview
GET  /api/v1/system/status      # System-wide status
```

### Metrics
```
GET  /metrics                   # Prometheus metrics
```

## Configuration

### Environment Variables

```bash
# Service Configuration
SERVICE_NAME=api-gateway
SERVICE_VERSION=1.0.0
SERVICE_HOST=0.0.0.0
SERVICE_PORT=8080
ENVIRONMENT=development

# Backend Services
MARKET_DATA_URL=http://localhost:8083
ORDER_MANAGEMENT_URL=http://localhost:8081
STRATEGY_EXECUTOR_URL=http://localhost:8082

# HTTP Client Configuration
CLIENT_TIMEOUT=30s
CLIENT_MAX_RETRIES=3
CLIENT_RETRY_DELAY=1s

# Rate Limiting
RATE_LIMIT_ENABLED=true
RATE_LIMIT_REQUESTS_PER_MINUTE=60
RATE_LIMIT_BURST=10

# Circuit Breaker
CIRCUIT_BREAKER_ENABLED=true
CIRCUIT_BREAKER_THRESHOLD=5
CIRCUIT_BREAKER_TIMEOUT=60s

# Authentication (Future)
AUTH_ENABLED=false
JWT_SECRET=your-secret-key

# Logging
LOG_LEVEL=info
LOG_FORMAT=json
LOG_OUTPUT=stdout
```

## Quick Start

### Prerequisites
- Go 1.21 or higher
- Backend services running:
  - market-data (port 8083)
  - order-management (port 8081)
  - strategy-executor (port 8082)

### Installation

```bash
# Navigate to service directory
cd services/api-gateway

# Install dependencies
go mod download

# Build the service
go build -o api-gateway ./cmd/main.go

# Run the service
./api-gateway
```

### Using Docker

```bash
# Build Docker image
docker build -t api-gateway:latest .

# Run container
docker run -p 8080:8080 \
  -e MARKET_DATA_URL=http://market-data:8083 \
  -e ORDER_MANAGEMENT_URL=http://order-management:8081 \
  -e STRATEGY_EXECUTOR_URL=http://strategy-executor:8082 \
  api-gateway:latest
```

## Project Structure

```
services/api-gateway/
├── cmd/
│   └── main.go                          # Application entry point
├── internal/
│   ├── api/                             # HTTP handlers
│   │   ├── handlers.go
│   │   ├── market_data_handlers.go
│   │   ├── order_handlers.go
│   │   ├── strategy_handlers.go
│   │   ├── aggregation_handlers.go
│   │   └── response.go
│   ├── client/                          # HTTP clients
│   │   ├── types.go
│   │   ├── market_data_client.go
│   │   ├── order_management_client.go
│   │   ├── strategy_executor_client.go
│   │   └── client_factory.go
│   ├── config/                          # Configuration
│   │   ├── config.go
│   │   └── config_test.go
│   ├── logger/                          # Structured logging
│   │   └── logger.go
│   ├── metrics/                         # Prometheus metrics
│   │   └── prometheus.go
│   ├── middleware/                      # HTTP middleware
│   │   ├── logging.go
│   │   ├── metrics.go
│   │   ├── rate_limiter.go
│   │   ├── circuit_breaker.go
│   │   ├── auth.go
│   │   ├── cors.go
│   │   ├── recovery.go
│   │   └── timeout.go
│   ├── router/                          # Router
│   │   ├── router.go
│   │   └── routes.go
│   ├── server/                          # HTTP server
│   │   └── http_server.go
│   └── validation/                      # Request validation
│       ├── validator.go
│       └── rules.go
├── test/
│   ├── integration/                     # Integration tests
│   └── unit/                            # Unit tests
├── Dockerfile
├── go.mod
├── go.sum
├── README.md
├── API_GATEWAY_ANALYSIS.md              # Complete analysis
├── IMPLEMENTATION_CHECKLIST.md          # Implementation checklist
└── IMPLEMENTATION_SUMMARY.md            # Implementation summary
```

## Development

### Running Tests

```bash
# Run all tests
go test ./...

# Run with verbose output
go test ./... -v

# Run with coverage
go test ./... -coverprofile=coverage.out
go tool cover -html=coverage.out

# Run integration tests
go test ./test/integration/... -v

# Run unit tests only
go test ./internal/... -v
```

### Development Workflow

1. Make changes
2. Run tests: `go test ./...`
3. Run linters: `go vet ./...`
4. Build: `go build ./cmd/main.go`
5. Test locally
6. Commit changes

## Monitoring

### Metrics

The service exposes Prometheus metrics at `/metrics`:

**HTTP Metrics:**
- `http_requests_total` - Total HTTP requests
- `http_request_duration_seconds` - Request duration histogram
- `http_requests_in_flight` - In-flight requests gauge

**Backend Client Metrics:**
- `backend_calls_total` - Total backend service calls
- `backend_call_duration_seconds` - Backend call duration
- `backend_errors_total` - Backend errors

**Circuit Breaker Metrics:**
- `circuit_breaker_state` - Circuit breaker state by service
- `circuit_breaker_operations_total` - Circuit breaker operations

**Rate Limiter Metrics:**
- `rate_limit_hits_total` - Rate limit hits
- `rate_limit_allows_total` - Allowed requests

### Health Checks

- **Liveness**: `GET /health/live` - Returns 200 if service is alive
- **Readiness**: `GET /health/ready` - Returns 200 if ready to serve traffic
- **Detailed**: `GET /health` - Returns detailed health status

### Logging

All logs are structured JSON with:
- `timestamp` - ISO 8601 timestamp
- `level` - Log level (debug, info, warn, error)
- `message` - Log message
- `request_id` - Request ID for tracing
- Additional context fields

## Performance

### Targets

| Metric | Target | Notes |
|--------|--------|-------|
| Gateway Overhead | <50ms | Added latency |
| Throughput | >1000 req/sec | Concurrent requests |
| P99 Latency | <100ms | 99th percentile |
| Error Rate | <0.1% | Gateway errors only |

## Deployment

### Kubernetes

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: api-gateway
spec:
  replicas: 3
  selector:
    matchLabels:
      app: api-gateway
  template:
    metadata:
      labels:
        app: api-gateway
    spec:
      containers:
      - name: api-gateway
        image: api-gateway:latest
        ports:
        - containerPort: 8080
        env:
        - name: MARKET_DATA_URL
          value: "http://market-data:8083"
        - name: ORDER_MANAGEMENT_URL
          value: "http://order-management:8081"
        - name: STRATEGY_EXECUTOR_URL
          value: "http://strategy-executor:8082"
        livenessProbe:
          httpGet:
            path: /health/live
            port: 8080
          initialDelaySeconds: 30
          periodSeconds: 10
        readinessProbe:
          httpGet:
            path: /health/ready
            port: 8080
          initialDelaySeconds: 5
          periodSeconds: 5
```

## Documentation

- **[API Gateway Analysis](./API_GATEWAY_ANALYSIS.md)** - Complete analysis and architecture
- **[Implementation Checklist](./IMPLEMENTATION_CHECKLIST.md)** - Detailed implementation plan
- **[Implementation Summary](./IMPLEMENTATION_SUMMARY.md)** - High-level implementation summary

## Related Services

- [Market Data Service](../market-data/) - Market data ingestion and processing
- [Order Management Service](../order-management/) - Order lifecycle management
- [Strategy Executor Service](../strategy-executor/) - Trading strategy execution
- [Shared Package](../../shared/) - Common utilities and models

## Contributing

### Development Guidelines

1. **Code Style**: Follow Go conventions
2. **Testing**: Write tests for all new features (>80% coverage)
3. **Documentation**: Update documentation for new features
4. **Logging**: Use structured logging
5. **Metrics**: Add metrics for new operations
6. **Error Handling**: Use consistent error responses

### Pull Request Process

1. Fork the repository
2. Create a feature branch
3. Make changes with tests
4. Run `go test ./...`
5. Update documentation
6. Submit pull request

## Troubleshooting

### Common Issues

**1. Backend Service Connection Issues**
- Check backend service URLs in configuration
- Verify services are running and accessible
- Check network connectivity

**2. Rate Limiting Issues**
- Check rate limit configuration
- Verify client IP extraction
- Review rate limit metrics

**3. Circuit Breaker Tripping**
- Check backend service health
- Review circuit breaker thresholds
- Check error rates in metrics

### Debug Mode

Enable debug logging:

```bash
export LOG_LEVEL=debug
export LOG_FORMAT=console
./api-gateway
```

## License

See repository root for license information.

## Support

For issues and questions:
- Create an issue in the repository
- Check existing documentation
- Review troubleshooting guide

---

**Version**: 1.0.0  
**Status**: Planning Complete  
**Last Updated**: October 27, 2025

