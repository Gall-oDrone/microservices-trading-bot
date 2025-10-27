# Market Data Service Testing Guide

This document provides comprehensive information about testing the market-data service.

## Overview

The market-data service includes a comprehensive test suite covering all core functionalities:

- **Trade Processor Tests** - Tests for processing incoming trade data
- **Trade Publisher Tests** - Tests for publishing trade events to Kafka
- **WebSocket Manager Tests** - Tests for WebSocket connection management
- **Configuration Tests** - Tests for configuration loading and validation
- **Main Application Tests** - Tests for the main application logic

## Test Structure

```
services/market-data/
├── internal/
│   ├── processor/
│   │   ├── trade_processor.go
│   │   └── trade_processor_test.go
│   ├── publisher/
│   │   ├── trade_publisher.go
│   │   └── trade_publisher_test.go
│   ├── websocket/
│   │   ├── manager.go
│   │   └── manager_test.go
│   └── config/
│       ├── config.go
│       └── config_test.go
├── cmd/
│   ├── main.go
│   └── main_test.go
├── run_tests.sh
└── TESTING.md
```

## Running Tests

### Quick Start

Run all tests with the provided script:

```bash
./run_tests.sh
```

This script will:
- Run all unit tests
- Run benchmarks
- Run race condition detection
- Generate coverage reports
- Run timeout tests

### Manual Test Execution

#### Run All Tests
```bash
go test -v ./...
```

#### Run Specific Package Tests
```bash
# Trade processor tests
go test -v ./internal/processor

# Trade publisher tests
go test -v ./internal/publisher

# WebSocket manager tests
go test -v ./internal/websocket

# Configuration tests
go test -v ./internal/config

# Main application tests
go test -v ./cmd
```

#### Run Benchmarks
```bash
go test -bench=. -benchmem ./...
```

#### Run with Race Detection
```bash
go test -race ./...
```

#### Generate Coverage Report
```bash
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out -o coverage.html
```

## Test Categories

### 1. Trade Processor Tests (`trade_processor_test.go`)

**Purpose**: Tests the trade processing functionality

**Key Test Cases**:
- `TestProcessor` - Basic processor functionality
- `TestProcessorConcurrency` - Concurrent processing
- `TestProcessorStatistics` - Statistics tracking
- `TestProcessorLatency` - Latency calculation
- `TestProcessorErrorHandling` - Error handling
- `BenchmarkProcessor` - Performance benchmarks

**Coverage**:
- Trade data processing
- Statistics tracking
- Error handling
- Concurrent processing
- Performance metrics

### 2. Trade Publisher Tests (`trade_publisher_test.go`)

**Purpose**: Tests the Kafka publishing functionality

**Key Test Cases**:
- `TestPublisher` - Basic publishing functionality
- `TestPublisherErrorHandling` - Error handling
- `TestPublisherConcurrency` - Concurrent publishing
- `TestPublisherStatistics` - Statistics tracking
- `TestPublisherPerformance` - Performance testing
- `BenchmarkPublisher` - Performance benchmarks

**Coverage**:
- Kafka message publishing
- Error handling and retry logic
- Statistics tracking
- Concurrent publishing
- Performance metrics

### 3. WebSocket Manager Tests (`manager_test.go`)

**Purpose**: Tests the WebSocket connection management

**Key Test Cases**:
- `TestManager` - Basic manager functionality
- `TestManagerReconnection` - Reconnection logic
- `TestManagerMessageRouting` - Message routing
- `TestManagerBackoffCalculation` - Backoff calculation
- `BenchmarkManager` - Performance benchmarks

**Coverage**:
- WebSocket connection management
- Message routing
- Reconnection logic
- Backoff calculation
- Performance metrics

### 4. Configuration Tests (`config_test.go`)

**Purpose**: Tests configuration loading and validation

**Key Test Cases**:
- `TestLoadConfig` - Configuration loading
- `TestLoadConfigWithCustomValues` - Custom configuration
- `TestConfigValidation` - Configuration validation
- `TestHelperFunctions` - Helper function testing
- `TestConfigCopy` - Configuration copying

**Coverage**:
- Environment variable parsing
- Configuration validation
- Default value handling
- Helper function functionality

### 5. Main Application Tests (`main_test.go`)

**Purpose**: Tests the main application logic

**Key Test Cases**:
- `TestParseBook` - Book parsing functionality
- `TestParseBookEdgeCases` - Edge case handling
- `TestParseBookPerformance` - Performance testing
- `TestParseBookConcurrency` - Concurrent parsing
- `BenchmarkParseBook` - Performance benchmarks

**Coverage**:
- Book parsing logic
- Edge case handling
- Performance testing
- Concurrent access

## Mock Objects

### MockProducer

A mock implementation of the Kafka producer for testing:

```go
type MockProducer struct {
    producedMessages []kafka.Message
    produceError     error
    mu               sync.RWMutex
}
```

**Features**:
- Tracks produced messages
- Simulates producer errors
- Thread-safe operations
- Statistics tracking

### MockWebSocketConn

A mock implementation of the WebSocket connection for testing:

```go
type MockWebSocketConn struct {
    receiveChan chan interface{}
    connected   bool
    subscribed  map[string][]string
}
```

**Features**:
- Simulates WebSocket messages
- Connection state management
- Subscription tracking
- Message routing

## Test Data

### Test Trade Events

```go
func createTestTradeEvent() *models.TradeEvent {
    return &models.TradeEvent{
        ID:           12345,
        Book:         "btc_mxn",
        Price:        50000.0,
        Amount:       0.001,
        Value:        50.0,
        MakerOrderID: "maker-order-123",
        TakerOrderID: "taker-order-456",
        MakerSide:    "buy",
        Timestamp:    time.Now(),
        CreatedAt:    uint64(time.Now().UnixMilli()),
        ReceivedAt:   time.Now(),
        Source:       "bitso_websocket",
        Metadata: map[string]interface{}{
            "test": true,
        },
    }
}
```

### Test WebSocket Trades

```go
func createTestWebSocketTrade() *bitso.WebSocketTrade {
    return &bitso.WebSocketTrade{
        Book: bitso.ToBook("btc_mxn"),
        Payload: []bitso.WebSocketTradePayload{
            {
                TID:               12345,
                Price:             bitso.Monetary{Value: 50000.0},
                Amount:            bitso.Monetary{Value: 0.001},
                Value:             bitso.Monetary{Value: 50.0},
                MakerOrderID:      "maker-order-123",
                TakerOrderID:      "taker-order-456",
                MakerSide:         "0",
                CreationTimestamp: uint64(time.Now().UnixMilli()),
            },
        },
        Sent: uint64(time.Now().UnixMilli()),
    }
}
```

## Performance Testing

### Benchmarks

The test suite includes comprehensive benchmarks:

- **Processor Benchmarks** - Trade processing performance
- **Publisher Benchmarks** - Kafka publishing performance
- **Manager Benchmarks** - WebSocket management performance
- **ParseBook Benchmarks** - Book parsing performance

### Performance Metrics

Tests measure:
- Processing throughput
- Memory usage
- Latency
- Error rates
- Resource utilization

## Coverage

The test suite aims for comprehensive coverage:

- **Unit Tests** - Individual component testing
- **Integration Tests** - Component interaction testing
- **Performance Tests** - Load and stress testing
- **Error Handling Tests** - Error scenario testing
- **Concurrency Tests** - Thread safety testing

## Best Practices

### Test Organization

1. **One test file per source file**
2. **Descriptive test names**
3. **Clear test structure**
4. **Comprehensive error checking**
5. **Performance benchmarking**

### Test Data

1. **Realistic test data**
2. **Edge case coverage**
3. **Boundary value testing**
4. **Error condition simulation**

### Mocking

1. **Interface-based mocking**
2. **Behavioral verification**
3. **State tracking**
4. **Error simulation**

## Continuous Integration

### GitHub Actions

The test suite is designed to run in CI/CD pipelines:

```yaml
- name: Run Tests
  run: |
    cd services/market-data
    ./run_tests.sh
```

### Docker Testing

Tests can be run in Docker containers:

```dockerfile
FROM golang:1.21-alpine
WORKDIR /app
COPY . .
RUN go test ./...
```

## Troubleshooting

### Common Issues

1. **Import Path Errors**
   - Ensure Go modules are properly configured
   - Check import paths in test files

2. **Race Conditions**
   - Use `go test -race` to detect race conditions
   - Review concurrent test code

3. **Timeout Issues**
   - Increase test timeouts for slow tests
   - Use context cancellation for long-running tests

4. **Mock Issues**
   - Ensure mock objects implement interfaces correctly
   - Check mock state management

### Debug Tips

1. **Verbose Output**
   ```bash
   go test -v ./...
   ```

2. **Single Test Execution**
   ```bash
   go test -run TestSpecificTest ./...
   ```

3. **Coverage Analysis**
   ```bash
   go tool cover -html=coverage.out
   ```

4. **Race Detection**
   ```bash
   go test -race ./...
   ```

## Contributing

When adding new tests:

1. **Follow naming conventions**
2. **Include comprehensive test cases**
3. **Add benchmarks for performance-critical code**
4. **Update this documentation**
5. **Ensure tests pass in CI/CD**

## Resources

- [Go Testing Documentation](https://golang.org/pkg/testing/)
- [Go Benchmarking Guide](https://golang.org/pkg/testing/#hdr-Benchmarks)
- [Go Race Detector](https://golang.org/doc/articles/race_detector.html)
- [Go Coverage Tool](https://golang.org/cmd/cover/)
