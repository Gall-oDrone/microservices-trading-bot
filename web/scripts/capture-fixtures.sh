#!/bin/sh
# Re-capture the MSW fixtures in src/mocks/fixtures from a running ui-api.
#   (cd services/ui-api && go run ./cmd -ledgers stage=...,dry-run=... -archive s3://<bucket>) &   then:   npm run fixtures
# Layout: healthz.json and ledgers.json at the top, then one directory per
# ledger with forward-tests.json, risk.json, health-data.json, controls.json, ledger-<book>.json, candles-<book>.json.
set -eu
API="${UI_API_URL:-http://127.0.0.1:8090}"
OUT="$(dirname "$0")/../src/mocks/fixtures"
mkdir -p "$OUT"
curl -fsS "$API/api/ui/healthz" > "$OUT/healthz.json"
curl -fsS "$API/api/ui/ledgers" > "$OUT/ledgers.json"
# Live snapshot (needs ui-api -live, the default); mock mode replays it over SSE.
curl -fsS "$API/api/ui/live?books=btc_mxn,btc_usd" > "$OUT/live.json"
# The Market page's view (?market=1): depth, spread and the trade tape per book.
curl -fsS "$API/api/ui/live?books=btc_mxn,btc_usd&market=1" > "$OUT/live-market.json"
# Strategies page: ledgers, kill switch and the strategy-executor's strategies
# (needs ui-api -strategy-executor-url and -operator-token-file for the
# controls to read "enabled"). GET only; the capture never starts or stops anything.
curl -fsS "$API/api/ui/strategies" > "$OUT/strategies.json"
LEDGERS=$(node -e 'const l=JSON.parse(require("fs").readFileSync(process.argv[1],"utf8")).ledgers;console.log(l.map(x=>x.name).join(" "))' "$OUT/ledgers.json")
for l in $LEDGERS; do
  D="$OUT/$l"
  rm -rf "$D"
  mkdir -p "$D"
  curl -fsS "$API/api/ui/forward-tests?ledger=$l" > "$D/forward-tests.json"
  curl -fsS "$API/api/ui/risk?ledger=$l" > "$D/risk.json"
  # Data health: the archive section is "off" unless ui-api runs with -archive.
  curl -fsS "$API/api/ui/health/data?ledger=$l" > "$D/health-data.json"
  # Operator controls (R4): "enabled" only when ui-api runs with -operator-token-file.
  # GET only; the capture never halts or resumes anything.
  curl -fsS "$API/api/ui/controls?ledger=$l" > "$D/controls.json"
  for b in btc_mxn btc_usd; do
    curl -fsS "$API/api/ui/forward-tests/$b/ledger?ledger=$l" > "$D/ledger-$b.json"
    # A ledger without a candles dir yet has no candle file (404): skip it.
    curl -fsS "$API/api/ui/forward-tests/$b/candles?days=365&ledger=$l" > "$D/candles-$b.json" || rm -f "$D/candles-$b.json"
  done
done
echo "fixtures written to $OUT from $API (ledgers: $LEDGERS)"

# Research fixtures (src/mocks/research): the study list and every study.
# Capture from committed docs only, so local drafts never land in fixtures:
#   git archive HEAD docs/backtest-readiness | tar -x -C /tmp/c && touch /tmp/c/.git
#   go run ./cmd -live=false -addr 127.0.0.1:8091 -studies-dir /tmp/c/docs/backtest-readiness
#   UI_API_RESEARCH_URL=http://127.0.0.1:8091 npm run fixtures
RAPI="${UI_API_RESEARCH_URL:-$API}"
R="$(dirname "$0")/../src/mocks/research"
rm -rf "$R"
mkdir -p "$R"
curl -fsS "$RAPI/api/ui/research/studies" > "$R/studies.json"
for n in $(node -e 'console.log(JSON.parse(require("fs").readFileSync(process.argv[1],"utf8")).studies.map(s=>s.name).join(" "))' "$R/studies.json"); do
  curl -fsS "$RAPI/api/ui/research/studies/$n" > "$R/study-$n.json"
done
# Research runs (daily-research -json reports): the list and every report.
curl -fsS "$RAPI/api/ui/research/runs" > "$R/runs.json"
for id in $(node -e 'console.log(JSON.parse(require("fs").readFileSync(process.argv[1],"utf8")).runs.map(r=>r.id).join(" "))' "$R/runs.json"); do
  curl -fsS "$RAPI/api/ui/research/runs/$id" > "$R/run-$(printf %s "$id" | sed 's#/#--#').json"
done
echo "research fixtures written to $R from $RAPI"
