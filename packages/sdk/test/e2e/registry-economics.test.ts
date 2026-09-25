// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Provenance: VAPOR-6eabb1be532bdef4
//
// Registry economics, measured on a LIVE localnet + Alto + sponsor service.
// Puts the on-registry path next to every way of going around the registry
// and records what each one costs the user and earns the developer:
//
//   registered   app registered, contract attributed, zero-gas user pays with
//                one sponsored UserOp (gas paid from the app's quota)
//   bypass-plain no registry, no Settle: plain ERC-20 transfer; the user must
//                hold gas credits first
//   bypass-rails no registry but using Settle.pay from an EOA: the 50% app
//                share is forfeited to the treasury
//   sponsor      an unregistered / unaccepted contract trying to ride the
//                protocol sponsor
//   fake apps    registration alone buys no sponsored gas; base quota is
//                bought with capital bonded behind the app
//   sybil-proof  the same capital split over fake apps buys exactly the same
//                total quota; unbonding stops it at once
//   farm bound   paying fees to farm sponsored gas loses money, from the live
//                params, even at the sponsor's max fee cap
//
// Gas is priced as infrastructure cost (credit_price), so every unsponsored
// transaction costs real money; registered, verified apps make it $0 for
// their users.
//
// Assertions encode the chain's actual behaviour; the measured numbers are
// written to $VAPOR_E2E_REPORT (JSON) for docs/benchmarks/mainnet-sim.md.
// Run: VAPOR_E2E_OWNER_KEY=0x... VAPOR_E2E_REPORT=/tmp/econ.json pnpm test:e2e
import { readFileSync, writeFileSync } from 'node:fs'
import {
  createPublicClient,
  createWalletClient,
  erc20Abi,
  http,
  keccak256,
  parseAbi,
  parseEventLogs,
  stringToHex,
  type Address,
  type Hash,
  type Hex,
} from 'viem'
import { entryPoint08Abi } from 'viem/account-abstraction'
import { generatePrivateKey, privateKeyToAccount } from 'viem/accounts'
import { afterAll, beforeAll, describe, expect, it } from 'vitest'
import { createVaporClient, settleCalls, vaporLocalnet } from '../../src/index.js'
import { bondApp, TEST_BOND } from './localnet.js'

const root = new URL('../../../../', import.meta.url).pathname
const dep = JSON.parse(readFileSync(`${root}contracts/deployments/779700.json`, 'utf8')) as Record<string, Address>
const checkoutArtifact = JSON.parse(readFileSync(`${root}contracts/out/VaporCheckout.sol/VaporCheckout.json`, 'utf8'))
const ownerKey = process.env.VAPOR_E2E_OWNER_KEY as Hex
const reportPath = process.env.VAPOR_E2E_REPORT
const chain = vaporLocalnet.chain
const rest = vaporLocalnet.restUrl
const pub = createPublicClient({ chain, transport: http() })
const usdc = dep.usdc!
const contracts = { tokenFactory: dep.tokenFactory!, usdc }
const depositAbi = parseAbi(['function getDeposit() view returns (uint256)'])
const PAYMENT = 10_000_000n // 10 USDC
// sponsor default VAPOR_MAX_FEE_PER_GAS (services/sponsor/cmd/sponsor/main.go)
const SPONSOR_MAX_FEE_PER_GAS = 4_000_000_000n
// an unsponsored transaction must cost real money, not dust
const REAL_MONEY_USD = 0.0005

const getJson = async <T>(path: string): Promise<T> => (await fetch(`${rest}${path}`)).json() as Promise<T>
const pool = async (name: string): Promise<bigint> => {
  const r = await getJson<{ pools: { pool: string; denom: string; amount: string }[] }>('/vaporchain/settle/v1/pools')
  return BigInt(r.pools.find((p) => p.pool === name && p.denom === 'uusdc')?.amount ?? '0')
}
const usdcBal = (a: Address) => pub.readContract({ address: usdc, abi: erc20Abi, functionName: 'balanceOf', args: [a] })
const gasCost = async (hash: Hash) => {
  const r = await pub.waitForTransactionReceipt({ hash, confirmations: 2 })
  expect(r.status).toBe('success')
  return { gasUsed: r.gasUsed, cost: r.gasUsed * r.effectiveGasPrice, price: r.effectiveGasPrice }
}

describe.skipIf(!ownerKey)('registry economics (live localnet)', () => {
  const owner = privateKeyToAccount(ownerKey)
  const ownerWallet = createWalletClient({ account: owner, chain, transport: http() })
  const ownerClient = createVaporClient({ network: vaporLocalnet, account: owner, contracts })
  const report: Record<string, unknown> = {}
  let creditPrice: bigint // acredit minted per uusdc by buyCredits
  let appId: bigint
  let checkout: Address
  let merchant: Address
  // acredit -> USDC at the protocol credit price (float: reporting only)
  const toUsd = (acredit: bigint) => Number(acredit) / Number(creditPrice) / 1e6

  beforeAll(async () => {
    const p = await getJson<{ params: { assets: { denom: string; credit_price: string }[] } }>('/vaporchain/settle/v1/params')
    creditPrice = BigInt(p.params.assets.find((a) => a.denom === 'uusdc')!.credit_price)
    report.creditPriceAcreditPerUusdc = creditPrice.toString()
    report.usdPerCredit = toUsd(10n ** 18n)
  })

  afterAll(() => {
    const json = JSON.stringify(report, (_, v) => (typeof v === 'bigint' ? v.toString() : v), 2)
    if (reportPath) writeFileSync(reportPath, json)
    console.log(json)
  })

  it('registered: registration burns the anti-spam fee', async () => {
    const { params } = await getJson<{ params: { registration_fee: { amount: string } } }>('/vaporchain/apps/v1/params')
    const fee = BigInt(params.registration_fee.amount)
    const before = await pub.getBalance({ address: owner.address })
    const reg = await ownerClient.apps.register({ recipient: owner.address, metadataUri: 'ipfs://econ-shop', referrerBps: 0 })
    appId = reg.appId
    const g = await gasCost(reg.hash)
    const after = await pub.getBalance({ address: owner.address })
    expect(before - after - g.cost).toBe(fee) // exactly the fee left the owner, on top of gas
    report.registration = { feeAcredit: fee, feeUsd: toUsd(fee), gasUsed: g.gasUsed, gasAcredit: g.cost, gasUsd: toUsd(g.cost) }

    merchant = privateKeyToAccount(generatePrivateKey()).address
    const hash = await ownerWallet.deployContract({ abi: checkoutArtifact.abi, bytecode: checkoutArtifact.bytecode.object, args: [appId, merchant] })
    checkout = (await pub.waitForTransactionReceipt({ hash, confirmations: 2 })).contractAddress!
    await gasCost((await ownerClient.apps.acceptContract(appId, checkout)).hash as Hash)
    // base sponsorship is bought with bonded capital (no identity checks)
    report.bondedBaseGas = await bondApp(ownerClient, appId)
  })

  it('registered: a zero-gas user pays 10 USDC with one sponsored UserOp; the dev earns 50% of the fee', async () => {
    const user = privateKeyToAccount(generatePrivateKey())
    await gasCost(await ownerWallet.writeContract({ address: usdc, abi: erc20Abi, functionName: 'transfer', args: [user.address, 50_000_000n] }))
    expect(await pub.getBalance({ address: user.address })).toBe(0n)

    const treasury0 = await pool('treasury')
    const deposit0 = await pub.readContract({ address: dep.verifyingPaymaster!, abi: depositAbi, functionName: 'getDeposit' })
    const userClient = createVaporClient({ network: vaporLocalnet, account: user, appId, contracts })
    const res = await userClient.checkout({ checkout, orderId: keccak256(stringToHex(`econ-${Date.now()}`)), token: usdc, amount: PAYMENT })
    expect(res.kind).toBe('userop')
    const rcpt = await pub.waitForTransactionReceipt({ hash: (res as { txHash: Hash }).txHash, confirmations: 2 })
    const [ev] = parseEventLogs({ abi: entryPoint08Abi, logs: rcpt.logs, eventName: 'UserOperationEvent' })
    expect(ev?.args.success).toBe(true)
    expect(ev!.args.paymaster.toLowerCase()).toBe(dep.verifyingPaymaster!.toLowerCase())
    const deposit1 = await pub.readContract({ address: dep.verifyingPaymaster!, abi: depositAbi, functionName: 'getDeposit' })

    const fee = PAYMENT / 100n
    expect(await pub.getBalance({ address: user.address })).toBe(0n) // the user spent no gas at all
    expect(await usdcBal(merchant)).toBe(PAYMENT - fee)
    expect(await ownerClient.apps.claimable(appId, usdc)).toBe(fee / 2n)
    const treasuryDelta = (await pool('treasury')) - treasury0
    expect(treasuryDelta).toBe((fee * 20n) / 100n)
    expect(deposit0 - deposit1).toBe(ev!.args.actualGasCost) // the protocol paymaster paid exactly the op's gas

    report.registered = {
      paymentUusdc: PAYMENT, feeUusdc: fee, merchantNetUusdc: PAYMENT - fee, devShareUusdc: fee / 2n, treasuryShareUusdc: treasuryDelta,
      userGasPaidAcredit: 0n,
      sponsoredOpGasUsed: ev!.args.actualGasUsed, sponsoredOpCostAcredit: ev!.args.actualGasCost, sponsoredOpCostUsd: toUsd(ev!.args.actualGasCost),
      note: 'first op also carries the EIP-7702 authorization for the new account',
    }
  })

  it('bypass-plain: a zero-balance user cannot transact at all, not even to buy credits', async () => {
    const user = privateKeyToAccount(generatePrivateKey())
    await gasCost(await ownerWallet.writeContract({ address: usdc, abi: erc20Abi, functionName: 'transfer', args: [user.address, 50_000_000n] }))
    const w = createWalletClient({ account: user, chain, transport: http() })
    const transfer = w.writeContract({ address: usdc, abi: erc20Abi, functionName: 'transfer', args: [merchant, PAYMENT], gas: 100_000n })
    await expect(transfer).rejects.toThrow(/insufficient|exceeds the balance/i)
    const buy = settleCalls.buyCredits(usdc, 1_000_000n)
    await expect(w.sendTransaction({ to: buy.to, data: buy.data, gas: 200_000n })).rejects.toThrow(/insufficient|exceeds the balance/i)
    report.bypassOnboardingWall = {
      usdcHeldUusdc: 50_000_000n, creditsHeld: 0n, plainTransfer: 'rejected: insufficient funds for gas', buyCredits: 'rejected: insufficient funds for gas',
      consequence: 'off-registry a new user needs a third party to hand them gas credits before their first transaction',
    }
  })

  it('bypass-plain: after a third party funds gas, a plain transfer pays no protocol fee and earns the dev nothing', async () => {
    const user = privateKeyToAccount(generatePrivateKey())
    const payee = privateKeyToAccount(generatePrivateKey()).address
    await gasCost(await ownerWallet.writeContract({ address: usdc, abi: erc20Abi, functionName: 'transfer', args: [user.address, 50_000_000n] }))
    // credits move peer-to-peer as EVM value even though bank sends are disabled
    const grant = 10n ** 16n // 0.01 CREDIT
    const fund = await gasCost(await ownerWallet.sendTransaction({ to: user.address, value: grant }))
    expect(await pub.getBalance({ address: user.address })).toBe(grant)

    const w = createWalletClient({ account: user, chain, transport: http() })
    const t = await gasCost(await w.writeContract({ address: usdc, abi: erc20Abi, functionName: 'transfer', args: [payee, PAYMENT] }))
    expect(await usdcBal(payee)).toBe(PAYMENT) // 0% protocol fee
    expect(await pub.getBalance({ address: user.address })).toBe(grant - t.cost)
    expect(toUsd(t.cost)).toBeGreaterThan(REAL_MONEY_USD) // the chain's compute is paid for
    report.bypassPlain = {
      thirdPartyFundingGasAcredit: fund.cost, creditsGrantedAcredit: grant,
      transferGasUsed: t.gasUsed, effectiveGasPriceWei: t.price, transferCostAcredit: t.cost, transferCostUsd: toUsd(t.cost),
      protocolFeeUusdc: 0n, payeeReceivedUusdc: PAYMENT, devRevenueUusdc: 0n,
      transfersPerGrant: Number(grant / t.cost),
    }
  })

  it('bypass-rails: Settle.pay from an EOA with no app forfeits the 50% app share to the treasury', async () => {
    const user = privateKeyToAccount(generatePrivateKey())
    const payee = privateKeyToAccount(generatePrivateKey()).address
    await gasCost(await ownerWallet.writeContract({ address: usdc, abi: erc20Abi, functionName: 'transfer', args: [user.address, 50_000_000n] }))
    await gasCost(await ownerWallet.sendTransaction({ to: user.address, value: 10n ** 16n }))
    const treasury0 = await pool('treasury')
    const c = settleCalls.pay(usdc, PAYMENT, payee)
    const w = createWalletClient({ account: user, chain, transport: http() })
    const g = await gasCost(await w.sendTransaction({ to: c.to, data: c.data }))
    const fee = PAYMENT / 100n
    const treasuryDelta = (await pool('treasury')) - treasury0
    expect(await usdcBal(payee)).toBe(PAYMENT - fee)
    expect(treasuryDelta).toBe((fee * 70n) / 100n) // 20% treasury + the 50% nobody claimed
    expect(toUsd(g.cost)).toBeGreaterThan(REAL_MONEY_USD)
    report.bypassRails = {
      feeUusdc: fee, payeeNetUusdc: PAYMENT - fee, treasuryShareUusdc: treasuryDelta, appShareForfeitedUusdc: fee / 2n, devRevenueUusdc: 0n,
      userGasUsed: g.gasUsed, userGasAcredit: g.cost, userGasUsd: toUsd(g.cost),
    }
  })

  it('sponsor: an unregistered contract cannot ride the protocol sponsor', async () => {
    // a checkout that self-claims app `appId` but was never accepted by the owner
    const hash = await ownerWallet.deployContract({ abi: checkoutArtifact.abi, bytecode: checkoutArtifact.bytecode.object, args: [appId, merchant] })
    const rogue = (await pub.waitForTransactionReceipt({ hash, confirmations: 2 })).contractAddress!
    const user = privateKeyToAccount(generatePrivateKey())
    const userClient = createVaporClient({ network: vaporLocalnet, account: user, appId, contracts })
    const refusals: string[] = []
    const expectDenied = async (p: Promise<unknown>) => {
      const e = await p.then(() => null, (err: Error) => err)
      expect(e).not.toBeNull()
      expect(e!.message).toMatch(/sponsorship denied|not a contract of app|sponsoring app/)
      const i = e!.message.indexOf('sponsorship denied')
      refusals.push((i >= 0 ? e!.message.slice(i) : e!.message).split('\n')[0]!.slice(0, 140))
    }
    // calls into a contract that is not (yet) attributed to the app
    await expectDenied(userClient.pay({ calls: [{ to: rogue, data: '0x' }], sponsor: 'app' }))
    // a plain token transfer (not an app contract)
    await expectDenied(userClient.pay({ calls: [{ to: usdc, data: '0xa9059cbb000000000000000000000000000000000000000000000000000000000000dead0000000000000000000000000000000000000000000000000000000000000001' }], sponsor: 'app' }))
    // spending this app's quota to approve a different app
    await expectDenied(userClient.pay({ calls: [settleCalls.approveApp(appId + 1000n, usdc, 1n)], sponsor: 'app' }))
    expect(await pub.getBalance({ address: user.address })).toBe(0n)
    report.sponsorRefusals = refusals
  })

  it('fake apps: registration alone buys no sponsored gas', async () => {
    const { params } = await getJson<{ params: { registration_fee: { amount: string }; gas_per_bonded_unit: string; unbonding_blocks: string; epoch_length_blocks: string } }>('/vaporchain/apps/v1/params')
    const ids: bigint[] = []
    for (let i = 0; i < 3; i++) ids.push((await ownerClient.apps.register({ recipient: owner.address, metadataUri: `ipfs://fake-${i}`, referrerBps: 0 })).appId)
    await new Promise((r) => setTimeout(r, 3000))
    const quotas = await Promise.all(ids.map((id) => ownerClient.sponsor.quota(id)))
    for (const q of quotas) expect(q.gas).toBe(0n) // nothing for the protocol paymaster to pay
    // a fresh fake app cannot ride the sponsor either: it has no quota to spend
    const user = privateKeyToAccount(generatePrivateKey())
    const fakeClient = createVaporClient({ network: vaporLocalnet, account: user, appId: ids[0]!, contracts })
    await expect(fakeClient.pay({ calls: [settleCalls.approveApp(ids[0]!, usdc, 1n)], sponsor: 'app' })).rejects.toThrow(/sponsorship denied|quota/)
    // the bonded app's base is exactly bond x rate
    const rate = BigInt(params.gas_per_bonded_unit)
    const info = await ownerClient.apps.bondInfo(appId)
    expect(info.bonded).toBe(TEST_BOND)
    expect(info.baseGas).toBe((TEST_BOND * rate) / 1_000_000n)
    // what a bond is worth: gas per USDC per epoch at the floor price, per year on 1-day epochs
    const usdPerBondedUsdcPerEpoch = toUsd(rate * 1_000_000_000n)
    report.fakeApps = {
      appsRegistered: ids.length, registrationFeeAcredit: BigInt(params.registration_fee.amount), registrationFeeUsd: toUsd(BigInt(params.registration_fee.amount)),
      quotaPerFakeAppGas: quotas.map((q) => q.gas),
      gasPerBondedUsdcPerEpoch: rate, unbondingBlocks: Number(params.unbonding_blocks), epochLengthBlocks: Number(params.epoch_length_blocks),
      bondYieldInGasPerYear1DayEpochs: usdPerBondedUsdcPerEpoch * 365,
    }
  })

  it('sybil-proof: the same capital split across fake apps buys exactly the same quota', async () => {
    const reg = async (tag: string) => (await ownerClient.apps.register({ recipient: owner.address, metadataUri: `ipfs://${tag}`, referrerBps: 0 })).appId
    const [one, halfA, halfB] = [await reg('one'), await reg('half-a'), await reg('half-b')]
    await bondApp(ownerClient, one, TEST_BOND)
    await bondApp(ownerClient, halfA, TEST_BOND / 2n)
    await bondApp(ownerClient, halfB, TEST_BOND / 2n)
    const [q1, qa, qb] = await Promise.all([one, halfA, halfB].map((id) => ownerClient.sponsor.quota(id)))
    expect(qa!.gas + qb!.gas).toBe(q1!.gas) // linear in capital: splitting gains nothing
    // unbonding stops counting at once; the capital stays locked until release
    const before = await usdcBal(owner.address)
    await gasCost((await ownerClient.apps.unbond(halfA, TEST_BOND / 2n)).hash as Hash)
    expect((await ownerClient.sponsor.quota(halfA)).gas).toBe(0n)
    const pending = (await ownerClient.apps.bondInfo(halfA)).unbonding
    expect(pending).toHaveLength(1)
    expect(await usdcBal(owner.address)).toBe(before) // not returned yet
    const head = await pub.getBlockNumber()
    report.sybilProof = {
      oneAppQuota: q1!.gas, twoFakeAppsQuota: qa!.gas + qb!.gas,
      unbondedQuota: 0n, releaseHeight: pending[0]!.releaseHeight, lockedForBlocks: pending[0]!.releaseHeight - head,
    }
  })

  it('farm bound: paying fees to farm sponsored gas always loses money', async () => {
    const { params } = await getJson<{ params: { split: { app_bps: number }; assets: { denom: string; quota_weight: string }[] } }>('/vaporchain/settle/v1/params')
    const qw = BigInt(params.assets.find((a) => a.denom === 'uusdc')!.quota_weight)
    const appShare = Number(params.split.app_bps) / 10_000
    // quota earned per uusdc of fee is `quota_weight` gas; spent at <= the sponsor's max fee
    const rebateAtCap = Number(qw * SPONSOR_MAX_FEE_PER_GAS) / Number(creditPrice)
    const rebateAtFloor = Number(qw * 1_000_000_000n) / Number(creditPrice)
    // a self-dealing app gets its own app share back plus the sponsored gas it earned
    expect(appShare + rebateAtCap).toBeLessThan(1)
    report.farmBound = { quotaWeight: qw, appShare, rebateAtFloor, rebateAtSponsorCap: rebateAtCap, worstCaseReturnPerFeeDollar: appShare + rebateAtCap }
  })
})
