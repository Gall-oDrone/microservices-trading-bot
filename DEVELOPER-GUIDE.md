# Developer Guide: Microservices Architecture

## Quick Start

### Working with Shared Packages

The `shared/` module contains common code used across all services:

```go
// Import shared packages
import (
    "bitso-trading-platform/shared/pkg/bitso"
    "bitso-trading-platform/shared/pkg/config"
    "bitso-trading-platform/shared/pkg/database"
    "bitso-trading-platform/shared/pkg/models"
    "bitso-trading-platform/shared/pkg/utils"
)
```

### Service Structure

Each service follows this structure:

```
services/<service-name>/
├── cmd/
│   └── main.go           # Service entry point
├── internal/             # Private application code
│   ├── <domain>/         # Domain-specific packages
│   └── ...
├── go.mod                # Service dependencies
└── Dockerfile            # Container definition
```

## Shared Packages Reference

### 1. Models (`shared/pkg/models/`)

```go
// TradingConfig - Configuration for trading operations
config := models.NewTradingConfig()
config.Book = bitso.NewBook(bitso.BTC, bitso.MXN)
config.MinTradeAmount = 0.001
config.MaxTradeAmount = 0.1
config.MaxOpenPositions = 3

// Validate configuration
if err := config.Validate(); err != nil {
    log.Fatal(err)
}

// Check trading limits
if config.IsWithinTradeLimits(amount, price) {
    // Execute trade
}

// TradeSignalEvent - Event for trade signals
event := &models.TradeSignalEvent{
    EventID:   "buy-123",
    Timestamp: time.Now().Unix(),
    Book:      "btc_mxn",
    Strategy:  "basic",
    Signal:    "BUY",
    Price:     1000000.00,
    Amount:    0.001,
    Metadata:  map[string]interface{}{"reason": "Price dip"},
}
```

### 2. Database (`shared/pkg/database/`)

```go
// Initialize Redis client
client, err := database.InitializeWithConfig(
    "localhost",  // host
    6379,         // port
    "",           // password
    0,            // db
    10,           // pool size
)
if err != nil {
    log.Fatal(err)
}
defer client.Close()

// Save ticker
ticker := bitso.Ticker{Book: "btc_mxn", ...}
err = client.SaveTicker(ticker)

// Get user balance
balance, err := client.GetUserBalance("btc")

// Save user order
order := bitso.UserOrder{OID: "12345", ...}
err = client.SaveUserOrder(order)

// Get all orders
orders, err := client.GetAllUserOrders()
```

### 3. Config (`shared/pkg/config/`)

```go
// Load configuration from environment
cfg, err := config.LoadConfig()
if err != nil {
    log.Fatal(err)
}

// Validate configuration
if err := cfg.Validate(); err != nil {
    log.Fatal(err)
}

// Access configuration values
apiKey := cfg.BitsoAPIKey
redisHost := cfg.RedisHost
kafkaBrokers := cfg.KafkaBrokers
```

### 4. Utils (`shared/pkg/utils/`)

```go
// Rate limiting
limiter := utils.NewRateLimiter()
limiter.UpdatePublicAPIRequest()
limiter.UpdatePrivateAPIRequest()

// Rate calculations
rate, err := utils.NewRate(bidRate, askRate, amount)
if err != nil {
    log.Fatal(err)
}

spread := rate.GetSpread()
isProfitable := rate.IsProfitable(utils.MarketSideBuy, 1.5)

// Utilities
randomID := utils.GenRandomId()
randomOID := utils.GenRandomOId(16)
timestamp := utils.MillisToLocal(milliseconds)

// Balance retrieval
balance, err := utils.GetBalance(bitso.BTC, balances)
```

## Service-Specific Development

### Strategy Executor Service

```go
import (
    "bitso-trading-platform/services/strategy-executor/internal/strategies"
    "bitso-trading-platform/services/strategy-executor/internal/behaviors"
    "bitso-trading-platform/services/strategy-executor/internal/risk"
    "bitso-trading-platform/services/strategy-executor/internal/signals"
    "bitso-trading-platform/shared/pkg/bitso"
)

// Create a strategy
book := bitso.NewBook(bitso.BTC, bitso.MXN)
strategy := strategies.NewBasicStrategy(&book)

// Execute strategy
ticker, _ := bitsoClient.Ticker(&book)
err := strategy.Execute(ticker)

// Listen to signals
buySignals := strategy.GetBuySignalChannel()
sellSignals := strategy.GetSellSignalChannel()

go func() {
    for signal := range buySignals {
        // Process buy signal
        log.Printf("Buy signal: %s", signal.Reason)
    }
}()

// Risk management
riskMgr := risk.NewManager(tradingConfig)
err = riskMgr.ValidateTradeAmount(amount, price)
err = riskMgr.CheckPositionLimits(currentPositions)
stopLoss := riskMgr.CalculateStopLoss(entryPrice, true)
```

### Trading Engine Service

```go
import (
    "bitso-trading-platform/services/trading-engine/internal/engine"
    "bitso-trading-platform/services/trading-engine/internal/execution"
    "bitso-trading-platform/shared/pkg/bitso"
    "bitso-trading-platform/shared/pkg/models"
)

// Create trading engine
tradingEngine := engine.NewTradingEngine(
    tradingConfig,
    appConfig,
    bitsoClient,
    dbClient,
)

// Initialize and start
err := tradingEngine.Initialize()
if err != nil {
    log.Fatal(err)
}

err = tradingEngine.Start()
if err != nil {
    log.Fatal(err)
}

// Process trade signals
signal := &models.TradeSignalEvent{
    Signal: "BUY",
    Book:   "btc_mxn",
    Price:  1000000.00,
    Amount: 0.001,
}

err = tradingEngine.ProcessTradeSignal(signal)
```

## Building and Testing

### Build All Services

```bash
# Build shared module
cd shared && go build ./...

# Build strategy-executor
cd services/strategy-executor && go build ./...

# Build trading-engine
cd services/trading-engine && go build ./...
```

### Run Tests

```bash
# Test shared module
cd shared && go test ./...

# Test strategy-executor
cd services/strategy-executor && go test ./...

# Test trading-engine
cd services/trading-engine && go test ./...
```

### Update Dependencies

```bash
# Update shared module dependencies
cd shared && go mod tidy

# Update service dependencies
cd services/strategy-executor && go mod tidy
cd services/trading-engine && go mod tidy
```

## Common Development Tasks

### Adding a New Strategy

1. Create strategy file in `services/strategy-executor/internal/strategies/`
2. Implement the `Strategy` interface
3. Embed `*BaseStrategy` for common functionality
4. Implement the `Execute()` method

```go
type MyStrategy struct {
    *BaseStrategy
    // Custom fields
}

func NewMyStrategy(book *bitso.Book) *MyStrategy {
    return &MyStrategy{
        BaseStrategy: NewBaseStrategy("my_strategy"),
    }
}

func (s *MyStrategy) Execute(ticker *bitso.Ticker) error {
    // Strategy logic
    // Use SendBuySignal() or SendSellSignal() to emit signals
    return nil
}
```

### Adding a New Shared Model

1. Add model to `shared/pkg/models/`
2. Run `go mod tidy` in shared directory
3. Use in any service that imports shared

```go
// In shared/pkg/models/my_model.go
package models

type MyModel struct {
    Field1 string
    Field2 int
}

func NewMyModel() *MyModel {
    return &MyModel{}
}
```

### Environment Variables

Create a `.env` file in the project root:

```env
# Bitso API
BITSO_API_KEY=your_api_key
BITSO_API_SECRET=your_api_secret
STAGE_BITSO_API_KEY=your_stage_api_key
STAGE_BITSO_API_SECRET=your_stage_api_secret

# Redis
REDIS_HOST=localhost
REDIS_PORT=6379
REDIS_PASSWORD=
REDIS_DB=0

# Kafka (for future use)
KAFKA_BROKERS=localhost:9092
KAFKA_GROUP_ID=trading-bot

# Service
SERVICE_NAME=trading-engine
SERVICE_PORT=8080
```

## Best Practices

1. **Use Shared Packages**: Always use shared packages for common functionality
2. **Dependency Injection**: Pass dependencies through constructors
3. **Error Handling**: Return errors, don't panic
4. **Logging**: Use structured logging with context
5. **Configuration**: Use environment variables via config package
6. **Testing**: Write unit tests for business logic
7. **Documentation**: Document exported functions and types

## Troubleshooting

### Import Errors

If you see import errors, make sure:
1. You've run `go mod tidy` in the shared module
2. You've run `go mod tidy` in the service
3. The replace directive points to the correct shared path

### Build Errors

```bash
# Clean module cache
go clean -modcache

# Re-download dependencies
cd shared && go mod download
cd services/strategy-executor && go mod download
cd services/trading-engine && go mod download
```

### Redis Connection Issues

```bash
# Check Redis is running
redis-cli ping

# Start Redis if needed
redis-server
```

## Directory Structure Reference

```
microservices-trading-bot/
├── shared/                      # Shared packages
│   ├── pkg/
│   │   ├── bitso/              # Bitso API client
│   │   ├── config/             # Configuration
│   │   ├── database/           # Database clients
│   │   ├── models/             # Data models
│   │   └── utils/              # Utilities
│   └── go.mod
│
├── services/
│   ├── strategy-executor/      # Strategy execution service
│   │   ├── internal/
│   │   │   ├── strategies/
│   │   │   ├── behaviors/
│   │   │   ├── risk/
│   │   │   └── signals/
│   │   └── go.mod
│   │
│   └── trading-engine/         # Trading engine service
│       ├── internal/
│       │   ├── engine/
│       │   └── execution/
│       └── go.mod
│
└── golang_server/staging/      # Original monolithic code (reference)
```

## Support

For questions or issues, refer to:
- `MIGRATION-PHASE1-PHASE2-SUMMARY.md` - Migration details
- `README.md` - Project overview
- Original staging code in `golang_server/staging/` for reference

