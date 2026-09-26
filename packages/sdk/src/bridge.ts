// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 VaporChain / muthu2201
// Provenance: VAPOR-6eabb1be532bdef4

import { fetchJson, VaporError } from './errors.js'

/**
 * Skip:Go client (cross-chain routing). VaporChain never builds its own bridge
 * or router: USDC arrives as USDC.inj (Circle CCTP V2 -> Injective -> IBC),
 * everything else via Skip-routed bridges. The affiliate fee is charged by
 * Skip on the route and paid to the VaporChain treasury on the destination.
 */
export const SKIP_API = 'https://api.skip.build'

export interface RouteRequest {
  amountIn: string
  sourceAssetDenom: string
  sourceAssetChainId: string
  destAssetDenom: string
  destAssetChainId: string
  affiliateFeeBps?: number
  allowMultiTx?: boolean
}

export interface SkipRoute {
  amount_in: string
  amount_out: string
  source_asset_chain_id: string
  dest_asset_chain_id: string
  operations: unknown[]
  required_chain_addresses: string[]
  estimated_route_duration_seconds?: number
  estimated_fees?: Array<{ fee_type: string; amount: string; usd_amount?: string }>
  [k: string]: unknown
}

export type BridgeStage = 'submitted' | 'bridging' | 'included' | 'final' | 'failed'

export function skipClient(opts: { apiUrl?: string; apiKey?: string; affiliateAddressByChain?: Record<string, string> } = {}) {
  const base = (opts.apiUrl ?? SKIP_API).replace(/\/$/, '')
  const headers: Record<string, string> = opts.apiKey ? { authorization: opts.apiKey } : {}
  return {
    async route(r: RouteRequest): Promise<SkipRoute> {
      return fetchJson<SkipRoute>(`${base}/v2/fungible/route`, {
        method: 'POST', headers,
        body: JSON.stringify({
          amount_in: r.amountIn,
          source_asset_denom: r.sourceAssetDenom,
          source_asset_chain_id: r.sourceAssetChainId,
          dest_asset_denom: r.destAssetDenom,
          dest_asset_chain_id: r.destAssetChainId,
          cumulative_affiliate_fee_bps: String(r.affiliateFeeBps ?? 20),
          allow_multi_tx: r.allowMultiTx ?? true,
          smart_swap_options: { split_routes: true, evm_swaps: true },
        }),
      })
    },
    async msgs(route: SkipRoute, addresses: string[], slippageTolerancePercent = '0.5'): Promise<{ msgs?: unknown[]; txs?: unknown[] }> {
      const affiliates = opts.affiliateAddressByChain
        ? Object.fromEntries(Object.entries(opts.affiliateAddressByChain).map(([chain, address]) => [chain, { affiliates: [{ address, basis_points_fee: '20' }] }]))
        : undefined
      return fetchJson(`${base}/v2/fungible/msgs`, {
        method: 'POST', headers,
        body: JSON.stringify({
          source_asset_denom: route['source_asset_denom'],
          source_asset_chain_id: route.source_asset_chain_id,
          dest_asset_denom: route['dest_asset_denom'],
          dest_asset_chain_id: route.dest_asset_chain_id,
          amount_in: route.amount_in,
          amount_out: route.amount_out,
          address_list: addresses,
          operations: route.operations,
          slippage_tolerance_percent: slippageTolerancePercent,
          ...(affiliates ? { chain_ids_to_affiliates: affiliates } : {}),
        }),
      })
    },
    async track(txHash: string, chainId: string): Promise<void> {
      await fetchJson(`${base}/v2/tx/track`, { method: 'POST', headers, body: JSON.stringify({ tx_hash: txHash, chain_id: chainId }) })
    },
    async status(txHash: string, chainId: string): Promise<{ state: string; [k: string]: unknown }> {
      return fetchJson(`${base}/v2/tx/status?tx_hash=${encodeURIComponent(txHash)}&chain_id=${encodeURIComponent(chainId)}`, { headers })
    },
    /** Async stream of stages until the transfer completes or fails. */
    async *watch(txHash: string, chainId: string, pollMs = 3000, timeoutMs = 30 * 60_000): AsyncGenerator<BridgeStage> {
      await this.track(txHash, chainId)
      yield 'submitted'
      const deadline = Date.now() + timeoutMs
      let last: BridgeStage = 'submitted'
      while (Date.now() < deadline) {
        const s = await this.status(txHash, chainId)
        const stage: BridgeStage =
          s.state === 'STATE_COMPLETED_SUCCESS' ? 'final'
          : s.state?.startsWith('STATE_COMPLETED') || s.state === 'STATE_ABANDONED' ? 'failed'
          : s.state === 'STATE_PENDING' ? 'bridging'
          : 'submitted'
        if (stage !== last) {
          yield stage
          last = stage
        }
        if (stage === 'final' || stage === 'failed') return
        await new Promise((r) => setTimeout(r, pollMs))
      }
      throw new VaporError('bridge status timed out', 'TIMEOUT')
    },
  }
}
