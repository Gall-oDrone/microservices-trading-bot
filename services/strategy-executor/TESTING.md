# Strategy Executor Service - Testing Guide

## Overview

This document provides comprehensive information about testing the Strategy Executor Service.

## Test Coverage Summary

| Package | Tests | Status | Coverage |
|---------|-------|--------|----------|
| `config` | 15+ | ✅ Passing | High |
| `consumer` | 10+ | ✅ Passing | High |
| `processor` | 16+ | ✅ Passing | High |
| `strategies` | 12+ | ✅ Passing | High |
| `manager` | 11+ | ✅ Passing | Medium |
| `publisher` | 4+ | ✅ Passing | Medium |
| **Total** | **60+** | **✅ All Passing** | **High** |

## Running Tests

### All Tests

```bash
# Run all tests
go test ./...

# Run with verbose output
go test ./... -v

# Run with race detection
go test ./... -race

# Run with count (disable caching)
go test ./... -count=1
```

### Package-Specific Tests

```bash
# Configuration
go test ./internal/config/... -v

# Market Data Consumer
go test ./internal/consumer/... -v

# Data Processor
go test ./internal/processor/... -v

# Strategies
go test ./internal/strategies/... -v

# Strategy Manager
go test ./internal/manager/... -v

# Signal Publisher
go test ./internal/publisher/... -v
```

### Specific Test

```bash
# Run a specific test
go test ./internal/config/... -run TestLoad -v

# Run tests matching pattern
go test ./internal/processor/... -run "Filter" -v
```

## Test Categories

### 1. Unit Tests

#### Configuration Tests (`internal/config/config_test.go`)

**Tests:**
- `TestLoad` - Configuration loading from environment
  - Default configuration
  - Custom configuration
  - Invalid port
  - Empty service name
- `TestConfig_Validate` - Configuration validation
  - Valid configuration
  - Invalid values
  - Missing required fields
- `TestConfig_GetTradingConfig` - Trading config generation
- `TestGetEnv*` - Environment variable parsing
  - Integer parsing
  - Float parsing
  - Boolean parsing
  - Duration parsing
  - Slice parsing

**Coverage:** Environment variable parsing, validation, defaults

---

#### Consumer Tests (`internal/consumer/market_data_consumer_test.go`)

**Tests:**
- `TestNewConsumer` - Consumer creation
  - Valid configuration
  - Nil parameters
- `TestConsumer_SubscribeToBook` - Book subscription
- `TestConsumer_UnsubscribeFromBook` - Book unsubscription
- `TestConsumer_GetStatistics` - Statistics retrieval
- `TestConsumer_GetConsumedMessages` - Channel access
- `TestConsumer_Stop` - Graceful shutdown
- `TestTickerEvent` - Ticker event structure
- `TestOrderBookEvent` - Order book event structure
- `TestOrderBookEntry` - Order book entry structure
- `TestConsumerStatistics` - Statistics structure

**Coverage:** Kafka consumption, subscriptions, statistics, shutdown

---

#### Processor Tests (`internal/processor/data_processor_test.go`)

**Tests:**
- `TestNewProcessor` - Processor creation
- `TestProcessor_ProcessTradeEvent` - Trade event processing
- `TestProcessor_ProcessTickerEvent` - Ticker event processing
- `TestProcessor_ProcessOrderBookEvent` - Order book event processing
- `TestProcessor_GetStatistics` - Statistics retrieval
- `TestProcessor_Stop` - Graceful shutdown
- `TestProcessedEvent` - Event structure
- `TestEventType` - Event type constants
- `TestProcessorStatistics` - Statistics structure

**Coverage:** Event processing, statistics, shutdown

---

#### Filter Tests (`internal/processor/filters_test.go`)

**Tests:**
- `TestBookFilter` - Book-based filtering
  - Allowed/disallowed books
  - Add/remove books
- `TestTypeFilter` - Type-based filtering
  - Allowed/disallowed types
  - Add/remove types
- `TestTimeFilter` - Time range filtering
  - Events within/outside range
  - Update time range
- `TestRateLimitFilter` - Rate limiting
  - Events within/exceeding limits
  - Per-book rate limiting
  - Update rate limits
- `TestCompositeFilter` - Combined filters
  - AND logic
  - Add/remove filters
- `TestVolumeFilter` - Volume-based filtering
  - Volume thresholds
  - Missing volume field
  - Update thresholds
- `TestFilterIntegration` - Filter integration
  - Multiple filters working together
  - Event filtering in processor

**Coverage:** All filter types, edge cases, integration

---

#### Strategy Tests (`internal/strategies/strategy_registry_test.go`)

**Tests:**
- `TestNewRegistry` - Registry creation
  - Built-in strategies registered
- `TestRegistry_RegisterFactory` - Factory registration
  - Successful registration
  - Duplicate registration
- `TestRegistry_UnregisterFactory` - Factory unregistration
- `TestRegistry_CreateStrategy` - Strategy creation
  - Built-in strategies
  - Non-existent strategy
- `TestRegistry_GetStrategy` - Strategy retrieval
- `TestRegistry_RemoveStrategy` - Strategy removal
- `TestRegistry_GetAllStrategies` - Get all strategies
- `TestRegistry_GetAvailableStrategies` - Get available strategies
- `TestRegistry_GetActiveStrategies` - Get active strategies
- `TestRegistry_StopAll` - Stop all strategies
- `TestRegistry_ConcurrentAccess` - Thread safety
- `TestStrategyFactory` - Factory implementations
  - Basic strategy factory
  - Trend strategy factory
  - Arbitrage strategy factory
  - Nil config handling

**Coverage:** Strategy lifecycle, registration, concurrency

---

#### Manager Tests (`internal/manager/strategy_manager_test.go`)

**Tests:**
- `TestNewManager` - Manager creation
- `TestManager_StartStop` - Manager lifecycle
- `TestManager_StartStrategy` - Start strategy
  - Successful start
  - Duplicate start prevention
- `TestManager_StopStrategy` - Stop strategy
  - Successful stop
  - Non-existent strategy
- `TestManager_GetStrategyStatus` - Status retrieval
- `TestManager_UpdateStrategyConfig` - Config update
- `TestManager_GetAllStatuses` - Get all statuses
- `TestManager_ProcessMarketData` - Market data processing
- `TestManager_ConvertEventToTicker` - Event conversion
  - Valid event
  - Invalid book
- `TestStrategyStatus` - Status structure
- `TestStrategyStatusType` - Status type constants

**Coverage:** Strategy management, market data processing, status tracking

---

#### Publisher Tests (`internal/publisher/signal_publisher_test.go`)

**Tests:**
- `TestNewPublisher` - Publisher creation
  - Valid configuration
  - Nil parameters
- `TestPublisher_RegisterUnregisterStrategy` - Strategy registration
- `TestPublisher_GetStatistics` - Statistics retrieval
- `TestPublisher_ConvertSignalToEvent` - Signal conversion
  - Buy signals
  - Sell signals
  - Hold signals
- `TestPublisherStatistics` - Statistics structure
- `TestPublisherConfig` - Configuration structure

**Coverage:** Signal publishing, registration, statistics

---

## Integration Tests (To Be Implemented)

### Kafka Integration

```go
// Test actual Kafka integration
func TestKafkaIntegration(t *testing.T) {
    // Requires running Kafka instance
    // Test message consumption and production
}
```

### Market-Data Service Integration

```go
// Test HTTP client with actual market-data service
func TestMarketDataIntegration(t *testing.T) {
    // Requires running market-data service
    // Test API calls and data retrieval
}
```

### End-to-End Test

```go
// Test complete flow from market data to signal publishing
func TestEndToEnd(t *testing.T) {
    // Start service
    // Send market data
    // Verify signals are generated
    // Verify signals are published
}
```

## Performance Tests (To Be Implemented)

### Load Testing

```go
// Test service under load
func TestLoadPerformance(t *testing.T) {
    // Simulate high message volume
    // Measure latency and throughput
    // Verify no memory leaks
}
```

### Concurrent Processing

```go
// Test concurrent strategy execution
func TestConcurrentStrategies(t *testing.T) {
    // Start multiple strategies
    // Process market data concurrently
    // Verify no race conditions
}
```

## Benchmark Tests (To Be Implemented)

```go
// Benchmark critical paths
func BenchmarkStrategyExecution(b *testing.B) {
    // Measure strategy execution time
}

func BenchmarkEventProcessing(b *testing.B) {
    // Measure event processing time
}

func BenchmarkSignalPublishing(b *testing.B) {
    // Measure signal publishing time
}
```

## Test Utilities

### Mock Kafka

```go
// Create mock Kafka producer/consumer for testing
type MockKafkaProducer struct {
    messages []kafka.Message
}

func (m *MockKafkaProducer) Produce(ctx context.Context, key, value []byte) error {
    // Store message for verification
    return nil
}
```

### Mock Market Data Client

```go
// Create mock market data client
type MockMarketDataClient struct {
    trades []*models.TradeEvent
}

func (m *MockMarketDataClient) GetRecentTrades(ctx context.Context, book string, limit int) ([]*models.TradeEvent, error) {
    return m.trades, nil
}
```

### Test Fixtures

```go
// Create test data fixtures
func CreateTestTradeEvent() *models.TradeEvent {
    return &models.TradeEvent{
        ID:     12345,
        Book:   "btc_mxn",
        Price:  1000.0,
        Amount: 0.5,
        // ... more fields
    }
}

func CreateTestTicker() *bitso.Ticker {
    return &bitso.Ticker{
        Book: bitso.NewBook(bitso.BTC, bitso.MXN),
        Bid:  bitso.ToMonetary(999.0),
        Ask:  bitso.ToMonetary(1001.0),
        Last: bitso.ToMonetary(1000.0),
    }
}
```

## Test Data

### Sample Market Data

```json
{
  "id": 12345,
  "book": "btc_mxn",
  "price": 1000.0,
  "amount": 0.5,
  "value": 500.0,
  "maker_side": "buy",
  "timestamp": "2025-10-24T12:00:00Z"
}
```

### Sample Trading Signal

```json
{
  "event_id": "signal-1234567890",
  "timestamp": 1729785600,
  "book": "btc_mxn",
  "strategy": "basic",
  "signal": "BUY",
  "price": 1000.0,
  "amount": 0.5,
  "metadata": {
    "reason": "Price decreased below threshold"
  }
}
```

## Continuous Integration

### GitHub Actions Workflow

```yaml
name: Test Strategy Executor

on: [push, pull_request]

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      
      - name: Set up Go
        uses: actions/setup-go@v4
        with:
          go-version: '1.21'
      
      - name: Run tests
        working-directory: ./services/strategy-executor
        run: |
          go test ./... -v -race -count=1
      
      - name: Build
        working-directory: ./services/strategy-executor
        run: |
          go build -o strategy-executor ./cmd/main.go
```

## Test Best Practices

### 1. Isolation
- Each test is independent
- No shared state between tests
- Clean up resources after tests

### 2. Naming
- Clear, descriptive test names
- Follow pattern: `Test{Component}_{Method}_{Scenario}`
- Example: `TestManager_StartStrategy_DuplicatePrevention`

### 3. Structure
- Arrange: Set up test data
- Act: Execute the operation
- Assert: Verify the results

### 4. Coverage
- Test happy paths
- Test error cases
- Test edge cases
- Test concurrent access

### 5. Mocking
- Mock external dependencies
- Use interfaces for testability
- Avoid testing implementation details

## Troubleshooting

### Common Issues

**Issue:** Tests fail with "connection refused"
**Solution:** Ensure Kafka/Redis are not required for unit tests. Use mocks.

**Issue:** Race conditions detected
**Solution:** Review mutex usage, ensure proper synchronization.

**Issue:** Tests timeout
**Solution:** Check for goroutine leaks, ensure proper cleanup.

**Issue:** Flaky tests
**Solution:** Avoid time-dependent assertions, use proper synchronization.

## Test Metrics

```bash
# Generate test coverage report
go test ./... -coverprofile=coverage.out
go tool cover -html=coverage.out -o coverage.html

# View coverage in terminal
go test ./... -cover

# Benchmark tests
go test ./... -bench=. -benchmem
```

## Next Steps

1. ✅ Implement unit tests (COMPLETED)
2. ⏳ Add integration tests
3. ⏳ Add performance tests
4. ⏳ Add benchmark tests
5. ⏳ Increase test coverage to 80%+
6. ⏳ Set up CI/CD pipeline
7. ⏳ Add mutation testing

## Resources

- [Go Testing Documentation](https://golang.org/pkg/testing/)
- [Table-Driven Tests](https://github.com/golang/go/wiki/TableDrivenTests)
- [Testify Library](https://github.com/stretchr/testify)
- [Gomock](https://github.com/golang/mock)
