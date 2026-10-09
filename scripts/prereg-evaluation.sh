#!/usr/bin/env bash
# Runs the procedure frozen in the two SMA50 pre-registrations and writes the
# verdicts (plan §6.4.13):
#
#   docs/backtest-readiness/FORWARD-TEST-PREREGISTRATION-SMA50-2026-09-27.md        (btc_mxn)
#   docs/backtest-readiness/FORWARD-TEST-PREREGISTRATION-SMA50-BTCUSD-2026-09-29.md (btc_usd)
#
#   scripts/prereg-evaluation.sh                      # progress report as of the latest bar
#   scripts/prereg-evaluation.sh --phase interim      # on/after 2027-03-26 (report only)
#   scripts/prereg-evaluation.sh --phase final        # on/after 2027-09-26 (decides)
#
# Steps, exactly as registered: fetch both Bitso daily series (cmd/bitso-daily), run
# cmd/daily-research on the forward windows at primary and secondary costs (with -json
# next to the text output), cross-check the btc_usd MXN terms with
# scripts/research/mxn-terms.py, then cmd/prereg-report turns the JSON into the H1/H2/H3
# verdicts (report.md / report.json). Everything lands in --out (default
# docs/backtest-readiness/evidence-<today>/prereg-<phase>/) with SHA256SUMS; commit it
# with a short results note linking the pre-registrations.
#
# Options:
#   --phase as-of|interim|final   (default as-of)
#   --out DIR
#   --usd-csv FILE --mxn-csv FILE  use these daily CSVs instead of fetching (offline re-runs)
#
# Read-only: public Bitso market data; no keys, no orders.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
phase="as-of" out="" usd_csv="" mxn_csv=""
while [ $# -gt 0 ]; do
  case "$1" in
    --phase) phase="$2"; shift 2 ;;
    --out) out="$2"; shift 2 ;;
    --usd-csv) usd_csv="$2"; shift 2 ;;
    --mxn-csv) mxn_csv="$2"; shift 2 ;;
    -h | --help) sed -n '2,25p' "$0"; exit 0 ;;
    *) echo "unknown option $1" >&2; exit 2 ;;
  esac
done
case "$phase" in
  as-of | final) end="2027-09-26" ;;
  interim) end="2027-03-26" ;;
  *) echo "--phase must be as-of, interim or final" >&2; exit 2 ;;
esac
if { [ -n "$usd_csv" ] && [ -z "$mxn_csv" ]; } || { [ -z "$usd_csv" ] && [ -n "$mxn_csv" ]; }; then
  echo "--usd-csv and --mxn-csv go together" >&2
  exit 2
fi
out="${out:-$ROOT/docs/backtest-readiness/evidence-$(date -u +%F)/prereg-$phase}"
mkdir -p "$out/usd" "$out/mxn" "$out/mxn-on-usd"
out="$(cd "$out" && pwd)"
W_MXN="2026-09-27:$end"
W_USD="2026-09-29:$end"

cd "$ROOT/services/strategy-executor"
if [ -n "$usd_csv" ]; then
  cp "$usd_csv" "$out/usd/btc_usd_daily.csv"
  cp "$mxn_csv" "$out/mxn/btc_mxn_daily.csv"
else
  go run ./cmd/bitso-daily -book btc_usd -from 2020-04-01 -out "$out/usd/btc_usd_daily.csv"
  go run ./cmd/bitso-daily -book btc_mxn -from 2017-06-01 -out "$out/mxn/btc_mxn_daily.csv"
fi
# The btc_usd H2 benchmark is btc_mxn over the btc_usd window; a separate dir keeps
# daily-research from loading both books.
cp "$out/mxn/btc_mxn_daily.csv" "$out/mxn-on-usd/btc_mxn_daily.csv"

go build -o "$out/.daily-research" ./cmd/daily-research
go build -o "$out/.prereg-report" ./cmd/prereg-report
run() { # name prices window costs…
  local name="$1" prices="$2" win="$3"
  shift 3
  "$out/.daily-research" -prices "$prices" -windows "$win" "$@" -json "$out/$name.json" >"$out/$name.txt"
}
run mxn-primary "$out/mxn" "$W_MXN" -buy-bps 60 -sell-bps 60 -slippage-bps 10
run mxn-secondary "$out/mxn" "$W_MXN" -buy-bps 78 -sell-bps 78 -slippage-bps 10
run usd-primary "$out/usd" "$W_USD" -buy-bps 30 -sell-bps 30 -slippage-bps 10
run usd-secondary "$out/usd" "$W_USD" -buy-bps 36 -sell-bps 36 -slippage-bps 10
run mxn-on-usd-primary "$out/mxn-on-usd" "$W_USD" -buy-bps 60 -sell-bps 60 -slippage-bps 10
run mxn-on-usd-secondary "$out/mxn-on-usd" "$W_USD" -buy-bps 78 -sell-bps 78 -slippage-bps 10

# Cross-check with the registered script (its numbers must match report.md's H2 row).
python3 "$ROOT/scripts/research/mxn-terms.py" "$out/usd/btc_usd_daily.csv" "$out/mxn/btc_mxn_daily.csv" \
  "$out/usd-primary.txt" "$out/mxn-on-usd-primary.txt" 60 >"$out/mxn-terms-primary.txt"
python3 "$ROOT/scripts/research/mxn-terms.py" "$out/usd/btc_usd_daily.csv" "$out/mxn/btc_mxn_daily.csv" \
  "$out/usd-secondary.txt" "$out/mxn-on-usd-secondary.txt" 78 >"$out/mxn-terms-secondary.txt"

"$out/.prereg-report" -phase "$phase" \
  -mxn-primary "$out/mxn-primary.json" -mxn-secondary "$out/mxn-secondary.json" \
  -usd-primary "$out/usd-primary.json" -usd-secondary "$out/usd-secondary.json" \
  -mxn-on-usd-primary "$out/mxn-on-usd-primary.json" -mxn-on-usd-secondary "$out/mxn-on-usd-secondary.json" \
  -usd-csv "$out/usd/btc_usd_daily.csv" -mxn-csv "$out/mxn/btc_mxn_daily.csv" \
  -out-json "$out/report.json" -out-md "$out/report.md"
rm -f "$out/.daily-research" "$out/.prereg-report"
(cd "$out" && find . -type f ! -name SHA256SUMS | sort | xargs sha256sum >SHA256SUMS)
echo "wrote $out/report.md"
