// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 VaporChain / muthu2201
// Provenance: VAPOR-6eabb1be532bdef4
import { describe, expect, it } from 'vitest'
import { addChainParams, vaporLocalnet, vaporNetwork } from '../../src/index.js'

const endpoints = {
  rpc: 'https://rpc.example', rest: 'https://rest.example', comet: 'https://comet.example', bundler: 'https://bundler.example',
  sponsor: 'https://sponsor.example', indexer: 'https://indexer.example', explorer: 'https://explorer.example',
}

describe('networks', () => {
  it('localnet matches the chain constants', () => {
    expect(vaporLocalnet.chain.id).toBe(779700)
    expect(vaporLocalnet.cosmosChainId).toBe('vapor-local-1')
    expect(vaporLocalnet.chain.nativeCurrency.decimals).toBe(18)
  })
  it('mainnet (7797) is the only non-testnet id', () => {
    expect(vaporNetwork({ id: 7797, name: 'VaporChain', cosmosChainId: 'vaporchain-1', ...endpoints }).chain.testnet).toBe(false)
    expect(vaporNetwork({ id: 77970, name: 'VaporChain Testnet', cosmosChainId: 'vapor-testnet-1', ...endpoints }).chain.testnet).toBe(true)
  })
  it('wallet_addEthereumChain params use a hex chain id', () => {
    const p = addChainParams(vaporNetwork({ id: 77970, name: 'VaporChain Testnet', cosmosChainId: 'vapor-testnet-1', ...endpoints }))
    expect(p.chainId).toBe('0x13092')
    expect(p.rpcUrls).toEqual(['https://rpc.example'])
    expect(p.blockExplorerUrls).toEqual(['https://explorer.example'])
  })
})
