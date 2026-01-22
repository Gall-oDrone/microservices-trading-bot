# Strategy Executor Service

A production-ready microservice for executing trading strategies, processing market data, and generating trading signals in real-time.

## Table of Contents

- [Overview](#overview)
- [Features](#features)
- [Architecture](#architecture)
- [Getting Started](#getting-started)
- [Configuration](#configuration)
- [API Reference](#api-reference)
- [Testing](#testing)
- [Monitoring](#monitoring)
- [Deployment](#deployment)
- [Additional Features (Suggested)](#additional-features-suggested)

## Overview

The Strategy Executor Service is a core component of the trading bot platform that:
- Consumes real-time market data from Kafka
- Executes multiple trading strategies concurrently
- Applies risk management rules
- Generates and publishes trading signals
- Provides HTTP API for management and monitoring

## Features

### Core Functionality

✅ **Multi-Strategy Support**
- Built-in strategies: Basic, Trend Following, Arbitrage
- Dynamic strategy registration via factory pattern
- Concurrent strategy execution
- Per-strategy configuration

✅ **Market Data Integration**
- Kafka consumer for real-time data (trades, tickers, order books)
- HTTP client for market-data service API
- Event filtering and processing
- Book-based subscriptions

✅ **Risk Management**
- Trade amount validation
- Position limits enforcement
- Stop-loss and take-profit calculations
- Trading hours validation

✅ **Signal Publishing**
- Kafka producer for trading signals
- Batch processing for efficiency
- Error handling and retries
- Signal tracking and metrics

✅ **Observability**
- Structured logging with zerolog
- Comprehensive Prometheus metrics
- Health checks (liveness, readiness)
- Request tracing

✅ **Production Ready**
- Graceful shutdown
- Error recovery
- Configuration management
- Thread-safe operations

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                  Strategy Executor Service                   │
├─────────────────────────────────────────────────────────────┤
│                                                               │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐      │
│  │   Consumer   │  │   Processor  │  │   Manager    │      │
│  │  (Kafka)     │─▶│  (Events)    │─▶│ (Strategies) │      │
│  └──────────────┘  └──────────────┘  └──────────────┘      │
│         │                                      │              │
│         │                                      ▼              │
│         │                            ┌──────────────┐       │
│         │                            │  Publisher   │       │
│         │                            │   (Kafka)    │       │
│         │                            └──────────────┘       │
│         │                                      │              │
└─────────┼──────────────────────────────────────┼────────────┘
          │                                      │
          │                                      │
    ┌─────▼──────┐                        ┌────▼──────┐
    │  Market    │                        │  Order    │
    │   Data     │                        │  Mgmt     │
    │  Service   │                        │  Service  │
    └────────────┘                        └───────────┘
```

### Component Diagram

```
Internal Components:
┌────────────┐
│   Config   │ → Configuration management
└────────────┘
┌────────────┐
│   Logger   │ → Structured logging
└────────────┘
┌────────────┐
│  Metrics   │ → Performance metrics
└────────────┘
┌────────────┐
│   Health   │ → Health checks
└────────────┘
┌────────────┐
│   Server   │ → HTTP API
└────────────┘
┌────────────┐
│  Consumer  │ → Kafka consumer
└────────────┘
┌────────────┐
│   Client   │ → HTTP client
└────────────┘
┌────────────┐
│ Processor  │ → Event processing
└────────────┘
┌────────────┐
│  Registry  │ → Strategy registry
└────────────┘
┌────────────┐
│  Manager   │ → Strategy manager
└────────────┘
┌────────────┐
│ Publisher  │ → Signal publisher
└────────────┘
┌────────────┐
│    Risk    │ → Risk management
└────────────┘
```

## Getting Started

### Prerequisites

- Go 1.21 or higher
- Kafka cluster
- Market-data service running
- Redis (optional, for caching)

### Installation

```bash
# Clone the repository
cd services/strategy-executor

# Install dependencies
go mod download

# Build the service
go build -o strategy-executor ./cmd/main.go

# Run the service
./strategy-executor
```

### Quick Start

```bash
# Set environment variables
export KAFKA_BROKERS=localhost:9092
export MARKET_DATA_BASE_URL=http://localhost:8081
export DEFAULT_BOOK=btc_mxn
export DEFAULT_STRATEGY=basic

# Run the service
./strategy-executor
```

## Configuration

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| **Service Configuration** |||
| `SERVICE_NAME` | strategy-executor | Service name |
| `SERVICE_VERSION` | 1.0.0 | Service version |
| `SERVICE_HOST` | 0.0.0.0 | HTTP server host |
| `SERVICE_PORT` | 8080 | HTTP server port |
| `ENVIRONMENT` | development | Environment (dev/staging/prod) |
| **Kafka Configuration** |||
| `KAFKA_BROKERS` | localhost:9092 | Kafka broker addresses (comma-separated) |
| `KAFKA_CONSUMER_GROUP` | strategy-executor-group | Consumer group ID |
| `KAFKA_TOPIC_MARKET_DATA_TRADES` | market-data.trades | Trades topic |
| `KAFKA_TOPIC_MARKET_DATA_TICKERS` | market-data.tickers | Tickers topic |
| `KAFKA_TOPIC_MARKET_DATA_ORDERBOOK` | market-data.orderbook | Order book topic |
| `KAFKA_TOPIC_SIGNALS` | strategy-executor.signals | Signals output topic |
| `KAFKA_TOPIC_EVENTS` | strategy-executor.events | Events output topic |
| **Market Data Configuration** |||
| `MARKET_DATA_BASE_URL` | http://localhost:8081 | Market-data service URL |
| `MARKET_DATA_TIMEOUT` | 30s | API request timeout |
| `MARKET_DATA_RETRY_COUNT` | 3 | Retry count for failed requests |
| **Strategy Configuration** |||
| `DEFAULT_BOOK` | btc_mxn | Default trading book |
| `DEFAULT_STRATEGY` | basic | Default strategy type |
| **Risk Management** |||
| `MAX_OPEN_POSITIONS` | 3 | Maximum concurrent positions |
| `MAX_TRADE_AMOUNT` | 0.1 | Maximum trade amount |
| `MIN_TRADE_AMOUNT` | 0.001 | Minimum trade amount |
| `MAX_TRADE_VALUE` | 10000.0 | Maximum trade value |
| `STOP_LOSS_PERCENT` | 2.0 | Stop loss percentage |
| `TAKE_PROFIT_PERCENT` | 4.0 | Take profit percentage |
| `MAX_TRADING_TIME` | 24h | Maximum position hold time |
| **Logging** |||
| `LOG_LEVEL` | info | Log level (trace/debug/info/warn/error) |
| `LOG_FORMAT` | json | Log format (json/console) |
| **Metrics** |||
| `METRICS_ENABLED` | true | Enable metrics collection |
| `METRICS_PATH` | /metrics | Metrics endpoint path |
| `METRICS_PORT` | 9090 | Metrics server port |

## API Reference

### Health Endpoints

#### GET /health
Returns detailed health status with all checks.

**Response:**
```json
{
  "status": "healthy",
  "timestamp": "2025-10-24T12:00:00Z",
  "checks": {
    "service": {
      "name": "service",
      "status": "healthy",
      "timestamp": "2025-10-24T12:00:00Z",
      "duration": 599
    }
  },
  "version": "1.0.0",
  "service": "strategy-executor"
}
```

#### GET /health/ready
Returns 200 if service is ready to accept traffic.

#### GET /health/live
Returns 200 if service is alive.

### Strategy Management Endpoints

#### GET /api/v1/status
Returns service status.

**Response:**
```json
{
  "service": "strategy-executor",
  "status": "running",
  "time": "2025-10-24T12:00:00Z"
}
```

#### GET /api/v1/strategies
Lists all active strategies.

**Response:**
```json
[
  {
    "name": "basic",
    "status": "active",
    "book": "btc_mxn"
  }
]
```

#### POST /api/v1/strategies/{name}/start
Starts a strategy.

#### POST /api/v1/strategies/{name}/stop
Stops a strategy.

#### GET /api/v1/strategies/{name}
Gets strategy status.

#### PUT /api/v1/strategies/{name}/config
Updates strategy configuration.

### Metrics Endpoint

#### GET /metrics
Returns Prometheus metrics.

## Testing

### Run All Tests

```bash
go test ./...
```

### Run Specific Package Tests

```bash
# Configuration tests
go test ./internal/config/... -v

# Consumer tests
go test ./internal/consumer/... -v

# Processor tests
go test ./internal/processor/... -v

# Strategy tests
go test ./internal/strategies/... -v

# Manager tests
go test ./internal/manager/... -v

# Publisher tests
go test ./internal/publisher/... -v
```

### Run Tests with Race Detection

```bash
go test ./... -race
```

### Test Statistics

- **Total Tests:** 60+
- **Packages with Tests:** 6
- **Test Coverage:** High for critical components
- **All Tests:** ✅ Passing

## Monitoring

### Metrics Categories

1. **Service Metrics**
   - Uptime
   - Health status
   - Active strategies count

2. **Strategy Metrics**
   - Executions per strategy
   - Errors per strategy
   - Execution latency
   - Signals generated

3. **Market Data Metrics**
   - Messages received/processed
   - Processing latency
   - Errors by topic and book

4. **Signal Metrics**
   - Signals generated/published
   - Publishing latency
   - Failed publications

5. **Risk Metrics**
   - Risk checks performed
   - Violations detected
   - Check latency

6. **Kafka Metrics**
   - Messages consumed/produced
   - Consumer lag
   - Producer latency

7. **HTTP Metrics**
   - Requests total
   - Request duration
   - In-flight requests

8. **Business Metrics**
   - Buy/sell/hold signals
   - Signals per strategy
   - Signals per book

### Logging

All logs are structured JSON format with fields:
- `timestamp`: ISO 8601 timestamp
- `level`: Log level
- `message`: Log message
- `service`: Service name
- Additional context fields

Example log:
```json
{
  "timestamp": "2025-10-24T12:00:00Z",
  "level": "info",
  "message": "Strategy 'basic' started successfully",
  "service": "strategy-executor",
  "strategy": "basic",
  "book": "btc_mxn"
}
```

## Deployment

### Docker

```bash
# Build Docker image
docker build -t strategy-executor:latest .

# Run container
docker run -p 8080:8080 \
  -e KAFKA_BROKERS=kafka:9092 \
  -e MARKET_DATA_BASE_URL=http://market-data:8081 \
  strategy-executor:latest
```

### Kubernetes

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: strategy-executor
spec:
  replicas: 3
  selector:
    matchLabels:
      app: strategy-executor
  template:
    metadata:
      labels:
        app: strategy-executor
    spec:
      containers:
      - name: strategy-executor
        image: strategy-executor:latest
        ports:
        - containerPort: 8080
        env:
        - name: KAFKA_BROKERS
          value: "kafka:9092"
        - name: MARKET_DATA_BASE_URL
          value: "http://market-data:8081"
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

## Additional Features (Suggested)

### 🚀 High Priority

#### 1. **Backtesting Framework**
- Historical data replay
- Strategy performance evaluation
- Parameter optimization
- Walk-forward analysis
- Sharpe ratio and other metrics

**Benefits:**
- Validate strategies before live trading
- Optimize parameters
- Risk assessment

**Estimated Complexity:** Medium
**Impact:** High

---

#### 2. **Machine Learning Strategy**
- ML-based signal generation
- Feature engineering from market data
- Model training and inference
- Online learning capability
- A/B testing framework

**Benefits:**
- Adaptive strategies
- Pattern recognition
- Superior performance

**Estimated Complexity:** High
**Impact:** Very High

---

#### 3. **Position Tracking**
- Track open positions
- P&L calculation
- Position history
- Portfolio management
- Exposure monitoring

**Benefits:**
- Better risk management
- Portfolio optimization
- Performance tracking

**Estimated Complexity:** Medium
**Impact:** High

---

#### 4. **Advanced Risk Management**
- Portfolio-level risk limits
- Correlation analysis
- VaR (Value at Risk) calculation
- Drawdown limits
- Dynamic position sizing

**Benefits:**
- Reduced risk exposure
- Better capital allocation
- Regulatory compliance

**Estimated Complexity:** Medium-High
**Impact:** High

---

### 💡 Medium Priority

#### 5. **Strategy Optimizer**
- Genetic algorithm for parameter optimization
- Grid search and random search
- Cross-validation
- Overfitting prevention
- Real-time parameter adjustment

**Benefits:**
- Optimal strategy parameters
- Continuous improvement
- Automated optimization

**Estimated Complexity:** High
**Impact:** Medium-High

---

#### 6. **Multi-Timeframe Analysis**
- Support for multiple timeframes (1m, 5m, 15m, 1h, 1d)
- Timeframe alignment
- Cross-timeframe signals
- Trend confirmation

**Benefits:**
- Better signal quality
- Reduced false positives
- Multi-dimensional analysis

**Estimated Complexity:** Medium
**Impact:** Medium-High

---

#### 7. **Strategy Composition**
- Combine multiple strategies
- Weighted signal aggregation
- Ensemble methods
- Strategy voting system

**Benefits:**
- Diversification
- Reduced risk
- Better performance

**Estimated Complexity:** Medium
**Impact:** Medium

---

#### 8. **Real-Time Dashboard**
- Web-based UI
- Real-time strategy performance
- Signal visualization
- P&L tracking
- Risk metrics display

**Benefits:**
- Better visibility
- Quick decision making
- Performance monitoring

**Estimated Complexity:** High
**Impact:** Medium

---

### 🔧 Low Priority (Infrastructure)

#### 9. **Distributed Caching**
- Redis integration for market data caching
- Strategy state persistence
- Session management
- Cache invalidation

**Benefits:**
- Faster data access
- Reduced API calls
- State recovery

**Estimated Complexity:** Low-Medium
**Impact:** Medium

---

#### 10. **Circuit Breaker Pattern**
- Protect against cascading failures
- Automatic recovery
- Fallback mechanisms
- Rate limiting per service

**Benefits:**
- Improved resilience
- Service protection
- Better error handling

**Estimated Complexity:** Low
**Impact:** Medium

---

#### 11. **Distributed Tracing**
- Jaeger integration
- Request tracing across services
- Performance profiling
- Bottleneck identification

**Benefits:**
- Better debugging
- Performance optimization
- Service dependency visualization

**Estimated Complexity:** Low-Medium
**Impact:** Medium

---

#### 12. **Service Discovery**
- Consul/etcd integration
- Dynamic service registration
- Health-based routing
- Load balancing

**Benefits:**
- Dynamic scaling
- Better availability
- Service mesh integration

**Estimated Complexity:** Low-Medium
**Impact:** Low-Medium

---

### 🎯 Advanced Features

#### 13. **Sentiment Analysis**
- News sentiment integration
- Social media sentiment
- Market sentiment indicators
- Alternative data sources

**Benefits:**
- Enhanced signal quality
- Early trend detection
- Market psychology insights

**Estimated Complexity:** High
**Impact:** Medium-High

---

#### 14. **Order Flow Analysis**
- Order book imbalance detection
- Large order detection
- Market microstructure analysis
- Liquidity analysis

**Benefits:**
- Better entry/exit timing
- Market impact reduction
- Liquidity optimization

**Estimated Complexity:** High
**Impact:** Medium-High

---

#### 15. **Smart Order Routing**
- Split orders across multiple exchanges
- VWAP execution
- TWAP execution
- Iceberg orders
- Minimize slippage

**Benefits:**
- Better execution prices
- Reduced market impact
- Optimal order placement

**Estimated Complexity:** Very High
**Impact:** High

---

#### 16. **Portfolio Rebalancing**
- Automatic portfolio rebalancing
- Target allocation enforcement
- Tax-loss harvesting
- Drift monitoring

**Benefits:**
- Maintain target allocation
- Risk control
- Tax optimization

**Estimated Complexity:** Medium-High
**Impact:** Medium

---

#### 17. **Alert System**
- Price alerts
- Strategy performance alerts
- Risk limit alerts
- System health alerts
- Multi-channel notifications (email, SMS, Slack)

**Benefits:**
- Proactive monitoring
- Quick response to issues
- Better oversight

**Estimated Complexity:** Medium
**Impact:** Medium

---

#### 18. **Strategy Marketplace**
- Plugin system for custom strategies
- Strategy versioning
- Strategy sharing
- Community strategies
- Performance leaderboard

**Benefits:**
- Extensibility
- Community engagement
- Innovation

**Estimated Complexity:** Very High
**Impact:** Low-Medium

---

## Development Priorities

### Recommended Implementation Order

1. **Phase 1 (Immediate):**
   - Position Tracking
   - Advanced Risk Management
   - Circuit Breaker Pattern

2. **Phase 2 (Short-term):**
   - Backtesting Framework
   - Multi-Timeframe Analysis
   - Strategy Optimizer

3. **Phase 3 (Medium-term):**
   - Machine Learning Strategy
   - Real-Time Dashboard
   - Alert System

4. **Phase 4 (Long-term):**
   - Order Flow Analysis
   - Sentiment Analysis
   - Smart Order Routing

---

## Contributing

See [DEVELOPER-GUIDE.md](../../DEVELOPER-GUIDE.md) for development guidelines.

## License

See repository root for license information.

## Support

For issues and questions, please open an issue in the repository.
