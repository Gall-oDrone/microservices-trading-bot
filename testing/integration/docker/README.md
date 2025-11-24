# Docker Build Testing

This directory contains scripts and utilities for testing Docker builds for all microservices.

## test-docker-builds.sh

Tests Docker image builds for all services locally before deploying to ECR.

### Purpose

- Validates that all Dockerfiles build successfully
- Verifies the fix for optional `go.sum` files
- Provides fast feedback before running in CI/CD

### Usage

From the project root:
```bash
./testing/integration/docker/test-docker-builds.sh
```

Or from this directory:
```bash
cd testing/integration/docker
./test-docker-builds.sh
```

### Prerequisites

- Docker Desktop running (or Docker daemon)
- BuildKit enabled (default in newer Docker versions)

### What It Does

1. Builds Docker images for all 6 services:
   - trading-engine
   - backtesting
   - strategy-executor
   - market-data
   - order-management
   - api-gateway

2. Uses BuildKit for faster builds with caching
3. Reports success/failure for each service
4. Saves build logs to `/tmp/docker-build-<service>.log`

### Output

- ✓ Successful builds are listed with checkmarks
- ✗ Failed builds show the last 20 lines of error output
- Full logs are available in `/tmp/docker-build-<service>.log`

### Exit Codes

- `0` - All builds successful
- `1` - One or more builds failed

