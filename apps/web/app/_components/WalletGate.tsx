// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Proprietary. See LICENSE. Provenance: VAPOR-6eabb1be532bdef4
'use client'

import { useState, type ReactNode } from 'react'
import { useWallet } from './Providers'

/** Renders children once the embedded wallet exists; otherwise offers to create one. */
export function WalletGate({ children }: { children: ReactNode }) {
  const wallet = useWallet()
  const [busy, setBusy] = useState(false)
  const [err, setErr] = useState<string | null>(null)
  if (wallet.status === 'loading') return <div className="card animate-pulse text-sm text-slate-500">Loading wallet…</div>
  if (wallet.status === 'error') return <div className="card text-sm text-red-600">Wallet unavailable: {wallet.error?.message}</div>
  if (wallet.status === 'none') {
    return (
      <div className="card space-y-3">
        <h2 className="text-lg font-bold">Create your wallet</h2>
        <p className="text-sm text-slate-600 dark:text-slate-400">
          No seed phrase and no gas token. Your key is created and encrypted in this browser; apps pay your network fees. You can export it any time from the Wallet page.
        </p>
        <button
          className="btn-primary"
          data-testid="create-wallet"
          disabled={busy}
          onClick={async () => {
            setBusy(true)
            setErr(null)
            try {
              await wallet.create()
            } catch (e) {
              setErr((e as Error).message)
            } finally {
              setBusy(false)
            }
          }}
        >
          {busy ? 'Creating…' : 'Create wallet'}
        </button>
        {err ? <p className="text-sm text-red-600">{err}</p> : null}
      </div>
    )
  }
  return <>{children}</>
}
