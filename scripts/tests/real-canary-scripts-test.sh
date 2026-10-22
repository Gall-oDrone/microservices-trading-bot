#!/usr/bin/env bash
# Tests the real-account canary's operator scripts (docs/etoro/
# ETORO-REAL-CANARY-PLAN-2026-10-21.md §7 C9) without systemd, go, aws or a
# network: a temporary HOME, fake systemctl / go / curl / aws on PATH.
#
#   scripts/install-ui-api-service.sh --real-canary
#     - --print changes nothing and shows the second unit on :8091 with
#       -etoro-real-canary on the command line and Wants=mtb-ui-api.service;
#     - refuses: demo unit absent; token file missing, same path, or same
#       content as the demo one; real env file missing, not 0600, not
#       ETORO_ENV=real; --no-trading;
#     - with everything in place writes the unit and the env file (0600) and
#       leaves the demo unit and ui-api.env untouched;
#     - --token-file with --real-canary names the canary's file only.
#   scripts/install-ops-cron.sh --print [--canary-observe]
#     - without the flag: no canary line, ETORO_CANARY_S3_URI empty;
#     - with it: the 18:00 New York guard line every day, the S3 prefix
#       daily-executor/etoro-real-canary, a warning when the real env file is
#       missing.
#   scripts/ops-run.sh etoro-canary-observe
#     - pins -ledger to real-canary/, keeps the observer's exit code, writes
#       run-<UTC>.log with exit= and uploads it last.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
T="$(mktemp -d)"
trap 'rm -rf "$T"' EXIT

fail=0
check() { # name, condition result
  if [ "$2" = 0 ]; then echo "ok   $1"; else echo "FAIL $1"; fail=1; fi
}

# ---- fakes on PATH ------------------------------------------------------------
mkdir -p "$T/bin"
cat >"$T/bin/systemctl" <<'EOF'
#!/usr/bin/env bash
echo "systemctl $*" >>"${FAKE_LOG:?}"
exit 0
EOF
cat >"$T/bin/go" <<'EOF'
#!/usr/bin/env bash
echo "go $*" >>"${FAKE_LOG:?}"
exit 0
EOF
cat >"$T/bin/curl" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
cat >"$T/bin/aws" <<'EOF'
#!/usr/bin/env bash
echo "aws $*" >>"${FAKE_LOG:?}"
exit 0
EOF
chmod +x "$T/bin/"*
export FAKE_LOG="$T/fake.log"
export PATH="$T/bin:$PATH"

# ---- a temporary HOME with the demo console installed ----------------------------
export HOME="$T/home"
CONF="$HOME/.config/microservices-trading-bot"
UNITS="$HOME/.config/systemd/user"
mkdir -p "$CONF" "$UNITS" "$HOME/.config/mtb"
echo "demo unit" >"$UNITS/mtb-ui-api.service"
echo "UI_API_OPERATOR_TOKEN_FILE=$HOME/.config/mtb/operator-token" >"$CONF/ui-api.env"
(umask 077; echo "demo-token-0123456789abcdef0123456789abcdef" >"$HOME/.config/mtb/operator-token")
cp -p "$UNITS/mtb-ui-api.service" "$T/demo-unit.orig"
cp -p "$CONF/ui-api.env" "$T/demo-env.orig"

inst() { # args… ; sets RC, OUT
  set +e
  OUT="$("$ROOT/scripts/install-ui-api-service.sh" "$@" 2>&1)"
  RC=$?
  set -e
}

# --print: nothing written, the right shape.
inst --real-canary --print
check "print: exit 0" "$([ "$RC" = 0 ]; echo $?)"
check "print: ExecStart carries -etoro-real-canary" "$(grep -q '^ExecStart=.*/bin/ui-api -etoro-real-canary$' <<<"$OUT"; echo $?)"
check "print: Wants the demo unit" "$(grep -q '^Wants=.*mtb-ui-api.service' <<<"$OUT"; echo $?)"
check "print: :8091" "$(grep -q '^UI_API_ADDR=127.0.0.1:8091$' <<<"$OUT"; echo $?)"
check "print: canary ledger under real-canary/" "$(grep -q '^UI_API_ETORO_LEDGER=etoro-real-canary=.*/etoro-daily-data/real-canary/ledger.jsonl$' <<<"$OUT"; echo $?)"
check "print: own token file" "$(grep -q '^UI_API_OPERATOR_TOKEN_FILE=.*/operator-token-real-canary$' <<<"$OUT"; echo $?)"
check "print: demo token file named for the comparison" "$(grep -q '^UI_API_DEMO_OPERATOR_TOKEN_FILE=.*/mtb/operator-token$' <<<"$OUT"; echo $?)"
check "print: no key in the env file" "$(! grep -q 'ETORO_PUBLIC_KEY\|ETORO_PRIVATE_KEY' <<<"$OUT"; echo $?)"
check "print: nothing written" "$([ ! -e "$UNITS/mtb-ui-api-real-canary.service" ] && [ ! -e "$CONF/ui-api-real-canary.env" ]; echo $?)"

# --no-trading is refused.
inst --real-canary --no-trading
check "refuses --no-trading" "$([ "$RC" = 2 ] && grep -q 'no sense' <<<"$OUT"; echo $?)"

# Demo unit absent.
mv "$UNITS/mtb-ui-api.service" "$T/demo-unit.away"
inst --real-canary
check "refuses without the demo unit" "$([ "$RC" = 1 ] && grep -q 'demo unit' <<<"$OUT"; echo $?)"
mv "$T/demo-unit.away" "$UNITS/mtb-ui-api.service"

# Token file: same path (via --token-file), missing, same content.
inst --real-canary --token-file "$HOME/.config/mtb/operator-token"
check "refuses the demo token path" "$([ "$RC" = 1 ] && grep -q 'is the demo one' <<<"$OUT"; echo $?)"
inst --real-canary
check "refuses a missing canary token" "$([ "$RC" = 1 ] && grep -q 'missing operator token file' <<<"$OUT"; echo $?)"
cp -p "$HOME/.config/mtb/operator-token" "$HOME/.config/mtb/operator-token-real-canary"
inst --real-canary
check "refuses the same token content" "$([ "$RC" = 1 ] && grep -q 'same token' <<<"$OUT"; echo $?)"
(umask 077; echo "canary-token-fedcba9876543210fedcba9876543210" >"$HOME/.config/mtb/operator-token-real-canary")

# Real env file: missing, wrong mode, not real.
inst --real-canary
check "refuses a missing real env file" "$([ "$RC" = 1 ] && grep -q 'missing real env file' <<<"$OUT"; echo $?)"
printf 'ETORO_ENV=demo\nETORO_PUBLIC_KEY=placeholder\nETORO_PRIVATE_KEY=placeholder\n' >"$CONF/etoro-real-canary.env"
chmod 644 "$CONF/etoro-real-canary.env"
inst --real-canary
check "refuses mode 644" "$([ "$RC" = 1 ] && grep -q 'chmod 600' <<<"$OUT"; echo $?)"
chmod 600 "$CONF/etoro-real-canary.env"
inst --real-canary
check "refuses ETORO_ENV=demo" "$([ "$RC" = 1 ] && grep -q 'does not say ETORO_ENV=real' <<<"$OUT"; echo $?)"
printf 'ETORO_ENV=real\nETORO_PUBLIC_KEY=placeholder\nETORO_PRIVATE_KEY=placeholder\n' >"$CONF/etoro-real-canary.env"
check "nothing written by the refusals" "$([ ! -e "$UNITS/mtb-ui-api-real-canary.service" ] && [ ! -e "$CONF/ui-api-real-canary.env" ]; echo $?)"

# Everything in place: unit + env written, demo files untouched, ack absence is a note.
: >"$FAKE_LOG"
inst --real-canary
check "installs: exit 0" "$([ "$RC" = 0 ]; echo $?)"
check "installs: notes the missing ack file" "$(grep -q 'REAL-ACCOUNT-ACK is not there yet' <<<"$OUT"; echo $?)"
check "installs: unit written" "$(grep -q '^ExecStart=.*ui-api -etoro-real-canary$' "$UNITS/mtb-ui-api-real-canary.service"; echo $?)"
check "installs: unit reads only its own env file" "$([ "$(grep -c '^EnvironmentFile=' "$UNITS/mtb-ui-api-real-canary.service")" = 1 ] && grep -q "^EnvironmentFile=$CONF/ui-api-real-canary.env$" "$UNITS/mtb-ui-api-real-canary.service"; echo $?)"
check "installs: env file 0600" "$([ "$(stat -c %a "$CONF/ui-api-real-canary.env")" = 600 ]; echo $?)"
check "installs: env file names the real env file" "$(grep -q "^UI_API_ETORO_ENV_FILE=$CONF/etoro-real-canary.env$" "$CONF/ui-api-real-canary.env"; echo $?)"
check "installs: enables and restarts the canary unit only" "$(grep -q 'systemctl --user enable mtb-ui-api-real-canary' "$FAKE_LOG" && grep -q 'systemctl --user restart mtb-ui-api-real-canary' "$FAKE_LOG" && ! grep -q 'restart mtb-ui-api$' "$FAKE_LOG"; echo $?)"
check "installs: builds ui-api" "$(grep -q '^go build -o bin/ui-api ./cmd$' "$FAKE_LOG"; echo $?)"
check "installs: ledger directory created" "$([ -d "$ROOT/services/strategy-executor/etoro-daily-data/real-canary" ]; echo $?)"
check "installs: demo unit untouched" "$(cmp -s "$UNITS/mtb-ui-api.service" "$T/demo-unit.orig"; echo $?)"
check "installs: demo ui-api.env untouched" "$(cmp -s "$CONF/ui-api.env" "$T/demo-env.orig"; echo $?)"

# A second run keeps the (possibly edited) env file.
echo "# edited" >>"$CONF/ui-api-real-canary.env"
inst --real-canary
check "re-run keeps the env file" "$(grep -q '^# edited$' "$CONF/ui-api-real-canary.env" && grep -q 'keeping' <<<"$OUT"; echo $?)"

# --uninstall removes the unit, keeps the env file.
inst --real-canary --uninstall
check "uninstall: unit gone, env kept" "$([ ! -e "$UNITS/mtb-ui-api-real-canary.service" ] && [ -e "$CONF/ui-api-real-canary.env" ]; echo $?)"
check "uninstall: demo unit still there" "$(cmp -s "$UNITS/mtb-ui-api.service" "$T/demo-unit.orig"; echo $?)"

# ---- install-ops-cron.sh --print ------------------------------------------------
cron() { set +e; OUT="$("$ROOT/scripts/install-ops-cron.sh" "$@" 2>&1)"; RC=$?; set -e; }
cron --print --bucket b
check "cron: no canary line without the flag" "$(! grep -q 'etoro-canary-observe' <<<"$OUT" && grep -q '^ETORO_CANARY_S3_URI=$' <<<"$OUT"; echo $?)"
cron --print --bucket b --canary-observe
check "cron: 18:00 New York guard, every day" "$(grep -qF '0 22,23 * * * [ "$(TZ=America/New_York date +\%H)" = 18 ] && '"$ROOT"'/scripts/ops-run.sh etoro-canary-observe' <<<"$OUT"; echo $?)"
check "cron: own S3 prefix" "$(grep -q '^ETORO_CANARY_S3_URI=s3://b/daily-executor/etoro-real-canary$' <<<"$OUT"; echo $?)"
check "cron: demo lines unchanged by the flag" "$(grep -q 'ops-run.sh etoro$' <<<"$OUT" && grep -q 'ops-run.sh etoro-etf$' <<<"$OUT"; echo $?)"
check "cron: no warning when the real env file exists" "$(! grep -q 'warning: --canary-observe' <<<"$OUT"; echo $?)"
MTB_ETORO_REAL_CANARY_ENV="$T/none" cron --print --canary-observe
check "cron: warns when the real env file is missing" "$(grep -q 'warning: --canary-observe' <<<"$OUT"; echo $?)"
cron --print --canary-observe
check "cron: no S3 without --bucket" "$(grep -q '^ETORO_CANARY_S3_URI=$' <<<"$OUT"; echo $?)"

# ---- ops-run.sh etoro-canary-observe -------------------------------------------
R="$T/root"
mkdir -p "$R/scripts" "$R/services/strategy-executor/etoro-daily-data"
cp "$ROOT/scripts/ops-run.sh" "$R/scripts/"
cat >"$T/bin/fake-observe" <<'EOF'
#!/usr/bin/env bash
echo "observe args: $*"
exit "${FAKE_OBSERVE_CODE:-0}"
EOF
chmod +x "$T/bin/fake-observe"
printf 'ETORO_CANARY_S3_URI=s3://b/daily-executor/etoro-real-canary/\nETORO_CANARY_OBSERVE_BIN=%s\n' "$T/bin/fake-observe" >"$T/ops.env"
: >"$FAKE_LOG"
set +e
MTB_OPS_ENV="$T/ops.env" XDG_STATE_HOME="$T/state" FAKE_OBSERVE_CODE=1 "$R/scripts/ops-run.sh" etoro-canary-observe
RC=$?
set -e
cdir="$R/services/strategy-executor/etoro-daily-data/real-canary"
runlog="$(ls "$cdir"/run-*.log)"
check "observe: exit code kept (1)" "$([ "$RC" = 1 ]; echo $?)"
check "observe: -ledger pinned to real-canary/" "$(grep -q '^observe args: -ledger ./etoro-daily-data/real-canary/ledger.jsonl -ledger-name etoro-real-canary$' "$runlog"; echo $?)"
check "observe: exit= line in the run log" "$(grep -qx 'exit=1' "$runlog"; echo $?)"
check "observe: run log uploaded last, trailing slash trimmed" "$([ "$(tail -1 "$FAKE_LOG")" = "aws s3 cp ./etoro-daily-data/real-canary/$(basename "$runlog") s3://b/daily-executor/etoro-real-canary/$(basename "$runlog") --only-show-errors" ]; echo $?)"
check "observe: nothing else uploaded before observations exist" "$([ "$(wc -l <"$FAKE_LOG")" = 1 ]; echo $?)"
: >"$cdir/observations.jsonl"; : >"$FAKE_LOG"
set +e
MTB_OPS_ENV="$T/ops.env" XDG_STATE_HOME="$T/state" "$R/scripts/ops-run.sh" etoro-canary-observe -dry
RC=$?
set -e
check "observe: exit 0 passed through" "$([ "$RC" = 0 ]; echo $?)"
check "observe: observations.jsonl uploaded, then the run log" "$(grep -q 'aws s3 cp ./etoro-daily-data/real-canary/observations.jsonl s3://b/daily-executor/etoro-real-canary/observations.jsonl' "$FAKE_LOG" && tail -1 "$FAKE_LOG" | grep -q 'run-.*\.log --only-show-errors$'; echo $?)"
check "observe: extra flags passed to the binary" "$(grep -q 'observe args: .* -dry$' "$(ls -t "$cdir"/run-*.log | head -1)"; echo $?)"
check "observe: job log in state dir" "$(grep -q 'ops-run etoro-canary-observe' "$T/state/mtb-ops/etoro-canary-observe.log"; echo $?)"

# Clean the directory the installer created in the real tree only if it is empty
# (never touch a ledger).
rmdir "$ROOT/services/strategy-executor/etoro-daily-data/real-canary" 2>/dev/null || true

[ "$fail" = 0 ] && echo "all ok"
exit "$fail"
