#!/usr/bin/env bash
# Full order exercise (INTRADAY plan — Priority 1 operational validation):
# publish BUY then SELL trade signals to Kafka (topic trading.signals) for the trading-engine
# to place limit orders on Bitso stage. Intended MXN notional per order: TARGET_NOTIONAL_MXN
# (default 2000, within 1000–3000). Order-management should show orders_created_total, orders_active,
# then orders_filled_total after Bitso sync.
#
# Prerequisites: trading-engine running (consumes trading.signals), Kafka reachable, order-management
# + Bitso sync + orders-placed consumer for metrics on Grafana.
#
# Usage:
#   ./scripts/exercise-stage-orders.sh
#   USE_KUBECTL=1 NAMESPACE=bitso-trading-dev ./scripts/exercise-stage-orders.sh   # produce via in-cluster Kafka pod
# Env:
#   USE_KUBECTL         (default 0) — set to 1 to use kubectl exec into Kafka pod (microservices on Kubernetes)
#   NAMESPACE           (default bitso-trading-dev) — namespace for Kafka pod lookup
#   KAFKA_BROKERS       (default localhost:9092) — used with kcat only
#   KAFKA_TOPIC         (default trading.signals)
#   BITSO_API_BASE      (default https://stage.bitso.com/api) — Bitso Stage REST
#   BOOK                (default btc_mxn) — must match trading-engine configured book
#   TARGET_NOTIONAL_MXN (default 2000) — target major*quote ≈ MXN for sizing amount
#   SLEEP_BETWEEN_SEC   (default 60) — wait after BUY before SELL (fills + sync)
#   KAFKA_PRODUCER_CMD  optional: overrides producer (stdin → Kafka); set automatically when USE_KUBECTL=1

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

KAFKA_BROKERS="${KAFKA_BROKERS:-localhost:9092}"
KAFKA_TOPIC="${KAFKA_TOPIC:-trading.signals}"
BITSO_API_BASE="${BITSO_API_BASE:-https://stage.bitso.com/api}"
BOOK="${BOOK:-btc_mxn}"
# Default notional: Bitso stage accounts often have limited MXN; override if your stage wallet is funded.
TARGET_NOTIONAL_MXN="${TARGET_NOTIONAL_MXN:-2000}"
SLEEP_BETWEEN_SEC="${SLEEP_BETWEEN_SEC:-60}"
MIN_MAJOR="${MIN_MAJOR:-0.001}"
MAX_MAJOR="${MAX_MAJOR:-0.1}"
USE_KUBECTL="${USE_KUBECTL:-0}"
NAMESPACE="${NAMESPACE:-bitso-trading-dev}"

RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
NC='\033[0m'
info() { echo -e "${BLUE}[INFO]${NC} $*"; }
ok() { echo -e "${GREEN}[OK]${NC} $*"; }
fail() { echo -e "${RED}[FAIL]${NC} $*"; exit 1; }

if ! command -v jq &>/dev/null; then
  fail "jq is required (sudo yum install -y jq / apt install jq)"
fi

if [[ "$USE_KUBECTL" == "1" ]]; then
  command -v kubectl &>/dev/null || fail "kubectl required when USE_KUBECTL=1"
  KAFKA_POD="$(kubectl get pods -n "$NAMESPACE" -l app=bitso-trading-platform,service=kafka -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || true)"
  [[ -n "$KAFKA_POD" ]] || fail "No Kafka pod in namespace $NAMESPACE (labels: app=bitso-trading-platform, service=kafka)"
  # shellcheck disable=SC2016
  KAFKA_PRODUCER_CMD="kubectl exec -i -n ${NAMESPACE} ${KAFKA_POD} -- /opt/kafka/bin/kafka-console-producer.sh --bootstrap-server localhost:9092 --topic ${KAFKA_TOPIC}"
  info "Kubernetes: Kafka pod=$KAFKA_POD namespace=$NAMESPACE (Bitso Stage ticker + cluster Kafka)"
fi

TICKER_URL="${BITSO_API_BASE}/v3/ticker?book=${BOOK}"
info "Fetching ticker: GET $TICKER_URL"
TICKER_JSON="$(curl -sS --max-time 15 "$TICKER_URL")" || fail "curl ticker failed"
echo "$TICKER_JSON" | jq -e '.success == true' >/dev/null || fail "Bitso ticker error: $TICKER_JSON"

LAST="$(echo "$TICKER_JSON" | jq -r '.payload.last')"
BID="$(echo "$TICKER_JSON" | jq -r '.payload.bid')"
ASK="$(echo "$TICKER_JSON" | jq -r '.payload.ask')"

# Amount (major units) from target MXN notional; clamp to sane BTC-style bounds (matches typical engine config)
RAW_AMT="$(awk -v t="$TARGET_NOTIONAL_MXN" -v l="$LAST" 'BEGIN { printf "%.8f", (t / l) }')"
AMT="$(awk -v a="$RAW_AMT" -v mn="$MIN_MAJOR" -v mx="$MAX_MAJOR" 'BEGIN {
  if (a < mn) a = mn
  if (a > mx) a = mx
  printf "%.8f", a
}')"

NOTIONAL_APPROX="$(awk -v a="$AMT" -v l="$LAST" 'BEGIN { printf "%.2f", a * l }')"
info "Ticker last=$LAST bid=$BID ask=$ASK (MXN)"
info "Target notional ≈ ${TARGET_NOTIONAL_MXN} MXN → amount=${AMT} major (≈ ${NOTIONAL_APPROX} MXN at last)"

# Engine validation: BUY price must be <= ask * 1.05; SELL price must be >= bid * 0.95
BUY_PRICE="$(awk -v a="$ASK" 'BEGIN { printf "%.2f", a * 0.9995 }')"
SELL_PRICE="$(awk -v b="$BID" 'BEGIN { printf "%.2f", b * 1.0005 }')"

TS="$(date +%s)"
BUY_EVENT_ID="exercise-buy-${TS}"
SELL_EVENT_ID="exercise-sell-${TS}"

buy_json="$(jq -nc \
  --arg id "$BUY_EVENT_ID" \
  --arg book "$BOOK" \
  --argjson ts "$(date +%s)" \
  --arg p "$BUY_PRICE" \
  --arg a "$AMT" \
  '{event_id:$id, timestamp:$ts, book:$book, strategy:"basic", signal:"BUY", price:($p|tonumber), amount:($a|tonumber), metadata:{reason:"exercise-stage-orders.sh", leg:"buy"}}')"

echo ""
echo "========== Parameters (record for Grafana) =========="
echo "  BITSO_API_BASE:     $BITSO_API_BASE"
echo "  BOOK:               $BOOK"
echo "  KAFKA_TOPIC:        $KAFKA_TOPIC"
if [[ "$USE_KUBECTL" == "1" ]]; then
  echo "  Kafka delivery:    kubectl exec → pod ${KAFKA_POD:-?} ($NAMESPACE)"
else
  echo "  KAFKA_BROKERS:      $KAFKA_BROKERS"
fi
echo "  TARGET_NOTIONAL_MXN: $TARGET_NOTIONAL_MXN"
echo "  Amount (major):     $AMT"
echo "  BUY  limit price:   $BUY_PRICE MXN (≤ ask×1.05)"
echo "  SELL limit price:   $SELL_PRICE MXN (≥ bid×0.95)"
echo "  Sleep before SELL:  ${SLEEP_BETWEEN_SEC}s"
echo "======================================================"
echo ""

produce_line() {
  local line="$1"
  if [[ -n "${KAFKA_PRODUCER_CMD:-}" ]]; then
    echo "$line" | eval "$KAFKA_PRODUCER_CMD"
    return
  fi
  if command -v kcat &>/dev/null; then
    echo "$line" | kcat -P -b "$KAFKA_BROKERS" -t "$KAFKA_TOPIC"
    return
  fi
  if command -v docker-compose &>/dev/null && [[ -f "$REPO_ROOT/docker-compose.yml" ]]; then
    echo "$line" | docker-compose -f "$REPO_ROOT/docker-compose.yml" exec -T kafka \
      kafka-console-producer --bootstrap-server kafka:9092 --topic "$KAFKA_TOPIC"
    return
  fi
  fail "No Kafka producer: USE_KUBECTL=1, install kcat, set KAFKA_PRODUCER_CMD, or use docker-compose kafka service."
}

info "Publishing BUY signal..."
echo "$buy_json"
produce_line "$buy_json"
ok "BUY published"

info "Waiting ${SLEEP_BETWEEN_SEC}s (allow fill + order-management Bitso sync + metrics)..."
sleep "$SLEEP_BETWEEN_SEC"

# Refresh ticker for SELL validation (bid may move)
TICKER_JSON2="$(curl -sS --max-time 15 "$TICKER_URL")" || fail "curl ticker failed (second)"
BID2="$(echo "$TICKER_JSON2" | jq -r '.payload.bid')"
SELL_PRICE="$(awk -v b="$BID2" 'BEGIN { printf "%.2f", b * 1.0005 }')"
sell_json="$(jq -nc \
  --arg id "$SELL_EVENT_ID" \
  --arg book "$BOOK" \
  --argjson ts "$(date +%s)" \
  --arg p "$SELL_PRICE" \
  --arg a "$AMT" \
  '{event_id:$id, timestamp:$ts, book:$book, strategy:"basic", signal:"SELL", price:($p|tonumber), amount:($a|tonumber), metadata:{reason:"exercise-stage-orders.sh", leg:"sell"}}')"

info "Publishing SELL signal (bid refreshed → sell price $SELL_PRICE MXN)..."
echo "$sell_json"
produce_line "$sell_json"
ok "SELL published"

echo ""
ok "Done. Watch Grafana: orders_created_total, sum(orders_active), orders_filled_total (order-management job)."
