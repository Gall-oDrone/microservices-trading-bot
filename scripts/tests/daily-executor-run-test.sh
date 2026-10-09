#!/usr/bin/env bash
# Tests scripts/daily-executor-run.sh's reconciliation step with fake
# executor and reconcile binaries (no network, no Bitso):
#   - after -stage: the reconcile output and "reconcile=ok|breaks|error" are in
#     the run log before "exit=", and the executor's exit code is kept;
#   - without -stage, or with DAILY_RECONCILE=0: no reconciliation;
#   - a missing reconcile binary is "reconcile=error ..." (exit code kept).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
T="$(mktemp -d)"
trap 'rm -rf "$T"' EXIT

cat >"$T/exec" <<'EOF'
#!/usr/bin/env bash
echo "daily-executor test | mode stage"
exit "${FAKE_EXEC_CODE:-0}"
EOF
cat >"$T/rec" <<'EOF'
#!/usr/bin/env bash
# -ledger L -out O
out=""
while [ $# -gt 0 ]; do case "$1" in -out) out="$2"; shift 2 ;; *) shift ;; esac; done
echo '{"ok":true}' >"$out"
echo "reconcile: 2 legs, 0 leg-less days checked, 0 breaks"
exit "${FAKE_REC_CODE:-0}"
EOF
chmod +x "$T/exec" "$T/rec"

fail=0
check() { # name, condition result
  if [ "$2" = 0 ]; then echo "ok   $1"; else echo "FAIL $1"; fail=1; fi
}
run() { # env... -- args
  local dir="$T/l$RANDOM"
  mkdir -p "$dir"
  set +e
  env DAILY_EXECUTOR_BIN="$T/exec" DAILY_RECONCILE_BIN="$T/rec" DAILY_EXECUTOR_LEDGER="$dir/ledger.jsonl" \
    "$@" "$ROOT/scripts/daily-executor-run.sh" ${ARGS:-} >/dev/null 2>&1
  RC=$?
  set -e
  LOGF="$(ls "$dir"/run-*.log)"
  DIRF="$dir"
}

ARGS=-stage run FAKE_EXEC_CODE=0 FAKE_REC_CODE=0
check "stage ok: exit 0" "$([ "$RC" = 0 ]; echo $?)"
check "stage ok: reconcile=ok before exit=0" "$(tail -2 "$LOGF" | tr '\n' '|' | grep -qx 'reconcile=ok|exit=0|'; echo $?)"
check "stage ok: report written" "$([ -f "$DIRF/reconcile.json" ]; echo $?)"
check "stage ok: reconcile output in log" "$(grep -q '^reconcile: 2 legs' "$LOGF"; echo $?)"

ARGS=-stage run FAKE_EXEC_CODE=1 FAKE_REC_CODE=3
check "breaks: executor exit 1 kept" "$([ "$RC" = 1 ]; echo $?)"
check "breaks: reconcile=breaks line" "$(grep -q '^reconcile=breaks .*reconcile.json$' "$LOGF"; echo $?)"
check "breaks: last line exit=1" "$([ "$(tail -1 "$LOGF")" = "exit=1" ]; echo $?)"

ARGS=-stage run FAKE_REC_CODE=1
check "error: reconcile=error, exit 0 kept" "$(grep -qx 'reconcile=error' "$LOGF" && [ "$RC" = 0 ]; echo $?)"

ARGS= run
check "dry run: no reconcile" "$(! grep -q '^reconcile' "$LOGF"; echo $?)"

ARGS=-stage run DAILY_RECONCILE=0
check "DAILY_RECONCILE=0: no reconcile" "$(! grep -q '^reconcile' "$LOGF"; echo $?)"

ARGS=-stage run DAILY_RECONCILE_BIN="$T/missing"
check "missing binary: reconcile=error with build hint" "$(grep -q '^reconcile=error .*go build' "$LOGF" && [ "$RC" = 0 ]; echo $?)"

exit "$fail"
