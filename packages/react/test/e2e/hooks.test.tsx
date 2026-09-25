// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Provenance: VAPOR-6eabb1be532bdef4
//
// Hooks against the LIVE localnet + Alto + sponsor service, rendered in a
// browser-like DOM (happy-dom enforces CORS, so this also proves every
// endpoint is callable from a web page).
import { QueryClient } from '@tanstack/react-query'
import { act, renderHook, waitFor } from '@testing-library/react'
import { IDBFactory } from 'fake-indexeddb'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import type { ReactNode } from 'react'
import { createPublicClient, createWalletClient, erc20Abi, http, keccak256, stringToHex, type Address, type Hex } from 'viem'
import { privateKeyToAccount, type PrivateKeyAccount } from 'viem/accounts'
import { afterEach, beforeAll, describe, expect, it } from 'vitest'
import { createVaporClient, vaporLocalnet } from '@vaporchain/sdk'
import { bondApp } from './localnet.js'
import {
  indexedDbKeyStore,
  useCheckout,
  useEmbeddedWallet,
  useFeeQuote,
  useLaunchToken,
  usePredictedTokenAddress,
  useSponsorQuota,
  useUnifiedBalance,
  VaporProvider,
} from '../../src/index.js'

const root = `${resolve(process.cwd(), '../..')}/` // vitest runs from packages/react
const dep = JSON.parse(readFileSync(`${root}contracts/deployments/779700.json`, 'utf8')) as Record<string, Address>
const checkoutArtifact = JSON.parse(readFileSync(`${root}contracts/out/VaporCheckout.sol/VaporCheckout.json`, 'utf8'))
const ownerKey = process.env.VAPOR_E2E_OWNER_KEY as Hex
const chain = vaporLocalnet.chain
const contracts = { tokenFactory: dep.tokenFactory!, usdc: dep.usdc! }

describe.skipIf(!ownerKey)('react hooks (live localnet)', () => {
  const owner = privateKeyToAccount(ownerKey)
  const pub = createPublicClient({ chain, transport: http() })
  const ownerWallet = createWalletClient({ account: owner, chain, transport: http() })
  let appId: bigint
  let checkout: Address
  const merchant = privateKeyToAccount(keccak256(stringToHex(`merchant-${Date.now()}`))).address

  beforeAll(async () => {
    const ownerClient = createVaporClient({ network: vaporLocalnet, account: owner, contracts })
    ;({ appId } = await ownerClient.apps.register({ recipient: owner.address, metadataUri: 'ipfs://react-e2e', referrerBps: 0 }))
    const hash = await ownerWallet.deployContract({ abi: checkoutArtifact.abi, bytecode: checkoutArtifact.bytecode.object, args: [appId, merchant] })
    checkout = (await pub.waitForTransactionReceipt({ hash, confirmations: 2 })).contractAddress!
    await ownerClient.apps.acceptContract(appId, checkout)
    await bondApp(ownerClient, appId) // base sponsorship is bought with bonded capital
  })

  let qc = new QueryClient()
  afterEach(async () => {
    // stop polling and let in-flight reads settle before the DOM is torn down
    await qc.cancelQueries()
    qc.clear()
    await new Promise((r) => setTimeout(r, 300))
    qc = new QueryClient()
  })
  const wrap = (account: PrivateKeyAccount | undefined) =>
    function Wrapper({ children }: { children: ReactNode }) {
      return (
        <VaporProvider network={vaporLocalnet} contracts={contracts} appId={appId} queryClient={qc} {...(account ? { account } : {})}>
          {children}
        </VaporProvider>
      )
    }

  it('embedded wallet with zero gas checks out through useCheckout', async () => {
    // 1. a brand-new embedded wallet
    const w = renderHook(() => useEmbeddedWallet({ store: indexedDbKeyStore(new IDBFactory()) }))
    await waitFor(() => expect(w.result.current.status).toBe('none'))
    await act(async () => void (await w.result.current.create()))
    const user = w.result.current.account!
    const fund = await ownerWallet.writeContract({ address: dep.usdc!, abi: erc20Abi, functionName: 'transfer', args: [user.address, 5_000_000n] })
    await pub.waitForTransactionReceipt({ hash: fund, confirmations: 2 })
    expect(await pub.getBalance({ address: user.address })).toBe(0n)

    // 2. read hooks
    const h = renderHook(
      () => ({
        quota: useSponsorQuota(),
        balance: useUnifiedBalance({ symbol: 'USDC' }),
        fee: useFeeQuote({ token: dep.usdc!, amount: 1_000_000n }),
        checkout: useCheckout(),
      }),
      { wrapper: wrap(user) },
    )
    await waitFor(() => expect(h.result.current.quota.data?.gas).toBeGreaterThan(0n), { timeout: 20_000 })
    await waitFor(() => expect(h.result.current.balance.data?.local).toBe(5_000_000n), { timeout: 20_000 })
    await waitFor(() => expect(h.result.current.fee.data).toBeDefined(), { timeout: 20_000 })
    expect(h.result.current.fee.data!.fee + h.result.current.fee.data!.net).toBe(1_000_000n)
    expect(h.result.current.fee.data!.sponsored).toBe(true)

    // 3. one gasless signature: approveApp + payOrder
    await act(async () => {
      const r = await h.result.current.checkout.mutateAsync({ checkout, orderId: keccak256(stringToHex(`react-${Date.now()}`)), token: dep.usdc!, amount: 1_000_000n })
      expect(r.kind).toBe('userop')
    })
    // the mutation invalidates balances: the hook refreshes on its own
    await waitFor(() => expect(h.result.current.balance.data?.local).toBe(4_000_000n), { timeout: 30_000 })
    expect(await pub.getBalance({ address: user.address })).toBe(0n)
    expect(await pub.readContract({ address: dep.usdc!, abi: erc20Abi, functionName: 'balanceOf', args: [merchant] })).toBe(1_000_000n - h.result.current.fee.data!.fee)
    h.unmount() // stop polling before the DOM is torn down
    w.unmount()
  })

  it('predicts and launches a token', async () => {
    const params = { name: 'React Gem', symbol: 'RGEM', initialSupply: 10n ** 24n, owner: owner.address, salt: `react-${Date.now()}` }
    const h = renderHook(() => ({ predicted: usePredictedTokenAddress(params), launch: useLaunchToken() }), { wrapper: wrap(owner) })
    await waitFor(() => expect(h.result.current.predicted.data).toBeDefined(), { timeout: 20_000 })
    let token: Address | undefined
    await act(async () => {
      token = (await h.result.current.launch.mutateAsync(params)).token
    })
    expect(token).toBe(h.result.current.predicted.data)
    h.unmount()
  })
})
