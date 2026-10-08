#!/usr/bin/env bash
# Cluster kill switch (docs/frontend/FRONTEND-UI-PLAN-2026-10-03.md §6.4.5).
#
# trading-engine and order-management read /etc/trading-halt/risk-state.json
# (ConfigMap trading-halt, k8s/base/trading-halt.yaml) before every order. This
# script is the only supported way to change it:
#
#   halt    writes {halted: true, reason, by, at}; every new order is rejected
#   resume  writes {halted: false, reason, by, at}
#   status  prints the ConfigMap and what each pod actually sees
#
# A change is applied with kubectl apply, then the pods are annotated so the
# kubelet refreshes the volume within seconds (otherwise up to ~1-2 min), then
# each pod is read back until it sees the new file (--timeout, default 90 s).
# Every attempt, including refused and failed ones, is appended to the audit
# log (K8S_HALT_AUDIT_LOG, default ~/.trading-ops/k8s-halt-audit.jsonl), and the
# ConfigMap carries the last change in its annotations.
#
# Usage:
#   scripts/k8s-halt.sh -n bitso-trading-staging status
#   scripts/k8s-halt.sh -n bitso-trading-staging halt   --reason "bad fills on btc_mxn" --confirm bitso-trading-staging
#   scripts/k8s-halt.sh -n bitso-trading-staging resume --reason "fills reconciled"     --confirm bitso-trading-staging
#
# Options:
#   -n, --namespace NS   required
#   --context CTX        kubectl context (default: current)
#   --reason TEXT        required for halt/resume, at least 8 characters
#   --by WHO             default: git user.email, else $USER@hostname
#   --confirm NS         must repeat the namespace (typed confirmation)
#   --dry-run            print the ConfigMap that would be applied; change nothing
#   --no-verify          do not wait for the pods to see the change
#   --timeout SECONDS    read-back deadline (default 90)
#
# Exit: 0 done (status: not halted), 1 refused/failed, 3 status: halted.
# Needs kubectl and python3. It never touches exchange keys or ledgers.
set -euo pipefail

NS="" CTX="" REASON="" BY="" CONFIRM="" DRY=0 VERIFY=1 TIMEOUT=90 ACTION=""
KUBECTL="${KUBECTL:-kubectl}"
AUDIT="${K8S_HALT_AUDIT_LOG:-$HOME/.trading-ops/k8s-halt-audit.jsonl}"
CM=trading-halt
FILE=risk-state.json
SELECTOR='service in (trading-engine,order-management)'

die() { echo "k8s-halt: $*" >&2; exit 1; }

while [ $# -gt 0 ]; do
  case "$1" in
    -n|--namespace) NS="${2:-}"; shift 2 ;;
    --context) CTX="${2:-}"; shift 2 ;;
    --reason) REASON="${2:-}"; shift 2 ;;
    --by) BY="${2:-}"; shift 2 ;;
    --confirm) CONFIRM="${2:-}"; shift 2 ;;
    --dry-run) DRY=1; shift ;;
    --no-verify) VERIFY=0; shift ;;
    --timeout) TIMEOUT="${2:-}"; shift 2 ;;
    halt|resume|status) [ -z "$ACTION" ] || die "one action only"; ACTION="$1"; shift ;;
    -h|--help) sed -n '2,33p' "$0"; exit 0 ;;
    *) die "unknown argument: $1 (see --help)" ;;
  esac
done

[ -n "$ACTION" ] || die "action required: halt | resume | status"
[ -n "$NS" ] || die "--namespace is required (no default: a kill switch must name its target)"
case "$TIMEOUT" in ''|*[!0-9]*) die "--timeout must be whole seconds" ;; esac
command -v python3 >/dev/null || die "python3 is required"

k() {
  if [ -n "$CTX" ]; then "$KUBECTL" --context "$CTX" -n "$NS" "$@"; else "$KUBECTL" -n "$NS" "$@"; fi
}

# The halt file as each pod sees it ("" if unreadable).
pod_view() { k exec "$1" -- cat "/etc/trading-halt/$FILE" 2>/dev/null || true; }

if [ "$ACTION" = status ]; then
  cur=$(k get configmap "$CM" -o "jsonpath={.data.risk-state\.json}") || die "cannot read configmap $CM in $NS"
  echo "configmap $NS/$CM: $cur"
  halted=$(printf '%s' "$cur" | python3 -c 'import json,sys
try: print("1" if json.load(sys.stdin).get("halted") else "0")
except Exception: print("invalid")')
  for p in $(k get pods -l "$SELECTOR" -o name); do
    echo "  ${p#pod/}: $(pod_view "$p" | tr -d '\n')"
  done
  case "$halted" in
    1) echo "HALTED: new orders are rejected"; exit 3 ;;
    0) echo "not halted"; exit 0 ;;
    *) echo "INVALID halt file: every order is rejected (fail closed); fix with halt or resume"; exit 3 ;;
  esac
fi

# halt | resume
if [ -z "$BY" ]; then
  BY=$(git config user.email 2>/dev/null || true)
  [ -n "$BY" ] || BY="${USER:-unknown}@$(hostname)"
fi
AT=$(date -u +%Y-%m-%dT%H:%M:%SZ)

audit() { # result detail
  [ "$DRY" = 1 ] && return 0
  mkdir -p "$(dirname "$AUDIT")"
  python3 - "$AUDIT" "$ACTION" "$NS" "${CTX:-current}" "$BY" "$AT" "$REASON" "$1" "$2" <<'PY'
import json, sys
path, action, ns, ctx, by, at, reason, result, detail = sys.argv[1:]
with open(path, "a") as f:
    f.write(json.dumps({"at": at, "action": action, "namespace": ns, "context": ctx,
                        "by": by, "reason": reason, "result": result, "detail": detail}) + "\n")
PY
}

refuse() { audit refused "$1"; die "$1"; }

[ "${#REASON}" -ge 8 ] || refuse "--reason is required (at least 8 characters): it is shown to whoever finds the halt"
[ "$CONFIRM" = "$NS" ] || refuse "--confirm must repeat the namespace ($NS) to $ACTION it"

HALTED=false; [ "$ACTION" = halt ] && HALTED=true
STATE=$(python3 -c 'import json,sys; print(json.dumps({"halted": sys.argv[1]=="true", "reason": sys.argv[2], "by": sys.argv[3], "at": sys.argv[4]}))' \
  "$HALTED" "$REASON" "$BY" "$AT")

MANIFEST=$(python3 - "$CM" "$FILE" "$STATE" "$ACTION" "$BY" "$AT" <<'PY'
import json, sys
cm, key, state, action, by, at = sys.argv[1:]
# JSON is valid YAML; kubectl apply accepts it.
print(json.dumps({
    "apiVersion": "v1", "kind": "ConfigMap",
    "metadata": {"name": cm,
                 "labels": {"app": "bitso-trading-platform", "component": "risk-kill-switch"},
                 "annotations": {"trading-halt/last-action": action,
                                 "trading-halt/last-by": by,
                                 "trading-halt/last-at": at}},
    "data": {key: state + "\n"}}, indent=2))
PY
)

if [ "$DRY" = 1 ]; then
  echo "# dry run: would apply to namespace $NS"
  echo "$MANIFEST"
  exit 0
fi

if ! printf '%s\n' "$MANIFEST" | k apply -f - >/dev/null; then
  audit failed "kubectl apply failed"; die "kubectl apply failed; nothing changed"
fi
echo "applied $ACTION to $NS/$CM: $STATE"

# Nudge: an annotation change makes the kubelet sync the pod (and its
# ConfigMap volume) now instead of at its next periodic sync.
k annotate pods -l "$SELECTOR" "trading-halt/at=$AT" --overwrite >/dev/null 2>&1 || \
  echo "warning: could not annotate pods; the kubelet will still pick the change up within ~2 min" >&2

if [ "$VERIFY" = 0 ]; then
  audit applied "not verified (--no-verify)"
  exit 0
fi

pods=$(k get pods -l "$SELECTOR" -o name 2>/dev/null || true)
[ -n "$pods" ] || { audit applied "no trading-engine/order-management pods to verify"; echo "no pods to verify"; exit 0; }

deadline=$(( $(date +%s) + TIMEOUT ))
pending="$pods"
while [ -n "$pending" ]; do
  next=""
  for p in $pending; do
    seen=$(pod_view "$p" | python3 -c 'import json,sys
try: print(json.load(sys.stdin).get("at",""))
except Exception: print("")')
    if [ "$seen" = "$AT" ]; then echo "  ${p#pod/}: sees the change"; else next="$next $p"; fi
  done
  pending="${next# }"
  [ -z "$pending" ] && break
  if [ "$(date +%s)" -ge "$deadline" ]; then
    audit unverified "pods not updated after ${TIMEOUT}s: $pending"
    die "applied, but these pods do not see it after ${TIMEOUT}s: $pending (check: $0 -n $NS status)"
  fi
  sleep 2
done
audit applied "verified on $(echo "$pods" | wc -w | tr -d ' ') pod(s)"
echo "done: $ACTION verified on every trading-engine and order-management pod"
