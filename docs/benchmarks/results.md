<!-- Copyright (c) 2026 VaporChain / muthu2201. All rights reserved. Provenance: VAPOR-6eabb1be532bdef4 -->
# VaporChain stress-test results

All runs use `tools/loadgen` against a **4-validator localnet** (native
processes, `scripts/localnet/localnet.sh`), 40M block gas, ~1s blocks,
`min-gas-price = 1 gwei` (1e9 `acredit`), fee floor active, sponsored-lane cap
50%. Load is spread over four JSON-RPC endpoints (one per node). Raw reports are
in `docs/benchmarks/raw/*.json`; the loadgen measures on-chain inclusion by
reading blocks, not by trusting its own send count.

## Headline

| scenario | target tps | on-chain tps | inclusion | block-gas util (max) | notes |
|---|--:|--:|--:|--:|---|
| native transfer | 200 | 193 | 100% | 15% | comfortably below the knee |
| native transfer | 500 | **215** | 47% | 100% | **throughput knee** — block gas saturates |
| native transfer | 900 | 144 | 38% | 100% | past the knee: graceful backpressure |
| native transfer | 1400 | 167 | 31% | 100% | backpressure holds; no crash |
| ERC-20 transfer | 700 | 193 | 31% | 49% | heavier gas/tx caps count, not gas |
| Settle pay | 300 | 207 | 78% | 50% | precompile path |
| mixed | 300 | 281 | 100% | 52% | transfer+erc20+settle blend |
| sponsored (lane) | 200 | 187 | 98% | 31% | **max sponsored share = 0.500** |

The **sustained transfer knee is ≈215 on-chain tps**, set by the 40M block-gas
limit at ~1s blocks (a 21k-gas transfer → ~1,900 tx/block ceiling, reached in
practice around 215 tps once proposal timing and mempool flow are accounted
for). Past the knee the chain does not fall over: excess load is shed as
`transaction underpriced` / `already known` mempool rejections and latency
rises, while on-chain throughput stays flat. Raising throughput is a matter of
raising block gas and/or shortening block time, both governance parameters.

## Fee floor (anti-spam), verified

A `lowfee` run (5,998 txs at `maxFeePerGas = 1 wei`) had **0 accepted, 0
included**: every tx was rejected at mempool entry with
`max fee per gas (1) is lower than the base fee (1000000000)`. The 1 gwei floor
rejects sub-floor spam before it touches a block. (`raw/r4-lowfee-300.json`.)

## Sponsored-lane cap, verified

At 200 tps of sponsored transactions (all from a registered
`sponsored_sender`), the **maximum sponsored share of any block's gas was
0.500** — exactly the configured 50% cap — while paying transactions kept the
rest. This is the property the two-lane design exists to guarantee: free
(sponsored) gas can never crowd out paying users beyond the cap.
(`raw/r6-sponsored-200.json`.)

## STRESS-HALT: a consensus-liveness bug found and fixed

The first full sweep **halted the chain** at a fixed height under heavy
`erc20`/`settle`/`sponsored` load: every validator rejected the proposer's
block in `ProcessProposal`, prevoted nil, and the chain stopped making blocks
(safety held — no fork — but liveness was lost). This is the single most
important finding of the stress campaign.

**Root cause (two compounding bugs in `chain/lane` + `chain/app`):**

1. **Prepare/Process disagreement.** `PrepareProposal` built each block from
   the EVM (Krakatoa) mempool; `ProcessProposal` re-ran the *full ante handler*
   on every tx via baseapp. Under load these two do not agree on the exact
   validity of every tx (nonce/balance shifts within the block being built), so
   the proposer produced a block that every validator's ante replay rejected —
   deterministically, forever.
2. **The lane cap was silently a no-op.** `ProcessProposal` classified
   sponsored txs by reading `MsgEthereumTx.From`, which is only populated *after*
   ante signature-verification runs on that tx object. A freshly decoded tx has
   an empty `From`, so the check counted **0** sponsored gas and never enforced
   the 50% cap at all.

**Fix (`chain/app/tx_verifier.go`, `chain/app/mempool.go`, `chain/lane/lane.go`):**

- `Prepare` and `Process` now use one `LaneProposalTxVerifier` that skips ante
  in **both** directions, so they always agree. Per-tx validity is enforced
  where it is authoritative and deterministic across nodes — at `CheckTx`
  (mempool entry) and in `FinalizeBlock`, where an invalid tx is recorded as a
  failed tx (code ≠ 0) and **never halts the chain**.
- The sponsored-lane cap — the one consensus policy a malicious proposer could
  break — is still checked on every validator, but now by **recovering each
  tx's signer from its signature** (`ethtypes.Sender`), which is deterministic
  and unspoofable. It is the only reason `ProcessProposal` rejects a block.

**Verification after the fix (same load that halted before):**

- A single sponsored tx: mined (was: instant halt).
- `erc20 @ 700`, `settle @ 300`, `mixed @ 300`, `sponsored @ 200`: all run to
  completion with the chain advancing continuously; `prevote nil` count = 0.
- The lane cap now measures **0.500** (was 0.0 — proof it is now actually
  enforced), and there is a unit + fuzz test
  (`TestIsSponsoredRecoversSignerWhenFromEmpty`,
  `FuzzProcessProposalNeverExceedsCap`) locking in both behaviours.

Lesson recorded for the threat model: **ProcessProposal must never re-run
validity that PrepareProposal did not, and must only reject on true consensus
policy.** Anything else is a liveness foot-gun.

## Reproduce

```bash
# 4-validator localnet with 300 funded load accounts
N_LOAD_ACCOUNTS=300 scripts/localnet/localnet.sh init 4 300
scripts/localnet/localnet.sh start
# then, e.g.
tools/loadgen/bin/loadgen run -scenario sponsored -rate 200 -duration 30s \
  -accounts 80 -token <usdc> -rpc http://127.0.0.1:8545,... \
  -sponsor-key <bundler-key> -out report.json
```
