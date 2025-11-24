#!/bin/bash
# Script to test Docker builds locally for all services
# This helps verify the Dockerfiles work before running in GitHub Actions
#
# Usage: Run from project root:
#   ./testing/integration/docker/test-docker-builds.sh
#   Or from this directory:
#   cd testing/integration/docker && ./test-docker-builds.sh

set -e

# Get the project root directory (where this script is located relative to root)
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/../../.." && pwd)"

# Change to project root to ensure paths work correctly
cd "${PROJECT_ROOT}"

echo "Testing Docker builds for all services..."
echo "=========================================="
echo "Project root: ${PROJECT_ROOT}"
echo ""

# Enable BuildKit
export DOCKER_BUILDKIT=1

# List of services
SERVICES=(
  "trading-engine"
  "backtesting"
  "strategy-executor"
  "market-data"
  "order-management"
  "api-gateway"
)

FAILED_SERVICES=()
SUCCESS_SERVICES=()

for service in "${SERVICES[@]}"; do
  echo "Building ${service}..."
  echo "---"
  
  if docker build \
    --build-arg BUILDKIT_INLINE_CACHE=1 \
    -t "test-${service}:local" \
    -f "./services/${service}/Dockerfile" \
    . > "/tmp/docker-build-${service}.log" 2>&1; then
    echo "✓ ${service} built successfully"
    SUCCESS_SERVICES+=("${service}")
  else
    echo "✗ ${service} build FAILED"
    echo "Last 20 lines of build log:"
    tail -20 "/tmp/docker-build-${service}.log"
    FAILED_SERVICES+=("${service}")
  fi
  echo ""
done

echo "=========================================="
echo "Build Summary:"
echo "=========================================="
echo "Successful builds: ${#SUCCESS_SERVICES[@]}"
for service in "${SUCCESS_SERVICES[@]}"; do
  echo "  ✓ ${service}"
done

if [ ${#FAILED_SERVICES[@]} -gt 0 ]; then
  echo ""
  echo "Failed builds: ${#FAILED_SERVICES[@]}"
  for service in "${FAILED_SERVICES[@]}"; do
    echo "  ✗ ${service}"
    echo "    Full log: /tmp/docker-build-${service}.log"
  done
  exit 1
else
  echo ""
  echo "All builds successful! ✓"
  exit 0
fi

