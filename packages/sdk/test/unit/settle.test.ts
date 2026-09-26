// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 VaporChain / muthu2201
// Provenance: VAPOR-6eabb1be532bdef4
import { decodeFunctionData, toFunctionSelector } from 'viem'
import { describe, expect, it } from 'vitest'
import { SETTLE_ADDRESS, settleAbi, settleCalls } from '../../src/index.js'

const USDC = '0xb235A1c5b84eaEe836DC48DFD50325190371303F'
const PAYEE = '0x00000000000000000000000000000000000000AA'

describe('settleCalls', () => {
  it('targets the Settle precompile', () => {
    for (const c of [settleCalls.approveApp(1n, USDC, 5n), settleCalls.pay(USDC, 1n, PAYEE), settleCalls.claim(1n, USDC), settleCalls.buyCredits(USDC, 1n)]) {
      expect(c.to).toBe(SETTLE_ADDRESS)
    }
  })

  it('approveApp selector is the one the sponsor service allowlists (0xba3014bf)', () => {
    expect(toFunctionSelector('approveApp(uint64,address,uint256)')).toBe('0xba3014bf')
    expect(settleCalls.approveApp(7n, USDC, 10n).data.slice(0, 10)).toBe('0xba3014bf')
  })

  it('round-trips arguments through the ABI', () => {
    const d = decodeFunctionData({ abi: settleAbi, data: settleCalls.pay(USDC, 123n, PAYEE).data })
    expect(d.functionName).toBe('pay')
    expect(d.args).toEqual([USDC, 123n, PAYEE, '0x0000000000000000000000000000000000000000'])
    const a = decodeFunctionData({ abi: settleAbi, data: settleCalls.approveApp(2n ** 64n - 1n, USDC, 2n ** 255n).data })
    expect(a.args).toEqual([2n ** 64n - 1n, USDC, 2n ** 255n])
  })

  it('refuses app ids that do not fit uint64 instead of truncating them', () => {
    expect(() => settleCalls.approveApp(2n ** 64n, USDC, 1n)).toThrow()
  })
})
