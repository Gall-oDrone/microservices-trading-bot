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
# Real-account canary (2026-10-22, docs/etoro/ETORO-REAL-CANARY-PLAN-2026-10-21.md
# §3.2-3.3, runbook §14): --real-canary installs a SECOND unit,
# mtb-ui-api-real-canary, from the same binary on 127.0.0.1:8091 with its own
# env file ui-api-real-canary.env (its own operator token file, the real env
# file, the canary ledger etoro-real-canary=…/etoro-daily-data/real-canary/
# ledger.jsonl, UI_API_ETORO_TRADING=1) and `-etoro-real-canary` on the
# command line (the flag is never an environment variable). It refuses to
# install when the demo unit is absent, when the two token files are the same
# path, or when the real env file does not say ETORO_ENV=real; the unit has
# Wants=mtb-ui-api.service so the canary never runs without the demo console
# beside it. The six-part acknowledgement is checked by the binary at start:
# a refusal is in `journalctl --user -u mtb-ui-api-real-canary`.
#
#   scripts/install-ui-api-service.sh --real-canary            # build, write, enable --now
#   scripts/install-ui-api-service.sh --real-canary --print    # show, change nothing
#   scripts/install-ui-api-service.sh --real-canary --uninstall
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
# The real canary's defaults (plan §3.2).
REAL_UNIT_NAME="mtb-ui-api-real-canary"
REAL_UNIT="$UNIT_DIR/$REAL_UNIT_NAME.service"
REAL_UNIT_ENV="$CONF_DIR/ui-api-real-canary.env"
REAL_ENV_FILE="$CONF_DIR/etoro-real-canary.env"
REAL_ACK_FILE="$CONF_DIR/REAL-ACCOUNT-ACK"
REAL_TOKEN_FILE="$HOME/.config/mtb/operator-token-real-canary"
REAL_LEDGER="$ROOT/services/strategy-executor/etoro-daily-data/real-canary/ledger.jsonl"
REAL_ADDR="127.0.0.1:8091"

print=0 uninstall=0 trading=1 real=0 opt_addr="" opt_token=""
while [ $# -gt 0 ]; do
  case "$1" in
    --print) print=1 ;;
    --uninstall) uninstall=1 ;;
    --no-trading) trading=0 ;;
    --real-canary) real=1 ;;
    --addr) opt_addr=$2; shift ;;
    --token-file) opt_token=$2; shift ;;
    -h|--help) sed -n '2,48p' "$0"; exit 0 ;;
    *) echo "unknown option $1" >&2; exit 2 ;;
  esac
  shift
done
# --addr / --token-file name the unit being installed, never the other one:
# with --real-canary the demo paths stay what they are, so the "same token
# file" refusal below compares the canary's against the demo's.
if [ "$real" = 1 ]; then
  [ -n "$opt_addr" ] && REAL_ADDR=$opt_addr
  [ -n "$opt_token" ] && REAL_TOKEN_FILE=$opt_token
else
  [ -n "$opt_addr" ] && ADDR=$opt_addr
  [ -n "$opt_token" ] && TOKEN_FILE=$opt_token
fi

# ---------------------------------------------------------------- real canary
if [ "$real" = 1 ]; then
  if [ "$uninstall" = 1 ]; then
    systemctl --user disable --now "$REAL_UNIT_NAME" 2>/dev/null || true
    rm -f "$REAL_UNIT"
    systemctl --user daemon-reload
    echo "removed $REAL_UNIT ($REAL_UNIT_ENV kept; the real env file, the ack and the token are yours to revoke/delete)"
    exit 0
  fi
  if [ "$trading" = 0 ]; then
    echo "--real-canary --no-trading makes no sense: the canary unit exists to trade the canary ledger; for a read-only view of it use the demo console" >&2
    exit 2
  fi

  real_unit_text() {
    cat <<EOF
# Written by scripts/install-ui-api-service.sh --real-canary on $(date -u +%Y-%m-%dT%H:%M:%SZ); re-run it to rebuild.
[Unit]
Description=microservices-trading-bot ui-api REAL ACCOUNT CANARY (measurement only, loopback :8091)
Documentation=file://$ROOT/docs/etoro/ETORO-DAILY-EXECUTOR.md
After=network-online.target mtb-ui-api.service
Wants=network-online.target mtb-ui-api.service

[Service]
Type=simple
WorkingDirectory=$ROOT/services/ui-api
# Everything this process is comes from its own env file: port, token file,
# the real env file, the canary ledger. Nothing is shared with ops.env or
# ui-api.env (the demo unit's), except the binary.
EnvironmentFile=$REAL_UNIT_ENV
# -etoro-real-canary is given on the command line, visible in ps, never an
# environment variable (plan §5.2 condition 1). The binary refuses to start
# unless every condition of the acknowledgement holds.
ExecStart=$BIN -etoro-real-canary
Restart=on-failure
RestartSec=10
TimeoutStopSec=20
NoNewPrivileges=yes
PrivateTmp=yes

[Install]
WantedBy=default.target
EOF
  }

  real_env_text() {
    cat <<EOF
# ui-api REAL ACCOUNT CANARY settings (scripts/install-ui-api-service.sh --real-canary).
# Edit, then: systemctl --user restart $REAL_UNIT_NAME
# This file holds no key: the keys stay in $REAL_ENV_FILE (0600), read by the binary.
UI_API_ADDR=$REAL_ADDR
UI_API_OPERATOR_TOKEN_FILE=$REAL_TOKEN_FILE
UI_API_DEMO_OPERATOR_TOKEN_FILE=$TOKEN_FILE
UI_API_ETORO_ENV_FILE=$REAL_ENV_FILE
UI_API_REAL_ACK_FILE=$REAL_ACK_FILE
UI_API_ETORO_LEDGER=etoro-real-canary=$REAL_LEDGER
UI_API_ETORO_TRADING=1
# The canary process serves no Bitso ledger of its own; the default stage path keeps /healthz happy.
UI_API_LEDGER=$ROOT/services/strategy-executor/daily-executor-data/stage/ledger.jsonl
UI_API_LIVE=0
EOF
  }

  if [ "$print" = 1 ]; then
    echo "== $REAL_UNIT"; real_unit_text; echo; echo "== $REAL_UNIT_ENV (written only if missing)"; real_env_text
    exit 0
  fi

  # Refusals (plan C9): the demo unit must exist, the token files must differ,
  # the real env file must exist, be 0600 and say ETORO_ENV=real. The binary
  # repeats these and the rest of the acknowledgement at start.
  if [ ! -f "$UNIT" ]; then
    echo "refusing: the demo unit $UNIT is not installed; run scripts/install-ui-api-service.sh first (the canary never runs without the demo console beside it)" >&2
    exit 1
  fi
  if [ "$(readlink -f "$REAL_TOKEN_FILE" 2>/dev/null || echo "$REAL_TOKEN_FILE")" = "$(readlink -f "$TOKEN_FILE" 2>/dev/null || echo "$TOKEN_FILE")" ]; then
    echo "refusing: the canary's operator token file ($REAL_TOKEN_FILE) is the demo one; create a different one: umask 077; openssl rand -hex 32 > $REAL_TOKEN_FILE" >&2
    exit 1
  fi
  if [ ! -r "$REAL_TOKEN_FILE" ]; then
    echo "refusing: missing operator token file $REAL_TOKEN_FILE (umask 077; openssl rand -hex 32 > $REAL_TOKEN_FILE)" >&2
    exit 1
  fi
  if [ -r "$TOKEN_FILE" ] && cmp -s "$TOKEN_FILE" "$REAL_TOKEN_FILE"; then
    echo "refusing: $REAL_TOKEN_FILE holds the same token as $TOKEN_FILE" >&2
    exit 1
  fi
  if [ ! -f "$REAL_ENV_FILE" ]; then
    echo "refusing: missing real env file $REAL_ENV_FILE (write it by hand, chmod 600, with ETORO_ENV=real and the canary's keys)" >&2
    exit 1
  fi
  mode=$(stat -c %a "$REAL_ENV_FILE" 2>/dev/null || echo "")
  case "$mode" in
    600|400) ;;
    *) echo "refusing: $REAL_ENV_FILE is mode $mode; chmod 600 it" >&2; exit 1 ;;
  esac
  if ! grep -qE '^(export )?ETORO_ENV=["'"'"']?real["'"'"']?$' "$REAL_ENV_FILE"; then
    echo "refusing: $REAL_ENV_FILE does not say ETORO_ENV=real" >&2
    exit 1
  fi
  if [ ! -f "$REAL_ACK_FILE" ]; then
    echo "note: $REAL_ACK_FILE is not there yet; the unit will refuse to start until it is (the binary names the condition)" >&2
  fi
  case "$REAL_LEDGER" in
    */real-canary/ledger.jsonl) ;;
    *) echo "refusing: the canary ledger must live under a directory named real-canary (got $REAL_LEDGER)" >&2; exit 1 ;;
  esac
  mkdir -p "$(dirname "$REAL_LEDGER")"

  echo "building $BIN"
  (cd "$ROOT/services/ui-api" && go build -o bin/ui-api ./cmd)
  mkdir -p "$CONF_DIR" "$UNIT_DIR"
  if [ -f "$REAL_UNIT_ENV" ]; then
    echo "keeping $REAL_UNIT_ENV"
  else
    umask 077; real_env_text > "$REAL_UNIT_ENV"; echo "wrote $REAL_UNIT_ENV"
  fi
  real_unit_text > "$REAL_UNIT"
  echo "wrote $REAL_UNIT"
  systemctl --user daemon-reload
  systemctl --user enable "$REAL_UNIT_NAME" >/dev/null
  systemctl --user restart "$REAL_UNIT_NAME" || true
  for _ in $(seq 1 20); do
    if curl -fsS "http://$REAL_ADDR/api/ui/healthz" >/dev/null 2>&1; then
      echo "ui-api REAL CANARY healthy on http://$REAL_ADDR"
      break
    fi
    sleep 1
  done
  if ! curl -fsS "http://$REAL_ADDR/api/ui/healthz" >/dev/null 2>&1; then
    echo "the canary unit is not answering on $REAL_ADDR: it refused to start (expected until every acknowledgement condition holds). The reason: journalctl --user -u $REAL_UNIT_NAME -n 30" >&2
    systemctl --user --no-pager --lines=0 status "$REAL_UNIT_NAME" | sed -n '1,5p' || true
    exit 1
  fi
  echo "start line (the six facts): journalctl --user -u $REAL_UNIT_NAME | grep 'REAL CANARY ACK OK' | tail -1"
  systemctl --user --no-pager --lines=0 status "$REAL_UNIT_NAME" | sed -n '1,5p'
  exit 0
fi

# ---------------------------------------------------------------- demo (unchanged)
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
