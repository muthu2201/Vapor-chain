#!/usr/bin/env bash
# SPDX-License-Identifier: AGPL-3.0-only
# Copyright (c) 2026 VaporChain / muthu2201
# Provenance: VAPOR-6eabb1be532bdef4
#
# End-to-end test against a LIVE node and the REAL Settle precompile:
#   app registration (EVM) -> checkout deploy + self-claim -> owner accepts ->
#   user approves the app -> payOrder -> merchant paid net -> app claimable ->
#   claim -> token launch via factory -> paymaster refill via buyCredits.
# Every step asserts on-chain state; any mismatch exits non-zero.
#
# env: RPC_URL, OWNER_KEY (app owner / deployer), USER_KEY (payer), DEPLOYMENTS
set -euo pipefail
cd "$(dirname "$0")/.."
: "${RPC_URL:?}"; : "${OWNER_KEY:?}"; : "${USER_KEY:?}"
D="${DEPLOYMENTS:-deployments/$(cast chain-id --rpc-url "$RPC_URL").json}"
S=0x0000000000000000000000000000000000000900
USDC=$(jq -r .usdc "$D")
OWNER=$(cast wallet address "$OWNER_KEY"); USER=$(cast wallet address "$USER_KEY")
MERCHANT=$(cast wallet address "$(cast wallet new --json | jq -r '.data[0].private_key')")
pass() { printf '\033[1;32m  ✔ %s\033[0m\n' "$*"; }
fail() { printf '\033[1;31m  ✘ %s\033[0m\n' "$*"; exit 1; }
eq() { [[ "$1" == "$2" ]] && pass "$3 ($1)" || fail "$3: got $1 want $2"; }
send() { cast send "$@" --rpc-url "$RPC_URL" --json; }
num() { echo "$1" | awk '{print $1}'; }

echo "== provenance"
eq "$(cast call $S 'provenance()(bytes32)' --rpc-url "$RPC_URL")" 0x6eabb1be532bdef432109abc178d88669ab33aed940f18cd5169887d215c1fcf "Settle provenance fingerprint"

echo "== register app from EVM (burns registration fee in credits)"
R=$(send $S 'registerApp(address,string,uint32)' "$OWNER" "ipfs://vapor-e2e" 1000 --private-key "$OWNER_KEY")
[[ $(jq -r .status <<<"$R") == 0x1 ]] || fail "registerApp reverted"
TOPIC=$(cast keccak 'AppRegistered(uint64,address)')
APP=$(jq -r --arg t "$TOPIC" '.logs[] | select(.topics[0]==$t) | .topics[1]' <<<"$R" | xargs cast to-dec)
pass "app id $APP"

echo "== deploy checkout (constructor self-claims registration)"
CHECKOUT=$(forge create src/checkout/VaporCheckout.sol:VaporCheckout --rpc-url "$RPC_URL" --private-key "$OWNER_KEY" --broadcast --json --constructor-args "$APP" "$MERCHANT" 2>/dev/null | jq -r .deployedTo)
pass "checkout $CHECKOUT"
eq "$(cast call $S 'appOf(address)(uint64,bool)' "$CHECKOUT" --rpc-url "$RPC_URL" | tail -1)" false "claim alone gives no attribution"
send $S 'acceptContractClaim(uint64,address)' "$APP" "$CHECKOUT" --private-key "$OWNER_KEY" >/dev/null
eq "$(cast call $S 'appOf(address)(uint64,bool)' "$CHECKOUT" --rpc-url "$RPC_URL" | tr '\n' ' ' | xargs)" "$APP true" "owner accepted -> attributed"

ORDER=$(cast format-bytes32-string "order-$(date +%s%N | tail -c 12)")
echo "== user pays an order through the app"
R=$(send "$CHECKOUT" 'payOrder(bytes32,address,uint256,address)' "$ORDER" "$USDC" 10000000 "$OWNER" --private-key "$USER_KEY" 2>&1 || true)
[[ "$R" == *"insufficient app allowance"* || "$R" == *"revert"* || $(jq -r .status <<<"$R" 2>/dev/null) == 0x0 ]] && pass "payOrder without approveApp reverts" || fail "payOrder succeeded without approval: $R"
send $S 'approveApp(uint64,address,uint256)' "$APP" "$USDC" 25000000 --private-key "$USER_KEY" >/dev/null
eq "$(num "$(cast call $S 'appAllowance(address,uint64,address)(uint256)' "$USER" "$APP" "$USDC" --rpc-url "$RPC_URL")")" 25000000 "app-scoped allowance"
R=$(send "$CHECKOUT" 'payOrder(bytes32,address,uint256,address)' "$ORDER" "$USDC" 10000000 "$OWNER" --private-key "$USER_KEY")
[[ $(jq -r .status <<<"$R") == 0x1 ]] || fail "payOrder reverted"
eq "$(num "$(cast call "$USDC" 'balanceOf(address)(uint256)' "$MERCHANT" --rpc-url "$RPC_URL")")" 9900000 "merchant received net (10 - 1%)"
# referrer == owner here; referrer share (10% of the app's 50%) is paid instantly
eq "$(num "$(cast call $S 'claimable(uint64,address)(uint256)' "$APP" "$USDC" --rpc-url "$RPC_URL")")" 45000 "app claimable = 50% of fee minus referrer 10%"
eq "$(num "$(cast call $S 'appAllowance(address,uint64,address)(uint256)' "$USER" "$APP" "$USDC" --rpc-url "$RPC_URL")")" 15000000 "allowance decremented"
R=$(send "$CHECKOUT" 'payOrder(bytes32,address,uint256,address)' "$ORDER" "$USDC" 10000000 "$OWNER" --private-key "$USER_KEY" 2>&1 || true)
[[ "$R" == *"revert"* || $(jq -r .status <<<"$R" 2>/dev/null) == 0x0 ]] && pass "double payment of same order reverts" || fail "order paid twice"

echo "== claim revenue (anyone may trigger; funds go to recipient)"
BEFORE=$(num "$(cast call "$USDC" 'balanceOf(address)(uint256)' "$OWNER" --rpc-url "$RPC_URL")")
send $S 'claim(uint64,address)' "$APP" "$USDC" --private-key "$USER_KEY" >/dev/null
AFTER=$(num "$(cast call "$USDC" 'balanceOf(address)(uint256)' "$OWNER" --rpc-url "$RPC_URL")")
eq "$((AFTER - BEFORE))" 45000 "owner received claimed revenue"

echo "== launch a token through the factory"
F=$(jq -r .tokenFactory "$D")
R=$(send "$F" 'createToken((string,string,uint8,uint256,uint256,address,string),bytes32)' "(Vapor Gold,GOLD,18,1000000000000000000000000,0,$OWNER,ipfs://gold)" "$(cast format-bytes32-string "launch-$(date +%s%N | tail -c 12)")" --private-key "$OWNER_KEY")
TOK=$(jq -r --arg t "$(cast keccak 'TokenCreated(address,address,address,string,string,uint8,uint256,uint256)')" '.logs[] | select(.topics[0]==$t) | .topics[1]' <<<"$R" | sed 's/0x000000000000000000000000/0x/')
eq "$(cast call "$TOK" 'symbol()(string)' --rpc-url "$RPC_URL")" '"GOLD"' "token symbol"
eq "$(num "$(cast call "$TOK" 'totalSupply()(uint256)' --rpc-url "$RPC_URL")")" 1000000000000000000000000 "fixed supply minted to owner"

echo "== token paymaster refill: USDC -> gas credits via SETTLE.buyCredits -> EntryPoint"
TPM=$(jq -r .tokenPaymaster "$D")
send "$USDC" 'transfer(address,uint256)' "$TPM" 3000000 --private-key "$USER_KEY" >/dev/null
DEP0=$(num "$(cast call "$TPM" 'getDeposit()(uint256)' --rpc-url "$RPC_URL")")
send "$TPM" 'refill(uint256)' 3000000 --private-key "$USER_KEY" --gas-limit 500000 >/dev/null
DEP1=$(num "$(cast call "$TPM" 'getDeposit()(uint256)' --rpc-url "$RPC_URL")")
Q=$(num "$(cast call $S 'quoteCredits(address,uint256)(uint256)' "$USDC" 3000000 --rpc-url "$RPC_URL")")
eq "$(python3 -c "print($DEP1 - $DEP0)")" "$Q" "3 USDC bought exactly quoteCredits(3 USDC) and deposited"
echo
echo "E2E: ALL CHECKS PASSED"
