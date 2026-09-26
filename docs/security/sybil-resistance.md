<!--
SPDX-License-Identifier: CC-BY-4.0
Copyright (c) 2026 VaporChain / muthu2201
Provenance: VAPOR-6eabb1be532bdef4
-->
# Sybil resistance without gatekeepers

**Question:** how does the protocol decide which apps get free (sponsored) gas
without a trusted human, a biometric scan, face ID or identity documents — and
without leaving a loophole for fake apps?

**Answer:** it doesn't decide *who* is legitimate at all. Sponsored gas is a
**linear function of resources the app puts at stake** — fees it pays (earned
quota) and capital it locks (bonded base quota). Neither can be multiplied by
creating more identities, so fake apps gain nothing and no identity check of any
kind is needed.

## Why not identity?

The thing being rationed is *sponsored gas per app*. Every identity-based scheme
answers a different question — "is this a unique human?" — and each brings a
trust or privacy cost:

| approach | examples | problem for this use |
|---|---|---|
| Human attestors (what v2 used) | DNS/business review by appointed signers | a privileged, centralised gate; and domain control alone is cheap (~$10), so the attestors' judgement *was* the security |
| Biometric proof of personhood | World ID (iris), face/palm scans | privacy risk, hardware-operator centralisation, and it proves a *person*, not an app — one honest developer runs many apps |
| Device biometrics / passkeys | Face ID, Touch ID, WebAuthn | proves possession of a device, not uniqueness: one person can create unlimited passkeys |
| Documents / KYC | ID checks, zk-KYC | centralised issuers and custodians of personal data |
| Social-graph personhood | BrightID, Proof of Humanity vouching | relies on other humans' judgement; external network dependency; costs money to fake (e.g. ~$300 to reach a Passport score of 20 via Proof of Humanity) — which is just a price, paid to a third party |
| Synchronous ceremonies | Idena "flip" tests | no biometrics or documents, but forces periodic participation and still verifies people, not apps |
| Stamp aggregators | Gitcoin / Human Passport | scoring by an off-chain service; combines the dependencies above |
| DNSSEC proofs on chain | ENS-style DNSSEC oracle | trustless domain ownership, but domains are cheap, so no Sybil resistance |
| Proof of work | puzzles / VDFs per registration | wastes energy; a rich attacker is unaffected |
| Token-curated registry | staked voting on each app | decentralised but slow, plutocratic, needs a governance token |

The approach that *is* both decentralised and sound is the one Ethereum's own
account-abstraction standard uses against fake paymasters and factories: make
the attacker lock up capital. ERC-4337 requires paymasters to stake "to require
a potential attacker to lock up a non-trivial amount of capital", and proof of
stake weights validators by stake rather than counting them. VaporChain applies
the same principle to sponsored gas.

## The mechanism

```
quota(app, epoch) = min(max_gas_per_epoch,
                        bonded(app) / 1e6 × gas_per_bonded_unit     ← base: capital locked
                      + fee_weight × diversity)                     ← earned: fees paid
```

- **Bond** (`MsgBondApp` / `Settle.bondApp`): the app owner locks USDC in the
  `x/apps` module account. Owner-only; bond token only; positive amounts only.
- **Unbond** (`MsgUnbondApp` / `Settle.unbondApp`): the amount stops counting
  immediately and is returned to the owner after `unbonding_blocks` (21 days by
  default; must be ≥ one epoch). A revoked or suspended app's owner can always
  unbond.
- **Earned quota** is unchanged: `quota_weight` gas per uusdc of Settle fee,
  weighted by payer diversity.
- **No gatekeeper**: there is no attestor, verifier or allow-list anywhere. The
  domain field is self-declared display metadata that nothing trusts.

## Why fake apps gain nothing

- **Linearity.** Base quota is proportional to bonded capital. One app with a
  10,000 USDC bond gets exactly what two apps with 5,000 each get (asserted by
  `TestBondedBaseQuota` and, live, by the `sybil-proof` e2e test). Registering
  another app adds a $10 fee and zero quota.
- **Earned quota is also linear** (in fees) and returns at most $0.90 per $1 of
  fees even when self-dealing, at the sponsor's fee cap.
- **No flash capital.** Unbonding locks funds for 21 days; a bond can't be
  borrowed, used for one epoch and repaid.
- **Nothing to sell.** Quota only sponsors calls into the app's own contracts
  (enforced by the sponsor); it cannot be withdrawn or transferred.

## What it costs the protocol, and who wants it

At the default 2,500 gas per bonded USDC per epoch, the genesis price
(1 CREDIT = $50) and 1-day epochs, a bond buys gas worth about **4.6% of the bond
per year at the fee floor** (at most ~18% if every sponsored op paid the
sponsor's 4 gwei cap). That gas can only sponsor the app's own users, so a pure
capital holder gains nothing they can take out — they would earn more by putting
the same capital to work elsewhere. The apps that bond are the ones that want
gasless users. The protocol's total exposure is bounded by
`rate × total bonded`, and governance can lower `gas_per_bonded_unit` (or set it
to 0) at any time.

Example (measured): a 10,000 USDC bond → 25M gas/epoch. The sponsor charges
quota the gas an operation actually used — ~426k for a first-time user's op,
EIP-7702 upgrade included — while holding 1.75M of headroom to start one, so
that is about **58 first-time users a day** on 1-day epochs. The capital is
fully refundable.

## Limits, stated plainly

- Capital is the gate, so a well-funded attacker can buy base quota — but only
  proportionally to capital locked, exactly like an honest app, and only
  usable on their own contracts. The protocol's cost is capped by the rate.
- Sponsored transactions still compete for the 50% sponsored lane; bonded quota
  does not change that cap.
- An app with no capital and no fee history gets no protocol sponsorship. It can
  still sponsor its users by running its own paymaster, or let them pay gas in
  USDC through the token paymaster.

Sources consulted: [ERC-4337 (staking for Sybil resistance)](https://eips.ethereum.org/EIPS/eip-4337),
[ERC-4337 paymasters documentation](https://docs.erc4337.io/paymasters/index.html),
[Proof of personhood (overview)](https://en.wikipedia.org/wiki/Proof_of_personhood),
[Who Watches the Watchmen? — review of subjective Sybil resistance](https://www.frontiersin.org/journals/blockchain/articles/10.3389/fbloc.2020.590171/full),
[Gitcoin Passport](https://go.gitcoin.co/passport),
[Human Passport](https://human.tech/blog/human-passport-proof-of-personhood-and-sybil-resistance-for-web3).
