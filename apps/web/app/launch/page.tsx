// Copyright (c) 2026 VaporChain / muthu2201. Licensed under the Apache License, Version 2.0.
// SPDX-License-Identifier: Apache-2.0
// Provenance: VAPOR-6eabb1be532bdef4 (authorship watermark; see NOTICE)
'use client'

import { useLaunchToken, usePredictedTokenAddress, VaporProvider } from '@vaporchain/react'
import type { TokenLaunchParams } from '@vaporchain/sdk'
import { useDeferredValue, useMemo, useState } from 'react'
import { parseUnits } from 'viem'
import { config } from '@/lib/config'
import { explain } from '../_components/errors'
import { useAppQueryClient, useWallet } from '../_components/Providers'
import { WalletGate } from '../_components/WalletGate'

export default function LaunchPage() {
  const cfg = config()
  const wallet = useWallet()
  const queryClient = useAppQueryClient()
  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-black">Launch a token</h1>
        <p className="text-slate-600 dark:text-slate-400">
          An ERC-20 with permit and burn, optional capped minting, at an address you can see before you launch. {cfg.launchpadAppId !== undefined ? 'Gas is sponsored by the Launchpad.' : ''}
        </p>
      </div>
      <WalletGate>
        {cfg.launchpadAppId === undefined ? (
          <div className="card text-sm">The launchpad is not configured (NEXT_PUBLIC_LAUNCHPAD_APP_ID).</div>
        ) : (
          // gas for launches comes from the Launchpad app's quota, not the shop's
          <VaporProvider network={cfg.network} contracts={cfg.contracts} appId={cfg.launchpadAppId} queryClient={queryClient} {...(wallet.account ? { account: wallet.account } : {})}>
            <LaunchForm />
          </VaporProvider>
        )}
      </WalletGate>
    </div>
  )
}

function LaunchForm() {
  const wallet = useWallet()
  const [name, setName] = useState('')
  const [symbol, setSymbol] = useState('')
  const [supply, setSupply] = useState('1000000')
  const [cap, setCap] = useState('')
  const params = useMemo<TokenLaunchParams | undefined>(() => {
    if (!wallet.address || !name.trim() || !/^[A-Z0-9]{2,11}$/.test(symbol)) return undefined
    try {
      return {
        name: name.trim(),
        symbol,
        initialSupply: parseUnits(supply || '0', 18),
        cap: cap ? parseUnits(cap, 18) : 0n,
        owner: wallet.address,
      }
    } catch {
      return undefined
    }
  }, [wallet.address, name, symbol, supply, cap])
  const deferred = useDeferredValue(params)
  const predicted = usePredictedTokenAddress(deferred)
  const launch = useLaunchToken({ sponsored: true })
  const capTooLow = params && params.cap !== undefined && params.cap > 0n && params.cap < params.initialSupply

  return (
    <form
      className="card grid gap-4 sm:grid-cols-2"
      onSubmit={(e) => {
        e.preventDefault()
        if (params && !capTooLow) launch.mutate(params)
      }}
    >
      <label>
        <span className="label">Name</span>
        <input className="input" data-testid="token-name" value={name} maxLength={64} onChange={(e) => setName(e.target.value)} placeholder="Gem Shards" />
      </label>
      <label>
        <span className="label">Symbol (A–Z, 0–9)</span>
        <input className="input" data-testid="token-symbol" value={symbol} onChange={(e) => setSymbol(e.target.value.toUpperCase())} placeholder="GEM" />
      </label>
      <label>
        <span className="label">Initial supply (to you)</span>
        <input className="input" data-testid="token-supply" inputMode="decimal" value={supply} onChange={(e) => setSupply(e.target.value)} />
      </label>
      <label>
        <span className="label">Max supply (empty = fixed forever)</span>
        <input className="input" inputMode="decimal" value={cap} onChange={(e) => setCap(e.target.value)} />
      </label>
      <div className="sm:col-span-2">
        <span className="label">Your token will live at</span>
        <div className="mono" data-testid="predicted-address">
          {predicted.data ?? '—'}
        </div>
      </div>
      {capTooLow ? <p className="text-sm text-red-600 sm:col-span-2">Max supply must be at least the initial supply.</p> : null}
      <div className="sm:col-span-2">
        <button className="btn-primary" data-testid="launch" disabled={!params || !!capTooLow || launch.isPending}>
          {launch.isPending ? 'Launching…' : 'Launch (gasless)'}
        </button>
      </div>
      {launch.isSuccess ? (
        <p className="text-sm text-emerald-600 sm:col-span-2" data-testid="launched">
          Launched at <span className="font-mono">{launch.data.token}</span>
        </p>
      ) : null}
      {launch.isError ? <p className="text-sm text-red-600 sm:col-span-2">{explain(launch.error)}</p> : null}
    </form>
  )
}
