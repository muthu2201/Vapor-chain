// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Provenance: VAPOR-6eabb1be532bdef4
import { act, renderHook, waitFor } from '@testing-library/react'
import { IDBFactory } from 'fake-indexeddb'
import { generatePrivateKey, privateKeyToAccount } from 'viem/accounts'
import { describe, expect, it } from 'vitest'
import { indexedDbKeyStore, useEmbeddedWallet } from '../../src/index.js'

describe('useEmbeddedWallet', () => {
  it('creates, survives reload, exports, and forgets', async () => {
    const store = indexedDbKeyStore(new IDBFactory())
    const first = renderHook(() => useEmbeddedWallet({ store }))
    await waitFor(() => expect(first.result.current.status).toBe('none'))

    let created: string | undefined
    await act(async () => {
      created = (await first.result.current.create()).address
    })
    expect(first.result.current.status).toBe('ready')
    expect(first.result.current.address).toBe(created)

    // a second wallet cannot silently overwrite the first
    await expect(first.result.current.create()).rejects.toThrow('already exists')

    // "page reload": new hook instance, same storage
    const second = renderHook(() => useEmbeddedWallet({ store }))
    await waitFor(() => expect(second.result.current.status).toBe('ready'))
    expect(second.result.current.address).toBe(created)

    const exported = await second.result.current.exportKey()
    expect(privateKeyToAccount(exported).address).toBe(created)

    await act(async () => second.result.current.forget())
    expect(second.result.current.status).toBe('none')
    expect(second.result.current.account).toBeUndefined()
  })

  it('imports a backup', async () => {
    const store = indexedDbKeyStore(new IDBFactory())
    const key = generatePrivateKey()
    const { result } = renderHook(() => useEmbeddedWallet({ store }))
    await waitFor(() => expect(result.current.status).toBe('none'))
    await act(async () => void (await result.current.importKey(key)))
    expect(result.current.address).toBe(privateKeyToAccount(key).address)
  })

  it('keeps separate wallets per id', async () => {
    const store = indexedDbKeyStore(new IDBFactory())
    const a = renderHook(() => useEmbeddedWallet({ store, id: 'a' }))
    const b = renderHook(() => useEmbeddedWallet({ store, id: 'b' }))
    await waitFor(() => expect(a.result.current.status).toBe('none'))
    await waitFor(() => expect(b.result.current.status).toBe('none'))
    await act(async () => void (await a.result.current.create()))
    await act(async () => void (await b.result.current.create()))
    expect(a.result.current.address).not.toBe(b.result.current.address)
  })
})
