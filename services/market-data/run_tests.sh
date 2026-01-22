#!/bin/bash

# Market Data Service Test Runner
# This script runs all tests for the market-data service

set -e

echo "🧪 Running Market Data Service Tests"
echo "====================================="

# Change to the market-data service directory
cd "$(dirname "$0")"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Function to print colored output
print_status() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

print_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Check if Go is installed
if ! command -v go &> /dev/null; then
    print_error "Go is not installed or not in PATH"
    exit 1
fi

print_status "Go version: $(go version)"

# Check if we're in the right directory
if [ ! -f "go.mod" ]; then
    print_error "go.mod not found. Please run this script from the market-data service directory"
    exit 1
fi

# Check if go.mod has the correct module name
if ! grep -q "bitso-trading-platform/market-data" go.mod; then
    print_error "go.mod does not contain the expected module name"
    exit 1
fi

print_status "Running tests for market-data service..."

# Run tests with verbose output
print_status "Running unit tests..."
if go test -v ./...; then
    print_success "All unit tests passed!"
else
    print_error "Some unit tests failed"
    exit 1
fi

echo ""
print_status "Running benchmarks..."
if go test -bench=. -benchmem ./...; then
    print_success "All benchmarks completed!"
else
    print_warning "Some benchmarks failed or were skipped"
fi

echo ""
print_status "Running tests with race detection..."
if go test -race ./...; then
    print_success "All tests passed with race detection!"
else
    print_error "Race condition detected in tests"
    exit 1
fi

echo ""
print_status "Running tests with coverage..."
if go test -coverprofile=coverage.out ./...; then
    print_success "Coverage report generated!"
    
    # Show coverage summary
    echo ""
    print_status "Coverage summary:"
    go tool cover -func=coverage.out | tail -1
    
    # Generate HTML coverage report
    if command -v go &> /dev/null; then
        go tool cover -html=coverage.out -o coverage.html
        print_status "HTML coverage report generated: coverage.html"
    fi
else
    print_error "Coverage test failed"
    exit 1
fi

echo ""
print_status "Running tests with timeout..."
if timeout 30s go test -timeout=20s ./...; then
    print_success "All tests completed within timeout!"
else
    print_error "Tests timed out or failed"
    exit 1
fi

echo ""
print_success "🎉 All tests completed successfully!"
echo ""
print_status "Test Summary:"
echo "  ✅ Unit tests: PASSED"
echo "  ✅ Benchmarks: COMPLETED"
echo "  ✅ Race detection: PASSED"
echo "  ✅ Coverage: GENERATED"
echo "  ✅ Timeout tests: PASSED"
echo ""
print_status "Coverage report: coverage.html"
print_status "Coverage data: coverage.out"
echo ""
print_status "To run specific test packages:"
echo "  go test -v ./internal/processor"
echo "  go test -v ./internal/publisher"
echo "  go test -v ./internal/websocket"
echo "  go test -v ./internal/config"
echo "  go test -v ./cmd"
echo ""
print_status "To run benchmarks:"
echo "  go test -bench=. -benchmem ./..."
echo ""
print_status "To run with race detection:"
echo "  go test -race ./..."
echo ""
print_status "To generate coverage report:"
echo "  go test -coverprofile=coverage.out ./..."
echo "  go tool cover -html=coverage.out -o coverage.html"
