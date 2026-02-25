#!/usr/bin/env bash
# Validate Grafana dashboard JSON files under monitoring/grafana/dashboards.
# Ensures each file is valid JSON and has required keys (title, panels).
# Usage: ./scripts/validate-grafana-dashboards.sh

set -e
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
DASHBOARDS_DIR="$REPO_ROOT/monitoring/grafana/dashboards"

if ! command -v jq &>/dev/null; then
  echo "Error: jq is required. Install jq and try again." >&2
  exit 1
fi

failed=0
while IFS= read -r -d '' f; do
  if ! jq -e '.title and (.panels | type == "array")' "$f" &>/dev/null; then
    echo "Invalid or missing title/panels: $f" >&2
    ((failed++)) || true
  fi
  if ! jq empty "$f" 2>/dev/null; then
    echo "Invalid JSON: $f" >&2
    ((failed++)) || true
  fi
done < <(find "$DASHBOARDS_DIR" -name "*.json" -print0 2>/dev/null)

if [ "$failed" -gt 0 ]; then
  echo "Validation failed for $failed file(s)." >&2
  exit 1
fi
echo "All Grafana dashboard JSON files are valid."
exit 0
