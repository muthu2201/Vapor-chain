// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Proprietary. See LICENSE. Provenance: VAPOR-6eabb1be532bdef4

import {
  createPublicClient,
  createWalletClient,
  encodeFunctionData,
  erc20Abi,
  http,
  type Address,
  type Hash,
  type Hex,
  type PublicClient,
  type WalletClient,
} from 'viem'
import { vaporCheckoutAbi } from './abi/index.js'
import { sponsoredSender, toVaporAccount, type Call } from './aa.js'
import { appsRest, registerApp, type AppRecord, type Quota } from './apps.js'
import { skipClient, type BridgeStage, type RouteRequest } from './bridge.js'
import type { VaporNetwork } from './chains.js'
import { VaporError } from './errors.js'
import { appOf, claimable, quoteFee, settleCalls } from './settle.js'
import { launchToken, predictTokenAddress, type TokenLaunchParams } from './tokens.js'
import { SETTLE_ADDRESS } from './constants.js'
import type { PrivateKeyAccount } from 'viem/accounts'
import type { SkipRoute } from './bridge.js'

export interface VaporClientConfig {
  network: VaporNetwork
  /** Signs everything; for browsers pass a viem LocalAccount from your embedded wallet or use wagmi + the helpers directly. */
  account?: PrivateKeyAccount
  /** App that sponsors this user's gas (its earned quota). */
  appId?: bigint
  /** Protocol deployments (from contracts/deployments/<chainId>.json). */
  contracts: { tokenFactory: Address; usdc: Address }
  /** Skip:Go API (apiUrl defaults to https://api.skip.build; point it at your proxy to keep the API key server-side). */
  skip?: { apiUrl?: string; apiKey?: string; affiliateAddressByChain?: Record<string, string> }
}

export type TxHandle = { kind: 'tx'; hash: Hash } | { kind: 'userop'; hash: Hex; txHash: Hash }

/**
 * The blueprint's client surface:
 *   estimateFee / send / pay / bridge / sponsor / apps / tokens / status
 */
export interface VaporClient {
  publicClient: PublicClient
  network: VaporNetwork
  estimateFee(i: { token: Address; amount: bigint; appId?: bigint }): Promise<{ fee: bigint; net: bigint; sponsored: boolean }>
  send(i: { token: Address | 'CREDIT'; to: Address; amount: bigint }): Promise<TxHandle>
  pay(i: { calls: Call[]; sponsor?: 'app' | 'user' }): Promise<TxHandle>
  checkout(i: { checkout: Address; orderId: Hex; token: Address; amount: bigint; referrer?: Address; approve?: bigint }): Promise<TxHandle>
  bridge(i: RouteRequest): Promise<SkipRoute>
  status(h: { txHash: string; chainId: string }): AsyncIterable<BridgeStage>
  sponsor: {
    quota(appId: bigint): Promise<Quota>
    buyCredits(usdcAmount: bigint): Promise<TxHandle>
  }
  apps: {
    get(appId: bigint): Promise<AppRecord>
    register(m: { recipient: Address; metadataUri: string; referrerBps?: number }): Promise<{ appId: bigint; hash: Hash }>
    acceptContract(appId: bigint, contract: Address): Promise<TxHandle>
    claimable(appId: bigint, token: Address): Promise<bigint>
    claimRevenue(appId: bigint, token: Address): Promise<TxHandle>
  }
  tokens: {
    predict(creator: Address, p: TokenLaunchParams): Promise<Address>
    launch(p: TokenLaunchParams): Promise<{ token: Address; hash: Hash }>
  }
}

export function createVaporClient(cfg: VaporClientConfig): VaporClient {
  const { network } = cfg
  const publicClient: PublicClient = createPublicClient({ chain: network.chain, transport: http() }) as PublicClient
  const wallet: WalletClient | undefined = cfg.account
    ? createWalletClient({ account: cfg.account, chain: network.chain, transport: http() })
    : undefined
  const rest = appsRest(network.restUrl)
  const skip = skipClient(cfg.skip ?? {})

  const needAccount = () => {
    if (!cfg.account || !wallet) throw new VaporError('this operation needs `account`', 'CONFIG')
    return { account: cfg.account, wallet }
  }

  let senderPromise: ReturnType<typeof makeSender> | undefined
  async function makeSender() {
    const { account } = needAccount()
    if (cfg.appId === undefined) throw new VaporError('sponsored operations need `appId`', 'CONFIG')
    const smart = await toVaporAccount(publicClient, account)
    return sponsoredSender({
      client: publicClient, chain: network.chain, account: smart, owner: account, appId: cfg.appId,
      bundlerUrl: network.bundlerUrl, sponsorUrl: network.sponsorUrl,
    })
  }
  const sender = () => (senderPromise ??= makeSender())

  return {
    publicClient,
    network,

    /** Exact fee the chain will charge for a Settle payment. */
    async estimateFee(i: { token: Address; amount: bigint; appId?: bigint }): Promise<{ fee: bigint; net: bigint; sponsored: boolean }> {
      const { fee, net } = await quoteFee(publicClient, i.token, i.amount)
      let sponsored = false
      const appId = i.appId ?? cfg.appId
      if (appId !== undefined) {
        const q = await rest.quota(appId).catch(() => null)
        sponsored = !!q && q.gas > 0n
      }
      return { fee, net, sponsored }
    },

    /** Plain transfer (0 protocol fee). Native CREDIT or any ERC-20. */
    async send(i: { token: Address | 'CREDIT'; to: Address; amount: bigint }): Promise<TxHandle> {
      const { account, wallet: w } = needAccount()
      const hash =
        i.token === 'CREDIT'
          ? await w.sendTransaction({ account, chain: network.chain, to: i.to, value: i.amount })
          : await w.writeContract({ account, chain: network.chain, address: i.token, abi: erc20Abi, functionName: 'transfer', args: [i.to, i.amount] })
      return { kind: 'tx', hash }
    },

    /**
     * Run calls. sponsor='app' -> gasless UserOp paid by the app's quota
     * (EIP-7702 upgrade on first use); sponsor='user' -> normal transaction.
     */
    async pay(i: { calls: Call[]; sponsor?: 'app' | 'user' }): Promise<TxHandle> {
      if ((i.sponsor ?? 'app') === 'app') {
        const r = await (await sender()).send(i.calls)
        return { kind: 'userop', hash: r.userOpHash, txHash: r.receipt.receipt.transactionHash }
      }
      const { account, wallet: w } = needAccount()
      if (i.calls.length !== 1) throw new VaporError('multi-call without sponsorship: use a 7702 batch via sponsor="app"', 'CONFIG')
      const c = i.calls[0]!
      const hash = await w.sendTransaction({ account, chain: network.chain, to: c.to, data: c.data ?? '0x', value: c.value ?? 0n })
      return { kind: 'tx', hash }
    },

    /** One-signature checkout: approve the app + pay the order, gasless. */
    async checkout(i: { checkout: Address; orderId: Hex; token: Address; amount: bigint; referrer?: Address; approve?: bigint }): Promise<TxHandle> {
      const { appId } = await appOf(publicClient, i.checkout)
      if (appId === 0n) throw new VaporError(`checkout ${i.checkout} is not attributed to an app yet`, 'NOT_REGISTERED')
      const calls: Call[] = [
        settleCalls.approveApp(appId, i.token, i.approve ?? i.amount),
        {
          to: i.checkout,
          data: encodeFunctionData({ abi: vaporCheckoutAbi, functionName: 'payOrder', args: [i.orderId, i.token, i.amount, i.referrer ?? '0x0000000000000000000000000000000000000000'] }),
        },
      ]
      return this.pay({ calls, sponsor: 'app' })
    },

    /** Bring assets in from other chains via Skip:Go (returns the route to sign on the source chain). */
    async bridge(i: RouteRequest) {
      return skip.route(i)
    },

    status(h: { txHash: string; chainId: string }): AsyncIterable<BridgeStage> {
      return skip.watch(h.txHash, h.chainId)
    },

    sponsor: {
      quota: (appId: bigint): Promise<Quota> => rest.quota(appId),
      /** Buy gas credits with USDC for a paymaster/bundler float (fixed protocol price). */
      async buyCredits(usdcAmount: bigint): Promise<TxHandle> {
        const { account, wallet: w } = needAccount()
        const c = settleCalls.buyCredits(cfg.contracts.usdc, usdcAmount)
        return { kind: 'tx', hash: await w.sendTransaction({ account, chain: network.chain, to: c.to, data: c.data }) }
      },
    },

    apps: {
      get: (appId: bigint): Promise<AppRecord> => rest.get(appId),
      async register(m: { recipient: Address; metadataUri: string; referrerBps?: number }): Promise<{ appId: bigint; hash: Hash }> {
        const { wallet: w } = needAccount()
        return registerApp(w, publicClient, m)
      },
      async acceptContract(appId: bigint, contract: Address): Promise<TxHandle> {
        const { account, wallet: w } = needAccount()
        const c = settleCalls.acceptContractClaim(appId, contract)
        const hash = await w.sendTransaction({ account, chain: network.chain, to: c.to, data: c.data })
        const r = await publicClient.waitForTransactionReceipt({ hash, confirmations: 2 })
        if (r.status !== 'success') throw new VaporError('acceptContractClaim reverted (is the claim pending and are you the owner?)', 'NOT_REGISTERED')
        return { kind: 'tx', hash }
      },
      claimable: (appId: bigint, token: Address) => claimable(publicClient, appId, token),
      async claimRevenue(appId: bigint, token: Address): Promise<TxHandle> {
        const { account, wallet: w } = needAccount()
        const c = settleCalls.claim(appId, token)
        return { kind: 'tx', hash: await w.sendTransaction({ account, chain: network.chain, to: SETTLE_ADDRESS, data: c.data }) }
      },
    },

    tokens: {
      predict: (creator: Address, p: TokenLaunchParams) => predictTokenAddress(publicClient, cfg.contracts.tokenFactory, creator, p),
      async launch(p: TokenLaunchParams) {
        const { wallet: w } = needAccount()
        return launchToken(w, publicClient, cfg.contracts.tokenFactory, p)
      },
    },
  }
}

