#!/usr/bin/env bash
# Runs the procedures frozen in the three eToro pre-registrations and writes
# their verdicts (the eToro sibling of scripts/prereg-evaluation.sh):
#
#   FORWARD-TEST-PREREGISTRATION-SMA50-INDEX-CFD-2026-10-12.md   nsdq100 / spx500, 2026-10-13 -> 2027-10-12
#   FORWARD-TEST-PREREGISTRATION-SMA50-ETF-2026-10-21.md         qqq / spy,         2026-10-22 -> 2027-10-21
#   FORWARD-TEST-PREREGISTRATION-DUAL-MOMENTUM-ETF-2026-10-21.md qqq / spy / cash,  2026-11-02 -> 2027-10-29
#
#   scripts/etoro-prereg-evaluation.sh                   # progress report as of the latest closed bar
#   scripts/etoro-prereg-evaluation.sh --phase interim   # the interim looks (report only)
#   scripts/etoro-prereg-evaluation.sh --phase final     # the primary evaluations (decide)
#
# Bars: the eToro daily candles the demo executors save on every run
# (etoro-daily-data/<ledger>/candles/<book>_daily_<date>.csv, the newest file per
# book), which are the pre-registrations' judged series; no eToro key is needed.
# Benchmarks: the Yahoo series (QQQ / SPY total return for the CFD cell's H2b,
# ^IRX for the rotation engine), fetched fresh into --out/data unless --no-fetch
# (then the committed docs/backtest-readiness/evidence-2026-10-10/data is used; the
# CFD cell's H2b is then stale and says so).
#
# Costs are passed exactly as frozen. Output per cell under --out (default
# docs/backtest-readiness/evidence-<today>/etoro-prereg-<phase>/): the bars used,
# <cell>/forward.txt and forward.json, and SHA256SUMS. Commit it with a short
# results note linking the pre-registrations; the frozen figures decide.
#
# Options:
#   --phase as-of|interim|final   (default as-of)
#   --out DIR
#   --no-fetch                    reuse the committed Yahoo CSVs (offline)
#   --sims N                      random strategies / permutations (default 2000, as registered)
#
# Read-only: it reads the ledgers' candle directories and Yahoo; no keys, no orders.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
phase="as-of" out="" fetch=1 sims=2000
while [ $# -gt 0 ]; do
  case "$1" in
    --phase) phase="$2"; shift 2 ;;
    --out) out="$2"; shift 2 ;;
    --no-fetch) fetch=0; shift ;;
    --sims) sims="$2"; shift 2 ;;
    -h | --help) sed -n '2,32p' "$0"; exit 0 ;;
    *) echo "unknown option $1" >&2; exit 2 ;;
  esac
done
case "$phase" in
  as-of | final) W_CFD="2026-10-13:2027-10-12" W_ETF="2026-10-22:2027-10-21" W_ROT="2026-11-02:2027-10-29" ;;
  interim)       W_CFD="2026-10-13:2027-04-12" W_ETF="2026-10-22:2027-04-21" W_ROT="2026-11-02:2027-04-30" ;;
  *) echo "--phase must be as-of, interim or final" >&2; exit 2 ;;
esac
out="${out:-$ROOT/docs/backtest-readiness/evidence-$(date -u +%F)/etoro-prereg-$phase}"
mkdir -p "$out/etoro" "$out/cfd" "$out/etf" "$out/rotation"
out="$(cd "$out" && pwd)"
cd "$ROOT/services/strategy-executor"
EDATA="${ETORO_DATA_DIR:-./etoro-daily-data}"   # override for offline re-runs / tests

# newest_candles BOOK LEDGER...: copies the newest <book>_daily_<date>.csv found
# under the given ledgers' candles directories to $out/etoro/daily_<book>.csv.
newest_candles() {
  local book="$1"; shift
  local best=""
  for led in "$@"; do
    for f in "$EDATA/$led/candles/${book}_daily_"*.csv; do
      [ -f "$f" ] || continue
      if [ -z "$best" ] || [ "$(basename "$f")" \> "$(basename "$best")" ]; then best="$f"; fi
    done
  done
  if [ -z "$best" ]; then
    echo "etoro-prereg-evaluation: no candles for $book under $EDATA/{$*}/candles (has its executor run?)" >&2
    return 1
  fi
  cp "$best" "$out/etoro/daily_$book.csv"
  echo "bars $book: $(basename "$best") ($(tail -1 "$best" | cut -d, -f1) last)"
}
newest_candles nsdq100 demo
newest_candles spx500 demo
etf_ok=1
newest_candles qqq demo-etf demo-rotation || etf_ok=0
newest_candles spy demo-etf demo-rotation || etf_ok=0

# Yahoo inputs (research-only): fresh unless --no-fetch.
DATA="$out/data"
if [ "$fetch" = 1 ]; then
  mkdir -p "$DATA"
  fetch_flag="-fetch"
else
  DATA="$ROOT/docs/backtest-readiness/evidence-2026-10-10/data"
  fetch_flag=""
  echo "note: --no-fetch: Yahoo series from $DATA (the CFD cell's QQQ/SPY benchmark ends 2026-10-09)"
fi

echo "== index-CFD cell $W_CFD (costs frozen 2026-10-12)"
# shellcheck disable=SC2086
go run ./cmd/index-research $fetch_flag -data "$DATA" -etoro "$out/etoro" -out "$out/cfd" \
  -forward "$W_CFD" -spread-bps-nsdq100 15.5 -spread-bps-spx500 14.8 -overnight-bps 2.3 \
  -size 1100 -etf-fee 1.5 -sims "$sims" -seed 1 -costs-status "FROZEN 2026-10-12" | tee "$out/cfd/forward.txt"

if [ "$etf_ok" = 1 ]; then
  echo "== ETF cell $W_ETF (costs frozen 2026-10-21)"
  go run ./cmd/etf-trend-research -data "$DATA" -etoro "$out/etoro" -out "$out/etf" \
    -forward "$W_ETF" -etf-fee-bps 15 -spread-bps-qqq 0.28 -spread-bps-spy 0.26 \
    -sims "$sims" -seed 1 -costs-status "FROZEN 2026-10-21" | tee "$out/etf/forward.txt"
  echo "== dual-momentum cell $W_ROT (costs frozen 2026-10-21)"
  go run ./cmd/rotation-research -data "$DATA" -etoro-etf "$out/etoro" -out "$out/rotation" \
    -forward "$W_ROT" -primary dm_12_1 -etf-fee-bps 15 -spread-bps-qqq 0.28 -spread-bps-spy 0.26 \
    -sims "$sims" -seed 1 -costs-status "FROZEN 2026-10-21" | tee "$out/rotation/forward.txt"
  echo "== dual-momentum state at the latest bar"
  go run ./cmd/rotation-research -data "$DATA" -etoro-etf "$out/etoro" -out "$out/rotation" -state 2>/dev/null \
    | sed -n '/^{/,$p' | tee "$out/rotation/state.json"
else
  echo "ETF and dual-momentum cells skipped: no qqq / spy candles yet (their executors first run on 2026-10-22)"
fi

(cd "$out" && find . -type f ! -name SHA256SUMS | sort | xargs sha256sum > SHA256SUMS)
echo "written: $out"
