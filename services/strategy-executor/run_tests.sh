#!/bin/bash

# Strategy Executor Service - Test Runner
# This script runs all tests for the strategy executor service

set -e

echo "=================================================="
echo "  Strategy Executor Service - Test Suite"
echo "=================================================="
echo ""

# Colors
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Test counter
TOTAL_TESTS=0
PASSED_TESTS=0
FAILED_TESTS=0

echo "📦 Running go mod tidy..."
go mod tidy
echo ""

echo "🔨 Building service..."
go build -o strategy-executor ./cmd/main.go
if [ $? -eq 0 ]; then
    echo -e "${GREEN}✓ Build successful${NC}"
else
    echo -e "${RED}✗ Build failed${NC}"
    exit 1
fi
echo ""

echo "🧪 Running all tests..."
echo ""

# Run tests for each package
packages=(
    "internal/config"
    "internal/consumer"
    "internal/processor"
    "internal/strategies"
    "internal/manager"
    "internal/publisher"
)

for package in "${packages[@]}"; do
    echo "Testing $package..."
    if go test ./$package/... -count=1 -v > /tmp/test_output.txt 2>&1; then
        tests=$(grep -c "PASS:" /tmp/test_output.txt || echo "0")
        echo -e "${GREEN}✓ $package ($tests tests)${NC}"
        PASSED_TESTS=$((PASSED_TESTS + 1))
    else
        echo -e "${RED}✗ $package${NC}"
        cat /tmp/test_output.txt
        FAILED_TESTS=$((FAILED_TESTS + 1))
    fi
    TOTAL_TESTS=$((TOTAL_TESTS + 1))
done

echo ""
echo "=================================================="
echo "  Test Summary"
echo "=================================================="
echo -e "Total Packages: $TOTAL_TESTS"
echo -e "${GREEN}Passed: $PASSED_TESTS${NC}"
if [ $FAILED_TESTS -gt 0 ]; then
    echo -e "${RED}Failed: $FAILED_TESTS${NC}"
else
    echo -e "Failed: $FAILED_TESTS"
fi
echo ""

# Run all tests together
echo "🔍 Running all tests with race detection..."
if go test ./... -race -count=1 > /tmp/all_tests.txt 2>&1; then
    echo -e "${GREEN}✓ All tests passed with race detection${NC}"
else
    echo -e "${RED}✗ Some tests failed${NC}"
    cat /tmp/all_tests.txt
    exit 1
fi
echo ""

# Get test statistics
total_test_funcs=$(grep -r "^func Test" internal/ | wc -l | tr -d ' ')
echo "📊 Test Statistics:"
echo "   - Test Functions: $total_test_funcs"
echo "   - Test Packages: ${#packages[@]}"
echo "   - Binary Size: $(ls -lh strategy-executor | awk '{print $5}')"
echo ""

echo "=================================================="
if [ $FAILED_TESTS -eq 0 ]; then
    echo -e "${GREEN}  ✅ ALL TESTS PASSED!${NC}"
else
    echo -e "${RED}  ❌ SOME TESTS FAILED${NC}"
fi
echo "=================================================="

exit $FAILED_TESTS

