#!/usr/bin/env bash
#
# Analyze POST-POINT-10 Stage soak logs: regime agreement and health summary.
# See docs/strategy-fee-accuracy/STAGE-SOAK-OPERATOR-GUIDE-2026-06-02.md
#
# Usage:
#   ./scripts/analyze-stage-soak-agreement.sh report
#   ./scripts/analyze-stage-soak-agreement.sh sample   # one live bash vs Go compare → JSONL
#   ./scripts/analyze-stage-soak-agreement.sh pull-go  # refresh Go audit from cluster
#
set -euo pipefail

NAMESPACE="${NAMESPACE:-bitso-trading-dev}"
LOG_DIR="${LOG_DIR:-$(cd "$(dirname "$0")/.." && pwd)/tmp/stage-soak}"
BASH_LOG="${BASH_LOG:-$LOG_DIR/bash-router-soak.log}"
GO_LOG="${GO_LOG:-$LOG_DIR/go-router-audit.jsonl}"
SAMPLES="${SAMPLES:-$LOG_DIR/agreement-samples.jsonl}"
REPORT_JSON="${REPORT_JSON:-$LOG_DIR/soak-verification-report.json}"
BOOK="${BOOK:-btc_mxn}"
PF_PORT="${STRATEGY_EXECUTOR_LOCAL_PORT:-8084}"

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; BLUE='\033[0;34m'; NC='\033[0m'
info() { echo -e "${BLUE}[INFO]${NC} $*"; }
ok() { echo -e "${GREEN}[OK]${NC} $*"; }
warn() { echo -e "${YELLOW}[WARN]${NC} $*"; }
err() { echo -e "${RED}[ERROR]${NC} $*" 1>&2; }

require_python() {
  command -v python3 >/dev/null 2>&1 || { err "python3 required"; exit 1; }
}

pull_go_audit() {
  mkdir -p "$LOG_DIR"
  info "Pulling Go audit from strategy-router pod..."
  kubectl -n "$NAMESPACE" exec deploy/strategy-router -- \
    cat /tmp/strategy-regime-router.log 2>/dev/null >"$GO_LOG" || true
  local n
  n=$(wc -l <"$GO_LOG" | tr -d ' ')
  ok "Go audit: $n lines → $GO_LOG"
}

classify_from_inputs_py() {
  python3 - "$@" <<'PY'
import json, re, sys
from collections import Counter
from pathlib import Path

bash_log = Path(sys.argv[1])
go_log = Path(sys.argv[2])
report_path = Path(sys.argv[3])

ansi = re.compile(r'\x1b\[[0-9;]*m')
TH = dict(hi=1.5, lo=0.30, rsi_ob=70, rsi_os=30, bb_lo=0.15, bb_hi=0.85, ema=0.10)

def classify(price, atr_pct, rsi, pb, ema_dist):
    if float(price) <= 0:
        return 'neutral'
    atr_pct, rsi, pb, ema_dist = map(float, (atr_pct, rsi, pb, ema_dist))
    if atr_pct > TH['hi']:
        return 'high_vol'
    if atr_pct < TH['lo'] and TH['bb_lo'] < pb < TH['bb_hi']:
        return 'low_vol_range'
    if ema_dist > TH['ema'] and rsi < TH['rsi_ob']:
        return 'trending_up'
    if ema_dist < -TH['ema'] and rsi > TH['rsi_os']:
        return 'trending_down'
    return 'neutral'

def analyze_bash(path: Path):
    if not path.exists():
        return {'error': 'bash log missing'}
    text = ansi.sub('', path.read_text())
    lines = text.splitlines()
    inputs_re = re.compile(
        r'regime inputs: price=(\S+) atr_pct=([\d.]+) rsi=([\d.]+) pb=([\d.]+) ema_dist_pct=([-\d.]+)'
    )
    stated_re = re.compile(
        r'^(low_vol_range|trending_up|trending_down|high_vol|neutral)\s+preferred='
    )
    regime_eq_re = re.compile(
        r'regime=(low_vol_range|trending_up|trending_down|high_vol|neutral)\s+preferred='
    )
    pairs, mismatches = [], []
    for i, line in enumerate(lines):
        m = inputs_re.search(line)
        if not m:
            continue
        price, atr_pct, rsi, pb, ema_dist = m.groups()
        if float(price) <= 0:
            continue
        stated = None
        for j in range(i + 1, min(i + 6, len(lines))):
            s = lines[j].strip()
            m2 = stated_re.match(s) or regime_eq_re.search(s)
            if m2:
                stated = m2.group(1)
                break
        if not stated:
            continue
        expected = classify(price, atr_pct, rsi, pb, ema_dist)
        rec = dict(stated=stated, expected=expected, match=stated == expected)
        pairs.append(rec)
        if not rec['match']:
            mismatches.append(rec)
    comparable = len(pairs)
    matches = sum(1 for p in pairs if p['match'])
    return {
        'bash_log_lines': len(lines),
        'snapshot_fetch_failed': text.count('snapshot fetch failed'),
        'comparable_bash_cycles': comparable,
        'bash_stated_vs_recomputed_matches': matches,
        'bash_self_consistency_rate': round(matches / comparable, 6) if comparable else None,
        'stated_regime_counts': dict(Counter(p['stated'] for p in pairs)),
        'mismatch_count': len(mismatches),
        'mismatches_sample': mismatches[:5],
    }

def analyze_go(path: Path):
    if not path.exists():
        return {'error': 'go audit missing'}
    rows = []
    for line in path.read_text().splitlines():
        line = line.strip()
        if line:
            rows.append(json.loads(line))
    valid = [r for r in rows if r.get('snapshot', {}).get('Price', 0) > 0]
    invalid = len(rows) - len(valid)
    return {
        'go_audit_lines': len(rows),
        'comparable_go_cycles': len(valid),
        'missing_price_go_cycles': invalid,
        'go_regime_counts': dict(Counter(r['regime'] for r in valid)),
        'first_ts': rows[0]['timestamp'] if rows else None,
        'last_ts': rows[-1]['timestamp'] if rows else None,
        'note': (
            'Go audit is ephemeral (/tmp in pod). Pod restarts wipe history; '
            'pull-go before report after long soaks.'
        ),
    }

bash_stats = analyze_bash(bash_log)
go_stats = analyze_go(go_log)

pass_threshold = 0.99
# Full bash↔Go agreement needs time-aligned samples; see agreement-samples.jsonl
report = {
    'book': 'btc_mxn',
    'pass_threshold_agreement_rate': pass_threshold,
    'bash': bash_stats,
    'go': go_stats,
    'verdict': {
        'bash_self_consistency_pass': (
            bash_stats.get('bash_self_consistency_rate') is not None
            and bash_stats['bash_self_consistency_rate'] >= pass_threshold
        ),
        'go_comparable_cycles_sufficient': go_stats.get('comparable_go_cycles', 0) >= 100,
        'short_soak_time_gate_hours': 24,
        'note': (
            'Declare short-soak PASS only when agreement-samples.jsonl (or offline '
            'aligned export) shows >= 99% bash vs Go on comparable cycles over 24-48h.'
        ),
    },
}
report_path.parent.mkdir(parents=True, exist_ok=True)
report_path.write_text(json.dumps(report, indent=2) + '\n')
print(json.dumps(report, indent=2))
PY
}

live_sample() {
  require_python
  mkdir -p "$LOG_DIR"
  local se="http://127.0.0.1:${PF_PORT}"
  if ! curl -fsS --max-time 3 "${se}/health" >/dev/null 2>&1; then
    err "strategy-executor not reachable on :${PF_PORT} (start port-forward / start-bash)"
    exit 1
  fi
  local snap go_regime bash_regime ts
  ts=$(date -u +%FT%TZ)
  snap=$(curl -fsS "${se}/api/v1/indicators/${BOOK}/snapshot")
  # Force one fresh Go evaluation on current indicators (avoid stale last_decisions).
  go_regime=$(kubectl -n "$NAMESPACE" exec deploy/strategy-router -- \
    wget -qO- --post-data='' http://127.0.0.1:8092/api/v1/router/run 2>/dev/null \
    | jq -r '.decisions[0].regime // .decisions[0].Regime // "unknown"')
  if [[ -z "$go_regime" || "$go_regime" == "unknown" || "$go_regime" == "null" ]]; then
    go_regime=$(kubectl -n "$NAMESPACE" exec deploy/strategy-router -- \
      wget -qO- http://127.0.0.1:8092/api/v1/router/state 2>/dev/null \
      | jq -r '.last_decisions[0].regime // "unknown"')
  fi
  bash_regime=$(echo "$snap" | jq -r --argjson hi 0.85 --argjson lo 0.15 '
    def price: (.current_price // .bollinger.current_price // .bollinger.middle_band // .sma.value // .sma.Value // 0);
    def atr: (.atr.value // .atr.Value // 0);
    def atr_pct: if (price|tonumber) > 0 then (atr / (price|tonumber)) * 100 else 0 end;
    def pb:
      if (.bollinger.upper_band // .bollinger.Upper // 0) > (.bollinger.lower_band // .bollinger.Lower // 0) then
        ((price|tonumber) - (.bollinger.lower_band // .bollinger.Lower)) /
        ((.bollinger.upper_band // .bollinger.Upper) - (.bollinger.lower_band // .bollinger.Lower))
      else 0.5 end;
    def ema_dist_pct:
      if (price|tonumber) > 0 then (((price|tonumber) - (.ema.value // .ema.Value // price|tonumber)) / (price|tonumber)) * 100 else 0 end;
    def rsi: (.rsi.value // .rsi.Value // 50);
    if (price|tonumber) == 0 then "neutral"
    elif atr_pct > 1.5 then "high_vol"
    elif atr_pct < 0.30 and pb > $lo and pb < $hi then "low_vol_range"
    elif ema_dist_pct > 0.10 and rsi < 70 then "trending_up"
    elif ema_dist_pct < -0.10 and rsi > 30 then "trending_down"
    else "neutral" end
  ')
  local match="false"
  [[ "$go_regime" == "$bash_regime" ]] && match="true"
  local line
  line=$(jq -nc \
    --arg ts "$ts" --arg go "$go_regime" --arg bash "$bash_regime" \
    --argjson match "$match" \
    '{timestamp:$ts, go_regime:$go, bash_regime:$bash, match:$match}')
  echo "$line" >>"$SAMPLES"
  if [[ "$match" == "true" ]]; then
    ok "sample $ts go=$go_regime bash=$bash_regime match=true"
  else
    warn "sample $ts go=$go_regime bash=$bash_regime match=false"
  fi
  echo "$line"
}

summarize_samples() {
  require_python
  if [[ ! -f "$SAMPLES" ]]; then
    warn "No samples yet: run sample (or report with SOAK_SAMPLES=1)"
    return 0
  fi
  python3 - "$SAMPLES" <<'PY'
import json, sys
from pathlib import Path
p = Path(sys.argv[1])
rows = [json.loads(l) for l in p.read_text().splitlines() if l.strip()]
if not rows:
    print('{"samples":0}')
    sys.exit(0)
matches = sum(1 for r in rows if r.get('match'))
n = len(rows)
print(json.dumps({
    'live_samples': n,
    'live_matches': matches,
    'live_agreement_rate': round(matches / n, 6),
    'live_pass_99pct': matches / n >= 0.99,
}, indent=2))
PY
}

cmd="${1:-report}"
case "$cmd" in
  pull-go)
    pull_go_audit
    ;;
  sample)
    live_sample
    ;;
  report)
    require_python
    pull_go_audit
    classify_from_inputs_py "$BASH_LOG" "$GO_LOG" "$REPORT_JSON"
    summarize_samples || true
    info "Report written to $REPORT_JSON"
    ;;
  *)
    err "Usage: $0 {report|sample|pull-go}"
    exit 1
    ;;
esac
