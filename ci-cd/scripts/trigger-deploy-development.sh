#!/bin/bash
# Trigger Deploy (Development) workflow and monitor progress
# Uses GitHub CLI to run workflow_dispatch and watch the run.
# Same pattern as deploy-external-secrets.sh / trigger-ecr-publish.sh.
set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

print_info() {
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

# Script and repo paths
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
WORKFLOW_FILE="deploy-development.yml"

# Optional: branch/ref to run the workflow on (default: current branch)
GITHUB_REF="${GITHUB_REF:-}"
if [ -z "$GITHUB_REF" ] && [ -d "$REPO_ROOT/.git" ]; then
    GITHUB_REF="$(git -C "$REPO_ROOT" rev-parse --abbrev-ref HEAD 2>/dev/null || echo "")"
fi
REF="${1:-$GITHUB_REF}"

echo "🚀 Triggering Deploy (Development) workflow..."
print_info "Repo root: $REPO_ROOT"
print_info "Workflow: $WORKFLOW_FILE"
if [ -n "$REF" ]; then
    print_info "Ref: $REF"
fi

# ---------------------------------------------------------------------------
# Stage 1: Prerequisites
# ---------------------------------------------------------------------------
print_info "📦 Stage 1: Checking prerequisites..."

if ! command -v gh &>/dev/null; then
    print_error "GitHub CLI (gh) is not installed. Install from https://cli.github.com/"
    exit 1
fi
print_success "GitHub CLI is installed"

if ! gh auth status &>/dev/null; then
    print_error "Not logged in to GitHub. Run: gh auth login"
    exit 1
fi
print_success "GitHub CLI is authenticated"

# ---------------------------------------------------------------------------
# Stage 2: Trigger workflow
# ---------------------------------------------------------------------------
print_info "📦 Stage 2: Triggering workflow..."

cd "$REPO_ROOT"
if [ -n "$REF" ]; then
    if ! gh workflow run "$WORKFLOW_FILE" --ref "$REF"; then
        print_error "Failed to trigger workflow"
        exit 1
    fi
    print_success "Triggered workflow on ref: $REF"
else
    if ! gh workflow run "$WORKFLOW_FILE"; then
        print_error "Failed to trigger workflow"
        exit 1
    fi
    print_success "Triggered workflow (default ref)"
fi

# Brief delay so the run appears in the list
print_info "Waiting for run to appear..."
sleep 5

# ---------------------------------------------------------------------------
# Stage 3: Get latest run and watch
# ---------------------------------------------------------------------------
print_info "📦 Stage 3: Monitoring workflow run..."

RUN_ID=""
for _ in 1 2 3 4 5 6; do
    RUN_ID=$(gh run list --workflow="$WORKFLOW_FILE" --limit 1 --json databaseId --jq '.[0].databaseId' 2>/dev/null || true)
    if [ -n "$RUN_ID" ] && [ "$RUN_ID" != "null" ]; then
        break
    fi
    sleep 5
done

if [ -z "$RUN_ID" ] || [ "$RUN_ID" = "null" ]; then
    print_warning "Could not get run ID. Check runs manually: gh run list --workflow=$WORKFLOW_FILE"
    exit 1
fi

print_info "Run ID: $RUN_ID"
print_info "Watching progress (Ctrl+C to stop watching; run continues on GitHub)..."
echo ""

if ! gh run watch "$RUN_ID"; then
    print_warning "Watch exited or run failed. Check: gh run view $RUN_ID"
    exit 1
fi

print_success "✅ Workflow run completed"
print_info "View run: gh run view $RUN_ID"
echo ""
