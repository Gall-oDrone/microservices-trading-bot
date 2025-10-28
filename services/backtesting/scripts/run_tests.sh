#!/bin/bash

# Backtesting Service - Test Runner Script
# This script runs all tests and generates coverage reports

set -e

echo "🧪 Running Backtesting Service Tests..."
echo "========================================"
echo ""

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

# Navigate to service directory
cd "$(dirname "$0")/.."

echo "📁 Current directory: $(pwd)"
echo ""

# Run tests with coverage
echo "🧪 Running all tests..."
go test ./... -v -coverprofile=coverage.out -covermode=atomic

echo ""
echo "📊 Coverage Summary:"
echo "===================="
go test ./... -coverprofile=coverage.out -covermode=atomic 2>&1 | grep "coverage:"

echo ""
echo "📈 Detailed Coverage:"
echo "===================="
go tool cover -func=coverage.out | tail -20

echo ""
echo "📄 Generating HTML coverage report..."
go tool cover -html=coverage.out -o coverage.html

echo ""
echo -e "${GREEN}✅ Tests completed successfully!${NC}"
echo ""
echo "Coverage report: coverage.html"
echo "Coverage data: coverage.out"
echo ""
echo "To view coverage report:"
echo "  open coverage.html"
echo ""

# Check if coverage meets minimum threshold
TOTAL_COVERAGE=$(go tool cover -func=coverage.out | grep total | awk '{print $3}' | sed 's/%//')
MIN_COVERAGE=80

echo "Total coverage: ${TOTAL_COVERAGE}%"

if (( $(echo "$TOTAL_COVERAGE >= $MIN_COVERAGE" | bc -l) )); then
    echo -e "${GREEN}✅ Coverage meets minimum threshold (${MIN_COVERAGE}%)${NC}"
else
    echo -e "${YELLOW}⚠️  Coverage below minimum threshold (${MIN_COVERAGE}%)${NC}"
    echo "   Consider adding more tests"
fi

echo ""
echo "✨ Done!"

