// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Proprietary. See LICENSE. Provenance: VAPOR-6eabb1be532bdef4
'use client'

import { useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { config } from '@/lib/config'
import { usdc } from '@/lib/format'
import { useWallet } from './Providers'

export function FaucetButton() {
  const wallet = useWallet()
  const qc = useQueryClient()
  const [state, setState] = useState<{ busy: boolean; msg?: string; ok?: boolean }>({ busy: false })
  if (!config().faucetEnabled || !wallet.address) return null
  return (
    <div className="flex flex-wrap items-center gap-3">
      <button
        className="btn-ghost"
        data-testid="faucet"
        disabled={state.busy}
        onClick={async () => {
          setState({ busy: true })
          const r = await fetch('/api/faucet', { method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ address: wallet.address }) })
          const j = (await r.json().catch(() => ({}))) as { amount?: string; error?: string }
          if (r.ok) {
            await qc.invalidateQueries({ queryKey: ['vapor'] })
            setState({ busy: false, ok: true, msg: `Received ${usdc(BigInt(j.amount ?? '0'))}` })
          } else {
            setState({ busy: false, ok: false, msg: j.error ?? `faucet error ${r.status}` })
          }
        }}
      >
        {state.busy ? 'Requesting…' : 'Get test USDC'}
      </button>
      {state.msg ? (
        <span data-testid="faucet-result" className={`text-sm ${state.ok ? 'text-emerald-600' : 'text-red-600'}`}>
          {state.msg}
        </span>
      ) : null}
    </div>
  )
}
