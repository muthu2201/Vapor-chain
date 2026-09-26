<!--
SPDX-License-Identifier: CC-BY-4.0
Copyright (c) 2026 VaporChain / muthu2201
Provenance: VAPOR-6eabb1be532bdef4
-->
# VaporChain hardening audit — revenue & capital-drain review

This is the write-up of a targeted audit whose brief was blunt: **find every
way value can be drained out of the protocol, find every economic loophole
that lets someone use the chain without paying into it, and close what can be
closed.** It complements `threat-model.md` (which states the security contract)
and `red-team.md` (the live attack runs). Where those describe controls that
already existed, this document records what the drain audit *checked*, what it
*found*, and what it *changed*.

> Not legal or professional security advice. Independent audit is still required
> before mainnet. Every claim below maps to code in this repository and to tests
> that run in CI.

---

## 1. Scope and method

The audit walked every code path that (a) moves an asset out of a module
account, (b) mints or burns a supply-controlled denom, or (c) grants a
subsidy (sponsored gas / quota). For each path it asked three questions:

1. **Conservation** — can total value out ever exceed value in?
2. **Attribution** — can caller A move or earn against caller B's balance?
3. **Subsidy** — can anyone extract more subsidy than they pay for?

Paths reviewed: `x/settle` (`pay.go`, `treasury.go`, `payout.go`, `tabs.go`,
`fee.go`, the split math), the Settle precompile (`precompiles/settle`),
`x/apps` (registration, ownership proofs, quota, contract moves, referrer),
the sponsored lane (`lane/`, `app/tx_verifier.go`), and the paymaster + sponsor
service (`contracts/`, `services/sponsor`).

---

## 2. Findings — money paths are conservative (no drains)

The core money paths were found **sound**; they are documented here so the
"we checked" is on the record, not just the "we changed".

- **Split conservation.** `computeSplit` asserts `split.Total() == fee` before
  anything is credited; the net amount and referrer cut are the only funds that
  leave the module account, and the app/validator/relayer/treasury shares are
  retained on internal ledgers. Any negative delta is refused by `addInt`
  (`keeper.go:139`). Fuzzed by `FuzzComputeFee` / `FuzzComputeSplit`.
- **Referrer cut cannot inflate the payout.** The referrer share is carved
  *out of the app share* (≤10% of it), not added on top, so the referrer path
  can never make total disbursement exceed the fee.
- **Allowances are app-scoped.** `SpendAllowance` keys on `(payer, app, spender)`;
  there is no path for an app to spend an allowance granted to a different app.
  Confirmed live in the red-team run.
- **Credits are bought, never minted for free.** `acredit` is minted only by
  `BuyCredits` at the governance price (USDC → treasury), tracked by
  `CreditsMinted`, and bounded by the `supply ≤ genesis + minted` invariant.
  `SendEnabled=false` blocks bank transfers and the protocol never redeems
  credits for USDC. (They *can* move peer-to-peer as EVM native value — ERC-4337
  deposits need that — so holders can trade them OTC; that does not create
  supply, which only grows via `BuyCredits`.)
- **Withdrawals are authority-gated and pool-bounded.** `WithdrawTreasury` and
  `DisburseRelayerPool` can only draw from their own pool balance and only by
  the module authority / governed path; `addInt` refuses to overdraw a pool.
- **Farming *earned* quota is ~1000× unprofitable.** Earned quota is
  fee-weighted: 1 uusdc of fees paid earns quota worth on the order of 0.001
  uusdc of gas. There is no positive-EV loop where an app pays fees to farm more
  sponsored gas than it paid for.
- **CORRECTION (found in the mainnet-sim, `docs/benchmarks/mainnet-sim.md` F-1):
  the *base* quota is Sybil-farmable.** Every ACTIVE app gets
  `base_gas_per_epoch` (2,000,000 gas / 60 blocks) unconditionally — no payment,
  attestation or age required (`x/apps/keeper/keeper.go:QuotaFor`) — while
  registration costs only 10 CREDIT ($0.01 at genesis prices). Measured live:
  three fresh registrations each had the full 2M quota immediately. ~600 such
  apps can fill the entire 50% sponsored lane every epoch for about $6 one-off,
  paid for by the protocol paymaster. The dollar drain is tiny at genesis gas
  prices, but it scales 1:1 with any repricing of gas, and the lane contention
  is a real DoS on legitimate apps' gasless UX. An earlier version of this
  section said no subsidy loophole existed; that was wrong for base quota.
  (Those figures used the localnet's 60-block epoch; production epochs are
  86,400 blocks, so the per-app leak is ~1,440× slower — but still unbounded,
  and it grows with any repricing of gas.)
  **FIXED.** v2 gated base quota on domain verification by governance-appointed
  attestors; v3 removed that human gate. Base quota is now bought with capital
  bonded behind the app and is linear in it (`keeper.BaseQuota`,
  `TestBondedBaseQuota`), so splitting capital across fake apps gains nothing,
  with no attestors, biometrics or documents. Measured live: fresh apps get **0**
  and are refused; one bond split over two apps buys exactly what one app gets.
  See `docs/security/sybil-resistance.md`.

**Conclusion of the drain audit:** value into `x/settle` is conserved on every
path, and no attribution loophole lets one party drain another's balance. The
one subsidy gap found — the unconditional base quota — is closed, and the
repriced parameters keep farming unprofitable (worst case 0.90 returned per
dollar of fees, from live params; `registry-economics.test.ts`).

---

## 3. Fixes shipped by this audit

### 3.1 Gas-fee burn → deflationary compute voucher (the capture lever)

**Problem it solves.** The brief asked for a constraint that makes *all*
compute on the chain pay into the protocol, whether or not the deployer uses
our registry. On a permissionless EVM you cannot forbid an independent
contract from running (see §4), but you *can* make the gas it consumes flow to
the protocol. Before this change, EVM gas fees (`acredit`) went to the fee
collector and then to `x/distribution` for validators — so an independent
deployment's gas paid validators but left no deflationary pressure on the
credit supply.

**Change.** `x/settle` now runs a begin-blocker, ordered **before**
`x/distribution`, that burns a governance-set fraction (`gas_burn_bps`, default
`10000` = 100%) of the credit-denom balance sitting in the fee collector from
the previous block (`treasury.go:BurnGasFees`, wired in `invariants.go:BeginBlock`,
`module.go`, and the begin-blocker order in `app.go`).

**Why this is the right lever.** Credits are minted **only** by `BuyCredits`
(USDC → treasury) and can never be sold back (`SendEnabled=false`). Burning
what is spent forces circulating supply to be continuously replenished by
*more USDC purchases into the treasury*. So every unit of computation on the
chain — by a registered app or a completely independent contract — converts,
over time, into protocol revenue. This is the honest, EVM-compatible version of
"you cannot use the chain without paying us": not a block on deployment, but an
unavoidable economic funnel through a bought-and-burned compute voucher.

**Magnitude — measured.** The funnel is exact (burn == fees, to the wei, across
a whole load campaign) and its size is `gas × gas_price ÷ credit_price`. With the
original price (1 CREDIT = $0.001) an ERC-20 transfer burned only ~$3.4×10⁻⁸, so
the capture was negligible. Economics v2 prices gas as infrastructure cost
(1 CREDIT = $50): the same transfer now costs and burns **~$0.0017**, and every
unsponsored transaction pays real USDC for the compute it uses. See
`docs/benchmarks/mainnet-sim.md` §5.

**Why it is safe.**
- Validators are **not** paid from gas; they are paid from the USDC validator
  pool by `PayoutValidators`. Burning gas costs validators nothing.
- It **never halts a block**: every error (missing module, move failure, burn
  failure) is logged and skipped.
- It **preserves the credits invariant**. The invariant is an *upper* bound
  (`supply ≤ genesis + minted`); a burn only lowers supply, so it can never
  breach it.
- `gas_burn_bps` is a validated param (`0..10000`); governance can dial the
  burn from 0% (off) to 100% without a code change.
- Ordering is explicit and commented in `app.go`: settle's begin-blocker runs
  before distribution so it captures the *prior* block's fees before they are
  allocated.

**Verification.**
- Unit: `TestBurnGasFees` covers 100% / 50% / 0% and asserts both the
  fee-collector balance and the exact supply delta.
- Empirical: on localnet with `gas_burn_bps=10000`, `acredit` supply became
  strictly deflationary — it dropped by exactly the gas spent each block — and
  the chain kept producing blocks (advanced past height 79) with no halt.

### 3.2 Contract-move cap re-check at finalize time

**Problem.** `x/apps` lets an owner schedule a contract to move to another app
behind a timelock. The per-app contract cap (`MaxContractsPerApp`) was checked
when the move was *scheduled*, but the destination app could fill up during the
timelock, letting a matured move push the destination over its cap.

**Change.** `finalizeMoves` now re-checks the destination's `ContractCount`
against `MaxContractsPerApp` at unlock time; an over-cap move is dropped (the
contract stays with its source app) and `app_contract_move_dropped` is emitted
(`x/apps/keeper/keeper.go`).

**Verification.** `TestContractMoveDroppedWhenDestinationFull` sets the cap to
1, schedules a move, fills the destination during the timelock, matures the
move, and asserts it is dropped and the contract remains with the source.

---

## 4. What we deliberately did NOT build (and why)

The request included "devs or people who launch a coin separately, without
ours, cannot be profitable" and "constraints that they must use our registry."
An honest engineering answer has to separate what is *enforceable* from what is
*wishful*, because building the wishful version would mean shipping a backdoor.

- **You cannot make an independent EVM launch literally unprofitable.** The
  chain runs the EVM. Any bytecode a user deploys executes; a token that never
  calls our precompile still transfers. Trying to block that would mean
  inspecting and censoring contract bytecode at consensus — which breaks EVM
  equivalence, is trivially evadable (obfuscated bytecode, proxies), and turns
  the chain into something no serious project would build on. We did not do it.
- **You cannot patch away forks.** The source is open and forkable by design
  (AGPL-3.0 for the protocol, Apache-2.0 for the SDK; see `NOTICE`). A fork on a
  *different* chain is outside our consensus entirely; no on-chain mechanism can
  reach it. The defenses against forks are **legal** (AGPL-3.0 obliges a fork to
  publish its source, including when it only runs the code as a network service,
  and to keep the author attributions, the provenance line
  `VAPOR-6eabb1be532bdef4` among them; the names and logos are not licensed, see
  `TRADEMARKS.md`) and **economic** (the network effect of the registry-gated
  rails below), not technical kill-switches.
- **No confiscation, no backdoors, no kill-switches.** The watermarks are
  forensic-only. There is no admin path that seizes user funds, freezes an
  unaffiliated contract, or disables a competitor. Adding one would be the
  single largest security hole in the system and the first thing an audit would
  reject.

**What actually creates the "must use our registry" gravity** is legitimate and
already in the code:

- **Revenue share** (`x/settle` app claimables) is only earned by a
  **registered** app that owns the contract taking the fee. An unregistered
  deployment forfeits its cut to the protocol.
- **Sponsored / gasless UX** (the paymaster + sponsor service) is only granted
  to **ACTIVE registered** apps whose contracts pass policy. Independent
  deployments get no gasless onboarding — the single biggest UX advantage on
  the chain.
- **Quota** (fee-weighted sponsored-gas budget) accrues only to registered
  apps, and ownership proofs stop anyone from attributing someone else's
  contract to steal that quota or revenue.
- **The gas burn (§3.1)** is the universal backstop: even a deployment that
  wants none of the above still pays, because its gas is bought with USDC and
  burned into treasury-driven demand.

That combination — earn nothing / no gasless UX off-registry, and pay into the
treasury no matter what via the burn — is the strongest constraint that is
compatible with being a real, forkable, EVM-equivalent chain. It is economic
gravity, not a cage.

---

## 5. Residual risks (unchanged, restated for this audit)

- Independent EVM deployments remain *possible*; they are made economically
  unattractive relative to registering, not impossible.
- The burn's revenue capture depends on real usage: no traffic, no burn, no
  replenishment demand. It is a funnel, not a faucet.
- Everything in `threat-model.md §Residual risks` still applies (pre-audit,
  PoA centralization, sponsor-key custody, bundler liveness, upstream deps).
