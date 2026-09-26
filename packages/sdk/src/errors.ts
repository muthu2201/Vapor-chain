// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 VaporChain / muthu2201
// Provenance: VAPOR-6eabb1be532bdef4

export class VaporError extends Error {
  constructor(
    message: string,
    readonly code: 'SPONSORSHIP_DENIED' | 'QUOTA_EXHAUSTED' | 'NOT_REGISTERED' | 'HTTP' | 'CONFIG' | 'ROUTE' | 'TIMEOUT',
    readonly cause?: unknown,
  ) {
    super(message)
    this.name = 'VaporError'
  }
}

export async function fetchJson<T>(url: string, init?: RequestInit, timeoutMs = 15_000): Promise<T> {
  const ctl = new AbortController()
  const t = setTimeout(() => ctl.abort(), timeoutMs)
  try {
    const headers = new Headers(init?.headers)
    if (!headers.has('content-type')) headers.set('content-type', 'application/json')
    const res = await fetch(url, { ...init, signal: ctl.signal, headers })
    const text = await res.text()
    if (!res.ok) throw new VaporError(`${init?.method ?? 'GET'} ${url} -> ${res.status}: ${text.slice(0, 300)}`, 'HTTP')
    return JSON.parse(text) as T
  } catch (e) {
    if (e instanceof VaporError) throw e
    if ((e as Error).name === 'AbortError') throw new VaporError(`timeout calling ${url}`, 'TIMEOUT', e)
    throw new VaporError(`request to ${url} failed: ${(e as Error).message}`, 'HTTP', e)
  } finally {
    clearTimeout(t)
  }
}
