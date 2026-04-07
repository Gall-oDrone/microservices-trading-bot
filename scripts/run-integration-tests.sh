#!/bin/bash
# Run integration tests for the trading platform
#
# Usage:
#   ./scripts/run-integration-tests.sh [--local|--stage]
#
# Options:
#   --local   Run against local services (default)
#   --stage   Run against stage environment (requires kubectl port-forward)
#
# Prerequisites:
#   - For --local: docker-compose up (Kafka, Redis, services)
#   - For --stage: kubectl port-forward to order-management, trading-engine

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

# Default configuration
ORDER_MANAGEMENT_URL="${ORDER_MANAGEMENT_URL:-http://localhost:8082}"
KAFKA_BROKERS="${KAFKA_BROKERS:-localhost:9092}"
REDIS_HOST="${REDIS_HOST:-localhost:6379}"

# Parse arguments
ENV="local"
while [[ $# -gt 0 ]]; do
    case $1 in
        --local)
            ENV="local"
            shift
            ;;
        --stage)
            ENV="stage"
            ORDER_MANAGEMENT_URL="${ORDER_MANAGEMENT_URL:-http://localhost:8082}"
            shift
            ;;
        *)
            echo "Unknown option: $1"
            echo "Usage: $0 [--local|--stage]"
            exit 1
            ;;
    esac
done

echo "============================================"
echo "Running Integration Tests (${ENV} environment)"
echo "============================================"
echo "ORDER_MANAGEMENT_URL: ${ORDER_MANAGEMENT_URL}"
echo "KAFKA_BROKERS:        ${KAFKA_BROKERS}"
echo "REDIS_HOST:           ${REDIS_HOST}"
echo ""

# Check if order-management is reachable
echo "Checking order-management health..."
if curl -sf "${ORDER_MANAGEMENT_URL}/health" > /dev/null 2>&1; then
    echo "✓ order-management is healthy"
else
    echo "✗ order-management is not reachable at ${ORDER_MANAGEMENT_URL}"
    echo ""
    echo "Please ensure the service is running:"
    echo "  - Local: docker-compose up order-management"
    echo "  - Stage: kubectl port-forward svc/order-management 8082:8082"
    exit 1
fi

echo ""
echo "Running tests..."
echo ""

cd "${PROJECT_ROOT}/tests/integration"

# Run tests with integration tag
export ORDER_MANAGEMENT_URL
export KAFKA_BROKERS
export REDIS_HOST

go test -v -tags=integration -timeout=60s ./...

echo ""
echo "============================================"
echo "Integration Tests Completed"
echo "============================================"
