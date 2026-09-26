<!--
SPDX-License-Identifier: CC-BY-4.0
Copyright (c) 2026 VaporChain / muthu2201
Provenance: VAPOR-6eabb1be532bdef4
-->
# VaporChain red-team & attack-simulation report

This is the record of adversarial testing performed against a **live**
4-validator localnet plus the off-chain services, and of the automated attack
tests that run in CI. Read it alongside [threat-model.md](./threat-model.md) and
[../benchmarks/results.md](../benchmarks/results.md).

Method: every check either (a) runs an attack and passes only if the
chain/service **refused** it, or (b) runs the legitimate path as a positive
control to prove the refusal is specific, not a blanket failure. Adversarial
contracts live in `contracts/test/redteam/Attackers.sol` (never deployed to a
real network).

## Critical finding: STRESS-HALT (fixed)

Under sustained `erc20`/`settle`/`sponsored` load the chain **halted** — every
validator rejected the proposer's block in `ProcessProposal` and prevoted nil.
Safety held (no fork); liveness was lost. Two compounding bugs: (1)
PrepareProposal and a full-ante ProcessProposal disagreed on per-tx validity
under load; (2) the sponsored-lane cap was silently a no-op (it read the empty
`From` field). Both fixed; the lane cap now recovers signers from signatures and
measures exactly 0.500. Full analysis and post-fix verification in
[../benchmarks/results.md](../benchmarks/results.md). This is the highest-value
result of the campaign: an availability bug that only a real stress test, not a
unit test, surfaces.

## Live attack results

| # | attack | expectation | result | evidence |
|---|---|---|---|---|
| S1 | `DELEGATECALL` into the Settle precompile | refused | **PASS** — returns `false` | `ContextAbuser.viaDelegatecall` |
| S2 | `CALLCODE` into Settle | refused | **PASS** — `false` | `ContextAbuser.viaCallcode` |
| S3 | `STATICCALL` into a state-changing Settle method | refused | **PASS** — `false` | `ContextAbuser.viaStaticcall` |
| A1 | unattributed contract pulls a victim's app allowance (`payFrom`) | refused | **PASS** — `false`, victim balance unchanged | `AllowanceThief.steal` |
| C1 | bank `MsgSend` of `acredit` (gas credits) | refused | **PASS** — committed failed tx: "acredit transfers are currently disabled" (code 5) | live tx query |
| L1 | 200 tps of sponsored txs vs the 50% lane cap | capped | **PASS** — max sponsored share = **0.500** | `raw/r6-sponsored-200.json` |
| F1 | 5,998 txs below the 1 gwei fee floor | rejected | **PASS** — 0 accepted, 0 included | `raw/r4-lowfee-300.json` |
| K1 | sustained load that previously halted (erc20@700, settle@300, mixed@300, sponsored@200) | chain stays live | **PASS** — height advances continuously, prevote-nil = 0 | post-fix reruns |

## Automated attack tests (run in CI)

These encode adversarial cases as unit/fuzz tests so a regression fails the
build:

**Chain**
- `lane`: `TestProcessProposalRejectsMaliciousProposer` (proposer ignoring the
  lane rule is voted down), `TestSelectorEnforcesSponsoredCap`,
  `FuzzProcessProposalNeverExceedsCap`, `TestIsSponsoredRecoversSignerWhenFromEmpty`.
- `x/council`: `TestRemovalIsPermanentAndJails`,
  `TestGuardianCanPauseButNotUnpause`, `TestGuardianPauseExpires`,
  `TestAdmissionMintsNonTransferablePower`, `TestHooksEnforcePoA`.
- `x/council/ibcguard`: `TestCreditAndPowerNeverLeave`,
  `TestPauseBlocksBothDirectionsButNotRefunds`.
- `x/settle`: invariant property tests + `FuzzComputeFee`, `FuzzComputeSplit`.

**Contracts** (`VaporVerifyingPaymaster.t.sol`)
- Tampered calldata invalidates a sponsorship; cross-chain replay refused;
  expired sponsorship refused; foreign signer refused; EIP-7702 delegation to an
  unlisted implementation refused; withdrawals only reach the treasury; signer
  rotation is timelocked while revoke is instant; zero-signer rejected;
  `validatePaymasterUserOp`/`postOp` callable only by the EntryPoint.

**Sponsor service** (`internal/policy/policy_test.go`, `internal/server/http_test.go`)
- Quota cannot be spent outside the app; `approveApp` only for the sponsoring
  app (dirty high bits rejected); native value refused; rogue/absent 7702
  delegation refused; per-op gas/fee caps; batch bounded 1–16; undecodable
  calldata fails closed; wrong entrypoint/chain refused.
- Rate-limit key ignores spoofed `X-Forwarded-For`; CORS never sets credentials;
  a flood of new IPs cannot reset a throttled client's budget.

**Web app** (`apps/web/e2e/gasless.spec.ts`)
- Per-request nonce CSP present, no `unsafe-eval`, `frame-ancestors 'none'`,
  `x-powered-by` absent; faucet input validated; first-visit gasless purchase
  runs with **zero CSP violations**.

## Scope not covered here

- **Consensus-level attacks** (equivocation/double-sign slashing behaviour,
  Byzantine ≥1/3 partition liveness) were not run as destructive live
  simulations in this environment; the PoA guarantees rest on CometBFT's ≥2/3
  safety and on `x/council` removal being permanent (unit-tested). These belong
  in a dedicated testnet chaos exercise before mainnet.
- **Independent audit** of the Solidity and the Go modules is still required.
- **Economic/griefing analysis** of quota pricing and diversity weighting is
  modelled and unit-tested but not adversarially fuzzed end-to-end.

## Reproducing the live checks

Deploy the adversarial contracts against a running localnet and call them; each
must return `false` / a failed tx. Example:

```bash
AB=$(deploy contracts/out/Attackers.sol/ContextAbuser.json)
cast call $AB 'viaDelegatecall(bytes)(bool)' \
  $(cast calldata "pay(address,uint256,address,address)" $USDC 1000 $DEAD $ZERO) \
  --rpc-url http://127.0.0.1:8545      # -> false
```
