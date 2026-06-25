#!/usr/bin/env bash
#
# Validate market-data WebSocket freshness hardening on a target environment
# (Buckets C3 + D2 of RECOMMENDED-NEXT-STEPS-2026-06-25):
#   - C3: confirm the 5-layer freshness hardening is healthy on production.
#   - D2: confirm the reconnect rate dropped after the WS inbox-close fix.
#
# It scrapes market-data /metrics twice over an interval and checks:
#   * market_data_last_trade_age_seconds below threshold (stream is fresh)
#   * websocket reconnect rate (total + silence-triggered) over the window
#   * whether REST fallback is currently carrying the stream
#
# Usage:
#   ./scripts/validate-prod-ws-hardening.sh [--url URL] [--interval 120] [--max-age 300] [--max-reconnects 1]
#
# Environment:
#   MARKET_DATA_URL  base URL (default: http://localhost:8082)
#
set -euo pipefail

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; BLUE='\033[0;34m'; NC='\033[0m'
info() { echo -e "${BLUE}[INFO]${NC} $1"; }
ok()   { echo -e "${GREEN}[PASS]${NC} $1"; }
warn() { echo -e "${YELLOW}[WARN]${NC} $1"; }
fail() { echo -e "${RED}[FAIL]${NC} $1"; }

MARKET_DATA_URL="${MARKET_DATA_URL:-http://localhost:8082}"
INTERVAL=120
MAX_AGE=300
MAX_RECONNECTS=1

while [[ $# -gt 0 ]]; do
  case "$1" in
    --url) MARKET_DATA_URL="$2"; shift 2 ;;
    --interval) INTERVAL="$2"; shift 2 ;;
    --max-age) MAX_AGE="$2"; shift 2 ;;
    --max-reconnects) MAX_RECONNECTS="$2"; shift 2 ;;
    -h|--help) sed -n '2,22p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) fail "Unknown option: $1"; exit 1 ;;
  esac
done

scrape() { curl -sS --max-time 15 "$MARKET_DATA_URL/metrics" 2>/dev/null; }
# Sum all series for a metric (ignores labels). Prints 0 if absent.
metric_sum() { echo "$1" | grep -E "^$2(\{|[[:space:]])" | awk '{s+=$NF} END{printf "%.4f", s+0}'; }
metric_max() { echo "$1" | grep -E "^$2(\{|[[:space:]])" | awk '{if($NF>m)m=$NF} END{printf "%.4f", m+0}'; }

info "Target: $MARKET_DATA_URL  (interval=${INTERVAL}s, max_age=${MAX_AGE}s, max_reconnects=${MAX_RECONNECTS})"

M1=$(scrape) || { fail "Cannot scrape $MARKET_DATA_URL/metrics"; exit 2; }
WS_RECON_1=$(metric_sum "$M1" market_data_websocket_reconnects_total)
SIL_RECON_1=$(metric_sum "$M1" market_data_trade_silence_reconnects_total)
REST_1=$(metric_sum "$M1" market_data_rest_fallback_trades_ingested_total)

info "Sampling for ${INTERVAL}s..."
sleep "$INTERVAL"

M2=$(scrape) || { fail "Cannot scrape $MARKET_DATA_URL/metrics (2nd)"; exit 2; }
AGE=$(metric_max "$M2" market_data_last_trade_age_seconds)
WS_RECON_2=$(metric_sum "$M2" market_data_websocket_reconnects_total)
SIL_RECON_2=$(metric_sum "$M2" market_data_trade_silence_reconnects_total)
REST_2=$(metric_sum "$M2" market_data_rest_fallback_trades_ingested_total)
CONNS=$(metric_sum "$M2" market_data_websocket_connections)

WS_DELTA=$(awk "BEGIN{printf \"%.0f\", $WS_RECON_2-$WS_RECON_1}")
SIL_DELTA=$(awk "BEGIN{printf \"%.0f\", $SIL_RECON_2-$SIL_RECON_1}")
REST_DELTA=$(awk "BEGIN{printf \"%.0f\", $REST_2-$REST_1}")

echo ""
info "Results over ${INTERVAL}s window:"
echo "  last_trade_age_seconds (max):     $AGE  (threshold < $MAX_AGE)"
echo "  websocket reconnects (delta):     $WS_DELTA"
echo "  silence-triggered reconnects:     $SIL_DELTA"
echo "  REST-fallback trades ingested:    $REST_DELTA"
echo "  websocket connections (gauge):    $CONNS"
echo ""

FAILURES=0

if awk "BEGIN{exit !($AGE < $MAX_AGE)}"; then
  ok "Trade stream is fresh (age $AGE s < $MAX_AGE s)"
else
  fail "Trade stream STALE (age $AGE s >= $MAX_AGE s)"; FAILURES=$((FAILURES+1))
fi

if [[ "$WS_DELTA" -le "$MAX_RECONNECTS" ]]; then
  ok "WebSocket reconnects within budget ($WS_DELTA <= $MAX_RECONNECTS) — inbox-close fix holding"
else
  fail "Excess WebSocket reconnects ($WS_DELTA > $MAX_RECONNECTS) — investigate WS stability"; FAILURES=$((FAILURES+1))
fi

if [[ "$SIL_DELTA" -gt 0 ]]; then
  warn "Silence watchdog fired $SIL_DELTA time(s) — WS went quiet; mitigation worked but check upstream."
else
  ok "No silence-triggered reconnects"
fi

if [[ "$REST_DELTA" -gt 0 ]]; then
  warn "REST fallback ingested $REST_DELTA trades — WS not fully carrying the stream right now."
else
  ok "WS carrying the stream (no REST fallback needed)"
fi

echo ""
if [[ "$FAILURES" -eq 0 ]]; then
  ok "Production WS hardening validated."
  exit 0
else
  fail "$FAILURES check(s) failed."
  exit 1
fi
