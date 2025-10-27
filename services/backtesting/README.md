# Backtesting Service

A comprehensive backtesting service for validating trading strategies using historical market data, with support for parameter optimization and performance analysis.

**Version**: 1.0.0  
**Status**: 📋 Planning Complete - Ready for Implementation  
**Last Updated**: October 27, 2025

---

## Table of Contents

- [Overview](#overview)
- [Features](#features)
- [Architecture](#architecture)
- [Quick Start](#quick-start)
- [Configuration](#configuration)
- [API Reference](#api-reference)
- [Performance Metrics](#performance-metrics)
- [Integration](#integration)
- [Development](#development)
- [Testing](#testing)
- [Deployment](#deployment)
- [Troubleshooting](#troubleshooting)

---

## Overview

The Backtesting Service enables traders and developers to test trading strategies against historical market data without risking real capital. It provides:

- **Realistic Simulation**: Accurate market simulation with slippage and commissions
- **Strategy Testing**: Test existing strategies from strategy-executor
- **Parameter Optimization**: Find optimal strategy parameters using grid search
- **Performance Analytics**: Calculate 20+ performance metrics
- **Result Storage**: Persistent storage of backtest results
- **REST API**: Comprehensive API for backtest management

### Use Cases

1. **Strategy Validation**: Test new trading strategies before live deployment
2. **Parameter Tuning**: Optimize strategy parameters for better performance
3. **Risk Assessment**: Understand potential drawdowns and risks
4. **Strategy Comparison**: Compare multiple strategies side-by-side
5. **Historical Analysis**: Analyze how strategies perform in different market conditions

---

## Features

### ✅ Core Features

- **Historical Data Replay**
  - Time-based replay of market events
  - Support for trades, tickers, and order books
  - Multiple data sources (market-data service, files)
  - Data caching for faster reruns

- **Virtual Portfolio Management**
  - Realistic balance and position tracking
  - Accurate P&L calculation
  - Transaction cost accounting
  - Concurrent access safety

- **Market Simulation**
  - Order execution simulation
  - Configurable slippage models
  - Commission calculation
  - Order book simulation (optional)

- **Strategy Integration**
  - Reuse strategies from strategy-executor
  - Support for all strategy types
  - Custom strategy parameters
  - Strategy state management

- **Performance Analytics**
  - Total and annualized returns
  - Risk metrics (Sharpe, Sortino, max drawdown)
  - Trade statistics (win rate, profit factor)
  - Equity curve generation

- **Parameter Optimization**
  - Grid search optimization
  - Multi-parameter support
  - Parallel backtest execution
  - Best parameter selection

- **Result Management**
  - Persistent result storage
  - Result querying and filtering
  - Report generation (text, JSON, HTML)
  - Data export

### 🚀 API Features

- RESTful HTTP API
- Health checks (liveness, readiness)
- Prometheus metrics
- Progress tracking
- Cancellation support

---

## Architecture

### High-Level Architecture

```
┌─────────────────────────────────────────────────────────┐
│              Backtesting Service                         │
├─────────────────────────────────────────────────────────┤
│                                                           │
│  ┌──────────┐    ┌───────────┐    ┌──────────────┐     │
│  │   API    │───▶│  Manager  │───▶│   Engine     │     │
│  │ Handlers │    │           │    │   (Runner)   │     │
│  └──────────┘    └───────────┘    └──────────────┘     │
│                                            │             │
│                           ┌────────────────┴──────────┐ │
│                           │                           │ │
│                  ┌────────▼───────┐         ┌────────▼────────┐
│                  │   Simulator    │         │   Strategy      │
│                  │  (Virtual      │         │   Executor      │
│                  │   Market)      │         └─────────────────┘
│                  └────────┬───────┘                   │
│                           │                           │
│                  ┌────────▼───────┐         ┌────────▼────────┐
│                  │   Virtual      │         │  Performance    │
│                  │   Portfolio    │         │   Analyzer      │
│                  └────────────────┘         └─────────────────┘
│                           │                           │
│                           └───────────┬───────────────┘
│                                       │                         │
│                              ┌────────▼────────┐               │
│                              │  Result Storage │               │
│                              │  (Redis/File)   │               │
│                              └─────────────────┘               │
└─────────────────────────────────────────────────────────────────┘
         │                    │                    │
         ▼                    ▼                    ▼
  ┌────────────┐      ┌────────────┐      ┌────────────┐
  │  Market    │      │  Strategy  │      │   Order    │
  │   Data     │      │  Executor  │      │   Mgmt     │
  │  Service   │      │  (Library) │      │  (Logic)   │
  └────────────┘      └────────────┘      └────────────┘
```

### Component Responsibilities

| Component | Responsibility |
|-----------|----------------|
| **API Handlers** | HTTP request handling, validation, response formatting |
| **Backtest Manager** | Backtest lifecycle management, queuing, state tracking |
| **Engine (Runner)** | Main backtest execution, event loop, coordination |
| **Simulator** | Market simulation, order execution, slippage calculation |
| **Strategy Executor** | Strategy signal generation, event processing |
| **Virtual Portfolio** | Balance and position tracking, P&L calculation |
| **Performance Analyzer** | Metrics calculation, report generation |
| **Result Storage** | Result persistence, retrieval, filtering |

---

## Quick Start

### Prerequisites

- Go 1.21 or higher
- Redis (for result storage and caching)
- Market-data service running (for historical data)
- Strategy-executor library (for strategies)

### Installation

```bash
# Navigate to service directory
cd services/backtesting

# Install dependencies
go mod download

# Build the service
go build -o backtesting ./cmd/main.go

# Run the service
./backtesting
```

### Running with Docker

```bash
# Build Docker image
docker build -t backtesting:latest .

# Run container
docker run -p 8084:8084 \
  -e REDIS_HOST=redis \
  -e MARKET_DATA_BASE_URL=http://market-data:8083 \
  backtesting:latest
```

### Quick Example

```bash
# Create a backtest
curl -X POST http://localhost:8084/api/v1/backtests \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Basic Strategy Test",
    "start_date": "2024-01-01T00:00:00Z",
    "end_date": "2024-12-31T23:59:59Z",
    "book": "btc_mxn",
    "initial_balance": 100000.0,
    "strategy": "basic",
    "strategy_params": {
      "rsi_period": 14,
      "rsi_oversold": 30,
      "rsi_overbought": 70
    }
  }'

# Response
{
  "success": true,
  "data": {
    "id": "bt-123456",
    "status": "pending",
    "created_at": "2025-10-27T12:00:00Z"
  }
}

# Get backtest status
curl http://localhost:8084/api/v1/backtests/bt-123456

# Get backtest results
curl http://localhost:8084/api/v1/backtests/bt-123456/results
```

---

## Configuration

### Environment Variables

```bash
# Service Configuration
SERVICE_NAME=backtesting
SERVICE_VERSION=1.0.0
SERVICE_HOST=0.0.0.0
SERVICE_PORT=8084
ENVIRONMENT=development

# Market Data Service
MARKET_DATA_BASE_URL=http://localhost:8083
MARKET_DATA_TIMEOUT=30s
MARKET_DATA_RETRY_COUNT=3

# Redis Configuration
REDIS_HOST=localhost
REDIS_PORT=6379
REDIS_PASSWORD=
REDIS_DB=0
REDIS_POOL_SIZE=10

# Storage Configuration
STORAGE_TYPE=redis  # redis, file, s3
STORAGE_PATH=/var/lib/backtesting/results
STORAGE_RETENTION_DAYS=90

# Execution Settings
MAX_CONCURRENT_BACKTESTS=5
DEFAULT_SLIPPAGE_MODEL=percentage
DEFAULT_SLIPPAGE_VALUE=0.001
DEFAULT_COMMISSION_RATE=0.001

# Logging
LOG_LEVEL=info
LOG_FORMAT=json
LOG_OUTPUT=stdout

# Metrics
METRICS_ENABLED=true
METRICS_PATH=/metrics
METRICS_PORT=9094
```

### Configuration File (Optional)

```yaml
# config.yaml
service:
  name: backtesting
  port: 8084
  environment: production

market_data:
  base_url: http://market-data:8083
  timeout: 30s

redis:
  host: redis
  port: 6379
  db: 0

execution:
  max_concurrent: 10
  default_slippage: 0.001
  default_commission: 0.001
```

---

## API Reference

### Health Endpoints

#### GET /health
Full health check with all dependencies.

**Response**:
```json
{
  "status": "healthy",
  "timestamp": "2025-10-27T12:00:00Z",
  "checks": {
    "service": {"status": "healthy"},
    "redis": {"status": "healthy"},
    "market_data": {"status": "healthy"}
  }
}
```

#### GET /health/live
Liveness probe (Kubernetes).

#### GET /health/ready
Readiness probe (Kubernetes).

---

### Backtest Endpoints

#### POST /api/v1/backtests
Create a new backtest.

**Request**:
```json
{
  "name": "Basic Strategy Test",
  "description": "Testing basic strategy parameters",
  "start_date": "2024-01-01T00:00:00Z",
  "end_date": "2024-12-31T23:59:59Z",
  "book": "btc_mxn",
  "initial_balance": 100000.0,
  "strategy": "basic",
  "strategy_params": {
    "rsi_period": 14,
    "rsi_oversold": 30,
    "rsi_overbought": 70
  },
  "slippage_model": "percentage",
  "slippage_value": 0.001,
  "commission_rate": 0.001
}
```

**Response** (201 Created):
```json
{
  "success": true,
  "data": {
    "id": "bt-123456",
    "status": "pending",
    "created_at": "2025-10-27T12:00:00Z"
  }
}
```

---

#### GET /api/v1/backtests/{id}
Get backtest status.

**Response** (200 OK):
```json
{
  "success": true,
  "data": {
    "id": "bt-123456",
    "name": "Basic Strategy Test",
    "status": "running",
    "progress": 0.45,
    "started_at": "2025-10-27T12:00:00Z",
    "estimated_completion": "2025-10-27T12:05:00Z"
  }
}
```

---

#### GET /api/v1/backtests
List all backtests.

**Query Parameters**:
- `status` - Filter by status (pending, running, completed, failed, cancelled)
- `strategy` - Filter by strategy name
- `limit` - Maximum number of results (default: 20)
- `offset` - Offset for pagination (default: 0)

**Response** (200 OK):
```json
{
  "success": true,
  "data": {
    "backtests": [
      {
        "id": "bt-123456",
        "name": "Basic Strategy Test",
        "status": "completed",
        "created_at": "2025-10-27T12:00:00Z"
      }
    ],
    "total": 45,
    "limit": 20,
    "offset": 0
  }
}
```

---

#### POST /api/v1/backtests/{id}/cancel
Cancel a running backtest.

**Response** (200 OK):
```json
{
  "success": true,
  "data": {
    "id": "bt-123456",
    "status": "cancelled",
    "message": "Backtest cancelled successfully"
  }
}
```

---

#### DELETE /api/v1/backtests/{id}
Delete a backtest and its results.

**Response** (200 OK):
```json
{
  "success": true,
  "data": {
    "message": "Backtest deleted successfully"
  }
}
```

---

### Result Endpoints

#### GET /api/v1/backtests/{id}/results
Get complete backtest results.

**Response** (200 OK):
```json
{
  "success": true,
  "data": {
    "id": "bt-123456",
    "status": "completed",
    "summary": {
      "total_return": 15234.50,
      "total_return_percent": 15.23,
      "annualized_return": 15.85,
      "volatility": 0.23,
      "sharpe_ratio": 1.85,
      "sortino_ratio": 2.34,
      "max_drawdown": -5432.10,
      "max_drawdown_percent": -5.12,
      "total_trades": 127,
      "winning_trades": 83,
      "losing_trades": 44,
      "win_rate": 0.65,
      "profit_factor": 2.14
    },
    "trades": [...],
    "equity_curve": [...],
    "completed_at": "2025-10-27T12:04:32Z",
    "duration": 272
  }
}
```

---

#### GET /api/v1/backtests/{id}/summary
Get performance summary only.

**Response** (200 OK):
```json
{
  "success": true,
  "data": {
    "total_return": 15234.50,
    "sharpe_ratio": 1.85,
    "max_drawdown": -5432.10,
    "win_rate": 0.65,
    "total_trades": 127
  }
}
```

---

#### GET /api/v1/backtests/{id}/trades
Get trade history.

**Query Parameters**:
- `limit` - Maximum trades (default: 100)
- `offset` - Offset for pagination

**Response** (200 OK):
```json
{
  "success": true,
  "data": {
    "trades": [
      {
        "id": "trade-1",
        "entry_time": "2024-01-15T10:30:00Z",
        "exit_time": "2024-01-15T14:45:00Z",
        "side": "buy",
        "entry_price": 500000.0,
        "exit_price": 505000.0,
        "amount": 0.01,
        "profit_loss": 50.0,
        "profit_loss_percent": 1.0
      }
    ],
    "total": 127,
    "limit": 100,
    "offset": 0
  }
}
```

---

#### GET /api/v1/backtests/{id}/equity-curve
Get equity curve data.

**Response** (200 OK):
```json
{
  "success": true,
  "data": {
    "points": [
      {
        "timestamp": "2024-01-01T00:00:00Z",
        "balance": 100000.0,
        "equity": 100000.0,
        "return": 0.0
      },
      {
        "timestamp": "2024-01-01T06:00:00Z",
        "balance": 100234.50,
        "equity": 100234.50,
        "return": 0.00234
      }
    ]
  }
}
```

---

#### GET /api/v1/backtests/{id}/report
Download backtest report.

**Query Parameters**:
- `format` - Report format (text, json, html) (default: json)

**Response** (200 OK):
- Content-Type: application/json, text/plain, or text/html
- Report in requested format

---

### Optimization Endpoints

#### POST /api/v1/optimizations
Create parameter optimization.

**Request**:
```json
{
  "name": "RSI Parameter Optimization",
  "start_date": "2024-01-01T00:00:00Z",
  "end_date": "2024-12-31T23:59:59Z",
  "book": "btc_mxn",
  "initial_balance": 100000.0,
  "strategy": "basic",
  "param_ranges": {
    "rsi_period": {"min": 10, "max": 20, "step": 2},
    "rsi_oversold": {"min": 20, "max": 35, "step": 5},
    "rsi_overbought": {"min": 65, "max": 80, "step": 5}
  },
  "optimization_metric": "sharpe_ratio"
}
```

**Response** (201 Created):
```json
{
  "success": true,
  "data": {
    "id": "opt-789012",
    "total_combinations": 96,
    "status": "running",
    "progress": 0.0
  }
}
```

---

#### GET /api/v1/optimizations/{id}/results
Get optimization results.

**Response** (200 OK):
```json
{
  "success": true,
  "data": {
    "id": "opt-789012",
    "status": "completed",
    "best_params": {
      "rsi_period": 14,
      "rsi_oversold": 30,
      "rsi_overbought": 70
    },
    "best_result": {
      "sharpe_ratio": 2.14,
      "total_return_percent": 18.5
    },
    "all_results": [...]
  }
}
```

---

### Metrics Endpoint

#### GET /metrics
Prometheus metrics.

**Metrics**:
- `backtests_created_total` - Total backtests created
- `backtests_completed_total{status}` - Completed backtests by status
- `backtest_duration_seconds` - Backtest execution duration
- `active_backtests` - Number of currently running backtests
- `events_processed_total{type}` - Market events processed
- `service_uptime_seconds` - Service uptime

---

## Performance Metrics

### Available Metrics

| Metric | Description | Formula |
|--------|-------------|---------|
| **Total Return** | Absolute profit/loss | Final Balance - Initial Balance |
| **Total Return %** | Percentage return | (Final / Initial - 1) × 100 |
| **Annualized Return** | Yearly return rate | (1 + Total Return)^(365/days) - 1 |
| **Volatility** | Return volatility | StdDev(daily returns) × √252 |
| **Sharpe Ratio** | Risk-adjusted return | (Return - Risk Free) / Volatility |
| **Sortino Ratio** | Downside risk-adjusted | Return / Downside Volatility |
| **Max Drawdown** | Largest peak-to-trough decline | Max(Peak - Trough) |
| **Max Drawdown %** | Percentage drawdown | Max Drawdown / Peak × 100 |
| **Win Rate** | Percentage of profitable trades | Winning Trades / Total Trades |
| **Profit Factor** | Profit to loss ratio | Gross Profit / Gross Loss |
| **Average Win** | Average profitable trade | Sum(Winning P&L) / Winning Trades |
| **Average Loss** | Average losing trade | Sum(Losing P&L) / Losing Trades |

### Interpretation Guide

**Sharpe Ratio**:
- < 1.0: Subpar
- 1.0 - 1.9: Good
- 2.0 - 2.9: Very good
- \> 3.0: Excellent

**Win Rate**:
- < 50%: Need high profit factor
- 50-60%: Typical range
- \> 60%: Very good

**Profit Factor**:
- < 1.0: Losing strategy
- 1.0 - 1.5: Marginal
- 1.5 - 2.0: Good
- \> 2.0: Excellent

---

## Integration

### Integration with Market-Data Service

The backtesting service loads historical market data from the market-data service:

```go
// Configure market-data client
MARKET_DATA_BASE_URL=http://market-data:8083

// API calls made:
GET /api/v1/trades?book={book}&from={start}&to={end}&limit=10000
GET /api/v1/ticker/history?book={book}&from={start}&to={end}
```

**Data Flow**:
1. Backtest requests historical data
2. Market-data service returns trade events
3. Events are cached in Redis
4. Backtest replays events in time order

### Integration with Strategy-Executor

Strategies from strategy-executor can be reused:

```go
import "bitso-trading-platform/strategy-executor/internal/strategies"

// Use existing strategy
strategy := strategies.NewBasicStrategy(params)
```

**Strategies Available**:
- Basic Strategy (RSI-based)
- Trend Following Strategy
- Arbitrage Strategy

### Integration with Order-Management

Order validation logic can be reused for realistic backtesting:

```go
// Reuse validation rules
import "bitso-trading-platform/order-management/internal/validator"

// Apply same validation as live trading
validator := validator.NewOrderValidator(config)
```

---

## Development

### Project Structure

```
services/backtesting/
├── cmd/
│   └── main.go                         # Application entry point
├── internal/
│   ├── analyzer/                       # Performance analysis
│   ├── api/                            # HTTP handlers
│   ├── config/                         # Configuration
│   ├── data/                           # Data providers
│   ├── engine/                         # Backtest engine
│   ├── logger/                         # Logging
│   ├── manager/                        # Backtest management
│   ├── metrics/                        # Prometheus metrics
│   ├── models/                         # Domain models
│   ├── optimizer/                      # Parameter optimization
│   ├── portfolio/                      # Virtual portfolio
│   ├── server/                         # HTTP server
│   ├── simulator/                      # Market simulation
│   ├── storage/                        # Result storage
│   └── strategy/                       # Strategy execution
├── test/
│   ├── integration/                    # Integration tests
│   └── fixtures/                       # Test data
├── Dockerfile
├── go.mod
├── go.sum
├── README.md                           # This file
├── BACKTESTING_IMPLEMENTATION_PLAN.md  # Detailed implementation plan
└── FILES_AND_METHODS_CHECKLIST.md     # Implementation checklist
```

### Local Development

```bash
# Install dependencies
go mod download

# Run tests
go test ./...

# Run with debug logging
export LOG_LEVEL=debug
go run cmd/main.go

# Build binary
go build -o backtesting ./cmd/main.go

# Run binary
./backtesting
```

---

## Testing

### Running Tests

```bash
# Run all tests
go test ./...

# Run specific package
go test ./internal/engine/... -v

# Run with coverage
go test ./... -coverprofile=coverage.out
go tool cover -html=coverage.out

# Run benchmarks
go test ./... -bench=. -benchmem

# Run integration tests
go test ./test/integration/... -v
```

### Test Coverage

- **Target**: >80% code coverage
- **Unit Tests**: All packages
- **Integration Tests**: Critical flows
- **Benchmark Tests**: Performance validation

---

## Deployment

### Docker Deployment

```bash
# Build image
docker build -t backtesting:latest .

# Run container
docker run -d \
  --name backtesting \
  -p 8084:8084 \
  -e REDIS_HOST=redis \
  -e MARKET_DATA_BASE_URL=http://market-data:8083 \
  backtesting:latest
```

### Kubernetes Deployment

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: backtesting
spec:
  replicas: 3
  selector:
    matchLabels:
      app: backtesting
  template:
    metadata:
      labels:
        app: backtesting
    spec:
      containers:
      - name: backtesting
        image: backtesting:latest
        ports:
        - containerPort: 8084
        env:
        - name: REDIS_HOST
          value: "redis"
        - name: MARKET_DATA_BASE_URL
          value: "http://market-data:8083"
        livenessProbe:
          httpGet:
            path: /health/live
            port: 8084
          initialDelaySeconds: 30
          periodSeconds: 10
        readinessProbe:
          httpGet:
            path: /health/ready
            port: 8084
          initialDelaySeconds: 5
          periodSeconds: 5
```

---

## Troubleshooting

### Common Issues

**1. Backtest Runs Slowly**
- Check data volume (reduce date range)
- Verify Redis caching is working
- Increase `MAX_CONCURRENT_BACKTESTS`

**2. Out of Memory**
- Reduce batch size
- Use streaming data provider
- Limit concurrent backtests

**3. Historical Data Not Found**
- Verify market-data service is running
- Check date range has data
- Verify book name is correct

**4. Redis Connection Fails**
- Check Redis is running
- Verify `REDIS_HOST` and `REDIS_PORT`
- Check network connectivity

### Debug Mode

```bash
# Enable debug logging
export LOG_LEVEL=debug
export LOG_FORMAT=console
./backtesting
```

### Monitoring

```bash
# Check service health
curl http://localhost:8084/health

# View metrics
curl http://localhost:8084/metrics

# Check active backtests
curl http://localhost:8084/api/v1/backtests?status=running
```

---

## Documentation

- **[Implementation Plan](./BACKTESTING_IMPLEMENTATION_PLAN.md)** - Complete implementation specification
- **[Files & Methods Checklist](./FILES_AND_METHODS_CHECKLIST.md)** - Detailed implementation checklist
- **[API Documentation](./API.md)** - Complete API reference (TODO)

## Related Services

- [Market-Data Service](../market-data/) - Historical market data
- [Strategy-Executor Service](../strategy-executor/) - Trading strategies
- [Order-Management Service](../order-management/) - Order validation logic
- [Shared Package](../../shared/) - Common utilities and models

## Contributing

1. Follow Go conventions and best practices
2. Write tests for all new features (>80% coverage)
3. Update documentation for changes
4. Use structured logging
5. Add Prometheus metrics for new operations

## License

See repository root for license information.

## Support

For issues and questions:
- Create an issue in the repository
- Check existing documentation
- Review troubleshooting guide

---

**Version**: 1.0.0  
**Status**: 📋 Planning Complete - Ready for Implementation  
**Last Updated**: October 27, 2025

