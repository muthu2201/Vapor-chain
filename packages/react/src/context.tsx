// Copyright (c) 2026 VaporChain / muthu2201. Licensed under the Apache License, Version 2.0.
// SPDX-License-Identifier: Apache-2.0
// Provenance: VAPOR-6eabb1be532bdef4 (authorship watermark; see NOTICE)

import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { createContext, useContext, useMemo, useState, type ReactNode } from 'react'
import type { Address } from 'viem'
import type { PrivateKeyAccount } from 'viem/accounts'
import { createVaporClient, localnetAssets, type AssetRegistry, type VaporClient, type VaporNetwork } from '@vaporchain/sdk'

export interface VaporContracts {
  tokenFactory: Address
  usdc: Address
}

export interface VaporContextValue {
  client: VaporClient
  network: VaporNetwork
  contracts: VaporContracts
  assets: AssetRegistry
  appId: bigint | undefined
  account: PrivateKeyAccount | undefined
}

const VaporContext = createContext<VaporContextValue | null>(null)

export interface VaporProviderProps {
  network: VaporNetwork
  contracts: VaporContracts
  /** App whose earned quota sponsors this user's gas. */
  appId?: bigint
  /** Signer (e.g. from useEmbeddedWallet). Omit for read-only UIs. */
  account?: PrivateKeyAccount
  /** Symbol -> asset registry. Defaults to USDC at contracts.usdc. */
  assets?: AssetRegistry
  skip?: { apiUrl?: string; apiKey?: string; affiliateAddressByChain?: Record<string, string> }
  /** Pass your app's QueryClient to share its cache; one is created otherwise. */
  queryClient?: QueryClient
  children: ReactNode
}

/**
 * Supplies a VaporClient to the hooks. The client is rebuilt only when an
 * input that changes its behaviour changes (addresses are compared by value,
 * so inline `contracts={{...}}` objects do not thrash it).
 */
export function VaporProvider(p: VaporProviderProps) {
  const [ownQueryClient] = useState(() => p.queryClient ?? new QueryClient({ defaultOptions: { queries: { retry: 2, staleTime: 5_000 } } }))
  const { tokenFactory, usdc } = p.contracts
  const skipKey = p.skip?.apiKey
  const skipUrl = p.skip?.apiUrl
  const affiliates = p.skip?.affiliateAddressByChain
  const affiliatesKey = affiliates ? JSON.stringify(affiliates) : ''
  const value = useMemo<VaporContextValue>(() => {
    const contracts = { tokenFactory, usdc }
    return {
      client: createVaporClient({
        network: p.network,
        contracts,
        ...(p.account ? { account: p.account } : {}),
        ...(p.appId !== undefined ? { appId: p.appId } : {}),
        skip: {
          ...(skipUrl ? { apiUrl: skipUrl } : {}),
          ...(skipKey ? { apiKey: skipKey } : {}),
          ...(affiliates ? { affiliateAddressByChain: affiliates } : {}),
        },
      }),
      network: p.network,
      contracts,
      assets: p.assets ?? localnetAssets(usdc),
      appId: p.appId,
      account: p.account,
    }
    // affiliates is covered by affiliatesKey
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [p.network, tokenFactory, usdc, p.appId, p.account, p.assets, skipUrl, skipKey, affiliatesKey])

  return (
    <QueryClientProvider client={ownQueryClient}>
      <VaporContext.Provider value={value}>{p.children}</VaporContext.Provider>
    </QueryClientProvider>
  )
}

export function useVapor(): VaporContextValue {
  const v = useContext(VaporContext)
  if (!v) throw new Error('VaporChain hooks must be used inside <VaporProvider>')
  return v
}

/** Query-key prefix: react-query hashes keys with JSON, which cannot encode bigint. */
export function vaporKey(network: VaporNetwork, ...parts: Array<string | number | bigint | undefined>): unknown[] {
  return ['vapor', network.chain.id, ...parts.map((x) => (typeof x === 'bigint' ? x.toString() : x ?? null))]
}
