#!/usr/bin/env bash
# Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
# Provenance: VAPOR-6eabb1be532bdef4
#
# Off-chain stack for a running localnet: Postgres, protocol contracts,
# sponsor service (ERC-7677), Alto bundler (ERC-4337) and the indexer.
#
#   scripts/localnet/services.sh up [--redeploy]   build, deploy (if needed), start all
#   scripts/localnet/services.sh down              stop the services (Postgres stays up)
#   scripts/localnet/services.sh restart           down + up, no redeploy
#   scripts/localnet/services.sh status
#
# Why a separate script: the chain and its services have different
# lifecycles. Validators restart for upgrades; the sponsor/bundler restart for
# config changes; Postgres outlives both. Each process gets a pidfile so stop
# never has to pattern-match process names (which can hit unrelated shells).
#
# Secrets are exported from the localnet's TEST keyring into
# .localnet/secrets (mode 700, git-ignored). A real network mounts them from a
# secret manager instead; see docs/operations.md.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
NET_DIR="${NET_DIR:-$ROOT/.localnet}"
BIN="${BIN:-$ROOT/chain/build/vaporchaind}"
RUN="$NET_DIR/run"; LOGS="$NET_DIR/logs"; SECRETS="$NET_DIR/secrets"
EVM_RPC="${EVM_RPC:-http://127.0.0.1:8545}"
REST="${REST:-http://127.0.0.1:1317}"
COMET="${COMET:-http://127.0.0.1:26657}"
PG_PORT="${PG_PORT:-5433}"; PG_SOCK="${PG_SOCK:-/tmp}"
PG_DATA="${PG_DATA:-$NET_DIR/pg}"
PAYMASTER_DEPOSIT="${PAYMASTER_DEPOSIT:-20000ether}"
KR="--keyring-backend test --home $NET_DIR/node0"

log() { printf '\033[1;33m[services]\033[0m %s\n' "$*"; }
die() { echo "error: $*" >&2; exit 1; }
pg_url() { echo "postgres://postgres@localhost:$PG_PORT/$1?host=$PG_SOCK"; }

pg_bin() {
  local d
  d=$(ls -d /usr/lib/postgresql/*/bin 2>/dev/null | sort -V | tail -1)
  [[ -n "$d" ]] || die "PostgreSQL server binaries not found (install postgresql)"
  echo "$d"
}

ensure_postgres() {
  if pg_isready -q -h "$PG_SOCK" -p "$PG_PORT" 2>/dev/null; then
    log "postgres already running on :$PG_PORT"
  else
    local b; b=$(pg_bin)
    if [[ ! -f "$PG_DATA/PG_VERSION" ]]; then
      log "initialising postgres in $PG_DATA"
      mkdir -p "$PG_DATA"
      # trust auth on a unix socket bound to localhost only: dev use
      "$b/initdb" -D "$PG_DATA" -A trust -U postgres >/dev/null
    fi
    # postgres refuses to run as root; the container user may be root
    if [[ $(id -u) -eq 0 ]]; then
      id postgres >/dev/null 2>&1 || die "running as root needs a 'postgres' OS user"
      chown -R postgres "$PG_DATA"
      su postgres -c "$b/pg_ctl -D '$PG_DATA' -l '$LOGS/postgres.log' -o '-p $PG_PORT -k $PG_SOCK -c listen_addresses=localhost' -w start" >/dev/null
    else
      "$b/pg_ctl" -D "$PG_DATA" -l "$LOGS/postgres.log" -o "-p $PG_PORT -k $PG_SOCK -c listen_addresses=localhost" -w start >/dev/null
    fi
    log "postgres started on :$PG_PORT"
  fi
  for db in vapor_sponsor vapor_indexer; do
    psql -h "$PG_SOCK" -p "$PG_PORT" -U postgres -Atc "select 1 from pg_database where datname='$db'" | grep -q 1 \
      || createdb -h "$PG_SOCK" -p "$PG_PORT" -U postgres "$db"
  done
}

export_secrets() {
  mkdir -p "$SECRETS"; chmod 700 "$SECRETS"
  [[ -s "$SECRETS/executors" ]] || "$BIN" keys unsafe-export-eth-key bundler $KR >"$SECRETS/executors" 2>/dev/null
  [[ -s "$SECRETS/utility" ]] || "$BIN" keys unsafe-export-eth-key relayer $KR >"$SECRETS/utility" 2>/dev/null
  [[ -s "$SECRETS/deployer" ]] || "$BIN" keys unsafe-export-eth-key dev $KR >"$SECRETS/deployer" 2>/dev/null
  if [[ ! -s "$SECRETS/sponsor-signer" ]]; then
    # the sponsor signer is NOT a chain account: a fresh key only this
    # service holds, registered in the paymaster at deploy time
    cast wallet new --json | jq -r 'if type == "array" then .[0] else .data[0] end | .private_key' >"$SECRETS/sponsor-signer"
  fi
  chmod 600 "$SECRETS"/*
}

hex_of() { "$BIN" debug addr "$1" | awk '/^Address hex:/{print $3}'; }

deploy() {
  local dep="$ROOT/contracts/deployments/$(cast chain-id --rpc-url "$EVM_RPC").json"
  if [[ -f "$dep" && "${1:-}" != "--redeploy" ]] \
     && [[ "$(cast code "$(jq -r .verifyingPaymaster "$dep")" --rpc-url "$EVM_RPC")" != "0x" ]]; then
    log "protocol contracts already deployed ($dep)"
  else
    local admin; admin=$(hex_of "$(jq -r .admin "$NET_DIR/accounts.json")")
    log "deploying protocol contracts"
    (cd "$ROOT/contracts" && forge build >/dev/null)
    RPC_URL="$EVM_RPC" DEPLOYER_KEY="0x$(sed 's/^0x//' "$SECRETS/deployer")" PM_OWNER="$admin" TREASURY="$admin" \
      SPONSOR_SIGNER="$(cast wallet address --private-key "$(cat "$SECRETS/sponsor-signer")")" \
      USDC_TOKEN="$(jq -r .tusdc "$NET_DIR/accounts.json")" "$ROOT/contracts/script/deploy-protocol.sh" >/dev/null
    local vpm; vpm=$(jq -r .verifyingPaymaster "$dep")
    cast send "$vpm" "deposit()" --value "$PAYMASTER_DEPOSIT" \
      --private-key "0x$(sed 's/^0x//' "$SECRETS/deployer")" --rpc-url "$EVM_RPC" >/dev/null
    log "verifying paymaster $vpm funded with $PAYMASTER_DEPOSIT"
  fi
  PAYMASTER=$(jq -r .verifyingPaymaster "$dep")
}

build() {
  log "building sponsor + indexer"
  (cd "$ROOT/services/sponsor" && go build -trimpath -o bin/sponsor ./cmd/sponsor)
  (cd "$ROOT/services/indexer" && go build -trimpath -o bin/indexer ./cmd/indexer)
  [[ -x "$ROOT/services/bundler/node_modules/.bin/alto" ]] || (cd "$ROOT/services/bundler" && npm install --no-audit --no-fund >/dev/null)
}

running() { [[ -f "$RUN/$1.pid" ]] && kill -0 "$(cat "$RUN/$1.pid")" 2>/dev/null; }

launch() { # name, then env + command
  local name="$1"; shift
  if running "$name"; then log "$name already running (pid $(cat "$RUN/$name.pid"))"; return; fi
  nohup env "$@" >>"$LOGS/$name.log" 2>&1 &
  echo $! >"$RUN/$name.pid"
}

wait_http() { # name url
  for _ in $(seq 1 40); do
    curl -s -o /dev/null -X POST -H 'content-type: application/json' -d '{"jsonrpc":"2.0","id":1,"method":"pm_supportedEntryPoints","params":[]}' "$2" && { log "$1 up at $2"; return; }
    sleep 0.5
  done
  die "$1 did not come up; see $LOGS/$1.log"
}

cmd_up() {
  [[ -f "$NET_DIR/accounts.json" ]] || die "no localnet; run scripts/localnet/localnet.sh init && start"
  curl -s -o /dev/null "$COMET/status" || die "localnet is not running (scripts/localnet/localnet.sh start)"
  mkdir -p "$RUN" "$LOGS"
  ensure_postgres
  export_secrets
  build
  deploy "${1:-}"
  launch sponsor VAPOR_EVM_RPC="$EVM_RPC" VAPOR_REST="$REST" VAPOR_PAYMASTER="$PAYMASTER" \
    VAPOR_DATABASE_URL="$(pg_url vapor_sponsor)" VAPOR_SIGNER_KEY_FILE="$SECRETS/sponsor-signer" \
    "$ROOT/services/sponsor/bin/sponsor"
  launch bundler RPC_URL="$EVM_RPC" EXECUTOR_KEYS_FILE="$SECRETS/executors" UTILITY_KEY_FILE="$SECRETS/utility" \
    "$ROOT/services/bundler/start.sh"
  launch indexer VAPOR_EVM_RPC="$EVM_RPC" VAPOR_COMET_RPC="$COMET" VAPOR_PAYMASTER="$PAYMASTER" \
    VAPOR_DATABASE_URL="$(pg_url vapor_indexer)" VAPOR_ARCHIVE_DIR="$NET_DIR/archive" VAPOR_ARCHIVE_EVERY=500 \
    "$ROOT/services/indexer/bin/indexer"
  wait_http sponsor http://127.0.0.1:8800
  wait_http bundler http://127.0.0.1:4337
  log "indexer API at http://127.0.0.1:8900/v1/stats"
}

cmd_down() {
  for name in indexer bundler sponsor; do
    if running "$name"; then
      local pid; pid=$(cat "$RUN/$name.pid")
      # start.sh execs alto, so the pid is alto itself
      kill "$pid" 2>/dev/null || true
      for _ in $(seq 1 20); do kill -0 "$pid" 2>/dev/null || break; sleep 0.25; done
      log "$name stopped"
    fi
    rm -f "$RUN/$name.pid"
  done
}

cmd_status() {
  for name in sponsor bundler indexer; do
    if running "$name"; then echo "$name: running (pid $(cat "$RUN/$name.pid"))"; else echo "$name: stopped"; fi
  done
  pg_isready -h "$PG_SOCK" -p "$PG_PORT" || true
}

case "${1:-}" in
  up) cmd_up "${2:-}" ;;
  down) cmd_down ;;
  restart) cmd_down; cmd_up ;;
  status) cmd_status ;;
  *) echo "usage: $0 {up [--redeploy]|down|restart|status}" >&2; exit 2 ;;
esac
