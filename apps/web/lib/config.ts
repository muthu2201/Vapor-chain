// Copyright (c) 2026 VaporChain / muthu2201. Licensed under the Apache License, Version 2.0.
// SPDX-License-Identifier: Apache-2.0
// Provenance: VAPOR-6eabb1be532bdef4 (authorship watermark; see NOTICE)

import { isAddress, type Address } from 'viem'
import { vaporNetwork, type VaporNetwork } from '@vaporchain/sdk'

// NEXT_PUBLIC_* must be referenced literally so Next can inline them.
const raw = {
  name: process.env.NEXT_PUBLIC_VAPOR_NETWORK_NAME,
  evmChainId: process.env.NEXT_PUBLIC_VAPOR_EVM_CHAIN_ID,
  cosmosChainId: process.env.NEXT_PUBLIC_VAPOR_COSMOS_CHAIN_ID,
  rpc: process.env.NEXT_PUBLIC_VAPOR_RPC,
  rest: process.env.NEXT_PUBLIC_VAPOR_REST,
  comet: process.env.NEXT_PUBLIC_VAPOR_COMET,
  bundler: process.env.NEXT_PUBLIC_VAPOR_BUNDLER,
  sponsor: process.env.NEXT_PUBLIC_VAPOR_SPONSOR,
  indexer: process.env.NEXT_PUBLIC_VAPOR_INDEXER,
  explorer: process.env.NEXT_PUBLIC_VAPOR_EXPLORER,
  usdc: process.env.NEXT_PUBLIC_VAPOR_USDC,
  tokenFactory: process.env.NEXT_PUBLIC_VAPOR_TOKEN_FACTORY,
  shopAppId: process.env.NEXT_PUBLIC_SHOP_APP_ID,
  shopCheckout: process.env.NEXT_PUBLIC_SHOP_CHECKOUT,
  launchpadAppId: process.env.NEXT_PUBLIC_LAUNCHPAD_APP_ID,
  faucet: process.env.NEXT_PUBLIC_FAUCET_ENABLED,
}

function need(k: keyof typeof raw): string {
  const v = raw[k]
  if (!v) throw new Error(`missing NEXT_PUBLIC config "${k}" (see apps/web/.env.example)`)
  return v
}
function addr(k: keyof typeof raw): Address {
  const v = need(k)
  if (!isAddress(v)) throw new Error(`config "${k}" is not an address: ${v}`)
  return v
}
function id(k: keyof typeof raw): bigint | undefined {
  const v = raw[k]
  return v ? BigInt(v) : undefined
}

export interface AppConfig {
  network: VaporNetwork
  contracts: { usdc: Address; tokenFactory: Address }
  shop: { appId: bigint; checkout: Address } | undefined
  launchpadAppId: bigint | undefined
  faucetEnabled: boolean
  /** every origin the browser talks to (feeds the CSP connect-src) */
  endpoints: string[]
}

export function loadConfig(): AppConfig {
  const network = vaporNetwork({
    id: Number(need('evmChainId')),
    name: need('name'),
    cosmosChainId: need('cosmosChainId'),
    rpc: need('rpc'),
    rest: need('rest'),
    comet: need('comet'),
    bundler: need('bundler'),
    sponsor: need('sponsor'),
    indexer: need('indexer'),
    explorer: raw.explorer ?? '',
  })
  const shopAppId = id('shopAppId')
  return {
    network,
    contracts: { usdc: addr('usdc'), tokenFactory: addr('tokenFactory') },
    shop: shopAppId !== undefined ? { appId: shopAppId, checkout: addr('shopCheckout') } : undefined,
    launchpadAppId: id('launchpadAppId'),
    // a faucet on mainnet would be a free-money bug; refuse regardless of env
    faucetEnabled: raw.faucet === '1' && network.chain.id !== 7797,
    endpoints: [raw.rpc, raw.rest, raw.bundler, raw.sponsor, raw.indexer, 'https://api.skip.build'].filter((x): x is string => !!x),
  }
}

let cached: AppConfig | undefined
export const config = (): AppConfig => (cached ??= loadConfig())
