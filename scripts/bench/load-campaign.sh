#!/usr/bin/env bash
# Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
# Provenance: VAPOR-6eabb1be532bdef4
#
# Load campaign against a running localnet (scripts/localnet/localnet.sh):
# runs tools/loadgen through every scenario, then proves gas-burn conservation
# over the whole campaign (acredit supply drop == sum of all gas fees paid).
#
#   scripts/bench/load-campaign.sh [out-dir] [accounts]
#
# Load accounts must already hold CREDIT + USDC: either init the localnet with
# N_LOAD_ACCOUNTS, or fund `loadgen accounts` addresses at runtime.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
OUT="${1:-$ROOT/docs/benchmarks/raw}"
ACC="${2:-200}"
BIN="${BIN:-$ROOT/chain/build/vaporchaind}"
NET_DIR="${NET_DIR:-$ROOT/.localnet}"
LOADGEN="$ROOT/tools/loadgen/bin/loadgen"
REST=http://127.0.0.1:1317
COMET=http://127.0.0.1:26657
RPCS=http://127.0.0.1:8545,http://127.0.0.1:8555,http://127.0.0.1:8565,http://127.0.0.1:8575
PREFIX="${PREFIX:-r7}"
USDC=$(jq -r .tusdc "$NET_DIR/accounts.json")
SPONSOR_KEY=0x$("$BIN" keys unsafe-export-eth-key bundler --keyring-backend test --home "$NET_DIR/node0" 2>/dev/null)
mkdir -p "$OUT"

height() { curl -s "$COMET/status" | jq -r .result.sync_info.latest_block_height; }
supply_at() { curl -s -H "x-cosmos-block-height: $1" "$REST/cosmos/bank/v1beta1/supply/by_denom?denom=acredit" | jq -r .amount.amount; }
settle() { # let the mempool drain and a couple of blocks pass between runs
  local h; h=$(height)
  until (($(height) >= h + 3)); do sleep 1; done
}

# scenario:rate
RUNS=(${RUNS:-transfer:200 transfer:500 erc20:300 settle:300 mixed:300 sponsored:200 lowfee:300 transfer:1000})

settle
H0=$(height)
S0=$(supply_at "$H0")
echo "campaign start height=$H0 acredit_supply=$S0" >&2
for r in "${RUNS[@]}"; do
  sc=${r%%:*}; rate=${r#*:}
  f="$OUT/$PREFIX-$sc-$rate.json"
  echo "== $sc @ $rate tps" >&2
  args=(run -scenario "$sc" -rate "$rate" -duration "${DURATION:-30s}" -accounts "$ACC" -rpc "$RPCS" -comet "$COMET" -token "$USDC" -out "$f")
  [[ $sc == sponsored ]] && args+=(-sponsor-key "$SPONSOR_KEY")
  "$LOADGEN" "${args[@]}" >/dev/null
  jq -c '{scenario, target_rate_tps, onchain_tps, inclusion_rate, reverted_onchain, max_tx_per_block, block_gas_utilization, max_sponsored_gas_share, latency_ms, block_time_ms}' "$f" >&2
  settle
done
settle
H1=$(height)
S1=$(supply_at "$H1")

# Fees in blocks [H0, H1-1] are burned by the begin-blockers of [H0+1, H1],
# i.e. exactly the window between the two supply snapshots.
python3 - "$H0" "$H1" "$S0" "$S1" "$OUT/$PREFIX-burn-conservation.json" <<'EOF'
import json, sys, urllib.request
h0, h1, s0, s1, out = int(sys.argv[1]), int(sys.argv[2]), int(sys.argv[3]), int(sys.argv[4]), sys.argv[5]
def rpc(method, params):
    req = urllib.request.Request("http://127.0.0.1:8545", json.dumps({"jsonrpc": "2.0", "id": 1, "method": method, "params": params}).encode(), {"content-type": "application/json"})
    return json.load(urllib.request.urlopen(req))["result"]
fees = txs = 0
for h in range(h0, h1):
    for r in rpc("eth_getBlockReceipts", [hex(h)]) or []:
        fees += int(r["gasUsed"], 16) * int(r["effectiveGasPrice"], 16)
        txs += 1
rep = {"from_height": h0, "to_height": h1, "evm_txs": txs, "gas_fees_paid_acredit": str(fees),
       "supply_before": str(s0), "supply_after": str(s1), "supply_burned_acredit": str(s0 - s1),
       "burned_equals_fees": s0 - s1 == fees}
json.dump(rep, open(out, "w"), indent=2)
print(json.dumps(rep))
sys.exit(0 if rep["burned_equals_fees"] else 1)
EOF
