#!/bin/sh
# Re-capture the MSW fixtures in src/mocks/fixtures from a running ui-api.
#   (cd services/ui-api && go run ./cmd) &   then:   npm run fixtures
set -eu
API="${UI_API_URL:-http://127.0.0.1:8090}"
OUT="$(dirname "$0")/../src/mocks/fixtures"
mkdir -p "$OUT"
curl -fsS "$API/api/ui/healthz" > "$OUT/healthz.json"
curl -fsS "$API/api/ui/forward-tests" > "$OUT/forward-tests.json"
curl -fsS "$API/api/ui/risk" > "$OUT/risk.json"
for b in btc_mxn btc_usd; do
  curl -fsS "$API/api/ui/forward-tests/$b/ledger" > "$OUT/ledger-$b.json"
  curl -fsS "$API/api/ui/forward-tests/$b/candles?days=365" > "$OUT/candles-$b.json"
done
echo "fixtures written to $OUT from $API"
