<!--
SPDX-License-Identifier: Apache-2.0
Copyright (c) 2026 VaporChain / muthu2201
Provenance: VAPOR-6eabb1be532bdef4
-->
# Launch your token on VaporChain

A step-by-step guide to shipping an ERC-20 and, optionally, making it a
first-class VaporChain asset (usable in Settle payments and bridgeable over IBC).

## 1. Launch the ERC-20

`VaporTokenFactory` deploys an OpenZeppelin-based ERC-20 with **permit**,
**burn**, and an **optional supply cap** at a CREATE2 address you can show the
user *before* they sign. The salt mixes in the creator address, so nobody can
front-run your token's address.

```ts
import { createVaporClient } from '@vaporchain/sdk'
const client = createVaporClient({ network, account, contracts })

const params = {
  name: 'Gem Shards', symbol: 'GEM',
  initialSupply: 1_000_000n * 10n ** 18n, // minted to `owner`
  cap: 0n,                                 // 0 = fixed forever; >0 = mintable up to cap
  owner: account.address,
  metadataURI: 'ipfs://…',                 // optional
}

const predicted = await client.tokens.predict(account.address, params) // show in UI
const { token } = await client.tokens.launch(params)                   // token === predicted
```

Gasless launch (paid by a launchpad app's quota — the launchpad needs a bond or earned quota):
`client.tokens.launch(params, { sponsored: true })`, or the
`useLaunchToken({ sponsored: true })` hook. In React,
`usePredictedTokenAddress(params)` gives the live predicted address as the user
types.

**Determinism:** the default salt is derived from `(name, symbol, owner)`, so
`predict` and `launch` always agree and launching the exact same token twice
reverts (no accidental duplicates). Pass an explicit `salt` to launch variants.

## 2. Verify your source

Foundry deploys with solc 0.8.37, `via_ir`, 1,000,000 optimizer runs,
`evm_version = prague` (see `contracts/foundry.toml`). Match these exactly when
verifying on the explorer, and verify against
`contracts/out/VaporToken.sol/VaporToken.json`.

## 3. (Optional) Attribute the token's app for revenue & sponsorship

If your token has a shop/checkout contract, attribute it to your app so its
Settle revenue and sponsored gas route to you:

```bash
# prove you deployed it with CREATE at nonce N (or CREATE2 / Ownable):
vaporchaind tx apps add-contract <app-id> <0x-contract> \
  --proof '{"type":"PROOF_TYPE_DEPLOYER_CREATE","nonce":"<N>"}' \
  --from <key> --gas auto --gas-prices 1000000000acredit
```

`add-contract --help` lists all proof forms. `VaporCheckout` self-claims in its
constructor, so for it you only run `apps accept-claim`.

## 4. (Optional) Make it a Settle asset

To let users **pay in your token** through Settle (fees, tabs, quota), governance
adds it to `x/settle` params `assets` with its fee curve
(`min_fee`, `max_fee`, `micro_threshold`, `quota_weight`, `credit_price`). Submit
a `MsgUpdateParams` governance proposal; see `scripts/testnet/testnet.config.json`
for the asset shape. Until then your token is a normal ERC-20 and trades on any
DEX deployed to VaporChain.

## 5. (Optional) Make it bridgeable / a bank coin

VaporChain represents bank coins and ERC-20s as one balance via `x/erc20`. To
register an existing ERC-20 as a bank denom (needed for IBC transfer), governance
submits an `x/erc20` `RegisterERC20` proposal. The reverse (a Cosmos coin →
ERC-20) uses `RegisterCoin`. This is how testnet USDC (`uusdc`) is exposed at its
ERC-20 address.

## Checklist

- [ ] `predict` address shown to the user and matches `launch`
- [ ] source verified on the explorer with the exact compiler settings
- [ ] shop/checkout contract attributed to your app (if you want revenue)
- [ ] (optional) governance proposal to add it as a Settle asset
- [ ] (optional) `x/erc20` registration if it must bridge over IBC
