#!/usr/bin/env bash
# Fails when eToro credentials would be committed.
#
# Checks every tracked file plus the staged diff for:
#   - the literal values of ETORO_PUBLIC_KEY / ETORO_PRIVATE_KEY, read from
#     the environment, from the git-ignored .env.etoro.local, and (since
#     2026-10-22) from the demo and the real-canary env files under
#     ~/.config/microservices-trading-bot/ when they are readable: the values
#     are compared, never printed;
#   - the shape of an eToro user key (base64 JSON starting {"ci": -> eyJjaSI6);
#   - an assigned ETORO_PUBLIC_KEY / ETORO_PRIVATE_KEY value of key length
#     (a placeholder like <key> or ... passes);
#   - the real-account acknowledgement file (REAL-ACCOUNT-ACK, or any file
#     that starts a line with its sentence followed by a key_prefix line):
#     the file lives in ~/.config only. The sentence itself is in code (the
#     check needs it) and in the docs (quoted); what must never enter the
#     repo is the filled-in file;
#   - a tracked env file of either account (.env.etoro.*, etoro-*.env).
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
# The operator's env files (demo and the real canary): their values are
# added to the fixed-string scan when the files are readable by this user.
extra_values=()
for f in "$HOME/.config/microservices-trading-bot/etoro-demo.env" "$HOME/.config/microservices-trading-bot/etoro-real-canary.env"; do
  [ -r "$f" ] || continue
  while IFS= read -r v; do
    [ -n "$v" ] && extra_values+=("$v")
  done < <(sed -n 's/^\(export \)\{0,1\}ETORO_\(PUBLIC\|PRIVATE\)_KEY=["'"'"']\{0,1\}\([^"'"'"']*\)["'"'"']\{0,1\}$/\3/p' "$f")
done

if git ls-files --error-unmatch .env.etoro.local >/dev/null 2>&1; then
  echo "FAIL: .env.etoro.local is tracked by git" >&2
  exit 1
fi
tracked_env=$(git ls-files | grep -E '(^|/)(\.env\.etoro\.[^/]+|etoro-(demo|real|real-canary)\.env|REAL-ACCOUNT-ACK)$' || true)
if [ -n "$tracked_env" ]; then
  echo "FAIL: an eToro env or acknowledgement file is tracked by git:" >&2
  echo "$tracked_env" | sed 's/^/  /' >&2
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
for v in "${extra_values[@]+"${extra_values[@]}"}"; do
  [ ${#v} -ge 16 ] && scan "a key value from an operator env file" "$v" -F
done
scan "an eToro user key (eyJjaSI6...)" 'eyJjaSI6[A-Za-z0-9_-]{20,}'
# An assigned key of key length: UUID-like or base64-like, 24+ characters
# with at least one digit (placeholders such as <your key>, ... and
# your_etoro_partner_api_key pass).
scan "an assigned ETORO_*_KEY value" 'ETORO_(PUBLIC|PRIVATE)_KEY=["'"'"']?[A-Za-z_-]*[0-9][A-Za-z0-9_-]{23,}'

# The filled-in acknowledgement file: its sentence at the start of a line and
# a key_prefix line in the same tracked file.
ack_files=$(git grep -l -I -e '^I accept orders on the eToro REAL account for the measurement canary only' -- . ':!scripts/check-no-etoro-secrets.sh' 2>/dev/null || true)
for f in $ack_files; do
  if git grep -q -I -e '^key_prefix:' -- "$f" 2>/dev/null; then
    echo "FAIL: $f looks like a filled-in REAL-ACCOUNT-ACK (the sentence and a key_prefix line); the file lives in ~/.config only" >&2
    fail=1
  fi
done
if git diff --cached -U0 | grep -q -e '^+I accept orders on the eToro REAL account for the measurement canary only' && git diff --cached -U0 | grep -q -e '^+key_prefix:'; then
  echo "FAIL: the staged diff adds a filled-in REAL-ACCOUNT-ACK" >&2
  fail=1
fi

if [ "$fail" -ne 0 ]; then
  exit 1
fi
echo "ok: no eToro credentials or acknowledgement file in tracked files or the staged diff"
