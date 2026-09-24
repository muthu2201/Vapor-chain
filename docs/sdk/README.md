<!-- Copyright (c) 2026 VaporChain / muthu2201. All rights reserved. Provenance: VAPOR-6eabb1be532bdef4 -->
# VaporChain developer SDK

Build a gasless, USDC-native app on VaporChain in TypeScript. Two packages:

- **`@vaporchain/sdk`** — framework-agnostic (viem). Everything works from Node
  or the browser.
- **`@vaporchain/react`** — React 19 hooks + a no-seed-phrase embedded wallet.

```bash
pnpm add @vaporchain/sdk viem            # core
pnpm add @vaporchain/react @tanstack/react-query   # + React
```

## 1. Connect

```ts
import { createVaporClient, vaporNetwork } from '@vaporchain/sdk'
import { privateKeyToAccount } from 'viem/accounts'

const network = vaporNetwork({
  id: 77970, name: 'VaporChain Testnet', cosmosChainId: 'vapor-testnet-1',
  rpc: 'https://rpc.testnet.vaporchain.xyz', rest: 'https://rest.testnet…',
  comet: 'https://comet…', bundler: 'https://bundler…',
  sponsor: 'https://sponsor…', indexer: 'https://indexer…',
  explorer: 'https://explorer…',
})

const client = createVaporClient({
  network,
  account: privateKeyToAccount('0x…'),   // or omit for read-only
  appId: 42n,                            // your app sponsors this user's gas
  contracts: { tokenFactory: '0x…', usdc: '0x…' }, // from deployments/<id>.json
})
```

## 2. One-signature, gasless checkout

The user holds USDC and **zero gas credits**. `checkout` bundles `approveApp` +
`payOrder` into a single sponsored UserOperation; on first use it also upgrades
the user's EOA with EIP-7702. Gas is paid by your app's earned quota.

```ts
const res = await client.checkout({
  checkout: '0x…',                 // your VaporCheckout contract
  orderId: keccak256(stringToHex(`order-${id}`)),
  token: client.network /* usdc */ && contracts.usdc,
  amount: 10_000_000n,             // 10 USDC (6 decimals)
})
// res.kind === 'userop'; res.txHash is the on-chain bundle tx
```

Prefer building your own calls? `client.pay({ calls, sponsor: 'app' })` runs any
batch as a sponsored UserOp; `sponsor: 'user'` sends a normal transaction.

## 3. Read balances, fees, quota

```ts
await client.estimateFee({ token: usdc, amount: 10_000_000n }) // {fee, net, sponsored}
await client.sponsor.quota(appId)                              // remaining gas this epoch
await client.apps.claimable(appId, usdc)                       // your unclaimed revenue
```

In React, the same data is live:

```tsx
import { VaporProvider, useCheckout, useUnifiedBalance, useSponsorQuota } from '@vaporchain/react'

<VaporProvider network={network} contracts={contracts} appId={42n} account={account}>
  <App />
</VaporProvider>

const { data: balance } = useUnifiedBalance({ symbol: 'USDC' })
const { data: quota }   = useSponsorQuota()
const checkout = useCheckout()
checkout.mutate({ checkout: '0x…', orderId, token: usdc, amount: 10_000_000n })
```

## 4. Embedded wallet (no seed phrase, no gas)

```tsx
import { useEmbeddedWallet } from '@vaporchain/react'
const wallet = useEmbeddedWallet()
// wallet.status: 'none' | 'ready'; wallet.create(); wallet.exportKey(); wallet.forget()
```

The key is generated and encrypted **in the browser** under a non-extractable
WebCrypto key in IndexedDB (see the threat model). It never leaves the device and
never touches our servers. Always offer `exportKey()` before `forget()`.

## 5. Bring USDC in from another chain

```ts
const route = await client.bridge({
  amountIn: '10000000', sourceAssetDenom: 'uusdc', sourceAssetChainId: 'noble-1',
  destAssetDenom: 'uusdc', destAssetChainId: 'vapor-testnet-1',
})
for await (const stage of client.status({ txHash, chainId: 'noble-1' })) { /* … */ }
```

## 6. Register your app

```ts
const { appId } = await client.apps.register({
  recipient: '0xYourTreasury', metadataUri: 'ipfs://…', referrerBps: 500,
})
// attribute your checkout/contract so its revenue and sponsorship route to you:
await client.apps.acceptContract(appId, checkoutAddress)  // after a proof, see the CLI
```

Contract attribution needs an ownership proof (CREATE nonce, CREATE2, or
`Ownable`); the fastest path is `VaporCheckout`, which self-claims in its
constructor — you just `acceptContract`. Details in
[launch-your-token.md](./launch-your-token.md).

## Error handling

Every failure is a `VaporError` with a `.code`
(`SPONSORSHIP_DENIED`, `QUOTA_EXHAUSTED`, `NOT_REGISTERED`, `HTTP`, `TIMEOUT`, …)
so you can branch on cause. See `apps/web/app/_components/errors.ts` for a
user-facing mapping.
