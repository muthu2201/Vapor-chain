// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Proprietary. See LICENSE. Provenance: VAPOR-6eabb1be532bdef4

import { notFound } from 'next/navigation'
import { appsRest, type AppRecord, type Quota } from '@vaporchain/sdk'
import { config } from '@/lib/config'
import { gas, short, usdc } from '@/lib/format'

export const dynamic = 'force-dynamic'

// Server-side reads: the indexer and REST can sit on a private network.
const indexer = () => process.env.VAPOR_INDEXER_INTERNAL_URL ?? config().network.indexerUrl
const rest = () => process.env.VAPOR_REST_INTERNAL_URL ?? config().network.restUrl

async function get<T>(url: string): Promise<T | null> {
  const r = await fetch(url, { cache: 'no-store', signal: AbortSignal.timeout(5_000) }).catch(() => null)
  if (!r || !r.ok) return null
  return (await r.json()) as T
}

interface Revenue {
  series: Array<{ day: string; denom: string; payments: number; volume: string; fees: string; app_revenue: string }>
}
interface Sponsored {
  totals: { ops: number; credits_spent: string; unique_senders: number }
}
interface Settlements {
  items: Array<{ height: number; time: string; payer: string; amount: string; fee: string; app_share: string }>
}

export default async function AppDashboard({ params }: { params: Promise<{ id: string }> }) {
  const { id } = await params
  if (!/^\d{1,19}$/.test(id)) notFound()
  const chain = appsRest(rest())
  const [app, quota, revenue, sponsored, sets] = await Promise.all([
    chain.get(BigInt(id)).catch((): AppRecord | null => null),
    chain.quota(BigInt(id)).catch((): Quota | null => null),
    get<Revenue>(`${indexer()}/v1/apps/${id}/revenue?days=30`),
    get<Sponsored>(`${indexer()}/v1/apps/${id}/sponsored`),
    get<Settlements>(`${indexer()}/v1/apps/${id}/settlements?limit=10`),
  ])
  if (!app) notFound()
  const total = (k: 'volume' | 'app_revenue') => (revenue?.series ?? []).reduce((s, r) => s + BigInt(r[k]), 0n)
  const payments = (revenue?.series ?? []).reduce((s, r) => s + Number(r.payments), 0)

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-black">App #{id}</h1>
        <p className="text-sm text-slate-500">
          {app.metadataUri} · {app.status.replace('APP_STATUS_', '').toLowerCase()}
        </p>
      </div>
      <div className="grid gap-4 sm:grid-cols-4">
        <Stat label="Volume (30d)" value={usdc(total('volume'))} testId="volume" />
        <Stat label="Your revenue (30d)" value={usdc(total('app_revenue'))} />
        <Stat label="Payments (30d)" value={payments.toLocaleString('en-US')} testId="payments" />
        <Stat label="Sponsored ops" value={String(sponsored?.totals.ops ?? 0)} testId="sponsored-ops" />
      </div>
      <div className="grid gap-4 sm:grid-cols-2">
        <Stat label="Gas quota this epoch" value={gas(quota?.gas)} />
        <Stat label="Unique payers this epoch (est.)" value={String(quota?.uniquePayersEstimate ?? 0n)} />
      </div>
      <section className="card overflow-x-auto">
        <h2 className="mb-3 font-bold">Latest settlements</h2>
        <table className="w-full text-left text-sm">
          <thead className="text-xs uppercase text-slate-500">
            <tr>
              <th className="py-2">Block</th>
              <th>Payer</th>
              <th>Amount</th>
              <th>Fee</th>
              <th>Your share</th>
            </tr>
          </thead>
          <tbody data-testid="settlements">
            {(sets?.items ?? []).map((s) => (
              <tr key={`${s.height}-${s.payer}-${s.amount}-${s.time}`} className="border-t border-slate-100 dark:border-slate-800">
                <td className="py-2">{s.height}</td>
                <td className="font-mono">{short(s.payer)}</td>
                <td>{usdc(BigInt(s.amount))}</td>
                <td>{usdc(BigInt(s.fee))}</td>
                <td>{usdc(BigInt(s.app_share))}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </section>
    </div>
  )
}

function Stat({ label, value, testId }: { label: string; value: string; testId?: string }) {
  return (
    <div className="card">
      <span className="label">{label}</span>
      <div className="text-xl font-bold" data-testid={testId}>
        {value}
      </div>
    </div>
  )
}
