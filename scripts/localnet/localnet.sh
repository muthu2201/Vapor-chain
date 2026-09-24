#!/usr/bin/env bash
# Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
# Provenance: VAPOR-6eabb1be532bdef4
#
# VaporChain multi-validator localnet (native processes, no Docker needed).
#
#   scripts/localnet/localnet.sh init   [N_VALIDATORS=4] [N_LOAD_ACCOUNTS=0]
#   scripts/localnet/localnet.sh start
#   scripts/localnet/localnet.sh stop
#   scripts/localnet/localnet.sh status
#   scripts/localnet/localnet.sh reset
#
# Genesis is produced exactly the way a real network is: every validator runs
# `init`, signs a gentx for its own admitted power, and the coordinator runs
# `collect-gentxs`. Nothing is faked: council admissions, Settle assets, the
# sponsored lane, governance and IBC are all live.
#
# Node i listens on:  p2p 26656+100i  rpc 26657+100i  grpc 9090+10i
#                     api 1317+10i    evm-rpc 8545+10i  evm-ws 8546+10i
#                     prometheus 26660+100i
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
BIN="${BIN:-$ROOT/chain/build/vaporchaind}"
NET_DIR="${NET_DIR:-$ROOT/.localnet}"
CHAIN_ID="${CHAIN_ID:-vapor-local-1}"
EVM_CHAIN_ID="${EVM_CHAIN_ID:-779700}"
N="${2:-${N_VALIDATORS:-4}}"
N_LOAD="${3:-${N_LOAD_ACCOUNTS:-0}}"
KR="--keyring-backend test"

CREDIT=acredit
POWER=avpower
USDC=uusdc
E18=000000000000000000

# deterministic ERC-20 precompile address of the TESTNET-ONLY USDC stand-in
TUSDC_ADDR="$(cast to-check-sum-address "0x$(cast keccak 'vaporchain/testnet/uusdc' | tail -c 41)")"

home_of() { echo "$NET_DIR/node$1"; }
log() { printf '\033[1;36m[localnet]\033[0m %s\n' "$*"; }

need() { command -v "$1" >/dev/null || { echo "missing dependency: $1" >&2; exit 1; }; }

jqi() { # in-place jq on a genesis file
  local f="$1"; shift
  jq "$@" "$f" >"$f.tmp" && mv "$f.tmp" "$f"
}

cmd_init() {
  need jq; need cast
  [[ -x "$BIN" ]] || { echo "build the binary first: (cd chain && make build)" >&2; exit 1; }
  rm -rf "$NET_DIR"; mkdir -p "$NET_DIR"
  local coord; coord="$(home_of 0)"

  log "initialising $N validators (chain-id $CHAIN_ID, evm-chain-id $EVM_CHAIN_ID)"
  for ((i = 0; i < N; i++)); do
    local h; h="$(home_of "$i")"
    "$BIN" init "val$i" --chain-id "$CHAIN_ID" --home "$h" >/dev/null 2>&1
    "$BIN" keys add "val$i" $KR --home "$h" --output json >"$h/val$i.key.json" 2>&1
  done

  # operational accounts live in node0's keyring
  for k in admin guardian faucet bundler relayer attestor dev; do
    "$BIN" keys add "$k" $KR --home "$coord" --output json >"$NET_DIR/$k.key.json" 2>&1
  done
  addr() { "$BIN" keys show "$1" -a $KR --home "${2:-$coord}"; }
  hexof() { "$BIN" debug addr "$1" 2>/dev/null | awk '/^Address hex:/{print $3}'; }

  local ADMIN GUARDIAN FAUCET BUNDLER RELAYER ATTESTOR DEV BUNDLER_HEX
  ADMIN=$(addr admin); GUARDIAN=$(addr guardian); FAUCET=$(addr faucet)
  BUNDLER=$(addr bundler); RELAYER=$(addr relayer); ATTESTOR=$(addr attestor); DEV=$(addr dev)
  BUNDLER_HEX=$(hexof "$BUNDLER")
  echo "{\"admin\":\"$ADMIN\",\"guardian\":\"$GUARDIAN\",\"faucet\":\"$FAUCET\",\"bundler\":\"$BUNDLER\",\"bundler_hex\":\"$BUNDLER_HEX\",\"relayer\":\"$RELAYER\",\"attestor\":\"$ATTESTOR\",\"dev\":\"$DEV\",\"tusdc\":\"$TUSDC_ADDR\"}" | jq . >"$NET_DIR/accounts.json"

  local G="$coord/config/genesis.json"

  # ---- balances (bulk)
  local bal="$NET_DIR/balances.json"
  {
    echo '['
    for ((i = 0; i < N; i++)); do
      echo "{\"address\":\"$(addr "val$i" "$(home_of "$i")")\",\"coins\":[{\"denom\":\"$CREDIT\",\"amount\":\"100000$E18\"},{\"denom\":\"$POWER\",\"amount\":\"100$E18\"}]},"
    done
    echo "{\"address\":\"$ADMIN\",\"coins\":[{\"denom\":\"$CREDIT\",\"amount\":\"1000000$E18\"},{\"denom\":\"$USDC\",\"amount\":\"1000000000000\"}]},"
    echo "{\"address\":\"$GUARDIAN\",\"coins\":[{\"denom\":\"$CREDIT\",\"amount\":\"10000$E18\"}]},"
    echo "{\"address\":\"$FAUCET\",\"coins\":[{\"denom\":\"$CREDIT\",\"amount\":\"100000000$E18\"},{\"denom\":\"$USDC\",\"amount\":\"100000000000000\"}]},"
    echo "{\"address\":\"$BUNDLER\",\"coins\":[{\"denom\":\"$CREDIT\",\"amount\":\"1000000$E18\"}]},"
    echo "{\"address\":\"$RELAYER\",\"coins\":[{\"denom\":\"$CREDIT\",\"amount\":\"10000$E18\"}]},"
    echo "{\"address\":\"$ATTESTOR\",\"coins\":[{\"denom\":\"$CREDIT\",\"amount\":\"10000$E18\"}]},"
    if ((N_LOAD > 0)); then
      # load-test accounts derived from a seed by tools/loadgen (hex -> bech32)
      "$ROOT/tools/loadgen/bin/loadgen" accounts --count "$N_LOAD" --seed "${LOAD_SEED:-vapor-load}" --hrp vapor |
        while read -r a; do
          echo "{\"address\":\"$a\",\"coins\":[{\"denom\":\"$CREDIT\",\"amount\":\"100000$E18\"},{\"denom\":\"$USDC\",\"amount\":\"100000000000\"}]},"
        done
    fi
    echo "{\"address\":\"$DEV\",\"coins\":[{\"denom\":\"$CREDIT\",\"amount\":\"1000000$E18\"},{\"denom\":\"$USDC\",\"amount\":\"1000000000000\"}]}"
    echo ']'
  } >"$bal"
  "$BIN" genesis bulk-add-genesis-account "$bal" --home "$coord" >/dev/null

  # ---- council admissions (validator power is granted, never bought)
  local adm="[]"
  for ((i = 0; i < N; i++)); do
    adm=$(jq --arg op "$(addr "val$i" "$(home_of "$i")")" --arg p "100$E18" \
      '. + [{operator:$op, power_granted:$p, admitted_height:"0", removed:false, memo:"genesis validator"}]' <<<"$adm")
  done

  # ---- module params for a fast, fully-featured localnet
  jqi "$G" --arg cid "$CHAIN_ID" --argjson adm "$adm" \
    --arg guardian "$GUARDIAN" --arg admin "$ADMIN" --arg relayer "$RELAYER" \
    --arg bundler "$BUNDLER" --arg bhex "$BUNDLER_HEX" --arg attestor "$ATTESTOR" \
    --arg tusdc "$TUSDC_ADDR" --arg maxgas "${BLOCK_MAX_GAS:-40000000}" '
    .chain_id = $cid
    | .consensus.params.block.max_gas = $maxgas
    | .consensus.params.block.max_bytes = "4194304"
    | .app_state.council.admissions = $adm
    | .app_state.council.params.guardians = [$guardian]
    | .app_state.council.params.guardian_pause_max_blocks = "300"
    | .app_state.apps.params.attestors = [$attestor]
    | .app_state.apps.params.epoch_length_blocks = "60"
    | .app_state.settle.params.treasury_admin = $admin
    | .app_state.settle.params.relayer_admin = $relayer
    | .app_state.settle.params.relayer_recipients = [$bundler]
    | .app_state.settle.params.relayer_payout_cap = [{denom:"uusdc", amount:"1000000000"}]
    | .app_state.settle.params.sponsored_senders = [$bhex]
    | .app_state.settle.params.validator_payout_interval_blocks = "50"
    | .app_state.settle.params.assets = [{
        denom:"uusdc", enabled:true, symbol:"USDC", decimals:6,
        min_fee:"2000", max_fee:"5000000", micro_threshold:"250000",
        tab_settle_threshold:"1000000", quota_weight:"1000",
        credit_price:"1000000000000000"}]
    | .app_state.erc20.token_pairs = [{erc20_address:$tusdc, denom:"uusdc", enabled:true, contract_owner:"OWNER_MODULE"}]
    | .app_state.erc20.dynamic_precompiles = [$tusdc]
    | .app_state.bank.denom_metadata += [{
        description:"TESTNET-ONLY USDC stand-in (no value). Mainnet uses USDC.inj over IBC.",
        denom_units:[{denom:"uusdc",exponent:0,aliases:[]},{denom:"usdc",exponent:6,aliases:[]}],
        base:"uusdc", display:"usdc", name:"Testnet USDC", symbol:"USDC", uri:"", uri_hash:""}]
    | .app_state.gov.params.voting_period = "45s"
    | .app_state.gov.params.expedited_voting_period = "20s"
    | .app_state.gov.params.max_deposit_period = "45s"
    | .app_state.slashing.params.signed_blocks_window = "200"
    '

  # distribute genesis to every node, then each validator signs its gentx
  for ((i = 1; i < N; i++)); do cp "$G" "$(home_of "$i")/config/genesis.json"; done
  mkdir -p "$coord/config/gentx"
  for ((i = 0; i < N; i++)); do
    local h; h="$(home_of "$i")"
    "$BIN" genesis gentx "val$i" "100$E18$POWER" $KR --home "$h" --chain-id "$CHAIN_ID" \
      --moniker "val$i" --commission-rate 0.05 --commission-max-rate 0.2 --commission-max-change-rate 0.01 \
      --gas-prices "1000000000$CREDIT" --output-document "$coord/config/gentx/gentx-val$i.json" >/dev/null 2>&1
  done
  "$BIN" genesis collect-gentxs --home "$coord" >/dev/null 2>&1
  "$BIN" genesis validate-genesis --home "$coord"

  # ---- per-node config
  local peers=""
  for ((i = 0; i < N; i++)); do
    local id; id=$("$BIN" comet show-node-id --home "$(home_of "$i")")
    peers+="${id}@127.0.0.1:$((26656 + 100 * i)),"
  done
  peers="${peers%,}"
  for ((i = 0; i < N; i++)); do
    local h; h="$(home_of "$i")"
    [[ $i -gt 0 ]] && cp "$G" "$h/config/genesis.json"
    local c="$h/config/config.toml" a="$h/config/app.toml"
    sed -i -E \
      -e "s#^laddr = \"tcp://127.0.0.1:26657\"#laddr = \"tcp://127.0.0.1:$((26657 + 100 * i))\"#" \
      -e "s#^laddr = \"tcp://0.0.0.0:26656\"#laddr = \"tcp://127.0.0.1:$((26656 + 100 * i))\"#" \
      -e "s#^persistent_peers = \".*\"#persistent_peers = \"$peers\"#" \
      -e "s#^allow_duplicate_ip = false#allow_duplicate_ip = true#" \
      -e "s#^addr_book_strict = true#addr_book_strict = false#" \
      -e "s#^timeout_commit = .*#timeout_commit = \"800ms\"#" \
      -e "s#^prometheus = false#prometheus = true#" \
      -e "s#^prometheus_listen_addr = \":26660\"#prometheus_listen_addr = \":$((26660 + 100 * i))\"#" \
      -e "s#^pprof_laddr = \".*\"#pprof_laddr = \"\"#" \
      "$c"
    sed -i -E \
      -e "s#^address = \"tcp://localhost:1317\"#address = \"tcp://127.0.0.1:$((1317 + 10 * i))\"#" \
      -e "s#^address = \"localhost:9090\"#address = \"127.0.0.1:$((9090 + 10 * i))\"#" \
      -e "s#^address = \"127.0.0.1:8545\"#address = \"127.0.0.1:$((8545 + 10 * i))\"#" \
      -e "s#^ws-address = \"127.0.0.1:8546\"#ws-address = \"127.0.0.1:$((8546 + 10 * i))\"#" \
      -e "s#^evm-chain-id = .*#evm-chain-id = $EVM_CHAIN_ID#" \
      -e "s#^min-retain-blocks = .*#min-retain-blocks = 0#" \
      -e "s#^enable-indexer = false#enable-indexer = true#" \
      -e "s#^pending-tx-proposal-timeout = .*#pending-tx-proposal-timeout = \"${PROPOSAL_TIMEOUT:-600ms}\"#" \
      "$a"
    # JSON-RPC enabled on every localnet node (validators disable it in prod)
    awk 'BEGIN{s=0} /^\[json-rpc\]/{s=1} s&&/^enable = false/{sub(/false/,"true");s=0} {print}' "$a" >"$a.tmp" && mv "$a.tmp" "$a"
    awk 'BEGIN{s=0} /^\[api\]/{s=1} s&&/^enable = false/{sub(/false/,"true");s=0} {print}' "$a" >"$a.tmp" && mv "$a.tmp" "$a"
  done
  cp "$NET_DIR/accounts.json" "$NET_DIR/../.localnet-accounts.json" 2>/dev/null || true
  log "genesis ready: $G"
  log "testnet USDC ERC-20: $TUSDC_ADDR"
}

cmd_start() {
  local count; count=$(ls -d "$NET_DIR"/node* 2>/dev/null | wc -l)
  for ((i = 0; i < count; i++)); do
    local h; h="$(home_of "$i")"
    nohup "$BIN" start --home "$h" --chain-id "$CHAIN_ID" --json-rpc.api eth,net,web3,txpool,debug \
      >"$h/node.log" 2>&1 &
    echo $! >"$h/node.pid"
  done
  log "started $count nodes; waiting for blocks..."
  for _ in $(seq 1 90); do
    if h=$(curl -s "http://127.0.0.1:26657/status" | jq -r '.result.sync_info.latest_block_height' 2>/dev/null) && [[ "$h" =~ ^[0-9]+$ ]] && ((h >= 3)); then
      log "chain is producing blocks (height $h)"; return 0
    fi
    sleep 1
  done
  echo "chain did not start; see $NET_DIR/node0/node.log" >&2; tail -50 "$NET_DIR/node0/node.log" >&2; return 1
}

cmd_stop() {
  for p in "$NET_DIR"/node*/node.pid; do
    [[ -f "$p" ]] && kill "$(cat "$p")" 2>/dev/null || true
    rm -f "$p"
  done
  sleep 2
  log "stopped"
}

cmd_status() {
  for p in "$NET_DIR"/node*/; do
    local i="${p%/}"; i="${i##*node}"
    local port=$((26657 + 100 * i))
    printf 'node%s  ' "$i"
    curl -s "http://127.0.0.1:$port/status" | jq -c '{h:.result.sync_info.latest_block_height, catching_up:.result.sync_info.catching_up}' 2>/dev/null || echo down
  done
}

case "${1:-}" in
  init) cmd_init ;;
  start) cmd_start ;;
  stop) cmd_stop ;;
  status) cmd_status ;;
  reset) cmd_stop; cmd_init ;;
  *) echo "usage: $0 {init|start|stop|status|reset} [validators] [load-accounts]"; exit 2 ;;
esac
