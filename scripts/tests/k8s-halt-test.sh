#!/usr/bin/env bash
# Offline test for scripts/k8s-halt.sh against a fake kubectl (no cluster).
# Checks the refusals, the audit log, the ConfigMap it applies, the pod nudge,
# the read-back wait, and that every file it writes (plus the ConfigMap default
# in k8s/base/trading-halt.yaml) parses with the real Go halt parser.
# Run from anywhere: scripts/tests/k8s-halt-test.sh   (CI: operator-ui.yml)
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
W="$(mktemp -d)"
trap 'rm -rf "$W"' EXIT
export FAKE="$W/fake" K8S_HALT_AUDIT_LOG="$W/audit.jsonl" KUBECTL="$W/kubectl"
mkdir -p "$FAKE"
printf '{"halted": false}\n' >"$FAKE/state"

# Fake kubectl: keeps the ConfigMap data in $FAKE/state. Pods see the new state
# only after FAKE_LAG exec calls (simulates kubelet propagation).
cat >"$KUBECTL" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
echo "$*" >>"$FAKE/calls"
args=()
while [ $# -gt 0 ]; do
  case "$1" in -n|--context) shift 2 ;; *) args+=("$1"); shift ;; esac
done
set -- "${args[@]}"
case "$1" in
  apply)
    cat >"$FAKE/applied.json"
    [ -f "$FAKE/fail-apply" ] && exit 1
    python3 -c 'import json,sys; d=json.load(open(sys.argv[1])); open(sys.argv[2],"w").write(d["data"]["risk-state.json"])' \
      "$FAKE/applied.json" "$FAKE/state.next"
    echo 0 >"$FAKE/execs" ;;
  annotate) : ;;
  get)
    if [ "$2" = pods ]; then printf 'pod/trading-engine-1\npod/order-management-1\n'
    else
      [ -f "$FAKE/state.next" ] && cat "$FAKE/state.next" || cat "$FAKE/state"
    fi ;;
  exec)
    n=$(( $(cat "$FAKE/execs" 2>/dev/null || echo 0) + 1 )); echo "$n" >"$FAKE/execs"
    if [ -f "$FAKE/state.next" ] && [ "$n" -gt "${FAKE_LAG:-0}" ]; then cat "$FAKE/state.next"; else cat "$FAKE/state"; fi ;;
  *) echo "fake kubectl: unexpected $*" >&2; exit 2 ;;
esac
SH
chmod +x "$KUBECTL"

S="$ROOT/scripts/k8s-halt.sh"
NS=bitso-trading-staging
pass=0
ok() { pass=$((pass+1)); echo "ok   $*"; }
bad() { echo "FAIL $*" >&2; exit 1; }
expect_fail() { # description, args...
  local d="$1"; shift
  if "$S" "$@" >"$W/out" 2>&1; then bad "$d: expected refusal"; fi
  ok "$d"
}
audit_last() { tail -n1 "$K8S_HALT_AUDIT_LOG" | python3 -c "import json,sys; print(json.load(sys.stdin)['$1'])"; }

# 1. Refusals: nothing applied, refused attempts audited.
expect_fail "no namespace" halt --reason "long enough reason" --confirm "$NS"
expect_fail "no action" -n "$NS"
expect_fail "short reason" -n "$NS" halt --reason short --confirm "$NS"
[ "$(audit_last result)" = refused ] || bad "short reason not audited as refused"
expect_fail "missing confirm" -n "$NS" halt --reason "long enough reason"
expect_fail "wrong confirm" -n "$NS" halt --reason "long enough reason" --confirm bitso-trading-prod
[ ! -f "$FAKE/applied.json" ] || bad "a refused call applied something"
ok "refusals applied nothing"

# 2. Dry run prints the manifest, applies nothing, audits nothing.
lines=$(wc -l <"$K8S_HALT_AUDIT_LOG")
"$S" -n "$NS" halt --reason "dry run check" --confirm "$NS" --by tester --dry-run >"$W/dry"
grep -q '"kind": "ConfigMap"' "$W/dry" || bad "dry run printed no ConfigMap"
[ ! -f "$FAKE/applied.json" ] || bad "dry run applied"
[ "$(wc -l <"$K8S_HALT_AUDIT_LOG")" = "$lines" ] || bad "dry run wrote the audit log"
ok "dry run"

# 3. Halt with propagation lag: waits for both pods, then succeeds.
FAKE_LAG=3 "$S" -n "$NS" --context stage-ctx halt --reason "drill: bad fills" --confirm "$NS" --by tester --timeout 20 >"$W/out"
grep -q "verified on every" "$W/out" || bad "halt not verified: $(cat "$W/out")"
grep -q -- "--context stage-ctx" "$FAKE/calls" || bad "--context not passed to kubectl"
grep -q "annotate pods" "$FAKE/calls" || bad "pods not nudged"
python3 - "$FAKE/applied.json" <<'PY' || bad "applied ConfigMap is wrong"
import json, sys
d = json.load(open(sys.argv[1]))
assert d["kind"] == "ConfigMap" and d["metadata"]["name"] == "trading-halt", d
assert d["metadata"]["annotations"]["trading-halt/last-action"] == "halt"
s = json.loads(d["data"]["risk-state.json"])
assert set(s) == {"halted", "reason", "by", "at"}, s
assert s["halted"] is True and s["reason"] == "drill: bad fills" and s["by"] == "tester", s
PY
cp "$FAKE/state.next" "$W/halted.json"
[ "$(audit_last result)" = applied ] && [ "$(audit_last action)" = halt ] || bad "halt not audited"
ok "halt applied, nudged, verified, audited"
mv "$FAKE/state.next" "$FAKE/state"

# 4. Status reports halted with exit 3.
set +e; "$S" -n "$NS" status >"$W/out"; rc=$?; set -e
[ "$rc" = 3 ] && grep -q HALTED "$W/out" || bad "status on a halt: rc=$rc $(cat "$W/out")"
ok "status halted (exit 3)"

# 5. Pods that never update: fails after the timeout, audited as unverified.
set +e; FAKE_LAG=100000 "$S" -n "$NS" resume --reason "never propagates" --confirm "$NS" --by tester --timeout 3 >"$W/out" 2>&1; rc=$?; set -e
[ "$rc" = 1 ] && [ "$(audit_last result)" = unverified ] || bad "stuck pods: rc=$rc result=$(audit_last result)"
ok "stuck pods fail and are audited as unverified"
rm -f "$FAKE/state.next"

# 6. kubectl apply failure: audited as failed.
touch "$FAKE/fail-apply"
set +e; "$S" -n "$NS" resume --reason "apply will fail" --confirm "$NS" --by tester >"$W/out" 2>&1; rc=$?; set -e
rm -f "$FAKE/fail-apply" "$FAKE/state.next"
[ "$rc" = 1 ] && [ "$(audit_last result)" = failed ] || bad "apply failure: rc=$rc"
ok "apply failure audited"

# 7. Resume: verified, status exit 0.
"$S" -n "$NS" resume --reason "fills reconciled" --confirm "$NS" --by tester --timeout 10 >"$W/out"
cp "$FAKE/state.next" "$W/resumed.json"; mv "$FAKE/state.next" "$FAKE/state"
"$S" -n "$NS" status >"$W/out" || bad "status after resume: $(cat "$W/out")"
ok "resume and status (exit 0)"

# 8. Every file parses with the real Go parser (shared/pkg/risk), and so does
#    the ConfigMap default shipped in k8s/base/trading-halt.yaml.
python3 - "$ROOT/k8s/base/trading-halt.yaml" "$W/default.json" <<'PY'
import sys, yaml
d = yaml.safe_load(open(sys.argv[1]))
open(sys.argv[2], "w").write(d["data"]["risk-state.json"])
PY
(cd "$ROOT/shared" && RISK_HALT_FILES_UNDER_TEST="$W/halted.json,$W/resumed.json,$W/default.json" \
  RISK_HALT_EXPECT="true,false,false" go test -count=1 -run TestExternalHaltFiles ./pkg/risk >"$W/go" 2>&1) \
  || bad "Go parser rejected a file: $(cat "$W/go")"
ok "halted, resumed and default files parse with shared/pkg/risk"

# 3 refused (short reason, missing and wrong confirm) + halt + unverified +
# failed + resume. Calls without a namespace or action are not auditable.
[ "$(wc -l <"$K8S_HALT_AUDIT_LOG")" -eq 7 ] || bad "audit log has $(wc -l <"$K8S_HALT_AUDIT_LOG") lines, want 7"
echo "k8s-halt-test: $pass checks passed"
