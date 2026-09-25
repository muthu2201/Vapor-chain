<!-- Copyright (c) 2026 VaporChain / muthu2201. All rights reserved. Provenance: VAPOR-6eabb1be532bdef4 -->
# VaporChain architecture

VaporChain is a low-cost consumer EVM appchain where users pay in **USDC** with
**one signature and no gas token**, and apps sponsor their users' gas out of an
earned quota. It is a Cosmos SDK chain with a full EVM (cosmos/evm), a native
settlement module, ERC-4337 + EIP-7702 account abstraction, and an IBC path that
brings USDC in from other chains.

This document explains **why** each part is built the way it is, so others can
understand, extend, and operate it. Every non-obvious decision has a rationale;
deviations from the original blueprint are called out explicitly at the end.

---

## 1. Design goals (and what they forced)

| goal | consequence in the code |
|---|---|
| Pay in USDC, see USDC — never a gas token | gas credit `acredit` is invisible to users; apps or the USDC paymaster pay it; `SendEnabled=false` so it is not a tradeable currency |
| One-signature checkout | EIP-7702 upgrades the user's own EOA in the same UserOp that pays; ERC-4337 v0.8 bundler + ERC-7677 sponsor |
| Sub-cent fees that stay 1% even on tiny amounts | integer fee math with a micro-threshold + tabs (aggregated micro-payments) |
| Apps earn from their volume and sponsor gas from it | `x/settle` revenue split + `x/apps` per-epoch quota |
| Cheap, fast, consumer-scale | PoA validators, ~1s blocks, 40M block gas, opt-in Block-STM |
| Free resources must be bounded | sponsored-lane cap (50% of block gas), per-epoch app quota, per-sender caps, fee floor |
| Money can never be silently lost | single module account + ledger invariants that auto-pause on breach |

---

## 2. Layers at a glance

```
        wallet / dapp (Next.js app, @vaporchain/sdk, @vaporchain/react)
                    │  one signature (EIP-7702 + ERC-4337)
        ┌───────────┴────────────┐
   sponsor service (ERC-7677)   Alto bundler (ERC-4337 v0.8)
        │  pm_getPaymasterData        │  eth_sendUserOperation
        └───────────┬────────────┘
              EntryPoint v0.8  ──►  VaporVerifyingPaymaster / VaporTokenPaymaster
                    │  MsgEthereumTx
        ┌───────────┴───────────────────────────────────────────┐
        │  vaporchaind (Cosmos SDK 0.54 + cosmos/evm 0.7)        │
        │   EVM  ──►  Settle precompile 0x…0900  ──►  x/settle   │
        │   x/apps (quota)   x/council (PoA + guardian + IBC guard)
        │   sponsored-lane Prepare/Process proposal handlers     │
        │   IBC: callbacks → ratelimit → PFM → ibcguard → transfer
        └───────────┬───────────────────────────────────────────┘
              indexer (Postgres + Parquet archive)  ──►  explorer / dashboards
```

---

## 3. The chain (`chain/`)

Forked from cosmos/evm's `evmd` example (Apache-2.0; see `NOTICE`). Pinned
versions, and why they matter:

- **Cosmos SDK v0.54.4, CometBFT v0.39.4, cosmos/evm v0.7.3, ibc-go/v11 v11.2.0.**
  These are a mutually compatible set; cosmos/evm 0.7.x targets SDK 0.54.
- **`go-ethereum` is replaced with `cosmos/go-ethereum v1.17.2-cosmos-1`** — the
  Cosmos geth fork. The EVM must share geth's core types with cosmos/evm, and the
  fork carries the precompile-context behaviour we rely on (below). Tools and
  services that are pure clients use upstream geth.
- **Go 1.26.8, not 1.27** — `bytedance/sonic` (a transitive JSON dep) does not
  build on 1.27 yet. Documented so nobody "upgrades" into a broken build.

### 3.1 Two denominations, neither of them a currency

- **`acredit`** — 18-decimal EVM gas credit. It is the EVM's fee denom. It is
  minted **only** by `x/settle.BuyCredits` at a governance-fixed price (so credit
  supply is always backed by USDC paid in), and it has `SendEnabled=false`: you
  cannot bank-transfer it. *Why:* gas should be a pass-through utility, not a
  speculative token; making it non-transferable removes an entire class of
  "gas-token" market and MEV.
- **`avpower`** — validator power. Non-transferable, minted only by `x/council`
  on admission. *Why:* PoA — power is granted by governance, never bought or
  moved.

Both are blocked from leaving over IBC (`x/council/ibcguard`) as defence in depth
on top of `SendEnabled`.

### 3.2 PoA without a licensed staking fork — `x/council`

The SDK's enterprise PoA and Group modules are evaluation-licensed, so PoA is
built on stock `x/staking` with a thin `x/council` that owns the policy:

- Admission mints non-transferable `avpower` to an operator; `AfterValidatorCreated`
  refuses any validator not admitted; `BeforeDelegationCreated` allows
  self-delegation only. *Why:* express PoA entirely through staking hooks so we
  keep the audited staking module and the EVM's staking assumptions.
- Removal jails **and tombstones** permanently. Because `x/slashing`'s `MsgUnjail`
  path calls no staking hook, an **ante guard** (`NewCouncilGuardAnte`) blocks a
  removed validator from unjailing itself — including inside nested authz
  `MsgExec` (bounded recursion, DoS-safe). *Why:* a removed validator must never
  climb back in through a side door.
- **Guardian**: can pause `settle`/`ibc` (optionally per-denom) for emergencies,
  but **cannot unpause**, and every guardian pause **expires**
  (`guardian_pause_max_blocks`). Only governance unpauses. *Why:* fast reaction
  without handing the guardian a permanent kill-switch.
- The provenance fingerprint is written into genesis here, immutably on-chain.

### 3.3 Settlement — `x/settle` + the Settle precompile

`x/settle` is the money module. Its design rules:

- **One module account.** All value sits in a single account; the module tracks
  who owns what through *ledgers* (claimables, pools, tabs). Two invariants run
  every block: **`LedgerTotal`** (bank balance ≥ Σ ledgers) and **credits**
  (supply ≤ genesis + minted). A breach **auto-pauses settle (safe mode)** rather
  than halting the chain. *Why:* a solvency bug should freeze payments for repair,
  not stop the world.
- **Fee formula, integer-only:** `fee = amount·bps/10000`, floored at `min_fee`
  only when `amount ≥ micro_threshold`, capped at `max_fee`, and never more than
  the amount. Split app 50 / validator 20 / relayer 10 / treasury 20, rounding
  dust to treasury; with no app the app share goes to treasury; a referrer takes
  ≤10% of the app share. *Why:* deterministic, no floats (floats + FMA are not
  reproducible across nodes → consensus risk), and the micro-threshold keeps the
  effective rate at ~1% on tiny amounts instead of everything becoming `min_fee`.
- **Tabs** aggregate micro-payments (tips, per-second billing) and settle once
  past a threshold, so the fee stays proportional.
- **Per-app-scoped allowances.** A user approves an *app*, not a contract; only
  that app's contracts can spend it, and `MaxAllowance` (2^255) means "infinite"
  and is never decremented. *Why:* a user's approval to a game can't be drained by
  an unrelated contract.
- **The Settle precompile at `0x…0900`** is the EVM's door into `x/settle`
  (`pay`, `payFrom`, `tabPay`, `approveApp`, `registerApp`, `claim`, `buyCredits`,
  `quoteCredits`, `provenance`, …). It refuses `DELEGATECALL`/`CALLCODE`/value and
  amounts > 2^255, and a `STATICCALL` into a state-changing method reverts. *Why:*
  a precompile that touches module state must never run in a caller's storage
  context or be tricked into a "view" that mutates.

`0x…0900` is chosen to sit outside every range used by go-ethereum (0x01–0x11,
0x0100) and cosmos/evm (0x0400, 0x0800–0x0807), verified in `app/config`.

### 3.4 App registry and quota — `x/apps`

- Apps register (burning a fee) and attribute contracts to themselves with an
  **ownership proof**: CREATE-nonce, CREATE2 salt+initcodehash, an `Ownable`
  static call (100k-gas-capped, cached ctx), or a contract self-claim + owner
  accept. Moving a contract between apps is **timelocked (7 days) with a veto**.
  *Why:* revenue and quota attribution must be provable and not stealable, and a
  hostile move must be catchable.
- **Quota** per epoch = `base + fee_weight × diversity`. Diversity comes from a
  deterministic **integer HyperLogLog** (64 registers, salted with provenance,
  **no floats** for the same FMA-reproducibility reason). *Why:* reward apps that
  bring *many distinct* paying users, not one whale looping, without floats in
  consensus.

### 3.5 Sponsored lane (block-space policy) — `chain/lane`

Sponsored (free-to-user) transactions from registered `sponsored_senders` are
capped at 50% of block gas so they can't crowd out paying users. Enforced in
**PrepareProposal** (the proposer stops adding sponsored txs past the cap) and
**ProcessProposal** (every validator rejects a block over the cap, identifying
sponsored txs by **recovering the signer from the signature** — unspoofable).

> **Quirk / hard-won lesson (STRESS-HALT).** ProcessProposal must *only* enforce
> this consensus policy. An earlier version also re-ran the full ante handler
> there; under load it disagreed with PrepareProposal and **halted the chain**.
> Per-tx validity is now left to CheckTx and FinalizeBlock (an invalid tx becomes
> a failed tx, never a halt). See `docs/benchmarks/results.md` and
> `docs/security/red-team.md`. This is the single most important operational
> lesson in the codebase.

### 3.6 EVM specifics

- Static precompiles enabled: p256, bech32, ics20, bank, ics02, **settle**.
  Staking/distribution/gov/slashing precompiles are **excluded** — those are
  governed by council/PoA and must not be driveable from arbitrary EVM code.
- `evm_version = prague` (solc 0.8.37). Osaka is not enabled.
- Fee floor: min gas price `1 gwei` (`1e9 acredit`), feemarket base fee/min
  1e9. Verified: sub-floor spam is rejected at mempool entry.
- **Gas is a bought-and-burned compute voucher.** `x/settle` runs a begin-blocker
  (ordered *before* `x/distribution`) that burns a governance-set fraction
  (`gas_burn_bps`, default 100%) of the previous block's gas fees from the fee
  collector. *Why:* `acredit` is minted only by `buyCredits` (USDC → treasury)
  and can never be sold back (`SendEnabled=false`); burning what is spent forces
  circulating supply to be replenished by more USDC purchases, so **all** compute
  on the chain — registered app or independent deployment — funnels into treasury
  demand. Validators are paid from the USDC validator pool, not gas, so the burn
  costs them nothing, and it can only *lower* supply so the `supply ≤ genesis +
  minted` invariant is preserved. Full rationale and the drain audit behind it:
  `docs/security/hardening.md`.
- **Block-STM** (parallel EVM execution) is opt-in via `app.toml [vaporchain]
  block-stm` (default off) — correctness-first default, throughput when you want
  it.
- Krakatoa app-side mempool (per-sender caps, nonce-gap limits, replacement bump,
  TTL). CometBFT runs in **app mempool mode** so the app owns ordering.
- A **`vapor` JSON-RPC namespace** overrides `eth_fillTransaction`, which in
  cosmos/evm v0.7.3 estimates gas against **block 0** (genesis) and so reverts
  for any call touching post-genesis state — that broke every viem
  `deployContract`/`sendTransaction`. The override estimates at the latest block;
  it also serves `vapor_provenance`. It is forced on and *after* `eth` so it wins.

### 3.7 IBC and bringing USDC in

Stack order (outermost first): `callbacks → ratelimit → PFM → ibcguard →
transfer`. `ibcguard` hard-blocks `acredit`/`avpower` and applies guardian pauses
(acks/timeouts always pass so stuck transfers refund). USDC.inj arrives via
Skip:Go and is presented to users as a single "USDC" asset (never `ibc/<hash>`),
using `x/erc20`'s single-token representation so the same balance is both a
Cosmos coin and an ERC-20.

---

## 4. Contracts (`contracts/`, Foundry)

solc 0.8.37, `via_ir`, 1,000,000 optimizer runs, OpenZeppelin 5.6, account-
abstraction v0.8.

- **EntryPoint v0.8** and **Simple7702Account** are deployed at their **canonical
  mainnet addresses** by replaying the exact mainnet CREATE2 payloads through the
  preinstalled deterministic deployer (`0x4e59…956c`). *Why:* wallets and
  tooling assume these addresses; keeping them canonical means MetaMask/Rabby and
  every 4337 SDK "just work". (v0.8, not v0.9, because Alto detects the version by
  the EntryPoint address prefix.)
- **VaporVerifyingPaymaster** sponsors gas for ops the sponsor service signs. The
  signed hash covers every gas field, `chainid`, the paymaster address and the
  provenance fingerprint (no cross-chain/cross-paymaster replay); signer rotation
  is 48h-timelocked while revoke is instant; withdrawals only to the immutable
  treasury; EIP-7702 senders must delegate to an allowlisted implementation.
- **VaporTokenPaymaster** lets users pay gas in USDC at the *protocol* price read
  live from the Settle precompile — not an oracle, not an owner-set number, so
  there is no price to manipulate; it is self-funding via `refill()`.
- **VaporTokenFactory** launches ERC-20s (permit + burn + optional cap) at a
  **predictable CREATE2 address** that mixes in the creator (front-run-proof).
- **VaporCheckout** is the one-signature checkout (`approveApp` + `payOrder`);
  `SwordShop` is a worked example.

---

## 5. Off-chain services (`services/`)

- **sponsor** (Go, ERC-7677): `pm_getPaymasterStubData/Data`,
  `pm_supportedEntryPoints`. Signs only after checking the app is ACTIVE, every
  call is into that app's own contracts (or `approveApp` for the same app) with
  zero value, gas/fee caps, and per-sender daily caps. `paymasterData =
  validUntil|validAfter|appId|sig`, hashed to mirror the on-chain `getHash`
  (parity pinned by tests). Rate-limited by real peer IP (spoofed XFF ignored).
- **bundler**: pinned Alto v0.0.21 (GPL; unmodified, launched via `start.sh`).
- **indexer** (Go): ingests blocks into Postgres, serves a read-only API, and
  seals **Parquet** segments with sha256 manifests for cheap long-term history.
  Because CometBFT v0.39 has no data-companion, the indexer exports a
  **retention-margin** metric so you get paged before a pruning node drops
  un-indexed data.

---

## 6. SDK, hooks, and the reference app

- **`@vaporchain/sdk`** (viem): chains, settle calls, apps, AA (`toVaporAccount`,
  `sponsoredSender`), token launch, Skip:Go bridging, and a `VaporClient` facade.
- **`@vaporchain/react`**: `VaporProvider` + hooks (`useSponsorQuota`,
  `useUnifiedBalance`, `usePay`, `useCheckout`, `useLaunchToken`, `useBringIn`)
  and a no-seed-phrase **embedded wallet** whose key is sealed with a
  non-extractable AES-GCM key in IndexedDB.
- **`apps/web`** (Next.js 16): shop, wallet, token launchpad, and an app
  dashboard, behind a per-request nonce CSP. See `deploy/` to run it.

See `docs/sdk/` for the developer guide and the "launch your token" guide.

---

## 7. Deviations from the original blueprint (and why)

| blueprint said | we did | why |
|---|---|---|
| SDK Enterprise POA / Group | `x/council` on stock `x/staking` | POA/Group are evaluation-licensed |
| CometBFT data companion for the indexer | retention-margin metric + history node | v0.39 has no data companion |
| Go 1.27 | Go 1.26.8 | `bytedance/sonic` doesn't build on 1.27 |
| EntryPoint v0.9 | v0.8 | Alto detects the version by address prefix |
| group-voted validator payouts | automatic payouts (power × uptime) | no Group module; simpler and non-gameable |
| IBC v2 router populated | v2 router left empty | v1 transfer path is what USDC uses today |
| — (not anticipated) | `confirmations: 2` in the SDK | upstream receipt/state race in cosmos/evm |
| — | `vapor_fillTransaction` override | upstream `eth_fillTransaction` estimates at block 0 |
| — | symmetric no-ante proposal handlers | full ante in ProcessProposal halted the chain under load |

---

## 8. Provenance & watermarks

The same 32-byte fingerprint is planted in the binary, the on-chain council
genesis record, `SETTLE.provenance()`, every protocol Solidity contract, the TS
SDK, and file headers (`chain/provenance`, `scripts/provenance/scan.sh`). It is
**forensic evidence only** — it has no effect on consensus and is not a
kill-switch. Real protection is the LICENSE plus keeping the repository private.
Honest limitation: code watermarks cannot stop an AI from rewriting the code;
they prove derivation of *this* code, which is a legal lever, not a technical
lock.

---

## 9. Where to go next

- Run it: [`deploy/README.md`](./deploy/README.md).
- Launch a testnet: [`scripts/testnet/`](./scripts/testnet/).
- Security: [`docs/security/threat-model.md`](./docs/security/threat-model.md),
  [`docs/security/red-team.md`](./docs/security/red-team.md).
- Performance: [`docs/benchmarks/results.md`](./docs/benchmarks/results.md).
- Build on it: [`docs/sdk/`](./docs/sdk/).
