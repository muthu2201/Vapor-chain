<!--
SPDX-License-Identifier: CC-BY-4.0
Copyright (c) 2026 VaporChain / muthu2201
Provenance: VAPOR-6eabb1be532bdef4
-->
# VaporChain

**A low-cost consumer EVM appchain where users pay in USDC with one signature and
no gas token, and apps sponsor their users' gas out of earned revenue.**

VaporChain is a Cosmos SDK chain (cosmos/evm) with native USDC settlement,
ERC-4337 + EIP-7702 account abstraction, and an IBC path that brings USDC in from
other chains. A first-time user with only USDC can check out in a single
signature — no seed phrase, no gas token, instant finality.

> **Open source, pre-audit.** The protocol is AGPL-3.0, the SDK and everything
> apps build on is Apache-2.0, and the docs are CC-BY-4.0 (see
> [License](#license--provenance)). It has **not** been audited yet: see the
> [threat model](./docs/security/threat-model.md). Want to help? Read
> [`CONTRIBUTING.md`](./CONTRIBUTING.md). Found a vulnerability? Follow
> [`SECURITY.md`](./SECURITY.md) and do not open a public issue.

## What's here

| path | what |
|---|---|
| `chain/` | `vaporchaind` — the node: EVM, `x/settle`, `x/apps`, `x/council`, Settle precompile, sponsored lane, IBC |
| `contracts/` | Foundry: EntryPoint v0.8 + Simple7702Account (canonical), paymasters, token factory, checkout |
| `services/` | sponsor (ERC-7677), Alto bundler, indexer (Postgres + Parquet) |
| `packages/sdk`, `packages/react` | TypeScript SDK and React hooks + embedded wallet |
| `apps/web` | Next.js 16 reference app (shop, wallet, launchpad, dashboard) |
| `deploy/` | Dockerfiles, docker-compose, systemd/Cosmovisor, Prometheus alerts |
| `scripts/` | localnet, testnet genesis builder, provenance scan |
| `docs/` | architecture, security, benchmarks, SDK guides |

Start with **[ARCHITECTURE.md](./ARCHITECTURE.md)** — it explains *why* each piece
is built the way it is.

## Quick start (localnet)

Requires Go 1.26.8, Foundry 1.8.3, Node 22 + pnpm 12, `jq`, `cast`, PostgreSQL.

```bash
# 1. build the node
cd chain && make build && cd ..

# 2. a 4-validator localnet with funded load accounts
scripts/localnet/localnet.sh init 4 300
scripts/localnet/localnet.sh start

# 3. contracts + sponsor + bundler + indexer + Postgres
scripts/localnet/services.sh up

# 4. wire the reference app to it and run
node apps/web/scripts/bootstrap-localnet.ts
pnpm --filter @vaporchain/web dev      # http://localhost:3000
```

Chain IDs: EVM mainnet **7797** / testnet **77970** / localnet **779700**;
Cosmos `vaporchain-1` / `vapor-testnet-1` / `vapor-local-1`. Bech32 prefix
`vapor`. Settle precompile `0x0000000000000000000000000000000000000900`.

## Build a dapp

```bash
pnpm add @vaporchain/sdk @vaporchain/react viem @tanstack/react-query
```

See the [SDK guide](./docs/sdk/README.md) and
[launch your token](./docs/sdk/launch-your-token.md). One-signature checkout:

```ts
await client.checkout({ checkout, orderId, token: usdc, amount: 10_000_000n })
```

## Test

```bash
cd chain && make test                       # Go, with the EVM reset tag
cd contracts && forge test && forge lint src
pnpm -r --filter './packages/**' run test   # SDK + React unit tests
# live E2E (needs a running localnet):
VAPOR_E2E_OWNER_KEY=0x… pnpm -r run test:e2e
```

CI (`.github/workflows/ci.yml`) runs all of the above plus a secret scan and the
provenance check on every push.

## Status

Production-ready **testnet** engineering, pre-audit. Highlights:

- Full one-signature USDC checkout proven end-to-end on a live chain (SDK +
  React + Playwright browser E2E).
- Stress-tested to a ~215 tps transfer knee with graceful backpressure; a
  consensus liveness bug was found under load and fixed
  ([results](./docs/benchmarks/results.md)).
- Adversarial testing of the precompile, allowances, credits, sponsor policy,
  paymaster and services ([red team](./docs/security/red-team.md)).

## License & provenance

| what | license |
|---|---|
| The protocol: `chain/`, `services/`, `contracts/src/paymaster`, `deploy/`, `tools/`, `scripts/` | [AGPL-3.0-only](./LICENSE) |
| What apps build on: `packages/sdk`, `packages/react`, `apps/web`, `contracts/src/{interfaces,checkout,examples,tokens,utils}`, `chain/proto`, `docs/sdk` | [Apache-2.0](./LICENSES/Apache-2.0.txt) |
| Other documentation (Markdown) | [CC-BY-4.0](./LICENSES/CC-BY-4.0.txt) |

Each file's SPDX header is authoritative. [`NOTICE`](./NOTICE) has the full map,
the AGPL section 7 attribution terms and the upstream notices. Building an app on
the Apache-2.0 SDK does **not** put your code under the AGPL. The name and logo
are not licensed: see [`TRADEMARKS.md`](./TRADEMARKS.md). Third-party components:
[`THIRD_PARTY_NOTICES.md`](./THIRD_PARTY_NOTICES.md). Authorship watermarks:
`scripts/provenance/scan.sh`.
