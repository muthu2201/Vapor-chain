// Copyright (c) 2026 VaporChain / muthu2201. Licensed under the Apache License, Version 2.0 (see LICENSE).
// Provenance: VAPOR-6eabb1be532bdef4
import { IDBFactory } from 'fake-indexeddb'
import { generatePrivateKey, privateKeyToAccount } from 'viem/accounts'
import { describe, expect, it } from 'vitest'
import { indexedDbKeyStore, seal, unseal } from '../../src/index.js'

const key = generatePrivateKey()
const addr = privateKeyToAccount(key).address

describe('seal / unseal', () => {
  it('round-trips the private key', async () => {
    expect(await unseal(await seal('default', key, addr))).toBe(key)
  })

  it('never exposes the wrapping key or the plaintext', async () => {
    const rec = await seal('default', key, addr)
    expect(rec.wrapKey.extractable).toBe(false)
    await expect(crypto.subtle.exportKey('raw', rec.wrapKey)).rejects.toThrow()
    const ct = Buffer.from(rec.ciphertext).toString('hex')
    expect(ct.includes(key.slice(2))).toBe(false)
    expect(rec.ciphertext.byteLength).toBe(32 + 16) // key + GCM tag
  })

  it('binds ciphertext to its record: swapping id or address fails authentication', async () => {
    const rec = await seal('default', key, addr)
    await expect(unseal({ ...rec, id: 'other' })).rejects.toThrow()
    await expect(unseal({ ...rec, address: privateKeyToAccount(generatePrivateKey()).address })).rejects.toThrow()
    const tampered = new Uint8Array(rec.ciphertext.slice(0))
    tampered[0]! ^= 1
    await expect(unseal({ ...rec, ciphertext: tampered.buffer })).rejects.toThrow()
  })

  it('uses a fresh IV per seal', async () => {
    const a = await seal('default', key, addr)
    const b = await seal('default', key, addr)
    expect(Buffer.from(a.iv).equals(Buffer.from(b.iv))).toBe(false)
  })

  it('rejects malformed keys', async () => {
    await expect(seal('default', '0x1234', addr)).rejects.toThrow('32 bytes')
  })
})

describe('indexedDbKeyStore', () => {
  it('persists records (CryptoKey survives structured clone) and deletes them', async () => {
    const factory = new IDBFactory()
    const store = indexedDbKeyStore(factory)
    await store.put(await seal('default', key, addr))
    // a second store instance = a page reload
    const reloaded = indexedDbKeyStore(factory)
    const rec = await reloaded.get('default')
    expect(rec?.address).toBe(addr)
    expect(await unseal(rec!)).toBe(key)
    await reloaded.delete('default')
    expect(await reloaded.get('default')).toBeUndefined()
  })
})
