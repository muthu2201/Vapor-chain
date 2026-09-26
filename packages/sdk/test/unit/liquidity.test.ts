// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 VaporChain / muthu2201
// Provenance: VAPOR-6eabb1be532bdef4
import { describe, expect, it } from 'vitest'
import { bestQuote, defaultSlippageBps, minOut, type LiquidityProvider, type NormalizedQuote } from '../../src/index.js'

const q = (provider: string, amountOut: bigint, etaSeconds: number): NormalizedQuote => ({
  provider, amountOut, etaSeconds, fees: {}, txCount: 1, minOut: minOut(amountOut, 50), raw: null,
})
const fixed = (name: string, out: NormalizedQuote | null | Error): LiquidityProvider => ({
  name,
  quote: async () => {
    if (out instanceof Error) throw out
    return out
  },
})
const req = { tokenIn: 'a', tokenOut: 'b', amountIn: 1_000_000n, slippageBps: 50, chainId: 'vapor-local-1' }

describe('minOut', () => {
  it('applies slippage in basis points, rounding down', () => {
    expect(minOut(1_000_000n, 50)).toBe(995_000n)
    expect(minOut(3n, 5000)).toBe(1n)
    expect(minOut(10n, 0)).toBe(10n)
    expect(minOut(10n, 10_000)).toBe(0n)
  })
  it('rejects nonsensical slippage', () => {
    for (const bad of [-1, 10_001, 1.5, Number.NaN]) expect(() => minOut(1n, bad)).toThrow(RangeError)
  })
  it('defaults: 0.5% stable, 2% volatile', () => {
    expect(defaultSlippageBps(true)).toBe(50)
    expect(defaultSlippageBps(false)).toBe(200)
  })
})

describe('bestQuote', () => {
  it('picks the largest output, then the fastest', async () => {
    const best = await bestQuote([fixed('slow', q('slow', 100n, 60)), fixed('fast', q('fast', 100n, 5)), fixed('low', q('low', 99n, 1))], req)
    expect(best?.provider).toBe('fast')
  })
  it('ignores providers that fail or have no route', async () => {
    const best = await bestQuote([fixed('boom', new Error('down')), fixed('none', null), fixed('ok', q('ok', 1n, 1))], req)
    expect(best?.provider).toBe('ok')
  })
  it('returns null when nobody can route', async () => {
    expect(await bestQuote([fixed('none', null)], req)).toBeNull()
  })
})
