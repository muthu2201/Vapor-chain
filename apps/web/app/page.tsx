// Copyright (c) 2026 VaporChain / muthu2201. Licensed under the Apache License, Version 2.0.
// SPDX-License-Identifier: Apache-2.0
// Provenance: VAPOR-6eabb1be532bdef4 (authorship watermark; see NOTICE)
'use client'

import { useCheckout, useFeeQuote, useSponsorQuota } from '@vaporchain/react'
import { keccak256, stringToHex } from 'viem'
import { catalog, type Item } from '@/lib/catalog'
import { config } from '@/lib/config'
import { gas, short, usdc } from '@/lib/format'
import { explain } from './_components/errors'
import { FaucetButton } from './_components/FaucetButton'
import { useWallet } from './_components/Providers'
import { WalletGate } from './_components/WalletGate'

export default function ShopPage() {
  const shop = config().shop
  const quota = useSponsorQuota()
  if (!shop) return <div className="card">The demo shop is not configured (NEXT_PUBLIC_SHOP_APP_ID).</div>
  return (
    <div className="space-y-6">
      <section className="space-y-2">
        <h1 className="text-3xl font-black tracking-tight">Pay in USDC. One tap. No gas.</h1>
        <p className="text-slate-600 dark:text-slate-400">
          Every purchase is one signature: it approves the shop and pays the order in a single sponsored operation. The shop&apos;s earned quota pays the network fee.
        </p>
        <p className="text-xs text-slate-500" data-testid="quota">
          Sponsored gas left this epoch: {gas(quota.data?.gas)}
        </p>
      </section>
      <WalletGate>
        <FaucetButton />
        <div className="grid gap-4 sm:grid-cols-3">
          {catalog.map((item) => (
            <ItemCard key={item.id} item={item} checkout={shop.checkout} />
          ))}
        </div>
      </WalletGate>
    </div>
  )
}

function ItemCard({ item, checkout }: { item: Item; checkout: `0x${string}` }) {
  const cfg = config()
  const wallet = useWallet()
  const fee = useFeeQuote({ token: cfg.contracts.usdc, amount: item.price })
  const buy = useCheckout()
  return (
    <div className="card flex flex-col gap-3" data-testid={`item-${item.id}`}>
      <div>
        <h3 className="font-bold">{item.name}</h3>
        <p className="text-sm text-slate-500">{item.blurb}</p>
      </div>
      <div className="text-2xl font-black">{usdc(item.price)}</div>
      <div className="text-xs text-slate-500">
        Protocol fee {usdc(fee.data?.fee)} · gas {fee.data?.sponsored ? 'paid by the shop' : '—'}
      </div>
      <button
        className="btn-primary mt-auto"
        data-testid={`buy-${item.id}`}
        disabled={buy.isPending || !wallet.address}
        onClick={() =>
          buy.mutate({
            checkout,
            token: cfg.contracts.usdc,
            amount: item.price,
            orderId: keccak256(stringToHex(`${wallet.address}|${item.id}|${Date.now()}|${crypto.randomUUID()}`)),
          })
        }
      >
        {buy.isPending ? 'Paying…' : 'Buy'}
      </button>
      {buy.isSuccess && buy.data.kind === 'userop' ? (
        <p className="text-sm text-emerald-600" data-testid={`paid-${item.id}`}>
          Paid · tx <span className="font-mono">{short(buy.data.txHash)}</span>
        </p>
      ) : null}
      {buy.isError ? (
        <p className="text-sm text-red-600" data-testid={`error-${item.id}`}>
          {explain(buy.error)}
        </p>
      ) : null}
    </div>
  )
}
