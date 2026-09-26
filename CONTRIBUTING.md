<!--
SPDX-License-Identifier: Apache-2.0
Copyright (c) 2026 VaporChain / muthu2201
Provenance: VAPOR-6eabb1be532bdef4
-->
# Contributing to the VaporChain SDK

Thanks for helping. This branch is the **developer distribution** of
VaporChain. Everything here is Apache-2.0:

- `@vaporchain/sdk` and `@vaporchain/react`;
- the reference web app;
- the developer-facing example contracts;
- the SDK guides.

The protocol (the node, its modules, the Settle precompile and the services)
lives on the repository's **default branch**. Its `CONTRIBUTING.md` explains
how the whole system is built, and lists the consensus and money rules you must
never break there. Read it before proposing anything that needs a chain change.

> **Status: open source, pre-audit testnet software. There is no mainnet.**
> **Vulnerabilities:** never open a public issue. Follow [`SECURITY.md`](./SECURITY.md).

## Quick checklist

1. Pick an issue labelled `good first issue` or `help wanted`, and comment to
   claim it. For anything else, open an issue first.
2. Fork, then branch from `claude/developer-sdk-docs-e6nxp4`, this branch.
3. Make the change **with tests**, then run the checks below.
4. Run `node scripts/license/headers.mjs --fix` to add license headers to new files.
5. Sign off every commit with `git commit -s` (DCO; see [Licensing](#licensing-of-contributions)).
6. Open a pull request from the template. Keep it to one topic, and make the description explain *why*.

## What's here

| path | what | notes |
|---|---|---|
| `packages/sdk` | framework-agnostic client (viem): checkout, sponsored UserOps, EIP-7702, token launch, app registry, bonds, bridging | public API: changes need a changelog note |
| `packages/react` | React hooks, embedded wallet (key sealed in IndexedDB) | wallet key storage is security-sensitive |
| `apps/web` | Next.js reference app: shop, wallet, launchpad, dashboard | CSP and faucet are security-sensitive |
| `contracts/src` | `ISettle` interface, `SettleAddress`, checkout, token + factory, examples | `ISettle.sol` mirrors the chain's precompile ABI |
| `docs/sdk` | SDK and token-launch guides | good first contributions |

## How it is built

- **Every call is a real chain call.**
  - The SDK talks to the chain through viem and three endpoints: EVM
    JSON-RPC, the Cosmos REST gateway (for `x/apps` and `x/settle` queries) and
    the ERC-4337 bundler.
  - Protocol actions go through the Settle precompile at `0x…0900`.
  - Sponsored ("gasless") calls are ERC-4337 v0.8 UserOps signed by the
    sponsor service (ERC-7677). They are paid by the verifying paymaster from
    the app's quota.
- **One signature.** A first-time user's EOA is upgraded with EIP-7702
  (`Simple7702Account`) inside the same UserOp that pays.
- **Receipts wait for 2 confirmations** (`confirmations: 2`). This works around
  an upstream cosmos/evm race between receipt and state; don't remove it.
- **Tests are the spec.** Unit tests run offline. The e2e suites run against a
  live localnet, bundler and sponsor started from the default branch.

## Set up, build and test

Node ≥ 22, pnpm 12.6.0, and Foundry 1.8.3 (only for the contracts).

```bash
pnpm install --frozen-lockfile
pnpm --filter @vaporchain/sdk build && pnpm --filter @vaporchain/react build
pnpm -r run typecheck
pnpm --filter @vaporchain/sdk exec vitest run --dir test/unit
pnpm --filter @vaporchain/react exec vitest run --dir test/unit
node scripts/license/headers.mjs
```

**Live e2e tests** need a running localnet with services. Start them from a
checkout of the default branch:

```bash
scripts/localnet/localnet.sh init 4 300 && scripts/localnet/localnet.sh start
scripts/localnet/services.sh up
```

Then, on this branch, run the suites with the localnet's test key.
`contracts/deployments/779700.json` must match the running localnet.

```bash
VAPOR_E2E_OWNER_KEY=0x… pnpm --filter @vaporchain/sdk run test:e2e
VAPOR_E2E_OWNER_KEY=0x… pnpm --filter @vaporchain/react run test:e2e
```

## Do not touch

- **Generated ABIs** (`packages/sdk/src/abi/*.ts`). They are generated on the
  default branch from the compiled contracts and the precompile ABI by
  `scripts/gen-abis.mjs`, then ported here. Never edit them by hand.
- **`contracts/src/interfaces/ISettle.sol`** must stay byte-identical to the
  chain's copy. Existing method signatures are a public ABI.
- **Protocol constants** in `packages/sdk/src/constants.ts` must match the
  chain:
  - chain IDs (7797 / 77970 / 779700);
  - the precompile address;
  - the EntryPoint and `Simple7702Account` addresses;
  - the provenance fingerprint. It is mixed into sponsorship signatures, so
    changing it breaks them.
- **`contracts/deployments/*.json`** is written by the localnet tooling.
- **`pnpm-lock.yaml`** changes only through pnpm, in a dependency PR.
- **Legal files and headers.** Never remove or alter any of these:
  - `LICENSE`, `NOTICE`, `TRADEMARKS.md`, `scripts/license/rules.json`;
  - the SPDX, copyright and `Provenance:` header lines.

  Apache-2.0 section 4 requires redistributors to keep them.
- **CI and CODEOWNERS** (`.github/`) are maintainer-only.
- **Never commit** private keys, mnemonics, `.env` files (other than
  `.env.example`), `.localnet/` data or personal data.

## Where to start

- **Web:** show an app's bond, base quota and pending unbondings on
  `apps/web/app/apps/[id]/page.tsx`, using `apps.bondInfo` or
  `appsRest(...).bond(appId)`.
- **SDK tests:** offline unit tests for REST parsing in
  `packages/sdk/src/apps.ts` (`bond`, `quota`) against JSON fixtures.
- **Docs:** fill gaps in `docs/sdk/`. Useful additions are error-handling
  recipes and a sponsored-quota walkthrough.
- **SDK (help wanted):** a helper that trims over-declared gas limits (protocol
  finding F-5). Discuss the approach in an issue first.

## Licensing of contributions

- **Inbound = outbound.** Everything on this branch is Apache-2.0, and so is
  your contribution. There is no CLA.
- **DCO.** Every commit must be signed off (`git commit -s`), certifying the
  [Developer Certificate of Origin 1.1](https://developercertificate.org/). A
  CI check enforces this on pull requests. To fix a missing sign-off, run
  `git commit --amend -s --no-edit`, or `git rebase --signoff <base>` for
  several commits.
- **AI-generated code** is welcome, but you are responsible for it. You must be
  able to explain it and certify the DCO for it, and it must meet the same test bar.
- **Trademarks.** Contributing grants no rights in the VaporChain name or logo.
  See [`TRADEMARKS.md`](./TRADEMARKS.md).

Everyone here follows the [Code of Conduct](./CODE_OF_CONDUCT.md).
