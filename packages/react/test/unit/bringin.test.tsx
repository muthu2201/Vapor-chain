// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Provenance: VAPOR-6eabb1be532bdef4
//
// useBringIn state machine against a local server that speaks the Skip:Go
// REST contract (route / track / status), so every stage transition runs
// through the real SDK client and fetch.
import { act, renderHook, waitFor } from '@testing-library/react'
import { createServer, type Server } from 'node:http'
import type { AddressInfo } from 'node:net'
import type { ReactNode } from 'react'
import { vaporLocalnet } from '@vaporchain/sdk'
import { afterAll, beforeAll, describe, expect, it } from 'vitest'
import { useBringIn, VaporProvider } from '../../src/index.js'

let server: Server
let base = ''
const polls = new Map<string, number>()
const script: Record<string, string[]> = {
  good: ['STATE_SUBMITTED', 'STATE_PENDING', 'STATE_PENDING', 'STATE_COMPLETED_SUCCESS'],
  bad: ['STATE_PENDING', 'STATE_COMPLETED_ERROR'],
}

beforeAll(async () => {
  server = createServer((req, res) => {
    res.setHeader('access-control-allow-origin', '*')
    res.setHeader('access-control-allow-headers', 'content-type, authorization')
    if (req.method === 'OPTIONS') return void res.writeHead(204).end()
    const u = new URL(req.url!, 'http://x')
    if (u.pathname === '/v2/fungible/route') {
      return void res.end(JSON.stringify({ amount_in: '1000000', amount_out: '998000', source_asset_chain_id: 'noble-1', dest_asset_chain_id: 'vapor-local-1', operations: [], required_chain_addresses: ['noble-1', 'vapor-local-1'], estimated_route_duration_seconds: 30 }))
    }
    if (u.pathname === '/v2/tx/track') return void res.end('{}')
    if (u.pathname === '/v2/tx/status') {
      const h = u.searchParams.get('tx_hash')!
      const n = polls.get(h) ?? 0
      polls.set(h, n + 1)
      const seq = script[h]!
      return void res.end(JSON.stringify({ state: seq[Math.min(n, seq.length - 1)] }))
    }
    res.writeHead(404).end()
  })
  await new Promise<void>((r) => server.listen(0, '127.0.0.1', r))
  base = `http://127.0.0.1:${(server.address() as AddressInfo).port}`
})
afterAll(() => server.close())

const contracts = { tokenFactory: '0x5062D1768F0272f9989946Bc9762FB0F37F57265', usdc: '0xb235A1c5b84eaEe836DC48DFD50325190371303F' } as const
const wrapper = ({ children }: { children: ReactNode }) => (
  <VaporProvider network={vaporLocalnet} contracts={contracts} skip={{ apiUrl: base }}>
    {children}
  </VaporProvider>
)
const req = { amountIn: '1000000', sourceAssetDenom: 'uusdc', sourceAssetChainId: 'noble-1', destAssetDenom: 'uusdc', destAssetChainId: 'vapor-local-1' }

describe('useBringIn', () => {
  it('routes, then tracks a transfer to final', async () => {
    const { result } = renderHook(() => useBringIn(), { wrapper })
    expect(result.current.stage).toBe('idle')
    await act(async () => void (await result.current.route(req)))
    expect(result.current.stage).toBe('routed')
    expect(result.current.routeResult?.amount_out).toBe('998000')
    const seen: string[] = []
    await act(async () => {
      const p = result.current.track('good', 'noble-1')
      await p
    })
    seen.push(result.current.stage)
    expect(seen.at(-1)).toBe('final')
    expect(result.current.error).toBeNull()
  }, 30_000)

  it('surfaces a failed transfer', async () => {
    const { result } = renderHook(() => useBringIn(), { wrapper })
    await act(async () => void (await result.current.track('bad', 'noble-1')))
    await waitFor(() => expect(result.current.stage).toBe('failed'))
  }, 30_000)

  it('reset returns to idle', async () => {
    const { result } = renderHook(() => useBringIn(), { wrapper })
    await act(async () => void (await result.current.route(req)))
    act(() => result.current.reset())
    expect(result.current.stage).toBe('idle')
    expect(result.current.routeResult).toBeNull()
  })
})
