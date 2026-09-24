// Copyright (c) 2026 VaporChain / muthu2201. Licensed under the Apache License, Version 2.0 (see LICENSE).
// Provenance: VAPOR-6eabb1be532bdef4
import { renderHook } from '@testing-library/react'
import type { ReactNode } from 'react'
import { vaporLocalnet } from '@vaporchain/sdk'
import { describe, expect, it } from 'vitest'
import { useVapor, VaporProvider, vaporKey } from '../../src/index.js'

const contracts = { tokenFactory: '0x5062D1768F0272f9989946Bc9762FB0F37F57265', usdc: '0xb235A1c5b84eaEe836DC48DFD50325190371303F' } as const

describe('VaporProvider', () => {
  it('hooks refuse to run outside the provider', () => {
    expect(() => renderHook(() => useVapor())).toThrow('inside <VaporProvider>')
  })

  it('keeps the same client across re-renders with equal inline props', () => {
    const wrapper = ({ children }: { children: ReactNode }) => (
      <VaporProvider network={vaporLocalnet} contracts={{ ...contracts }} appId={5n}>
        {children}
      </VaporProvider>
    )
    const { result, rerender } = renderHook(() => useVapor(), { wrapper })
    const first = result.current.client
    rerender()
    expect(result.current.client).toBe(first)
    expect(result.current.assets.USDC?.erc20).toBe(contracts.usdc)
  })

  it('query keys encode bigint safely', () => {
    expect(vaporKey(vaporLocalnet, 'quota', 7n, undefined)).toEqual(['vapor', 779700, 'quota', '7', null])
    expect(() => JSON.stringify(vaporKey(vaporLocalnet, 2n ** 70n))).not.toThrow()
  })
})
