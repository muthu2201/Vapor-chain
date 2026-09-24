// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Proprietary. See LICENSE. Provenance: VAPOR-6eabb1be532bdef4

import { NextResponse, type NextRequest } from 'next/server'

/**
 * Per-request nonce Content-Security-Policy. The embedded wallet's key can be
 * used by any script running on this origin, so script execution is locked
 * to Next's own nonce'd bundles ('strict-dynamic'), and the page may only
 * talk to the configured VaporChain endpoints.
 */
export function proxy(request: NextRequest) {
  const nonce = btoa(crypto.randomUUID())
  const connect = [
    process.env.NEXT_PUBLIC_VAPOR_RPC,
    process.env.NEXT_PUBLIC_VAPOR_REST,
    process.env.NEXT_PUBLIC_VAPOR_BUNDLER,
    process.env.NEXT_PUBLIC_VAPOR_SPONSOR,
    process.env.NEXT_PUBLIC_VAPOR_INDEXER,
    'https://api.skip.build',
  ]
    .filter(Boolean)
    .map((u) => new URL(u!).origin)
  const dev = process.env.NODE_ENV !== 'production'
  const allHttps = connect.every((o) => o.startsWith('https://'))
  const csp = [
    `default-src 'self'`,
    `script-src 'self' 'nonce-${nonce}' 'strict-dynamic'${dev ? ` 'unsafe-eval'` : ''}`,
    `style-src 'self' 'unsafe-inline'`,
    `img-src 'self' data: blob:`,
    `font-src 'self'`,
    `connect-src 'self' ${connect.join(' ')}${dev ? ' ws:' : ''}`,
    `object-src 'none'`,
    `base-uri 'self'`,
    `form-action 'self'`,
    `frame-ancestors 'none'`,
    ...(allHttps && !dev ? ['upgrade-insecure-requests'] : []),
  ].join('; ')

  const headers = new Headers(request.headers)
  headers.set('x-nonce', nonce)
  headers.set('Content-Security-Policy', csp)
  const res = NextResponse.next({ request: { headers } })
  res.headers.set('Content-Security-Policy', csp)
  if (request.nextUrl.protocol === 'https:') res.headers.set('Strict-Transport-Security', 'max-age=63072000; includeSubDomains; preload')
  return res
}

export const config = {
  matcher: [
    {
      source: '/((?!api|_next/static|_next/image|favicon.ico).*)',
      missing: [
        { type: 'header', key: 'next-router-prefetch' },
        { type: 'header', key: 'purpose', value: 'prefetch' },
      ],
    },
  ],
}
