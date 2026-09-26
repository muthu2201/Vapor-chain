<!--
SPDX-License-Identifier: Apache-2.0
Copyright (c) 2026 VaporChain / muthu2201
Provenance: VAPOR-6eabb1be532bdef4
-->
# VaporChain SDK

Build gasless, USDC-native apps on **VaporChain** — where users pay in USDC with
one signature and no gas token, and your app sponsors their gas out of earned
revenue.

This is the **public developer distribution**: the TypeScript SDK, React hooks,
a no-seed-phrase embedded wallet, developer-facing example contracts, and the
guides you need to ship. Apache-2.0 licensed.

> The VaporChain protocol (the node and its modules) is open source on this
> repository's default branch (AGPL-3.0, with the SDK-facing parts Apache-2.0).
> You don't need it to build on VaporChain: this SDK and a network endpoint are
> enough. Contributing? Read [`CONTRIBUTING.md`](./CONTRIBUTING.md). Found a
> vulnerability? Follow [`SECURITY.md`](./SECURITY.md) and don't open a public
> issue.

## Install

```bash
pnpm add @vaporchain/sdk viem                        # core
pnpm add @vaporchain/react @tanstack/react-query      # + React hooks
```

## One-signature, gasless checkout

```ts
import { createVaporClient, vaporNetwork } from '@vaporchain/sdk'
import { privateKeyToAccount } from 'viem/accounts'

const client = createVaporClient({
  network: vaporNetwork({ id: 77970, name: 'VaporChain Testnet', /* endpoints… */ }),
  account: privateKeyToAccount('0x…'),
  appId: 42n,                                   // your app sponsors the user's gas
  contracts: { tokenFactory: '0x…', usdc: '0x…' },
})

// user holds USDC, zero gas credits → approveApp + payOrder in one sponsored UserOp
await client.checkout({ checkout, orderId, token: usdc, amount: 10_000_000n })
```

## What's inside

| path | what |
|---|---|
| `packages/sdk` | `@vaporchain/sdk` — viem-based client, settle calls, AA, token launch, bridging |
| `packages/react` | `@vaporchain/react` — hooks + embedded wallet |
| `apps/web` | Next.js 16 reference app (shop, wallet, launchpad, dashboard) |
| `contracts/src` | developer contracts: `VaporCheckout`, `VaporToken(Factory)`, `ISettle`, examples |
| `contracts/deployments` | deployed protocol addresses per chain id |
| `docs/sdk` | [developer guide](./docs/sdk/README.md) · [launch your token](./docs/sdk/launch-your-token.md) |

## Guides

- **[SDK guide](./docs/sdk/README.md)** — connect, checkout, balances, quota,
  embedded wallet, bridging, register your app.
- **[Launch your token](./docs/sdk/launch-your-token.md)** — deploy an ERC-20 at a
  predictable address, verify it, attribute it to your app, and (optionally) make
  it a Settle asset or bridge it.

## Develop

```bash
pnpm install
pnpm --filter @vaporchain/sdk build && pnpm --filter @vaporchain/react build
pnpm -r --filter ./packages/** run test        # unit tests
```

Example contracts compile with Foundry (solc 0.8.37, via_ir, prague). Install the
libraries first:

```bash
cd contracts
forge install OpenZeppelin/openzeppelin-contracts eth-infinitism/account-abstraction
forge build
```

## License

Apache-2.0 — see [`LICENSE`](./LICENSE), [`NOTICE`](./NOTICE), and
[`THIRD_PARTY_NOTICES.md`](./THIRD_PARTY_NOTICES.md). Files retain the
`VAPOR-6eabb1be532bdef4` provenance watermark, which has no runtime effect.
