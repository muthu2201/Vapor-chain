#!/usr/bin/env bash
# Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
# Proprietary. See LICENSE. Provenance: VAPOR-6eabb1be532bdef4
#
# VaporChain testnet/mainnet genesis coordinator. Unlike the localnet script
# (which owns every key), this assumes validators are separate operators.
#
#   1. Coordinator:  build-genesis.sh prepare  <config.json> <out-dir>
#         -> writes <out-dir>/genesis-base.json (params, assets, council
#            admissions — NO gentxs yet) and prints the admitted operators.
#      Publish genesis-base.json to the validators.
#
#   2. Each validator (on their own node, holding their own keys):
#         vaporchaind init <moniker> --chain-id <id> --home ~/.vaporchain
#         cp genesis-base.json ~/.vaporchain/config/genesis.json
#         vaporchaind genesis gentx <key> 100000000000000000000avpower \
#            --chain-id <id> --moniker <moniker> --home ~/.vaporchain \
#            --gas-prices 1000000000acredit
#      -> sends gentx-<moniker>.json back to the coordinator. The gentx power
#         MUST equal the operator's admitted council power or collect fails.
#
#   3. Coordinator:  build-genesis.sh finalize <config.json> <out-dir> <gentx-dir>
#         -> collect-gentxs, validate, write <out-dir>/genesis.json (final).
#
# The build is deterministic given the same config + gentxs, so every operator
# can reproduce and diff the final genesis before launch.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
BIN="${BIN:-$ROOT/chain/build/vaporchaind}"
E18="000000000000000000"
CREDIT=acredit; POWER=avpower; USDC=uusdc
log() { printf '\033[1;35m[testnet]\033[0m %s\n' "$*"; }
die() { echo "error: $*" >&2; exit 1; }
cfg() { jq -r "$1" "$CONFIG"; }

jqi() { local f="$1"; shift; jq "$@" "$f" >"$f.tmp" && mv "$f.tmp" "$f"; }

cmd_prepare() {
  CONFIG="$1"; OUT="$2"; mkdir -p "$OUT"
  local CID EVMID; CID="$(cfg .chain_id)"; EVMID="$(cfg .evm_chain_id)"
  [[ -x "$BIN" ]] || die "build the binary first (cd chain && make build)"
  # sanity: the config's evm id must match what the binary hard-codes
  local expect; expect="$($BIN provenance >/dev/null 2>&1; echo ok)"
  local H; H="$(mktemp -d)"; trap 'rm -rf "$H"' RETURN
  "$BIN" init coordinator --chain-id "$CID" --home "$H" >/dev/null 2>&1
  local G="$H/config/genesis.json"

  # replace the whole app_state with our production defaults for this binary
  # (init already produced module defaults; we only override what the network
  #  needs so the file stays forward-compatible with new modules).
  local adm gt; gt="$(cfg '.genesis_time')"; [[ -z "$gt" || "$gt" == "null" ]] && gt="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  adm="$(jq '[.validators[] | {operator, power_granted:(.power + "'"$E18"'"), admitted_height:"0", removed:false, memo:(.memo // "genesis validator")}]' "$CONFIG")"
  local guardians relayers sponsored faucet
  guardians="$(cfg '[.guardians[]]' 2>/dev/null || echo '[]')"; guardians="$(jq -c '[.guardians[]]' "$CONFIG")"
  relayers="$(jq -c "[.relayer_recipients[]]" "$CONFIG")"
  sponsored="$(jq -c '[.sponsored_senders_hex[]]' "$CONFIG")"

  jqi "$G" \
    --arg gt "$gt" --arg cid "$CID" --argjson adm "$adm" \
    --arg admin "$(cfg .admin)" --argjson guardians "$guardians" \
    --argjson relayers "$relayers" --argjson sponsored "$sponsored" \
    --arg tusdc "$(cfg .usdc_erc20)" --arg maxgas "$(cfg .block_max_gas)" --arg burn "$(cfg .gas_burn_bps)" \
    --arg gpmax "$(cfg .guardian_pause_max_blocks)" --arg epoch "$(cfg .epoch_length_blocks)" \
    --arg vote "$(cfg .voting_period)" --arg xvote "$(cfg .expedited_voting_period)" \
    --arg dep "$(cfg .max_deposit_period)" --arg regfee "$(cfg .registration_fee_credit)" \
    --arg cprice "$(cfg .credit_price)" --arg qweight "$(cfg .quota_weight)" \
    --arg bondgas "$(cfg .gas_per_bonded_usdc)" --arg unbond "$(cfg .unbonding_blocks)" '
    .genesis_time = $gt
    | .chain_id = $cid
    | .consensus.params.block.max_gas = $maxgas
    | .consensus.params.block.max_bytes = "4194304"
    | .app_state.council.admissions = $adm
    | .app_state.council.params.guardians = $guardians
    | .app_state.council.params.guardian_pause_max_blocks = $gpmax
    | .app_state.apps.params.bond_denom = "'"$USDC"'"
    | .app_state.apps.params.gas_per_bonded_unit = ($bondgas | tostring)
    | .app_state.apps.params.unbonding_blocks = $unbond
    | .app_state.apps.params.epoch_length_blocks = $epoch
    | .app_state.apps.params.registration_fee = {denom:"'"$CREDIT"'", amount:$regfee}
    | .app_state.settle.params.treasury_admin = $admin
    | .app_state.settle.params.relayer_admin = $admin
    | .app_state.settle.params.relayer_recipients = $relayers
    | .app_state.settle.params.relayer_payout_cap = [{denom:"'"$USDC"'", amount:"1000000000"}]
    | .app_state.settle.params.sponsored_senders = $sponsored
    | .app_state.settle.params.validator_payout_interval_blocks = "600"
    | .app_state.settle.params.gas_burn_bps = ($burn | tonumber)
    | .app_state.settle.params.assets = [{
        denom:"'"$USDC"'", enabled:true, symbol:"USDC", decimals:6,
        min_fee:"2000", max_fee:"5000000", micro_threshold:"250000",
        tab_settle_threshold:"1000000", quota_weight:$qweight,
        credit_price:$cprice}]
    | .app_state.erc20.token_pairs = [{erc20_address:$tusdc, denom:"'"$USDC"'", enabled:true, contract_owner:"OWNER_MODULE"}]
    | .app_state.erc20.dynamic_precompiles = [$tusdc]
    | .app_state.gov.params.voting_period = $vote
    | .app_state.gov.params.expedited_voting_period = $xvote
    | .app_state.gov.params.max_deposit_period = $dep
    '

  # pre-fund each validator operator with the exact power they will bond, plus
  # a small credit balance for their gentx fee; and any faucet accounts.
  {
    echo '['
    local first=1
    while read -r op pw; do
      [[ -z "$op" ]] && continue
      [[ $first == 1 ]] || echo ','
      first=0
      printf '{"address":"%s","coins":[{"denom":"%s","amount":"%s%s"},{"denom":"%s","amount":"1%s"}]}' "$op" "$POWER" "$pw" "$E18" "$CREDIT" "$E18"
    done < <(jq -r '.validators[] | "\(.operator) \(.power)"' "$CONFIG")
    while read -r fa amt; do
      [[ -z "$fa" ]] && continue
      echo ','
      printf '{"address":"%s","coins":[{"denom":"%s","amount":"%s%s"},{"denom":"%s","amount":"100000000000"}]}' "$fa" "$CREDIT" "$amt" "$E18" "$USDC"
    done < <(jq -r '.faucet_accounts[]? | "\(.address) \(.credit)"' "$CONFIG")
    echo ']'
  } >"$OUT/genesis-balances.json"
  "$BIN" genesis bulk-add-genesis-account "$OUT/genesis-balances.json" --home "$H" >/dev/null

  cp "$G" "$OUT/genesis-base.json"
  "$BIN" genesis validate-genesis --home "$H" >/dev/null && log "genesis-base.json is valid (no gentxs yet)"
  log "admitted operators (each must submit a gentx for their exact power):"
  jq -r '.validators[] | "  \(.operator)  \(.power) VPOWER  (\(.moniker))"' "$CONFIG"
  log "published: $OUT/genesis-base.json  — distribute to validators"
}

cmd_finalize() {
  CONFIG="$1"; OUT="$2"; GENTX="$3"
  local CID; CID="$(cfg .chain_id)"
  [[ -f "$OUT/genesis-base.json" ]] || die "run 'prepare' first ($OUT/genesis-base.json missing)"
  local H; H="$(mktemp -d)"; trap 'rm -rf "$H"' RETURN
  "$BIN" init coordinator --chain-id "$CID" --home "$H" >/dev/null 2>&1
  cp "$OUT/genesis-base.json" "$H/config/genesis.json"
  mkdir -p "$H/config/gentx"
  local n=0
  for g in "$GENTX"/*.json; do
    [[ -e "$g" ]] || die "no gentx files in $GENTX"
    cp "$g" "$H/config/gentx/"; n=$((n+1))
  done
  log "collecting $n gentx(s)"
  "$BIN" genesis collect-gentxs --home "$H" >/dev/null 2>&1
  "$BIN" genesis validate-genesis --home "$H"
  cp "$H/config/genesis.json" "$OUT/genesis.json"
  local sha; sha=$(sha256sum "$OUT/genesis.json" | cut -d' ' -f1)
  log "final genesis: $OUT/genesis.json"
  log "sha256: $sha  (every operator should verify this matches)"
}

case "${1:-}" in
  prepare)  [[ $# -eq 3 ]] || die "usage: $0 prepare <config.json> <out-dir>"; cmd_prepare "$2" "$3" ;;
  finalize) [[ $# -eq 4 ]] || die "usage: $0 finalize <config.json> <out-dir> <gentx-dir>"; cmd_finalize "$2" "$3" "$4" ;;
  *) echo "usage: $0 {prepare|finalize}" >&2; exit 2 ;;
esac
