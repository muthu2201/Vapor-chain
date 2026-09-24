// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Proprietary. See LICENSE. Provenance: VAPOR-6eabb1be532bdef4

import { readFileSync } from 'node:fs'
import { createPublicClient, createWalletClient, erc20Abi, http, isAddress, type Address, type Hex } from 'viem'
import { privateKeyToAccount } from 'viem/accounts'
import { config } from '@/lib/config'

/**
 * Testnet USDC faucet. Server-side only: the key never reaches the browser.
 * Guard rails: disabled unless configured, refuses mainnet, one drip per
 * address per 24h, a global drip budget per minute, and sends are serialized
 * so concurrent requests cannot race the faucet's nonce.
 */
export const runtime = 'nodejs'
export const dynamic = 'force-dynamic'

const DAY = 24 * 60 * 60 * 1000
const lastDrip = new Map<string, number>()
let window = { start: 0, count: 0 }
const PER_MINUTE = 30
let queue: Promise<unknown> = Promise.resolve()

function faucet() {
  const file = process.env.VAPOR_FAUCET_KEY_FILE
  const cfg = config()
  if (!file || !cfg.faucetEnabled) return null
  const key = readFileSync(file, 'utf8').trim()
  const account = privateKeyToAccount((key.startsWith('0x') ? key : `0x${key}`) as Hex)
  const transport = http(cfg.network.chain.rpcUrls.default.http[0])
  return {
    cfg,
    account,
    wallet: createWalletClient({ account, chain: cfg.network.chain, transport }),
    pub: createPublicClient({ chain: cfg.network.chain, transport }),
  }
}

export async function POST(req: Request) {
  const f = faucet()
  if (!f) return Response.json({ error: 'faucet disabled' }, { status: 404 })
  const body = (await req.json().catch(() => null)) as { address?: string } | null
  const to = body?.address
  if (!to || !isAddress(to)) return Response.json({ error: 'address required' }, { status: 400 })

  const now = Date.now()
  const key = to.toLowerCase()
  const last = lastDrip.get(key) ?? 0
  if (now - last < DAY) return Response.json({ error: 'already funded today', retryAfterSeconds: Math.ceil((last + DAY - now) / 1000) }, { status: 429 })
  if (now - window.start > 60_000) window = { start: now, count: 0 }
  if (window.count >= PER_MINUTE) return Response.json({ error: 'faucet busy, try again in a minute' }, { status: 429 })
  window.count++
  lastDrip.set(key, now)
  if (lastDrip.size > 100_000) lastDrip.clear()

  const amount = BigInt(process.env.VAPOR_FAUCET_AMOUNT ?? '25000000')
  const send = async () => {
    const hash = await f.wallet.writeContract({ address: f.cfg.contracts.usdc, abi: erc20Abi, functionName: 'transfer', args: [to as Address, amount] })
    const rcpt = await f.pub.waitForTransactionReceipt({ hash })
    if (rcpt.status !== 'success') throw new Error('faucet transfer reverted')
    return hash
  }
  const run = queue.then(send, send)
  queue = run.catch(() => undefined)
  try {
    const hash = await run
    return Response.json({ hash, amount: amount.toString() })
  } catch (e) {
    lastDrip.delete(key) // let them retry after a failure
    console.error('faucet send failed', e)
    return Response.json({ error: 'faucet send failed' }, { status: 502 })
  }
}
