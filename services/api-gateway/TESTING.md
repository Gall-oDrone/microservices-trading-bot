# API Gateway - Testing Guide

## Overview

This document describes the testing strategy, test coverage, and how to run tests for the API Gateway service.

## Test Strategy

### Testing Pyramid

```
       /\
      /  \      E2E Tests (Minimal)
     /____\     - Full system tests
    /      \    Integration Tests (Moderate)
   /________\   - Service interaction tests
  /          \  Unit Tests (Comprehensive)
 /____________\ - Component tests
```

### Test Categories

1. **Unit Tests** - Test individual components in isolation
2. **Integration Tests** - Test interaction between components
3. **E2E Tests** - Test complete request flow through the gateway

## Running Tests

### Quick Start

```bash
# Run all tests
./run_tests.sh

# Run specific package tests
go test ./internal/config/... -v
go test ./internal/client/... -v
go test ./internal/middleware/... -v

# Run with coverage
go test ./... -coverprofile=coverage.out
go tool cover -html=coverage.out

# Run with race detection
go test ./... -race

# Run benchmarks
go test ./... -bench=. -benchmem
```

### Test Runner Script

The `run_tests.sh` script provides a comprehensive test suite:

```bash
./run_tests.sh
```

**What it does**:
- Cleans previous test artifacts
- Runs `go mod tidy`
- Executes all unit tests with race detection
- Generates coverage reports
- Runs integration tests (if available)
- Tests build
- Runs `go vet`
- Displays coverage summary

## Test Coverage

### Current Coverage

| Package | Coverage | Status |
|---------|----------|--------|
| internal/config | 79.5% | ✅ Good |
| internal/logger | 0.0% | ⚠️ No tests yet |
| internal/metrics | 0.0% | ⚠️ No tests yet |
| internal/client | 0.0% | ⚠️ No tests yet |
| internal/middleware | 0.0% | ⚠️ No tests yet |
| internal/api | 0.0% | ⚠️ No tests yet |
| internal/router | 0.0% | ⚠️ No tests yet |
| internal/validation | 0.0% | ⚠️ No tests yet |
| internal/server | 0.0% | ⚠️ No tests yet |
| **Overall** | **3.4%** | 🔄 In Progress |

### Target Coverage

- **Overall**: >80%
- **Critical Paths**: >90%
- **Configuration**: 100%

## Existing Tests

### Configuration Tests (`internal/config/config_test.go`)

✅ **TestLoad** - Configuration loading
- Default configuration
- Custom configuration
- Invalid port handling

✅ **TestValidate** - Configuration validation
- Valid configuration
- Missing service name
- Invalid port
- Missing backend URL
- Invalid log level
- TLS without cert

✅ **TestGetEnv** - Environment variable helpers
- With env value
- Without env value

✅ **TestGetEnvAsInt** - Integer parsing
- Valid integer
- Invalid integer
- Empty value

✅ **TestGetEnvAsBool** - Boolean parsing
- true/false strings
- 1/0 values
- Invalid values
- Empty values

✅ **TestGetEnvAsDuration** - Duration parsing
- Valid duration
- Invalid duration
- Empty value

**Total**: 6 test functions, 23 test cases, all passing ✅

## Test Templates

### Unit Test Template

```go
package mypackage

import (
    "testing"
)

func TestMyFunction(t *testing.T) {
    tests := []struct {
        name    string
        input   string
        want    string
        wantErr bool
    }{
        {
            name:    "valid input",
            input:   "test",
            want:    "test",
            wantErr: false,
        },
        {
            name:    "invalid input",
            input:   "",
            want:    "",
            wantErr: true,
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, err := MyFunction(tt.input)
            if (err != nil) != tt.wantErr {
                t.Errorf("MyFunction() error = %v, wantErr %v", err, tt.wantErr)
                return
            }
            if got != tt.want {
                t.Errorf("MyFunction() = %v, want %v", got, tt.want)
            }
        })
    }
}
```

### Integration Test Template

```go
package integration

import (
    "context"
    "net/http/httptest"
    "testing"
    "time"
)

func TestIntegration_EndToEnd(t *testing.T) {
    // Setup
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    // Create test server
    server := httptest.NewServer(handler)
    defer server.Close()

    // Test
    // ... test code ...

    // Assertions
    // ... assertions ...
}
```

## Testing Best Practices

### 1. Table-Driven Tests

Use table-driven tests for multiple test cases:

```go
tests := []struct {
    name    string
    input   interface{}
    want    interface{}
    wantErr bool
}{
    // Test cases here
}

for _, tt := range tests {
    t.Run(tt.name, func(t *testing.T) {
        // Test logic
    })
}
```

### 2. Mock Dependencies

Create mock implementations for testing:

```go
type mockClient struct {
    getOrderFunc func(ctx context.Context, id string) (*Order, error)
}

func (m *mockClient) GetOrder(ctx context.Context, id string) (*Order, error) {
    if m.getOrderFunc != nil {
        return m.getOrderFunc(ctx, id)
    }
    return nil, nil
}
```

### 3. Test Helpers

Create helper functions for common test operations:

```go
func setupTestServer(t *testing.T) *httptest.Server {
    t.Helper()
    // Setup code
    return server
}

func assertNoError(t *testing.T, err error) {
    t.Helper()
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
}
```

### 4. Cleanup

Always clean up resources:

```go
func TestSomething(t *testing.T) {
    server := setupServer()
    defer server.Close()

    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()

    // Test code
}
```

## Test Organization

### Directory Structure

```
services/api-gateway/
├── internal/
│   ├── config/
│   │   ├── config.go
│   │   └── config_test.go        ✅ Tests exist
│   ├── client/
│   │   ├── types.go
│   │   └── types_test.go         ⚠️ TODO
│   ├── middleware/
│   │   ├── logging.go
│   │   └── logging_test.go       ⚠️ TODO
│   └── ...
└── test/
    ├── integration/
    │   └── api_test.go            ⚠️ TODO
    └── e2e/
        └── gateway_test.go        ⚠️ TODO
```

## Testing Checklist

### Unit Tests (Pending)

#### Client Package
- [ ] TestMarketDataClient_GetRecentTrades
- [ ] TestMarketDataClient_GetTrade
- [ ] TestMarketDataClient_RetryLogic
- [ ] TestOrderManagementClient_ListOrders
- [ ] TestOrderManagementClient_CancelOrder
- [ ] TestStrategyExecutorClient_ListStrategies
- [ ] TestClientFactory_Creation

#### Middleware Package
- [ ] TestLoggingMiddleware
- [ ] TestMetricsMiddleware
- [ ] TestRateLimiter
- [ ] TestCircuitBreaker
- [ ] TestCORS
- [ ] TestRecovery
- [ ] TestTimeout
- [ ] TestAuth

#### API Package
- [ ] TestMarketDataHandlers
- [ ] TestOrderHandlers
- [ ] TestStrategyHandlers
- [ ] TestAggregationHandlers
- [ ] TestResponseHelpers

#### Validation Package
- [ ] TestValidationRules
- [ ] TestValidator

### Integration Tests (Pending)

- [ ] Test gateway → market-data flow
- [ ] Test gateway → order-management flow
- [ ] Test gateway → strategy-executor flow
- [ ] Test aggregation endpoints
- [ ] Test error propagation
- [ ] Test timeout handling

### E2E Tests (Pending)

- [ ] Test complete request flow
- [ ] Test middleware chain
- [ ] Test circuit breaker behavior
- [ ] Test rate limiting
- [ ] Test concurrent requests

## Manual Testing

### Health Check

```bash
# Liveness
curl http://localhost:8080/health/live

# Readiness
curl http://localhost:8080/health/ready

# Detailed health
curl http://localhost:8080/health
```

### Market Data Endpoints

```bash
# Get recent trades
curl http://localhost:8080/api/v1/market-data/trades?book=btc_mxn&limit=10

# Get order book
curl http://localhost:8080/api/v1/market-data/orderbook?book=btc_mxn

# Get ticker
curl http://localhost:8080/api/v1/market-data/ticker?book=btc_mxn

# Get market summary
curl http://localhost:8080/api/v1/market-data/summary
```

### Order Endpoints

```bash
# List orders
curl http://localhost:8080/api/v1/orders?book=btc_mxn&limit=20

# Get active orders
curl http://localhost:8080/api/v1/orders/active

# Get position summary
curl http://localhost:8080/api/v1/positions/summary
```

### Strategy Endpoints

```bash
# List strategies
curl http://localhost:8080/api/v1/strategies

# Get service status
curl http://localhost:8080/api/v1/strategies/status
```

### Aggregation Endpoints

```bash
# Get dashboard
curl http://localhost:8080/api/v1/dashboard

# Get portfolio
curl http://localhost:8080/api/v1/portfolio

# Get system status
curl http://localhost:8080/api/v1/system/status
```

### Metrics

```bash
# Prometheus metrics
curl http://localhost:8080/metrics
```

## Performance Testing

### Load Testing with Apache Bench

```bash
# Test health endpoint
ab -n 1000 -c 10 http://localhost:8080/health/live

# Test market data endpoint
ab -n 1000 -c 10 http://localhost:8080/api/v1/market-data/trades?book=btc_mxn
```

### Load Testing with hey

```bash
# Install hey
go install github.com/rakyll/hey@latest

# Run load test
hey -n 10000 -c 100 http://localhost:8080/api/v1/market-data/summary
```

## Debugging Tests

### Verbose Output

```bash
go test ./... -v
```

### Test Specific Function

```bash
go test ./internal/config -run TestLoad -v
```

### Debug with Delve

```bash
dlv test ./internal/config -- -test.run TestLoad
```

## CI/CD Integration

### GitHub Actions Example

```yaml
name: Test

on: [push, pull_request]

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - uses: actions/setup-go@v4
        with:
          go-version: '1.21'
      
      - name: Run tests
        run: |
          cd services/api-gateway
          ./run_tests.sh
      
      - name: Upload coverage
        uses: codecov/codecov-action@v3
        with:
          files: ./services/api-gateway/coverage.out
```

## Test Metrics

### Current Status

- **Total Test Files**: 1
- **Total Test Functions**: 6
- **Total Test Cases**: 23
- **Pass Rate**: 100% ✅
- **Overall Coverage**: 3.4% (config: 79.5%)
- **Race Conditions**: None detected ✅

### Target Metrics

- **Total Test Files**: 20+
- **Total Test Functions**: 50+
- **Total Test Cases**: 150+
- **Pass Rate**: 100% ✅
- **Overall Coverage**: >80%
- **Race Conditions**: None

## Troubleshooting

### Tests Failing

```bash
# Clean and retry
go clean -testcache
./run_tests.sh
```

### Coverage Not Generated

```bash
# Ensure coverage.out exists
go test ./... -coverprofile=coverage.out

# View coverage
go tool cover -func=coverage.out
```

### Build Failures

```bash
# Check dependencies
go mod tidy
go mod verify

# Rebuild
go build ./...
```

## Future Test Enhancements

### High Priority
1. **Client Tests** - Mock HTTP servers for client testing
2. **Middleware Tests** - Test each middleware in isolation
3. **Handler Tests** - Test API handlers with mock clients
4. **Integration Tests** - Test with real backend services

### Medium Priority
5. **Performance Tests** - Load testing and benchmarks
6. **Chaos Tests** - Test failure scenarios
7. **Security Tests** - Test auth and authorization

### Low Priority
8. **Mutation Tests** - Test suite quality
9. **Fuzz Tests** - Input fuzzing
10. **Property Tests** - Property-based testing

---

**Version**: 1.0  
**Last Updated**: October 27, 2025  
**Status**: Initial Release

## Next Steps

1. Add unit tests for all packages
2. Create integration test suite
3. Add E2E tests
4. Improve coverage to >80%
5. Add performance benchmarks

