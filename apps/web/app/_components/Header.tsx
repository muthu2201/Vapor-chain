// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Proprietary. See LICENSE. Provenance: VAPOR-6eabb1be532bdef4
'use client'

import Link from 'next/link'
import { usePathname } from 'next/navigation'
import { useUnifiedBalance } from '@vaporchain/react'
import { config } from '@/lib/config'
import { short, usdc } from '@/lib/format'
import { useWallet } from './Providers'

export function Header() {
  const path = usePathname()
  const wallet = useWallet()
  const balance = useUnifiedBalance({ symbol: 'USDC' })
  const shop = config().shop
  const links = [
    { href: '/', label: 'Shop' },
    { href: '/wallet', label: 'Wallet' },
    { href: '/launch', label: 'Launch a token' },
    ...(shop ? [{ href: `/apps/${shop.appId}`, label: 'Dashboard' }] : []),
  ]
  return (
    <header className="border-b border-slate-200 bg-white/70 backdrop-blur dark:border-slate-800 dark:bg-slate-950/70">
      <div className="mx-auto flex max-w-5xl flex-wrap items-center gap-4 px-4 py-3">
        <Link href="/" className="text-lg font-black tracking-tight">
          Vapor<span className="text-vapor">Chain</span>
        </Link>
        <nav className="flex flex-1 flex-wrap gap-1 text-sm">
          {links.map((l) => (
            <Link
              key={l.href}
              href={l.href}
              className={`rounded-lg px-3 py-1.5 ${path === l.href ? 'bg-mist font-semibold text-vapor-dark dark:bg-slate-800 dark:text-white' : 'hover:bg-slate-100 dark:hover:bg-slate-800'}`}
            >
              {l.label}
            </Link>
          ))}
        </nav>
        {wallet.address ? (
          <div data-testid="wallet-chip" className="rounded-xl border border-slate-200 px-3 py-1.5 text-xs dark:border-slate-700">
            <span className="font-mono">{short(wallet.address)}</span>
            <span className="mx-2 text-slate-400">·</span>
            <span data-testid="usdc-balance">{usdc(balance.data?.local)}</span>
          </div>
        ) : null}
      </div>
    </header>
  )
}
