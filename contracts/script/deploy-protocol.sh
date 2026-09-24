#!/usr/bin/env bash
# Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
# Provenance: VAPOR-6eabb1be532bdef4
#
# Deploys VaporChain's protocol contracts to a live network:
#   1. EntryPoint v0.8 + Simple7702Account at their CANONICAL addresses
#      (replaying the exact mainnet CREATE2 payloads through the preinstalled
#      deterministic deployer 0x4e59…956c)
#   2. VaporVerifyingPaymaster, VaporTokenPaymaster, VaporTokenFactory
# and writes deployments/<evm-chain-id>.json.
#
# Why bash + forge create instead of `forge script`: forge script simulates in
# a local EVM that has no Settle precompile; `forge create` / `cast send` go
# straight to the node, so constructors that call SETTLE run for real.
#
# env: RPC_URL, DEPLOYER_KEY, PM_OWNER, SPONSOR_SIGNER, TREASURY, USDC_TOKEN
set -euo pipefail
cd "$(dirname "$0")/.."

: "${RPC_URL:?}"; : "${DEPLOYER_KEY:?}"; : "${PM_OWNER:?}"; : "${SPONSOR_SIGNER:?}"; : "${TREASURY:?}"; : "${USDC_TOKEN:?}"
CREATE2=0x4e59b44847b379578588920ca78fbf26c0b4956c
EP=0x4337084D9E255Ff0702461CF8895CE9E3b5Ff108
IMPL7702=0x4Cd241E8d1510e30b2076397afc7508Ae59C66c9
CHAIN_ID=$(cast chain-id --rpc-url "$RPC_URL")
OUT="deployments/$CHAIN_ID.json"

log() { printf '\033[1;35m[deploy]\033[0m %s\n' "$*"; }
has_code() { [[ "$(cast code "$1" --rpc-url "$RPC_URL")" != "0x" ]]; }

[[ "$(cast code $CREATE2 --rpc-url "$RPC_URL")" != "0x" ]] || { echo "CREATE2 deployer missing (x/vm preinstalls)"; exit 1; }

for pair in "entrypoint-v0.8:$EP" "simple7702account-v0.8:$IMPL7702"; do
  name=${pair%%:*}; addr=${pair#*:}
  if has_code "$addr"; then
    log "$name already at $addr"
  else
    cast send "$CREATE2" "$(cat deploy/canonical/$name.calldata.hex)" --private-key "$DEPLOYER_KEY" --rpc-url "$RPC_URL" --gas-limit 8000000 >/dev/null
    has_code "$addr" || { echo "$name did not land at $addr"; exit 1; }
    log "$name deployed at canonical $addr"
  fi
done

create() { # contract, constructor args...
  local c="$1"; shift
  forge create "$c" --rpc-url "$RPC_URL" --private-key "$DEPLOYER_KEY" --broadcast --json --constructor-args "$@" | jq -r '.deployedTo'
}

log "deploying VaporVerifyingPaymaster"
VPM=$(create src/paymaster/VaporVerifyingPaymaster.sol:VaporVerifyingPaymaster "$EP" "$PM_OWNER" "$SPONSOR_SIGNER" "$TREASURY" "[$IMPL7702]")
log "deploying VaporTokenPaymaster"
TPM=$(create src/paymaster/VaporTokenPaymaster.sol:VaporTokenPaymaster "$EP" "$PM_OWNER" "$USDC_TOKEN" "$TREASURY" 1000)
log "deploying VaporTokenFactory"
FACTORY=$(forge create src/tokens/VaporTokenFactory.sol:VaporTokenFactory --rpc-url "$RPC_URL" --private-key "$DEPLOYER_KEY" --broadcast --json | jq -r '.deployedTo')

jq -n --arg ep "$EP" --arg impl "$IMPL7702" --arg vpm "$VPM" --arg tpm "$TPM" --arg f "$FACTORY" \
  --arg settle 0x0000000000000000000000000000000000000900 --arg usdc "$USDC_TOKEN" --argjson cid "$CHAIN_ID" \
  '{chainId:$cid, entryPoint:$ep, simple7702Account:$impl, verifyingPaymaster:$vpm, tokenPaymaster:$tpm,
    tokenFactory:$f, settle:$settle, usdc:$usdc,
    create2Deployer:"0x4e59b44847b379578588920ca78fbf26c0b4956c",
    multicall3:"0xcA11bde05977b3631167028862bE2a173976CA11", permit2:"0x000000000022D473030F116dDEE9F6B43aC78BA3"}' >"$OUT"
log "wrote $OUT"
cat "$OUT"
