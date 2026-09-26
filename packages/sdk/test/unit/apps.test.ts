// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 VaporChain / muthu2201
// Provenance: VAPOR-6eabb1be532bdef4
import { createServer, type Server } from 'node:http'
import type { AddressInfo } from 'node:net'
import { afterAll, beforeAll, describe, expect, it } from 'vitest'
import { appsRest, VaporError } from '../../src/index.js'

// ---------------------------------------------------------------------------
// Real HTTP server on an ephemeral localhost port — no fetch mocks allowed.
// ---------------------------------------------------------------------------

let server: Server
let base: string

/** Minimal fixture data keyed by URL path. */
const routes: Record<string, { status?: number; body: unknown }> = {
  // appsRest.get
  '/vaporchain/apps/v1/apps/42': {
    body: {
      app: {
        app_id: '42',
        owner: '0xOwner',
        revenue_recipient: '0xRevenue',
        metadata_uri: 'ipfs://Qm123',
        domain: 'example.com',
        referrer_bps: '100',
        status: 'APP_STATUS_ACTIVE',
        contract_count: '3',
      },
    },
  },
  // appsRest.get — optional fields absent
  '/vaporchain/apps/v1/apps/99': {
    body: {
      app: {
        app_id: '99',
        owner: '0xOwner2',
        revenue_recipient: '0xRevenue2',
        status: 'APP_STATUS_PAUSED',
        // metadata_uri, domain, referrer_bps, contract_count intentionally absent
      },
    },
  },
  // appsRest.bond — two unbondings
  '/vaporchain/apps/v1/apps/1/bond': {
    body: {
      denom: 'uvpor',
      bonded: '5000000',
      base_gas: '100000',
      unbonding: [
        { owner: '0xAlice', amount: '1000', release_height: '200' },
        { owner: '0xBob', amount: '500', release_height: '300' },
      ],
    },
  },
  // appsRest.bond — no unbonding array at all (tests default 0n)
  '/vaporchain/apps/v1/apps/2/bond': {
    body: { denom: 'uvpor', bonded: '0', base_gas: '0' },
  },
  // appsRest.bond — empty unbonding array
  '/vaporchain/apps/v1/apps/3/bond': {
    body: { denom: 'uvpor', bonded: '9999', base_gas: '4321', unbonding: [] },
  },
  // appsRest.quota
  '/vaporchain/apps/v1/apps/1/quota': {
    body: {
      quota: {
        epoch: '7',
        gas: '500000',
        unique_payers_estimate: '123',
        diversity_bps: 250,
      },
      epoch_ends_at_height: '8000',
    },
  },
  // appsRest.quota — all optional fields absent (tests default 0 / 0n)
  '/vaporchain/apps/v1/apps/2/quota': {
    body: { quota: {}, epoch_ends_at_height: '0' },
  },
  // HTTP error routes
  '/vaporchain/apps/v1/apps/404/bond': { status: 404, body: 'not found' },
  '/vaporchain/apps/v1/apps/500/bond': { status: 500, body: 'server error' },
}

beforeAll(
  async () =>
    new Promise<void>((resolve) => {
      server = createServer((req, res) => {
        const route = routes[req.url ?? '']
        if (!route) {
          res.statusCode = 404
          res.end(JSON.stringify({ error: 'fixture not found' }))
          return
        }
        res.statusCode = route.status ?? 200
        res.setHeader('content-type', 'application/json')
        res.end(typeof route.body === 'string' ? route.body : JSON.stringify(route.body))
      })
      server.listen(0, '127.0.0.1', () => {
        base = `http://127.0.0.1:${(server.address() as AddressInfo).port}`
        resolve()
      })
    }),
)

afterAll(() => server.closeAllConnections?.() ?? server.close())

/** Capture the rejection of a promise as a typed VaporError. */
const rejection = (p: Promise<unknown>): Promise<VaporError> =>
  p.then(
    () => {
      throw new Error('expected a rejection but promise resolved')
    },
    (e: unknown) => e as VaporError,
  )

// ---------------------------------------------------------------------------
// appsRest.bond
// ---------------------------------------------------------------------------
describe('appsRest.bond', () => {
  it('parses two unbonding entries with correct bigint fields', async () => {
    const client = appsRest(base)
    const bond = await client.bond(1n)
    expect(bond.denom).toBe('uvpor')
    expect(bond.bonded).toBe(5_000_000n)
    expect(bond.baseGas).toBe(100_000n)
    expect(bond.unbonding).toHaveLength(2)
    expect(bond.unbonding[0]).toEqual({ owner: '0xAlice', amount: 1000n, releaseHeight: 200n })
    expect(bond.unbonding[1]).toEqual({ owner: '0xBob', amount: 500n, releaseHeight: 300n })
  })

  it('returns empty unbonding array when field is absent (defaults to [])', async () => {
    const client = appsRest(base)
    const bond = await client.bond(2n)
    expect(bond.unbonding).toEqual([])
    expect(bond.bonded).toBe(0n)
    expect(bond.baseGas).toBe(0n)
  })

  it('returns empty unbonding array when field is explicitly []', async () => {
    const client = appsRest(base)
    const bond = await client.bond(3n)
    expect(bond.unbonding).toHaveLength(0)
    expect(bond.bonded).toBe(9999n)
  })

  it('throws VaporError(HTTP) on 404', async () => {
    const client = appsRest(base)
    const e = await rejection(client.bond(404n))
    expect(e).toBeInstanceOf(VaporError)
    expect(e.code).toBe('HTTP')
    expect(e.message).toContain('404')
  })

  it('throws VaporError(HTTP) on 500', async () => {
    const client = appsRest(base)
    const e = await rejection(client.bond(500n))
    expect(e).toBeInstanceOf(VaporError)
    expect(e.code).toBe('HTTP')
    expect(e.message).toContain('500')
  })
})

// ---------------------------------------------------------------------------
// appsRest.quota
// ---------------------------------------------------------------------------
describe('appsRest.quota', () => {
  it('parses all quota fields; diversityBps is a number not a bigint', async () => {
    const client = appsRest(base)
    const q = await client.quota(1n)
    expect(q.epoch).toBe(7n)
    expect(q.gas).toBe(500_000n)
    expect(q.uniquePayersEstimate).toBe(123n)
    // Critical: diversityBps must be number, NOT bigint
    expect(typeof q.diversityBps).toBe('number')
    expect(q.diversityBps).toBe(250)
    expect(q.epochEndsAtHeight).toBe(8000n)
  })

  it('defaults all missing fields to 0 / 0n without throwing', async () => {
    const client = appsRest(base)
    const q = await client.quota(2n)
    expect(q.epoch).toBe(0n)
    expect(q.gas).toBe(0n)
    expect(q.uniquePayersEstimate).toBe(0n)
    expect(q.diversityBps).toBe(0)
    expect(typeof q.diversityBps).toBe('number')
    expect(q.epochEndsAtHeight).toBe(0n)
  })
})

// ---------------------------------------------------------------------------
// appsRest.get
// ---------------------------------------------------------------------------
describe('appsRest.get', () => {
  it('parses all AppRecord fields correctly', async () => {
    const client = appsRest(base)
    const app = await client.get(42n)
    expect(app.appId).toBe(42n)
    expect(app.owner).toBe('0xOwner')
    expect(app.revenueRecipient).toBe('0xRevenue')
    expect(app.metadataUri).toBe('ipfs://Qm123')
    expect(app.domain).toBe('example.com')
    expect(app.referrerBps).toBe(100)
    expect(app.status).toBe('APP_STATUS_ACTIVE')
    expect(app.contractCount).toBe(3)
  })

  it('defaults optional fields to 0 / empty-string when absent', async () => {
    const client = appsRest(base)
    const app = await client.get(99n)
    expect(app.metadataUri).toBe('')
    expect(app.domain).toBe('')
    expect(app.referrerBps).toBe(0)
    expect(app.contractCount).toBe(0)
  })
})
