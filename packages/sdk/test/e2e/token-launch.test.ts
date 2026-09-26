// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 VaporChain / muthu2201
// Provenance: VAPOR-6eabb1be532bdef4
//
// Token launch against the LIVE factory: the address shown to a creator
// before launch is the address they get, supply/owner are exact, and a
// replayed launch cannot silently mint a second copy.
import { readFileSync } from 'node:fs'
import { createPublicClient, erc20Abi, http, type Address, type Hex } from 'viem'
import { privateKeyToAccount } from 'viem/accounts'
import { describe, expect, it } from 'vitest'
import { createVaporClient, vaporLocalnet, vaporTokenAbi } from '../../src/index.js'

const root = new URL('../../../../', import.meta.url).pathname
const dep = JSON.parse(readFileSync(`${root}contracts/deployments/779700.json`, 'utf8')) as Record<string, Address>
const ownerKey = process.env.VAPOR_E2E_OWNER_KEY as Hex

describe.skipIf(!ownerKey)('token launch (live localnet)', () => {
  const owner = privateKeyToAccount(ownerKey)
  const client = createVaporClient({ network: vaporLocalnet, account: owner, contracts: { tokenFactory: dep.tokenFactory!, usdc: dep.usdc! } })
  const pub = createPublicClient({ chain: vaporLocalnet.chain, transport: http() })

  it('launches at the predicted address with exact supply, cap and owner', async () => {
    const params = {
      name: 'Gem Shards', symbol: 'GEM', initialSupply: 1_000_000n * 10n ** 18n, cap: 5_000_000n * 10n ** 18n,
      owner: owner.address, metadataURI: 'ipfs://gem', salt: `e2e-${Date.now()}`,
    }
    const predicted = await client.tokens.predict(owner.address, params)
    const { token } = await client.tokens.launch(params)
    expect(token).toBe(predicted)
    expect(await pub.readContract({ address: token, abi: erc20Abi, functionName: 'totalSupply' })).toBe(params.initialSupply)
    expect(await pub.readContract({ address: token, abi: erc20Abi, functionName: 'balanceOf', args: [owner.address] })).toBe(params.initialSupply)
    expect(await pub.readContract({ address: token, abi: vaporTokenAbi, functionName: 'cap' })).toBe(params.cap)
    expect(await pub.readContract({ address: token, abi: vaporTokenAbi, functionName: 'owner' })).toBe(owner.address)

    // same params + salt again: CREATE2 collision, must revert (no duplicate)
    await expect(client.tokens.launch(params)).rejects.toThrow()
  })
})
