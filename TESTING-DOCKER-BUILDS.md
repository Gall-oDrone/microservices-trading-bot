# Testing Docker Builds

This guide explains how to test Docker builds for all services without needing ECR repositories.

## Option 1: Test Locally (Recommended First Step)

### Prerequisites
- Docker Desktop running (or Docker daemon)
- BuildKit enabled (usually enabled by default in newer Docker versions)

### Run the Test Script

```bash
# Make sure Docker is running first
docker ps

# Run the test script from project root
./testing/integration/docker/test-docker-builds.sh

# Or navigate to the directory first
cd testing/integration/docker
./test-docker-builds.sh
```

This will:
- Build all 6 service Docker images locally
- Verify they build successfully
- Show detailed logs for any failures

### Test Individual Services

```bash
# Enable BuildKit
export DOCKER_BUILDKIT=1

# Build a specific service (e.g., backtesting)
docker build -t test-backtesting -f ./services/backtesting/Dockerfile .

# Verify it works
docker images | grep test-backtesting
```

## Option 2: Test via GitHub Actions (No ECR Required)

### Use the Test Workflow

I've created two test workflows:

1. **`.github/workflows/test-docker-builds.yml`** - Simple build test
   - Trigger manually via `workflow_dispatch`
   - Builds all services without pushing to ECR
   - Good for PR validation

2. **`.github/workflows/ecr-publish-test.yml`** - Full workflow test (no push)
   - Same structure as production workflow
   - Skips all ECR operations
   - Use to validate the exact build process

### How to Use

1. Go to your GitHub repository
2. Click on "Actions" tab
3. Select "Test Docker Builds (No Push)" or "Build and Push to ECR (TEST MODE - No Push)"
4. Click "Run workflow"
5. Select your branch and click "Run workflow"

## Option 3: Temporarily Modify Main Workflow

You can temporarily modify `.github/workflows/ecr-publish.yml` to skip pushing:

1. Change `push: true` to `push: false` in the build step
2. Remove or comment out AWS/ECR steps if you want
3. Run the workflow via `workflow_dispatch`
4. Revert changes after testing

## Option 4: Use `act` for Local GitHub Actions Testing

Install `act` to run GitHub Actions locally:

```bash
# Install act (macOS)
brew install act

# Or download from: https://github.com/nektos/act/releases

# Run the workflow locally
act workflow_dispatch -W .github/workflows/test-docker-builds.yml
```

**Note:** `act` doesn't fully replicate GitHub Actions environment, but it's useful for quick validation.

## What to Look For

✅ **Success indicators:**
- Build completes without errors
- No "go.sum not found" errors
- Images are created successfully

❌ **Failure indicators:**
- COPY errors for go.sum
- Build failures during `go mod download`
- Missing file errors

## Next Steps

Once local testing passes:
1. Push your changes to a branch
2. Run the GitHub Actions test workflow
3. If successful, the production workflow should work once ECR repos are recreated

## Troubleshooting

### Docker daemon not running
```bash
# Start Docker Desktop or Docker daemon
# macOS: Open Docker Desktop app
# Linux: sudo systemctl start docker
```

### BuildKit not enabled
```bash
export DOCKER_BUILDKIT=1
# Or add to ~/.docker/config.json:
# { "features": { "buildkit": true } }
```

### Permission denied on script
```bash
chmod +x testing/integration/docker/test-docker-builds.sh
```

