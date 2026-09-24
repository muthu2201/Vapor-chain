// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Proprietary. See LICENSE. Provenance: VAPOR-6eabb1be532bdef4
'use client'

import { QueryClient } from '@tanstack/react-query'
import { useEmbeddedWallet, VaporProvider } from '@vaporchain/react'
import { createContext, useContext, useState, type ReactNode } from 'react'
import { config } from '@/lib/config'

type Wallet = ReturnType<typeof useEmbeddedWallet>
const WalletContext = createContext<Wallet | null>(null)
const QueryContext = createContext<QueryClient | null>(null)

export function Providers({ children }: { children: ReactNode }) {
  const cfg = config()
  const wallet = useEmbeddedWallet()
  const [queryClient] = useState(() => new QueryClient({ defaultOptions: { queries: { retry: 2, staleTime: 5_000 } } }))
  return (
    <WalletContext.Provider value={wallet}>
      <QueryContext.Provider value={queryClient}>
        <VaporProvider
          network={cfg.network}
          contracts={cfg.contracts}
          queryClient={queryClient}
          {...(cfg.shop ? { appId: cfg.shop.appId } : {})}
          {...(wallet.account ? { account: wallet.account } : {})}
        >
          {children}
        </VaporProvider>
      </QueryContext.Provider>
    </WalletContext.Provider>
  )
}

export function useWallet(): Wallet {
  const w = useContext(WalletContext)
  if (!w) throw new Error('useWallet outside <Providers>')
  return w
}

/** The app-wide QueryClient, for nested providers that sponsor with another app. */
export function useAppQueryClient(): QueryClient {
  const q = useContext(QueryContext)
  if (!q) throw new Error('useAppQueryClient outside <Providers>')
  return q
}
