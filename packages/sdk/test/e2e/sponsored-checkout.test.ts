// Copyright (c) 2026 VaporChain / muthu2201. Licensed under the Apache License, Version 2.0 (see LICENSE).
// Provenance: VAPOR-6eabb1be532bdef4
//
// Golden-path E2E against a LIVE localnet + Alto bundler + sponsor service:
// a brand-new EOA with ZERO gas credits upgrades itself with EIP-7702 and pays
// a checkout order (approveApp + payOrder in one UserOp), gas sponsored by the
// app's earned quota. Run: VAPOR_E2E_OWNER_KEY=0x... pnpm test:e2e
import { readFileSync } from 'node:fs'
import { createPublicClient, createWalletClient, erc20Abi, http, keccak256, parseEventLogs, stringToHex, type Address, type Hex } from 'viem'
import { generatePrivateKey, privateKeyToAccount } from 'viem/accounts'
import { beforeAll, describe, expect, it } from 'vitest'
import { createVaporClient, isDelegated, settleCalls, SIMPLE_7702_IMPLEMENTATION, vaporLocalnet, verifyingPaymasterAbi, VaporError } from '../../src/index.js'

const root = new URL('../../../../', import.meta.url).pathname
const dep = JSON.parse(readFileSync(`${root}contracts/deployments/779700.json`, 'utf8')) as Record<string, Address>
const checkoutArtifact = JSON.parse(readFileSync(`${root}contracts/out/VaporCheckout.sol/VaporCheckout.json`, 'utf8'))
const ownerKey = process.env.VAPOR_E2E_OWNER_KEY as Hex
const chain = vaporLocalnet.chain
const pub = createPublicClient({ chain, transport: http() })

describe.skipIf(!ownerKey)('sponsored 7702 checkout (live localnet)', () => {
  const owner = privateKeyToAccount(ownerKey)
  const ownerWallet = createWalletClient({ account: owner, chain, transport: http() })
  const ownerClient = createVaporClient({ network: vaporLocalnet, account: owner, contracts: { tokenFactory: dep.tokenFactory!, usdc: dep.usdc! } })
  const merchant = privateKeyToAccount(generatePrivateKey()).address
  let appId: bigint
  let checkout: Address

  beforeAll(async () => {
    ;({ appId } = await ownerClient.apps.register({ recipient: owner.address, metadataUri: 'ipfs://e2e-shop', referrerBps: 0 }))
    const hash = await ownerWallet.deployContract({ abi: checkoutArtifact.abi, bytecode: checkoutArtifact.bytecode.object, args: [appId, merchant] })
    checkout = (await pub.waitForTransactionReceipt({ hash, confirmations: 2 })).contractAddress!
    await ownerClient.apps.acceptContract(appId, checkout)
  })

  it('a zero-gas user pays an order with one sponsored 7702 UserOp', async () => {
    const userKey = generatePrivateKey()
    const user = privateKeyToAccount(userKey)
    // the user holds USDC but NOT A SINGLE gas credit
    const fund = await ownerWallet.writeContract({ address: dep.usdc!, abi: erc20Abi, functionName: 'transfer', args: [user.address, 50_000_000n] })
    await pub.waitForTransactionReceipt({ hash: fund, confirmations: 2 })
    expect(await pub.getBalance({ address: user.address })).toBe(0n)

    const userClient = createVaporClient({ network: vaporLocalnet, account: user, appId, contracts: { tokenFactory: dep.tokenFactory!, usdc: dep.usdc! } })
    const orderId = keccak256(stringToHex(`order-${Date.now()}`))
    const res = await userClient.checkout({ checkout, orderId, token: dep.usdc!, amount: 10_000_000n })
    expect(res.kind).toBe('userop')

    const rcpt = await pub.getTransactionReceipt({ hash: (res as { txHash: Hex }).txHash })
    const sponsored = parseEventLogs({ abi: verifyingPaymasterAbi, logs: rcpt.logs, eventName: 'Sponsored' })
    expect(sponsored).toHaveLength(1)
    expect(sponsored[0]!.args.appId).toBe(appId)
    expect(sponsored[0]!.args.sender.toLowerCase()).toBe(user.address.toLowerCase())

    expect(await pub.readContract({ address: dep.usdc!, abi: erc20Abi, functionName: 'balanceOf', args: [merchant] })).toBe(9_900_000n)
    expect(await pub.getBalance({ address: user.address })).toBe(0n) // still gasless
    expect(await isDelegated(pub, user.address)).toBe(true)
    expect((await pub.getCode({ address: user.address }))?.toLowerCase()).toBe(`0xef0100${SIMPLE_7702_IMPLEMENTATION.slice(2).toLowerCase()}`)
    expect(await ownerClient.apps.claimable(appId, dep.usdc!)).toBe(50_000n)

    // second purchase: account already delegated, no authorization needed
    const res2 = await userClient.checkout({ checkout, orderId: keccak256(stringToHex(`order2-${Date.now()}`)), token: dep.usdc!, amount: 2_000_000n })
    expect(res2.kind).toBe('userop')
    expect(await pub.readContract({ address: dep.usdc!, abi: erc20Abi, functionName: 'balanceOf', args: [merchant] })).toBe(9_900_000n + 1_980_000n)
  })

  it('refuses to sponsor calls outside the app (quota cannot be spent elsewhere)', async () => {
    const user = privateKeyToAccount(generatePrivateKey())
    const userClient = createVaporClient({ network: vaporLocalnet, account: user, appId, contracts: { tokenFactory: dep.tokenFactory!, usdc: dep.usdc! } })
    // a plain USDC transfer is not a call into the app's contracts
    const call = { to: dep.usdc!, data: '0xa9059cbb000000000000000000000000000000000000000000000000000000000000dead0000000000000000000000000000000000000000000000000000000000000001' as Hex }
    await expect(userClient.pay({ calls: [call], sponsor: 'app' })).rejects.toThrow(/not a contract of app|sponsorship denied/)
    // approveApp for ANOTHER app id is also refused
    await expect(userClient.pay({ calls: [settleCalls.approveApp(appId + 999n, dep.usdc!, 1n)], sponsor: 'app' })).rejects.toThrow(/sponsoring app|sponsorship denied/)
  })

  it('reports the app quota', async () => {
    const q = await ownerClient.sponsor.quota(appId)
    expect(q.gas).toBeGreaterThan(0n)
  })
})

void VaporError
