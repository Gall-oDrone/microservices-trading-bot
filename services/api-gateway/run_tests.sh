#!/bin/bash

# API Gateway Test Runner
# Runs all tests with coverage reporting

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}API Gateway - Test Runner${NC}"
echo -e "${GREEN}========================================${NC}"
echo ""

# Check if Go is installed
if ! command -v go &> /dev/null; then
    echo -e "${RED}Error: Go is not installed${NC}"
    exit 1
fi

# Get script directory
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
cd "$SCRIPT_DIR"

# Clean previous test artifacts
echo -e "${YELLOW}Cleaning previous test artifacts...${NC}"
rm -f coverage.out coverage.html
echo ""

# Run go mod tidy
echo -e "${YELLOW}Running go mod tidy...${NC}"
go mod tidy
echo ""

# Run unit tests
echo -e "${YELLOW}Running unit tests...${NC}"
echo -e "${GREEN}========================================${NC}"
go test ./internal/... -v -race -coverprofile=coverage.out
TEST_EXIT_CODE=$?
echo ""

if [ $TEST_EXIT_CODE -ne 0 ]; then
    echo -e "${RED}Unit tests failed!${NC}"
    exit $TEST_EXIT_CODE
fi

# Generate coverage report
if [ -f coverage.out ]; then
    echo -e "${YELLOW}Generating coverage report...${NC}"
    go tool cover -func=coverage.out | tail -n 1
    echo ""
    
    # Generate HTML coverage report
    go tool cover -html=coverage.out -o coverage.html
    echo -e "${GREEN}Coverage report generated: coverage.html${NC}"
    echo ""
fi

# Run integration tests (if they exist)
if [ -d "test/integration" ]; then
    echo -e "${YELLOW}Running integration tests...${NC}"
    echo -e "${GREEN}========================================${NC}"
    go test ./test/integration/... -v
    echo ""
fi

# Run build test
echo -e "${YELLOW}Testing build...${NC}"
go build -o api-gateway-test ./cmd/main.go
BUILD_EXIT_CODE=$?

if [ $BUILD_EXIT_CODE -eq 0 ]; then
    echo -e "${GREEN}Build successful!${NC}"
    rm -f api-gateway-test
else
    echo -e "${RED}Build failed!${NC}"
    exit $BUILD_EXIT_CODE
fi
echo ""

# Run go vet
echo -e "${YELLOW}Running go vet...${NC}"
go vet ./...
VET_EXIT_CODE=$?

if [ $VET_EXIT_CODE -eq 0 ]; then
    echo -e "${GREEN}go vet passed!${NC}"
else
    echo -e "${RED}go vet found issues!${NC}"
    exit $VET_EXIT_CODE
fi
echo ""

# Summary
echo -e "${GREEN}========================================${NC}"
echo -e "${GREEN}All tests passed! ✅${NC}"
echo -e "${GREEN}========================================${NC}"
echo ""

# Print coverage summary if available
if [ -f coverage.out ]; then
    echo -e "${YELLOW}Coverage Summary:${NC}"
    go tool cover -func=coverage.out | grep total:
    echo ""
fi

exit 0

