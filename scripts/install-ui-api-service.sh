#!/usr/bin/env bash
# Install ui-api (the operator console's API, loopback :8090) as a systemd
# --user service on this host, so it comes back after a reboot or a crash
# instead of living in a shell's `go run`.
#
# What it does:
#   - builds services/ui-api/bin/ui-api from the working tree (go build);
#   - writes ~/.config/microservices-trading-bot/ui-api.env (chmod 600) if it
#     does not exist: the operator token file and the assisted-trades switch;
#     the ledgers, the archive and the eToro cells come from ops.env, which
#     scripts/install-ops-cron.sh keeps up to date (UI_API_LEDGER,
#     UI_API_ETORO_LEDGER = etoro-demo,etoro-demo-etf,etoro-demo-rotation,
#     UI_API_ARCHIVE);
#   - writes ~/.config/systemd/user/mtb-ui-api.service, reloads, enables and
#     (re)starts it, and waits for /healthz.
#
# The service binds 127.0.0.1 only (ui-api refuses an operator token on a
# non-loopback address). It never carries the eToro keys itself: ui-api reads
# them from ~/.config/microservices-trading-bot/etoro-demo.env (ETORO_ENV=demo
# is enforced in code). Linger must be on for the unit to run without a login
# session (`loginctl enable-linger`); the script checks and says so.
#
#   scripts/install-ui-api-service.sh              # build, write, enable --now
#   scripts/install-ui-api-service.sh --print      # show the unit and env, change nothing
#   scripts/install-ui-api-service.sh --no-trading # read-only console (UI_API_ETORO_TRADING=0)
#   scripts/install-ui-api-service.sh --uninstall  # stop, disable, remove the unit (env kept)
#
# Afterwards:
#   systemctl --user status mtb-ui-api
#   journalctl --user -u mtb-ui-api -f
#   systemctl --user restart mtb-ui-api        # after a git pull: re-run this script to rebuild
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd)
CONF_DIR="$HOME/.config/microservices-trading-bot"
OPS_ENV="$CONF_DIR/ops.env"
UNIT_ENV="$CONF_DIR/ui-api.env"
UNIT_DIR="$HOME/.config/systemd/user"
UNIT="$UNIT_DIR/mtb-ui-api.service"
BIN="$ROOT/services/ui-api/bin/ui-api"
TOKEN_FILE="$HOME/.config/mtb/operator-token"
ADDR="127.0.0.1:8090"

print=0 uninstall=0 trading=1
while [ $# -gt 0 ]; do
  case "$1" in
    --print) print=1 ;;
    --uninstall) uninstall=1 ;;
    --no-trading) trading=0 ;;
    --addr) ADDR=$2; shift ;;
    --token-file) TOKEN_FILE=$2; shift ;;
    -h|--help) sed -n '2,32p' "$0"; exit 0 ;;
    *) echo "unknown option $1" >&2; exit 2 ;;
  esac
  shift
done

if [ "$uninstall" = 1 ]; then
  systemctl --user disable --now mtb-ui-api 2>/dev/null || true
  rm -f "$UNIT"
  systemctl --user daemon-reload
  echo "removed $UNIT ($UNIT_ENV and $OPS_ENV kept)"
  exit 0
fi

unit_text() {
  cat <<EOF
# Written by scripts/install-ui-api-service.sh on $(date -u +%Y-%m-%dT%H:%M:%SZ); re-run it to rebuild.
[Unit]
Description=microservices-trading-bot ui-api (operator console API, loopback)
Documentation=file://$ROOT/docs/etoro/ETORO-DAILY-EXECUTOR.md
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
WorkingDirectory=$ROOT/services/ui-api
# Ledgers, archive and the eToro cells (UI_API_LEDGER, UI_API_ETORO_LEDGER,
# UI_API_ARCHIVE) from the ops installer; token file and trading switch here.
EnvironmentFile=$OPS_ENV
EnvironmentFile=-$UNIT_ENV
ExecStart=$BIN
Restart=on-failure
RestartSec=5
TimeoutStopSec=20
NoNewPrivileges=yes
# The eToro keys are read from the env file at start, not held by systemd.
PrivateTmp=yes

[Install]
WantedBy=default.target
EOF
}

env_text() {
  cat <<EOF
# ui-api settings the ops installer does not write (scripts/install-ui-api-service.sh).
# Edit, then: systemctl --user restart mtb-ui-api
UI_API_ADDR=$ADDR
UI_API_OPERATOR_TOKEN_FILE=$TOKEN_FILE
# 1 = assisted trades on the eToro DEMO ledger (Trade page); 0 = read-only console.
UI_API_ETORO_TRADING=$trading
EOF
}

if [ "$print" = 1 ]; then
  echo "== $UNIT"; unit_text; echo; echo "== $UNIT_ENV (written only if missing)"; env_text
  exit 0
fi

[ -f "$OPS_ENV" ] || { echo "missing $OPS_ENV: run scripts/install-ops-cron.sh first (it writes the ledger settings)" >&2; exit 1; }
if [ "$trading" = 1 ] && [ ! -r "$TOKEN_FILE" ]; then
  echo "missing operator token file $TOKEN_FILE (use --token-file, or --no-trading)" >&2; exit 1
fi
grep -q '^UI_API_ETORO_LEDGER=' "$OPS_ENV" || echo "note: $OPS_ENV has no UI_API_ETORO_LEDGER; the eToro views will be off" >&2

echo "building $BIN"
(cd "$ROOT/services/ui-api" && go build -o bin/ui-api ./cmd)

mkdir -p "$CONF_DIR" "$UNIT_DIR"
if [ -f "$UNIT_ENV" ]; then
  echo "keeping $UNIT_ENV"
else
  umask 077; env_text > "$UNIT_ENV"; echo "wrote $UNIT_ENV"
fi
unit_text > "$UNIT"
echo "wrote $UNIT"

systemctl --user daemon-reload
systemctl --user enable mtb-ui-api >/dev/null
# A hand-run ui-api (`go run ./cmd …` in a shell) on the same port would make
# the unit fail with "address already in use": stop it, unless it is the unit.
unit_pid=$(systemctl --user show -p MainPID --value mtb-ui-api 2>/dev/null || echo 0)
for pid in $(ss -ltnpH "sport = :${ADDR##*:}" 2>/dev/null | grep -o 'pid=[0-9]*' | cut -d= -f2 | sort -u); do
  [ "$pid" = "$unit_pid" ] && continue
  echo "stopping the hand-run ui-api on $ADDR (pid $pid)"
  parent=$(ps -o ppid= -p "$pid" | tr -d ' ')
  kill "$pid" 2>/dev/null || true
  [ -n "$parent" ] && [ "$(ps -o comm= -p "$parent" 2>/dev/null)" = "go" ] && kill "$parent" 2>/dev/null || true
  for _ in 1 2 3 4 5; do ss -ltnH "sport = :${ADDR##*:}" | grep -q . || break; sleep 1; done
done
systemctl --user restart mtb-ui-api
for _ in $(seq 1 30); do
  if curl -fsS "http://$ADDR/api/ui/healthz" >/dev/null 2>&1; then
    echo "ui-api healthy on http://$ADDR"
    break
  fi
  sleep 1
done
curl -fsS "http://$ADDR/api/ui/healthz" >/dev/null 2>&1 || { echo "ui-api did not answer on $ADDR; see: journalctl --user -u mtb-ui-api -n 50" >&2; exit 1; }

if [ "$(loginctl show-user "$USER" -p Linger --value 2>/dev/null)" != "yes" ]; then
  echo "note: linger is off, so the unit stops when you log out; turn it on with: loginctl enable-linger $USER" >&2
fi
systemctl --user --no-pager --lines=0 status mtb-ui-api | sed -n '1,5p'
