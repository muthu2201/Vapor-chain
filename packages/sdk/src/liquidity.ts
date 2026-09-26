// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 VaporChain / muthu2201
// Provenance: VAPOR-6eabb1be532bdef4

import { parseAbi, type Address, type PublicClient } from 'viem'
import { skipClient } from './bridge.js'

/**
 * VaporChain does not run a DEX. It aggregates quotes behind one interface:
 * SkipGoProvider (cross-chain + Cosmos DEXs) and LocalEvmDexProvider (any
 * Uniswap-v3-style DEX deployed on VaporChain, via QuoterV2).
 */
export interface NormalizedQuote {
  provider: string
  amountOut: bigint
  priceImpactBps?: number
  fees: { lp?: bigint; bridge?: bigint; relayer?: bigint; affiliate?: bigint }
  etaSeconds: number
  txCount: number
  minOut: bigint
  raw: unknown
}

export interface LiquidityProvider {
  name: string
  quote(p: { tokenIn: string; tokenOut: string; amountIn: bigint; slippageBps: number; chainId: string }): Promise<NormalizedQuote | null>
}

export function minOut(amountOut: bigint, slippageBps: number): bigint {
  if (!Number.isInteger(slippageBps) || slippageBps < 0 || slippageBps > 10_000) {
    throw new RangeError(`slippageBps must be an integer in [0, 10000], got ${slippageBps}`)
  }
  return (amountOut * BigInt(10_000 - slippageBps)) / 10_000n
}

/** Default slippage: 0.5% for stable pairs, 2% for volatile. */
export const defaultSlippageBps = (isStablePair: boolean) => (isStablePair ? 50 : 200)

const quoterV2Abi = parseAbi([
  'function quoteExactInputSingle((address tokenIn,address tokenOut,uint256 amountIn,uint24 fee,uint160 sqrtPriceLimitX96) params) returns (uint256 amountOut,uint160 sqrtPriceX96After,uint32 initializedTicksCrossed,uint256 gasEstimate)',
])

export class LocalEvmDexProvider implements LiquidityProvider {
  name = 'local-evm-dex'
  constructor(
    private client: PublicClient,
    private quoter: Address,
    private feeTiers: number[] = [100, 500, 3000, 10000],
  ) {}
  async quote(p: { tokenIn: string; tokenOut: string; amountIn: bigint; slippageBps: number }): Promise<NormalizedQuote | null> {
    let best: NormalizedQuote | null = null
    for (const fee of this.feeTiers) {
      try {
        const { result } = await this.client.simulateContract({
          address: this.quoter, abi: quoterV2Abi, functionName: 'quoteExactInputSingle',
          args: [{ tokenIn: p.tokenIn as Address, tokenOut: p.tokenOut as Address, amountIn: p.amountIn, fee, sqrtPriceLimitX96: 0n }],
        })
        const out = result[0]
        if (!best || out > best.amountOut) {
          best = { provider: this.name, amountOut: out, fees: { lp: (p.amountIn * BigInt(fee)) / 1_000_000n }, etaSeconds: 2, txCount: 1, minOut: minOut(out, p.slippageBps), raw: { fee } }
        }
      } catch {
        /* pool for this tier does not exist */
      }
    }
    return best
  }
}

export class SkipGoProvider implements LiquidityProvider {
  name = 'skip-go'
  constructor(private skip = skipClient(), private affiliateBps = 20) {}
  async quote(p: { tokenIn: string; tokenOut: string; amountIn: bigint; slippageBps: number; chainId: string; destChainId?: string }): Promise<NormalizedQuote | null> {
    try {
      const r = await this.skip.route({
        amountIn: p.amountIn.toString(), sourceAssetDenom: p.tokenIn, sourceAssetChainId: p.chainId,
        destAssetDenom: p.tokenOut, destAssetChainId: p.destChainId ?? p.chainId, affiliateFeeBps: this.affiliateBps,
      })
      const out = BigInt(r.amount_out)
      const fee = (t: string) => r.estimated_fees?.filter((f) => f.fee_type === t).reduce((s, f) => s + BigInt(f.amount), 0n)
      return {
        provider: this.name, amountOut: out, etaSeconds: r.estimated_route_duration_seconds ?? 60, txCount: r.required_chain_addresses.length,
        fees: { bridge: fee('BRIDGE'), relayer: fee('RELAYER'), affiliate: (p.amountIn * BigInt(this.affiliateBps)) / 10_000n },
        minOut: minOut(out, p.slippageBps), raw: r,
      }
    } catch {
      return null
    }
  }
}

/** Query all providers in parallel and rank by net output, then ETA. */
export async function bestQuote(providers: LiquidityProvider[], p: { tokenIn: string; tokenOut: string; amountIn: bigint; slippageBps: number; chainId: string }): Promise<NormalizedQuote | null> {
  const quotes = (await Promise.all(providers.map((x) => x.quote(p).catch(() => null)))).filter((q): q is NormalizedQuote => q !== null)
  quotes.sort((a, b) => (a.amountOut === b.amountOut ? a.etaSeconds - b.etaSeconds : a.amountOut > b.amountOut ? -1 : 1))
  return quotes[0] ?? null
}
