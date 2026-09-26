// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 VaporChain / muthu2201
// Provenance: VAPOR-6eabb1be532bdef4

import type { Metadata } from 'next'
import { headers } from 'next/headers'
import type { ReactNode } from 'react'
import { Header } from './_components/Header'
import { Providers } from './_components/Providers'
import './globals.css'

export const metadata: Metadata = {
  title: 'VaporChain',
  description: 'Pay in USDC with one signature. No gas token, no seed phrase.',
  other: { 'vaporchain-provenance': 'VAPOR-6eabb1be532bdef4' },
}

export default async function RootLayout({ children }: { children: ReactNode }) {
  // reading headers makes every page render per request, so each response
  // carries the nonce the CSP in proxy.ts was built with
  await headers()
  return (
    <html lang="en">
      <body>
        <Providers>
          <Header />
          <main className="mx-auto max-w-5xl px-4 pb-24 pt-8">{children}</main>
        </Providers>
      </body>
    </html>
  )
}
