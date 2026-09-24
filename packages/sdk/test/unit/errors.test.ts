// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Provenance: VAPOR-6eabb1be532bdef4
import { createServer, type Server } from 'node:http'
import type { AddressInfo } from 'node:net'
import { afterAll, beforeAll, describe, expect, it } from 'vitest'
import { fetchJson, VaporError } from '../../src/index.js'

// a real HTTP server on an ephemeral port: exercises fetch, status handling
// and the abort timer end to end
let server: Server
let base: string
beforeAll(async () => {
  server = createServer((req, res) => {
    if (req.url === '/ok') {
      res.setHeader('x-echo-ct', req.headers['content-type'] ?? '')
      res.end(JSON.stringify({ ct: req.headers['content-type'], auth: req.headers.authorization ?? null }))
    } else if (req.url === '/fail') {
      res.statusCode = 503
      res.end('upstream busy')
    } else if (req.url === '/slow') {
      setTimeout(() => res.end('{}'), 2_000)
    } else {
      res.end('not json')
    }
  })
  await new Promise<void>((r) => server.listen(0, '127.0.0.1', r))
  base = `http://127.0.0.1:${(server.address() as AddressInfo).port}`
})
afterAll(() => server.closeAllConnections?.() ?? server.close())

const rejection = (p: Promise<unknown>): Promise<VaporError> =>
  p.then(
    () => {
      throw new Error('expected a rejection')
    },
    (e: unknown) => e as VaporError,
  )

describe('fetchJson', () => {
  it('parses JSON and sends a JSON content type while keeping caller headers', async () => {
    const r = await fetchJson<{ ct: string; auth: string }>(`${base}/ok`, { headers: new Headers({ authorization: 'Bearer t' }) })
    expect(r).toEqual({ ct: 'application/json', auth: 'Bearer t' })
  })
  it('maps HTTP errors to VaporError(HTTP) with status and body', async () => {
    const e = await rejection(fetchJson(`${base}/fail`))
    expect(e).toBeInstanceOf(VaporError)
    expect(e.code).toBe('HTTP')
    expect(e.message).toContain('503')
    expect(e.message).toContain('upstream busy')
  })
  it('times out with VaporError(TIMEOUT)', async () => {
    const e = await rejection(fetchJson(`${base}/slow`, undefined, 100))
    expect(e.code).toBe('TIMEOUT')
  })
  it('reports malformed JSON as an error, not a crash', async () => {
    const e = await rejection(fetchJson(`${base}/garbage`))
    expect(e).toBeInstanceOf(VaporError)
  })
})
