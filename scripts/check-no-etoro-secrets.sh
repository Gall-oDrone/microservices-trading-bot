#!/usr/bin/env bash
# Fails when eToro credentials would be committed.
#
# Checks every tracked file plus the staged diff for:
#   - the literal values of ETORO_PUBLIC_KEY / ETORO_PRIVATE_KEY, read from
#     the environment or from the git-ignored .env.etoro.local when present;
#   - the shape of an eToro user key (base64 JSON starting {"ci": -> eyJjaSI6).
#
# Usage: scripts/check-no-etoro-secrets.sh        (run before every push; CI runs it too)
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

pub=${ETORO_PUBLIC_KEY:-}
priv=${ETORO_PRIVATE_KEY:-}
if [ -f .env.etoro.local ]; then
  # shellcheck disable=SC1091
  pub=${pub:-$(sed -n 's/^ETORO_PUBLIC_KEY="\{0,1\}\([^"]*\)"\{0,1\}$/\1/p' .env.etoro.local)}
  priv=${priv:-$(sed -n 's/^ETORO_PRIVATE_KEY="\{0,1\}\([^"]*\)"\{0,1\}$/\1/p' .env.etoro.local)}
fi

if git ls-files --error-unmatch .env.etoro.local >/dev/null 2>&1; then
  echo "FAIL: .env.etoro.local is tracked by git" >&2
  exit 1
fi

fail=0
scan() { # label pattern [fixed]
  local label=$1 pat=$2 mode=${3:--E}
  local hits
  hits=$( { git grep -l -I $mode -e "$pat" -- . ':!scripts/check-no-etoro-secrets.sh' 2>/dev/null || true;
            git diff --cached -U0 | grep -q $mode -e "$pat" && echo '(staged diff)' || true; } | sort -u)
  if [ -n "$hits" ]; then
    echo "FAIL: $label found in:" >&2
    echo "$hits" | sed 's/^/  /' >&2
    fail=1
  fi
}

[ -n "$priv" ] && scan "ETORO_PRIVATE_KEY value" "$priv" -F
[ -n "$pub" ] && scan "ETORO_PUBLIC_KEY value" "$pub" -F
scan "an eToro user key (eyJjaSI6...)" 'eyJjaSI6[A-Za-z0-9_-]{20,}'

if [ "$fail" -ne 0 ]; then
  exit 1
fi
echo "ok: no eToro credentials in tracked files or the staged diff"
