#!/usr/bin/env bash
# Pins monitoring/alertmanager/alertmanager.yml (plan §6.4.7): the config is
# valid, every severity/team pair the alert rules use routes to the intended
# receiver, and no receiver has an integration (template: nothing is sent).
#
#   AMTOOL=/path/to/amtool scripts/tests/check-alertmanager-routes.sh
set -euo pipefail

root="$(cd "$(dirname "$0")/../.." && pwd)"
cfg="$root/monitoring/alertmanager/alertmanager.yml"
rules=("$root"/monitoring/prometheus/rules/*.yml)
amtool="${AMTOOL:-amtool}"
fail=0

"$amtool" check-config "$cfg" >/dev/null

route() { "$amtool" config routes test --config.file="$cfg" "$@" 2>/dev/null | tr -d '[:space:]'; }

expect() {
  local want="$1"; shift
  local got
  got="$(route "$@")"
  if [[ "$got" == "$want" ]]; then
    echo "ok   $* -> $got"
  else
    echo "FAIL $* -> $got (want $want)"; fail=1
  fi
}

expect risk-page      severity=critical team=risk
expect risk-review    severity=warning  team=risk
expect trading-page   severity=critical team=trading
expect trading-review severity=warning  team=trading
expect platform-page  severity=critical
expect platform       severity=warning
expect platform       alertname=Unlabelled

# Every severity and team value used by a rule must be one the routes know.
for s in $(grep -ho 'severity: *[a-z]*' "${rules[@]}" | awk '{print $2}' | sort -u); do
  [[ "$s" == critical || "$s" == warning ]] || { echo "FAIL unrouted severity '$s' in rules"; fail=1; }
done
for t in $(grep -ho 'team: *[a-z]*' "${rules[@]}" | awk '{print $2}' | sort -u); do
  [[ "$t" == risk || "$t" == trading ]] || { echo "FAIL unrouted team '$t' in rules"; fail=1; }
done

# Template: no integration may be configured in the repo.
if grep -Eq '^[[:space:]]+[a-z_]+_configs:' "$cfg"; then
  echo "FAIL a receiver has an integration; secrets and endpoints stay out of the repo"; fail=1
fi

# Inhibit rules must name alerts that exist.
for a in $(grep -o 'alertname="[A-Za-z]*"' "$cfg" | cut -d'"' -f2 | sort -u); do
  grep -q "alert: $a\$" "${rules[@]}" || { echo "FAIL inhibit rule names unknown alert $a"; fail=1; }
done

exit "$fail"
