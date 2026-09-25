<!-- Copyright (c) 2026 VaporChain / muthu2201. All rights reserved. Provenance: VAPOR-6eabb1be532bdef4 -->
# VaporChain threat model

This document is the security contract for the protocol: what it protects, who
it protects against, where the trust boundaries are, and how each control is
enforced and tested. It is written so an auditor can map every claim to code.

> Not legal or professional security advice. Independent audit is required
> before mainnet. This reflects the design and the tests that exist in this
> repository.

## Assets

| asset | what it is | worst case if lost |
|---|---|---|
| `acredit` (gas credit) | 18-dec EVM gas token, minted only by `x/settle.BuyCredits` at a governance-fixed price | inflation / free gas |
| `avpower` (validator power) | non-transferable PoA bond, minted only by `x/council` | validator-set capture |
| user USDC | bank-native `uusdc`, ERC-20 via `x/erc20` single-token representation | theft of user funds |
| app revenue / quota | claimable balances + earned sponsored-gas quota in `x/settle`/`x/apps` | revenue theft, quota drain |
| paymaster deposit | native credits staked in the EntryPoint | gas-sponsorship drain |
| sponsor signer key | signs ERC-7677 sponsorships off-chain | unbounded free gas |

## Adversaries and trust boundaries

1. **External user / dapp** — untrusted. Can submit any EVM tx, any Settle call,
   any UserOperation. Must never move another user's funds, mint credits, spend
   another app's quota, or exceed the sponsored-lane cap.
2. **Registered app** — semi-trusted for its OWN contracts only. Must never
   touch another app's allowances, revenue, or quota.
3. **Bundler / sponsor service** — permissioned (in `sponsored_senders`). Trusted
   to be live, NOT trusted to respect limits: the chain caps the sponsored lane
   regardless of what the bundler submits.
4. **Validator / proposer** — permissioned PoA. Trusted for liveness, NOT for
   honesty: a malicious proposer must not be able to violate the sponsored-lane
   policy or unmask a removed validator. Consensus safety is CometBFT's ≥2/3.
5. **Guardian** — emergency responder. Can PAUSE (bounded in time) but cannot
   UNPAUSE and cannot move funds. Only governance unpauses.
6. **Contract owner / operator** — trusted for their own contracts; timelocks
   protect users from a compromised owner key (paymaster signer rotation).

## Controls, and where they live

### Money integrity (x/settle)
- **Single module account; ledgers are claimables/pools/tabs.** The
  `LedgerTotal` invariant asserts bank balance ≥ sum of ledgers every block;
  the credits invariant asserts supply ≤ genesis + minted. A breach **auto-pauses
  settle** (safe mode) instead of halting the chain.
  (`x/settle/keeper/invariants.go`, `keeper_test.go`.)
- **Per-app-scoped allowances.** `payFrom`/`tabPay` can only spend the payer's
  allowance *for the calling app*. An unrelated or attacker-owned contract
  pulling a victim's allowance is refused. Verified live: `AllowanceThief.steal`
  with no approval → `false`; and even after the attacker attributes its own
  contract to its own app, it cannot touch an allowance granted to another app
  (`policy_test.go`, red-team live checks).
- **Fee math is integer-only and bounded** (bps, floor at `micro_threshold`,
  cap at `max_fee`, dust to treasury). Fuzzed: `FuzzComputeFee`,
  `FuzzComputeSplit`. App share ≤ 50%, referrer ≤ 10% of app share.

### Credits and power are not currency
- `acredit` and `avpower` have `SendEnabled = false`. Verified live: a bank
  `MsgSend` of `acredit` commits as a **failed tx** —
  `"acredit transfers are currently disabled"` (code 5). Credits still move as
  EVM native value (gas, and plain value transfers — required by ERC-4337
  deposits and bundler reimbursement), so holders can trade them peer-to-peer;
  the protocol never redeems them and supply only grows via `buyCredits`.
  Power has no EVM representation and is minted/burned only by the council.
- IBC can never export them: `x/council/ibcguard` hard-blocks `acredit`/`avpower`
  on send in both directions (acks/timeouts still pass so stuck transfers
  refund). `TestCreditAndPowerNeverLeave`.

### PoA without a licensed staking fork (x/council + x/staking)
- Power is a non-transferable bond minted only by council admission
  (`TestAdmissionMintsNonTransferablePower`). Admission gates
  `AfterValidatorCreated`; self-delegation only via `BeforeDelegationCreated`
  (`TestHooksEnforcePoA`).
- Removal = jail + tombstone, permanent; an ante guard blocks `MsgUnjail` by a
  removed validator, **including inside authz `MsgExec`** (bounded recursion).
  `TestRemovalIsPermanentAndJails`, `NewCouncilGuardAnte`.
- Guardian can pause but not unpause; pauses **expire**
  (`guardian_pause_max_blocks`); only governance unpauses.
  `TestGuardianCanPauseButNotUnpause`, `TestGuardianPauseExpires`.

### Settle precompile (0x…0900) — EVM boundary
- **Context confusion refused.** `DELEGATECALL`, `CALLCODE`, and a `STATICCALL`
  into a state-changing method all fail. Verified live via `ContextAbuser`:
  each returns `false`. Value sent with a Settle call is rejected; amounts
  above 2^255 are rejected. The geth fork runs a precompile reached by
  DELEGATECALL read-only with caller = the delegating contract, so a contract
  can never act "as" the precompile.

### Sponsored lane (consensus policy)
- The sponsored lane (txs from `sponsored_senders`) is capped at
  `sponsored_lane_max_bps` (50%) of block gas, enforced in **both**
  PrepareProposal (`Selector`) and ProcessProposal. ProcessProposal recovers
  each tx's signer from its **signature** (unspoofable) and rejects any block
  over the cap. Verified live at 200 tps sponsored: max sponsored share =
  **0.500**. `lane_test.go`, `FuzzProcessProposalNeverExceedsCap`.
- **STRESS-HALT (found + fixed):** an earlier design re-ran the full ante in
  ProcessProposal, which disagreed with PrepareProposal under load and halted
  the chain; and the cap was silently a no-op because it read the (empty)
  `From` field. Both fixed. See `docs/benchmarks/results.md`.

### Account abstraction (ERC-4337 v0.8 + EIP-7702)
- **VaporVerifyingPaymaster**: the signed hash covers every gas-relevant field,
  `chainid`, the paymaster address and the VaporChain provenance, so a
  sponsorship can never be replayed on another chain/paymaster; signer rotation
  is 48h-timelocked while revocation is instant; withdrawals only to the
  immutable treasury; EIP-7702 senders must delegate to an allowlisted
  implementation. Tests: tampered calldata, cross-chain replay, expiry, rogue
  signer, unlisted 7702 impl, treasury-only withdrawal, zero-signer guard
  (`VaporVerifyingPaymaster.t.sol`).
- **Sponsor service** signs only after checking the app is ACTIVE, every call
  targets that app's own contracts (or `approveApp` for the same app) with zero
  native value, gas/fee caps, and per-sender daily caps. Attack cases in
  `policy_test.go`: foreign-contract spend, cross-app approve, value transfer,
  rogue 7702 delegate, non-existent app, oversized batch, undecodable calldata.
  Go↔Solidity sponsor-hash parity is pinned by vectors taken from the deployed
  contract (`userop_test.go`).

### Service hardening
- Sponsor rate-limit key ignores client `X-Forwarded-For` unless the peer is a
  configured trusted proxy (read right-to-left); idle-first eviction stops an
  IP-rotation flood from resetting a throttled client (`http_test.go`).
- Indexer API is read-only, bounds every range/limit, and never leaks database
  errors to clients. All services set CORS without credentials.
- Web app: per-request nonce CSP with `strict-dynamic`, no `unsafe-eval` in
  production, `connect-src` pinned to configured endpoints; embedded-wallet key
  sealed with a non-extractable AES-GCM key in IndexedDB, AAD-bound to the
  record. Faucet is server-side, refuses mainnet, per-address + global limits.

## Residual risks (honest)

- **Not independently audited.** This is pre-audit engineering.
- **PoA centralization.** A colluding ≥1/3 of validators can halt; ≥2/3 can
  censor. This is inherent to PoA and is the operator's governance problem.
- **Sponsor signer key** is a live secret; its compromise means free gas until
  `revokeSigner` (instant) is called. Keep it in an HSM/KMS.
- **Bundler liveness** is a UX dependency, not a safety one: if the bundler is
  down, sponsored UX stops but funds and consensus are unaffected; users can
  always pay their own gas.
- **Upstream dependencies** (cosmos/evm, ibc-go, Alto, EntryPoint) carry their
  own risk; versions are pinned and their licenses tracked in
  `THIRD_PARTY_NOTICES`.
- **EIP-7702 delegation** lets a user delegate their EOA to arbitrary code
  outside sponsored flows; the paymaster/sponsor only guard *sponsored* ops.
