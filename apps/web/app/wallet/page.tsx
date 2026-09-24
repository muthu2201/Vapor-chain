// Copyright (c) 2026 VaporChain / muthu2201. Licensed under the Apache License, Version 2.0.
// SPDX-License-Identifier: Apache-2.0
// Provenance: VAPOR-6eabb1be532bdef4 (authorship watermark; see NOTICE)
'use client'

import { useQuery } from '@tanstack/react-query'
import { useBringIn, useUnifiedBalance, useVapor, vaporKey } from '@vaporchain/react'
import { useState } from 'react'
import { formatEther, parseUnits } from 'viem'
import { usdc } from '@/lib/format'
import { explain } from '../_components/errors'
import { FaucetButton } from '../_components/FaucetButton'
import { useWallet } from '../_components/Providers'
import { WalletGate } from '../_components/WalletGate'

export default function WalletPage() {
  return (
    <div className="space-y-6">
      <h1 className="text-2xl font-black">Wallet</h1>
      <WalletGate>
        <Balances />
        <BringIn />
        <Backup />
      </WalletGate>
    </div>
  )
}

function Balances() {
  const wallet = useWallet()
  const { client, network } = useVapor()
  const bal = useUnifiedBalance({ symbol: 'USDC' })
  const credits = useQuery({
    queryKey: vaporKey(network, 'native', wallet.address),
    queryFn: () => client.publicClient.getBalance({ address: wallet.address! }),
    enabled: !!wallet.address,
    refetchInterval: 10_000,
  })
  return (
    <section className="card space-y-3">
      <div>
        <span className="label">Address</span>
        <div className="mono" data-testid="wallet-address">
          {wallet.address}
        </div>
      </div>
      <div className="grid gap-4 sm:grid-cols-2">
        <div>
          <span className="label">USDC</span>
          <div className="text-xl font-bold">{usdc(bal.data?.total)}</div>
        </div>
        <div>
          <span className="label">Gas credits</span>
          <div className="text-xl font-bold" data-testid="credits">
            {credits.data === undefined ? '—' : formatEther(credits.data)}
          </div>
          <p className="text-xs text-slate-500">You don&apos;t need any: apps sponsor your actions.</p>
        </div>
      </div>
      <FaucetButton />
    </section>
  )
}

function BringIn() {
  const { network } = useVapor()
  const b = useBringIn()
  const [amount, setAmount] = useState('10')
  const [source, setSource] = useState('noble-1')
  return (
    <section className="card space-y-3">
      <h2 className="font-bold">Bring USDC in from another chain</h2>
      <p className="text-sm text-slate-500">Routes through Skip:Go. You sign once on the source chain; USDC arrives here as the same asset.</p>
      <div className="grid gap-3 sm:grid-cols-3">
        <label>
          <span className="label">From</span>
          <select className="input" value={source} onChange={(e) => setSource(e.target.value)}>
            <option value="noble-1">Noble</option>
            <option value="osmosis-1">Osmosis</option>
            <option value="injective-1">Injective</option>
          </select>
        </label>
        <label>
          <span className="label">Amount (USDC)</span>
          <input className="input" inputMode="decimal" value={amount} onChange={(e) => setAmount(e.target.value)} />
        </label>
        <div className="flex items-end">
          <button
            className="btn-ghost w-full"
            disabled={b.stage === 'routing'}
            onClick={() => {
              let amountIn: string
              try {
                amountIn = parseUnits(amount, 6).toString()
              } catch {
                return
              }
              b.route({ amountIn, sourceAssetDenom: 'uusdc', sourceAssetChainId: source, destAssetDenom: 'uusdc', destAssetChainId: network.cosmosChainId }).catch(() => undefined)
            }}
          >
            {b.stage === 'routing' ? 'Finding route…' : 'Get quote'}
          </button>
        </div>
      </div>
      {b.routeResult ? (
        <p className="text-sm">
          You receive {usdc(BigInt(b.routeResult.amount_out))} in ~{b.routeResult.estimated_route_duration_seconds ?? 60}s via {b.routeResult.required_chain_addresses.length} chains.
        </p>
      ) : null}
      {b.error ? <p className="text-sm text-red-600">No route: {explain(b.error)}</p> : null}
    </section>
  )
}

function Backup() {
  const wallet = useWallet()
  const [ack, setAck] = useState(false)
  const [key, setKey] = useState<string | null>(null)
  const [confirmForget, setConfirmForget] = useState('')
  return (
    <section className="card space-y-3">
      <h2 className="font-bold">Backup &amp; recovery</h2>
      <p className="text-sm text-slate-500">
        Your key lives only in this browser. Clearing site data deletes it. Export it to keep a backup or to move to MetaMask/Rabby.
      </p>
      {key ? (
        <div className="space-y-2">
          <div className="mono rounded-lg bg-amber-50 p-3 text-amber-900 dark:bg-amber-950 dark:text-amber-200">{key}</div>
          <button className="btn-ghost" onClick={() => setKey(null)}>
            Hide
          </button>
        </div>
      ) : (
        <div className="space-y-2">
          <label className="flex items-center gap-2 text-sm">
            <input type="checkbox" checked={ack} onChange={(e) => setAck(e.target.checked)} />
            I understand anyone with this key controls my funds.
          </label>
          <button className="btn-ghost" disabled={!ack} onClick={async () => setKey(await wallet.exportKey())}>
            Reveal private key
          </button>
        </div>
      )}
      <div className="border-t border-slate-200 pt-3 dark:border-slate-800">
        <span className="label">Forget this wallet (type FORGET)</span>
        <div className="flex gap-2">
          <input className="input" value={confirmForget} onChange={(e) => setConfirmForget(e.target.value)} />
          <button className="btn-ghost text-red-600" disabled={confirmForget !== 'FORGET'} onClick={() => wallet.forget()}>
            Forget
          </button>
        </div>
      </div>
    </section>
  )
}
