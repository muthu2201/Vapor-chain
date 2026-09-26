<!--
SPDX-License-Identifier: Apache-2.0
Copyright (c) 2026 VaporChain / muthu2201
Provenance: VAPOR-6eabb1be532bdef4
-->
# Security policy

VaporChain is **pre-audit testnet software**. There is no mainnet and no real
value on the network. We still treat every vulnerability as if there were,
because a bug found now is a bug that never reaches a mainnet.

## Reporting a vulnerability

**Do not open a public issue, discussion or pull request for a vulnerability.**

Report it privately through GitHub: go to the repository's **Security** tab,
choose **Report a vulnerability**, and fill in the form. This opens a private
advisory that only the maintainers can see.

Please include:

- the affected component and commit hash;
- what an attacker can do, and what it costs them;
- a proof of concept. A Go keeper test, a Foundry test or a localnet script is
  ideal; `CONTRIBUTING.md` §10 lists the test harnesses;
- any fix you suggest.

## What happens next

The project is maintained by a small team, so these are targets, not guarantees:

| step | target |
|---|---|
| acknowledge your report | within 7 days |
| confirm or reject it, with reasoning | within 14 days |
| fix for a confirmed critical or high issue | as fast as possible, usually within 30 days |
| public advisory | when the fix is released, or after 90 days, whichever comes first, agreed with you |

We credit reporters in the advisory and in the fix commit unless you ask us not to.
There is **no paid bug bounty** at the moment. If that changes, this file will
say so. Don't rely on anything that isn't written here.

## Scope

In scope: everything in this repository that VaporChain wrote. In particular:

- the chain: `x/settle`, `x/apps`, `x/council`, the Settle precompile, the
  sponsored lane, ante and mempool wiring, and the `vapor_*` RPC;
- the paymaster, checkout and token contracts in `contracts/src`;
- the sponsor service, the indexer API, the SDK, the React hooks and the web
  app (CSP, embedded-wallet key storage, faucet).

These are the vulnerabilities we most want to hear about:

- loss or theft of funds;
- minting credits without paying;
- spending another party's allowance, revenue or quota;
- exceeding the sponsored-lane cap;
- forging or replaying a sponsorship;
- halting or forking the chain;
- a validator escaping removal;
- `acredit`/`avpower` leaving through IBC;
- key extraction from the web wallet.

Out of scope:

- upstream projects: Cosmos SDK, CometBFT, cosmos/evm, go-ethereum, ibc-go,
  Alto, the EntryPoint, OpenZeppelin. Report those upstream, and tell us if
  VaporChain is affected;
- the known issues already documented in the threat model and in findings F-4
  and F-5 of `docs/benchmarks/mainnet-sim.md`;
- the trust any PoA chain places in its validator set;
- missing best-practice headers, clickjacking on pages with no sensitive
  actions, and volumetric DoS.

## Safe harbour

If you act in good faith, we will not pursue or support legal action against
you for security research on VaporChain. Good faith means you:

- test **only** on your own localnet or your own nodes. Never attack other
  people's nodes, wallets or funds, and never run DoS or spam against any public
  network;
- stop and report as soon as you find a vulnerability, and don't access more
  data than you need to show it;
- give us reasonable time to fix it before disclosing it.

## Supported versions

Only the latest commit on the default branch is supported. Fixes are not
backported.
