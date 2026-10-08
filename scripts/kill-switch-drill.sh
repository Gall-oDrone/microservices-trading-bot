#!/usr/bin/env bash
# Kill-switch drill (docs/frontend/FRONTEND-UI-PLAN-2026-10-03.md §8.13, §8.14).
#
# Rehearses ui-api's "halt all ledgers" end to end without touching a real
# ledger: it copies a ledger dir (default: the stage ledger) into three scratch
# ledgers (alpha, beta, gamma), starts a throwaway ui-api that knows ONLY those
# copies (own port, own 0600 token, live data off), and checks:
#
#   1. the gates: no/wrong token 401, foreign Origin 403, text/plain 415, wrong
#      phrase / short reason / unknown field 400, GET on a control 405;
#      nothing is written;
#   2. a ledger halted beforehand (gamma) is left alone and reported;
#   3. halt-all halts the others, one audited change per ledger, one group id;
#   4. ui-api's /risk reads every halt back (the executor's parser);
#   5. the daily-executor itself reports HALTED for a halted copy (dry run,
#      -no-record, no keys; it fetches public candles after printing the halt)
#      and refuses to run over a corrupt halt file;
#   6. ui-alerts -dry-run raises "Trading halted" for each halted copy and
#      names every halted ledger in the subject;
#   7. a double press is serialised: one press halts, the other reports
#      already_halted for every ledger;
#   8. halt-all over a corrupt halt file replaces it with a valid halt;
#   9. resume restores every copy; the source ledger dir is unchanged.
#
# It never writes outside its work dir, never talks to S3 and never places an
# order. Exit 0 when every check passes.
#
# Usage:
#   scripts/kill-switch-drill.sh                         # copies the stage ledger dir
#   scripts/kill-switch-drill.sh --source <ledger dir>   # another ledger dir to copy
#   scripts/kill-switch-drill.sh --keep                  # keep the work dir and ui-api logs
#   scripts/kill-switch-drill.sh --offline               # executor gets no candles (no network)
#   scripts/kill-switch-drill.sh --no-executor           # skip the daily-executor checks
#   DRILL_DIR=<dir> DRILL_PORT=8095 scripts/kill-switch-drill.sh
#
# CI (.github/workflows/operator-ui.yml) runs it on the checked-in fixture:
#   scripts/kill-switch-drill.sh --offline --source services/ui-api/internal/api/testdata
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SOURCE="$ROOT/services/strategy-executor/daily-executor-data/stage"
KEEP=0
SKIP_EXECUTOR=0
CANDLES_URL=()
while [ $# -gt 0 ]; do
  case "$1" in
    --source) SOURCE="$(cd "$2" && pwd)"; shift 2 ;;
    --keep) KEEP=1; shift ;;
    --no-executor) SKIP_EXECUTOR=1; shift ;; # skip steps 5 and 8's executor runs
    # The executor prints HALTED before it fetches candles; point the fetch at
    # a closed local port so the drill never reaches the Bitso API.
    --offline) CANDLES_URL=(-candles-base-url http://127.0.0.1:9); shift ;;
    -h|--help) sed -n '2,37p' "$0"; exit 0 ;;
    *) echo "unknown flag $1" >&2; exit 2 ;;
  esac
done
PORT="${DRILL_PORT:-8095}"
API="http://127.0.0.1:$PORT/api/ui"
[ -f "$SOURCE/ledger.jsonl" ] || { echo "no ledger.jsonl in $SOURCE" >&2; exit 2; }
if curl -s -o /dev/null "http://127.0.0.1:$PORT/"; then echo "port $PORT is in use (set DRILL_PORT)" >&2; exit 2; fi

WORK="${DRILL_DIR:-$(mktemp -d -t kill-switch-drill.XXXXXX)}"
mkdir -p "$WORK"
WORK="$(cd "$WORK" && pwd)"
case "$WORK" in "$SOURCE"|"$SOURCE"/*) echo "work dir must not be inside the source ledger dir" >&2; exit 2 ;; esac
UI_PID=""
cleanup() {
  [ -n "$UI_PID" ] && kill "$UI_PID" 2>/dev/null || true
  if [ "$KEEP" = 1 ]; then echo "work dir kept: $WORK"; else rm -rf "$WORK"; fi
}
trap cleanup EXIT

PASS=0; FAIL=0
ok()   { PASS=$((PASS + 1)); printf '  \033[32mPASS\033[0m %s\n' "$1"; }
bad()  { FAIL=$((FAIL + 1)); printf '  \033[31mFAIL\033[0m %s\n' "$1"; }
check() { if [ "$2" = "$3" ]; then ok "$1"; else bad "$1 (want $3, got $2)"; fi; }
step() { printf '\n\033[1m%s\033[0m\n' "$1"; }
json() { python3 -c "import json,sys; d=json.load(sys.stdin); print($1)"; }

# Snapshot of the source dir, compared at the end.
src_state() { (cd "$SOURCE" && { md5sum ledger.jsonl; ls -la risk-state.json 2>/dev/null || echo "no halt file"; }) }
BEFORE="$(src_state)"

step "Setup: scratch ledgers, token, binaries, drill-only ui-api on :$PORT"
for n in alpha beta gamma; do
  mkdir -p "$WORK/$n"
  cp "$SOURCE/ledger.jsonl" "$WORK/$n/"
  [ -d "$SOURCE/candles" ] && cp -r "$SOURCE/candles" "$WORK/$n/"
done
(umask 077; head -c 32 /dev/urandom | od -An -tx1 | tr -d ' \n' > "$WORK/operator-token")
TOK="$(cat "$WORK/operator-token")"
(cd "$ROOT/services/ui-api" && go build -o "$WORK/bin/ui-api" ./cmd && go build -o "$WORK/bin/ui-alerts" ./cmd/ui-alerts)
(cd "$ROOT/services/strategy-executor" && go build -o "$WORK/bin/daily-executor" ./cmd/daily-executor)
LEDGERS="alpha=$WORK/alpha/ledger.jsonl,beta=$WORK/beta/ledger.jsonl,gamma=$WORK/gamma/ledger.jsonl"
"$WORK/bin/ui-api" -addr "127.0.0.1:$PORT" -live=false -ledgers "$LEDGERS" \
  -operator-token-file "$WORK/operator-token" -studies-dir "$WORK/none" > "$WORK/ui-api.log" 2>&1 &
UI_PID=$!
for _ in $(seq 1 50); do curl -s -o /dev/null "$API/healthz" && break; sleep 0.2; done
curl -s "$API/strategies" | json "d['kill_switch']['targets']" | grep -q "'alpha', 'beta', 'gamma'" \
  && ok "ui-api up; kill-switch targets are the three copies only" || { bad "ui-api did not start (see $WORK/ui-api.log)"; KEEP=1; exit 1; }

post() { # post <path> <json> [extra curl args...] -> prints "<status> <body>"
  local p="$1" b="$2"; shift 2
  curl -s -o "$WORK/resp.json" -w '%{http_code}' -X POST -H 'Content-Type: application/json' "$@" -d "$b" "$API/$p"
}
auth=(-H "Authorization: Bearer $TOK")
PHRASE='{"reason":"Kill switch drill","by":"drill","confirm":"HALT ALL"}'
halted() { curl -s "$API/risk?ledger=$1" | json "str(d['halted']).lower() + ' ' + d['halt_source']"; }

step "1. Gates (nothing may be written)"
check "no token -> 401" "$(post risk/halt-all "$PHRASE")" 401
check "wrong token -> 401" "$(post risk/halt-all "$PHRASE" -H 'Authorization: Bearer wrong-token-0000000000000000000000')" 401
check "foreign Origin -> 403" "$(post risk/halt-all "$PHRASE" "${auth[@]}" -H 'Origin: https://evil.example')" 403
check "text/plain -> 415" "$(curl -s -o /dev/null -w '%{http_code}' -X POST "${auth[@]}" -H 'Content-Type: text/plain' -d "$PHRASE" "$API/risk/halt-all")" 415
check "wrong phrase -> 400" "$(post risk/halt-all '{"reason":"Kill switch drill","by":"drill","confirm":"halt all"}' "${auth[@]}")" 400
check "short reason -> 400" "$(post risk/halt-all '{"reason":"drill","by":"drill","confirm":"HALT ALL"}' "${auth[@]}")" 400
check "unknown field -> 400" "$(post risk/halt-all '{"reason":"Kill switch drill","by":"drill","confirm":"HALT ALL","ledgers":["alpha"]}' "${auth[@]}")" 400
check "no halt file written" "$(ls "$WORK"/*/risk-state.json 2>/dev/null | wc -l | tr -d ' ')" 0
check "every refusal audited in every ledger" "$(cat "$WORK"/{alpha,beta,gamma}/ui-audit.jsonl | python3 -c "import sys,json; print(sum(1 for l in sys.stdin if json.loads(l)['outcome'] in ('denied','refused')))")" 21
check "GET on a control -> 405 (Allow: POST)" "$(curl -s -o /dev/null -D - -w '%{http_code}' "${auth[@]}" "$API/risk/halt-all" | tr -d '\r' | awk 'tolower($1)=="allow:"{a=$2} /^[0-9]+$/{c=$0} END{print c" "a}')" "405 POST"

step "2-3. gamma halted beforehand, then the kill switch"
check "halt gamma alone" "$(post 'risk/halt?ledger=gamma' '{"reason":"Drill: gamma halted beforehand","by":"drill","confirm":"gamma"}' "${auth[@]}")" 200
t0=$(date +%s%N)
check "halt-all -> 200" "$(post risk/halt-all '{"reason":"Kill switch drill: first press","by":"drill","confirm":"HALT ALL"}' "${auth[@]}")" 200
ms=$(( ($(date +%s%N) - t0) / 1000000 ))
check "outcomes" "$(json "' '.join(r['ledger']+'='+r['outcome'] for r in d['results'])" < "$WORK/resp.json")" "alpha=halted beta=halted gamma=already_halted"
GROUP="$(json "d['group']" < "$WORK/resp.json")"
check "gamma keeps its own halt reason" "$(json "d['reason']" < "$WORK/gamma/risk-state.json")" "Drill: gamma halted beforehand"
check "one group id in every audit log" "$(grep -l "\"group\":\"$GROUP\"" "$WORK"/{alpha,beta,gamma}/ui-audit.jsonl | wc -l | tr -d ' ')" 3
echo "  (halt-all answered in ${ms} ms)"

step "4. ui-api /risk reads the halts back"
for n in alpha beta gamma; do check "$n" "$(halted $n)" "true file"; done

step "5. daily-executor reads the halt (dry run, -no-record, no keys)"
if [ "$SKIP_EXECUTOR" = 1 ]; then echo "  skipped (--no-executor)"; else
  out="$(env -u STAGE_BITSO_API_KEY -u STAGE_BITSO_API_SECRET timeout 60 "$WORK/bin/daily-executor" \
    -ledger "$WORK/beta/ledger.jsonl" -books btc_mxn -no-record -env-file "" ${CANDLES_URL[@]+"${CANDLES_URL[@]}"} 2>&1 || true)"
  echo "$out" | grep -q "^HALTED by $WORK/beta/risk-state.json: Kill switch drill: first press" \
    && ok "beta: HALTED, stage orders would be blocked and recorded" || bad "beta: no HALTED line: $(echo "$out" | head -3)"
fi

step "6. ui-alerts (dry run: prints, sends nothing, writes no state)"
alerts="$("$WORK/bin/ui-alerts" -dry-run -state "$WORK/alerts-state.json" -ledgers "$LEDGERS" 2>&1 || true)"
for n in alpha beta gamma; do
  echo "$alerts" | grep -q "Trading halted ($n)" && ok "alert for $n" || bad "no alert for $n"
done
echo "$alerts" | grep -q '^Subject: \[mtb-ops\] .*Trading halted (alpha, beta, gamma)' \
  && ok "subject names every halted ledger" || bad "subject: $(echo "$alerts" | grep '^Subject:')"
echo "$alerts" | grep -q '^Halted ledgers now: alpha, beta, gamma$' \
  && ok "body lists the halted ledgers" || bad "body lacks 'Halted ledgers now'"
check "no alert state written" "$([ -e "$WORK/alerts-state.json" ] && echo yes || echo no)" no

step "7. Double press is serialised"
for n in alpha beta gamma; do post "risk/resume?ledger=$n" "{\"reason\":\"Drill: reset before the double press\",\"by\":\"drill\",\"confirm\":\"$n\"}" "${auth[@]}" > /dev/null; done
B='{"reason":"Kill switch drill: double press","by":"drill","confirm":"HALT ALL"}'
curl -s -X POST "${auth[@]}" -H 'Content-Type: application/json' -d "$B" "$API/risk/halt-all" > "$WORK/p1.json" &
P1=$!
curl -s -X POST "${auth[@]}" -H 'Content-Type: application/json' -d "$B" "$API/risk/halt-all" > "$WORK/p2.json" &
P2=$!
wait "$P1" "$P2"
check "one press halted all three, the other found them halted" \
  "$(cat "$WORK/p1.json" "$WORK/p2.json" | python3 -c "
import sys, json
dec = json.JSONDecoder(); s = sys.stdin.read().strip(); i = 0; out = []
while i < len(s):
    d, i = dec.raw_decode(s, i); i = len(s) - len(s[i:].lstrip())
    out.append(','.join(sorted(set(r['outcome'] for r in d['results']))))
print(' '.join(sorted(out)))")" "already_halted halted"

step "8. Corrupt halt file"
for n in alpha beta gamma; do post "risk/resume?ledger=$n" "{\"reason\":\"Drill: reset before the corrupt file\",\"by\":\"drill\",\"confirm\":\"$n\"}" "${auth[@]}" > /dev/null; done
echo '{"halted": "yes"' > "$WORK/alpha/risk-state.json"
if [ "$SKIP_EXECUTOR" = 0 ]; then
  out="$("$WORK/bin/daily-executor" -ledger "$WORK/alpha/ledger.jsonl" -no-record -env-file "" ${CANDLES_URL[@]+"${CANDLES_URL[@]}"} 2>&1 || true)"
  echo "$out" | grep -q "nothing ran" && ok "daily-executor refuses to run (fails closed)" || bad "daily-executor ran over a corrupt halt file"
fi
check "page shows it as not halted with an error" "$(curl -s "$API/strategies" | json "[l for l in d['ledgers'] if l['name']=='alpha'][0]['halt_file'].get('error','') != ''")" True
check "halt-all over it -> 200" "$(post risk/halt-all '{"reason":"Kill switch drill: over a corrupt file","by":"drill","confirm":"HALT ALL"}' "${auth[@]}")" 200
check "alpha now holds a valid halt" "$(halted alpha)" "true file"

step "9. Resume and clean state"
for n in alpha beta gamma; do
  check "resume $n" "$(post "risk/resume?ledger=$n" "{\"reason\":\"Kill switch drill over\",\"by\":\"drill\",\"confirm\":\"$n\"}" "${auth[@]}")" 200
  check "$n trading allowed" "$(halted $n)" "false none"
done
check "source ledger dir unchanged ($SOURCE)" "$(src_state)" "$BEFORE"

printf '\n%d passed, %d failed\n' "$PASS" "$FAIL"
[ "$FAIL" = 0 ]
