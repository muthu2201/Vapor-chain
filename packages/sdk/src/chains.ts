// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 VaporChain / muthu2201
// Provenance: VAPOR-6eabb1be532bdef4

import { defineChain, type Chain } from 'viem'
import { ENTRY_POINT_ADDRESS, MULTICALL3 } from './constants.js'

/**
 * Network descriptors. The native currency is the gas CREDIT (18 decimals):
 * a fixed-price, non-redeemable voucher users normally never see, because
 * apps sponsor gas or users pay it in USDC through the token paymaster.
 */
export interface VaporNetwork {
  chain: Chain
  cosmosChainId: string
  restUrl: string
  cometUrl: string
  bundlerUrl: string
  sponsorUrl: string
  indexerUrl: string
  explorerUrl: string
}

const nativeCurrency = { name: 'Gas Credit', symbol: 'CREDIT', decimals: 18 } as const

export function vaporChain(p: { id: number; name: string; rpc: string; ws?: string; explorer: string; testnet: boolean }): Chain {
  return defineChain({
    id: p.id,
    name: p.name,
    nativeCurrency,
    rpcUrls: { default: { http: [p.rpc], ...(p.ws ? { webSocket: [p.ws] } : {}) } },
    blockExplorers: { default: { name: 'VaporScan', url: p.explorer } },
    contracts: {
      multicall3: { address: MULTICALL3 },
      entryPoint08: { address: ENTRY_POINT_ADDRESS },
    },
    testnet: p.testnet,
  })
}

export const vaporLocalnet: VaporNetwork = {
  chain: vaporChain({ id: 779700, name: 'VaporChain Localnet', rpc: 'http://127.0.0.1:8545', ws: 'ws://127.0.0.1:8546', explorer: 'http://127.0.0.1:4000', testnet: true }),
  cosmosChainId: 'vapor-local-1',
  restUrl: 'http://127.0.0.1:1317',
  cometUrl: 'http://127.0.0.1:26657',
  bundlerUrl: 'http://127.0.0.1:4337',
  sponsorUrl: 'http://127.0.0.1:8800',
  indexerUrl: 'http://127.0.0.1:8900',
  explorerUrl: 'http://127.0.0.1:4000',
}

/** Build the testnet/mainnet descriptors from your deployment's public endpoints. */
export function vaporNetwork(p: {
  id: 77970 | 7797 | number
  name: string
  cosmosChainId: string
  rpc: string
  ws?: string
  rest: string
  comet: string
  bundler: string
  sponsor: string
  indexer: string
  explorer: string
}): VaporNetwork {
  return {
    chain: vaporChain({ id: p.id, name: p.name, rpc: p.rpc, ...(p.ws ? { ws: p.ws } : {}), explorer: p.explorer, testnet: p.id !== 7797 }),
    cosmosChainId: p.cosmosChainId,
    restUrl: p.rest,
    cometUrl: p.comet,
    bundlerUrl: p.bundler,
    sponsorUrl: p.sponsor,
    indexerUrl: p.indexer,
    explorerUrl: p.explorer,
  }
}

/** wallet_addEthereumChain parameters (MetaMask / Rabby). */
export function addChainParams(n: VaporNetwork) {
  return {
    chainId: `0x${n.chain.id.toString(16)}`,
    chainName: n.chain.name,
    nativeCurrency,
    rpcUrls: n.chain.rpcUrls.default.http,
    blockExplorerUrls: [n.explorerUrl],
  }
}
