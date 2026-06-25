#!/usr/bin/env bash
#
# Regime-stratified classification agreement report (Bucket D1 of
# RECOMMENDED-NEXT-STEPS-2026-06-25). The short-soak verified ~100% overall
# bash<->Go agreement, but overall rate hides whether rare regimes (e.g.
# high_vol, trending_*) are well covered. This breaks agreement down per regime
# and emits a go-vs-bash confusion matrix so under-sampled regimes are visible.
#
# Input: agreement samples JSONL of {timestamp, go_regime, bash_regime, match},
# as produced by scripts/analyze-stage-soak-agreement.sh (sample/report).
#
# Usage:
#   ./scripts/analyze-regime-stratified-agreement.sh [samples.jsonl] [--out report.json]
#
# Environment:
#   LOG_DIR  default soak dir (default: <repo>/tmp/stage-soak)
#   SAMPLES  default samples path (default: $LOG_DIR/agreement-samples.jsonl)
#
set -euo pipefail

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; BLUE='\033[0;34m'; NC='\033[0m'
info() { echo -e "${BLUE}[INFO]${NC} $*"; }
err()  { echo -e "${RED}[ERROR]${NC} $*" 1>&2; }

LOG_DIR="${LOG_DIR:-$(cd "$(dirname "$0")/.." && pwd)/tmp/stage-soak}"
SAMPLES="${SAMPLES:-$LOG_DIR/agreement-samples.jsonl}"
OUT=""

ARGS=()
while [[ $# -gt 0 ]]; do
  case "$1" in
    --out) OUT="$2"; shift 2 ;;
    -h|--help) sed -n '2,20p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) ARGS+=("$1"); shift ;;
  esac
done
[[ ${#ARGS[@]} -gt 0 ]] && SAMPLES="${ARGS[0]}"

command -v python3 >/dev/null 2>&1 || { err "python3 required"; exit 1; }
[[ -f "$SAMPLES" ]] || { err "samples file not found: $SAMPLES (run analyze-stage-soak-agreement.sh sample)"; exit 1; }

info "Stratifying agreement from: $SAMPLES"

python3 - "$SAMPLES" "$OUT" <<'PY'
import json, sys
from collections import Counter, defaultdict
from pathlib import Path

samples_path = Path(sys.argv[1])
out_path = sys.argv[2] if len(sys.argv) > 2 and sys.argv[2] else None

REGIMES = ["high_vol", "low_vol_range", "trending_up", "trending_down", "neutral"]

rows = []
for line in samples_path.read_text().splitlines():
    line = line.strip()
    if not line:
        continue
    try:
        rows.append(json.loads(line))
    except json.JSONDecodeError:
        continue

n = len(rows)
overall_match = sum(1 for r in rows if r.get("match"))

# Per-regime agreement keyed on the Go (reference) regime.
per_regime = {}
for reg in set([r.get("go_regime") for r in rows] + REGIMES):
    if reg is None:
        continue
    subset = [r for r in rows if r.get("go_regime") == reg]
    matches = sum(1 for r in subset if r.get("match"))
    per_regime[reg] = {
        "samples": len(subset),
        "matches": matches,
        "agreement_rate": round(matches / len(subset), 6) if subset else None,
    }

# Confusion matrix: go_regime -> bash_regime counts.
confusion = defaultdict(Counter)
for r in rows:
    confusion[r.get("go_regime", "?")][r.get("bash_regime", "?")] += 1

# Coverage warnings: regimes with too few samples to trust the rate.
MIN_SAMPLES = 30
under_sampled = [reg for reg, s in per_regime.items() if s["samples"] < MIN_SAMPLES]

report = {
    "total_samples": n,
    "overall_matches": overall_match,
    "overall_agreement_rate": round(overall_match / n, 6) if n else None,
    "go_regime_distribution": dict(Counter(r.get("go_regime") for r in rows)),
    "per_regime_agreement": per_regime,
    "confusion_go_vs_bash": {g: dict(c) for g, c in confusion.items()},
    "min_samples_for_confidence": MIN_SAMPLES,
    "under_sampled_regimes": under_sampled,
    "verdict": {
        "all_observed_regimes_pass_99pct": all(
            s["agreement_rate"] is None or s["agreement_rate"] >= 0.99
            for s in per_regime.values() if s["samples"] > 0
        ),
        "coverage_complete": len(under_sampled) == 0,
        "note": (
            "A high overall rate can still hide a weak rare regime. Treat "
            "under_sampled_regimes as 'not yet verified', not 'passed'."
        ),
    },
}

text = json.dumps(report, indent=2)
print(text)

# Human-readable table to stderr so stdout stays pure JSON.
def f(x):
    return "n/a" if x is None else f"{x:.4f}"
print("\nPer-regime agreement (reference = Go):", file=sys.stderr)
print(f"  {'regime':<16} {'samples':>8} {'matches':>8} {'rate':>8}", file=sys.stderr)
for reg in sorted(per_regime, key=lambda r: -per_regime[r]['samples']):
    s = per_regime[reg]
    flag = "  <-- under-sampled" if reg in under_sampled and s['samples'] > 0 else ""
    if s['samples'] == 0:
        flag = "  <-- NOT OBSERVED"
    print(f"  {reg:<16} {s['samples']:>8} {s['matches']:>8} {f(s['agreement_rate']):>8}{flag}", file=sys.stderr)

if out_path:
    Path(out_path).parent.mkdir(parents=True, exist_ok=True)
    Path(out_path).write_text(text + "\n")
    print(f"\nReport written to {out_path}", file=sys.stderr)
PY
