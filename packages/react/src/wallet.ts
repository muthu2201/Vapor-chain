// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 VaporChain / muthu2201
// Provenance: VAPOR-6eabb1be532bdef4

import { useCallback, useEffect, useMemo, useState } from 'react'
import { generatePrivateKey, privateKeyToAccount, type PrivateKeyAccount } from 'viem/accounts'
import { indexedDbKeyStore, seal, unseal, type KeyStore } from './keystore.js'

export type EmbeddedWalletStatus = 'loading' | 'none' | 'ready' | 'error'

/**
 * A consumer wallet with no seed phrase and no gas: the key lives encrypted
 * in this browser (see keystore.ts for the threat model) and the account is
 * upgraded with EIP-7702 on its first sponsored action.
 */
export function useEmbeddedWallet(opts: { id?: string; store?: KeyStore } = {}) {
  const id = opts.id ?? 'default'
  const store = useMemo(() => opts.store ?? indexedDbKeyStore(), [opts.store])
  const [status, setStatus] = useState<EmbeddedWalletStatus>('loading')
  const [account, setAccount] = useState<PrivateKeyAccount | undefined>()
  const [error, setError] = useState<Error | null>(null)

  useEffect(() => {
    let live = true
    ;(async () => {
      try {
        const rec = await store.get(id)
        const acct = rec ? privateKeyToAccount(await unseal(rec)) : undefined
        if (!live) return
        setAccount(acct)
        setStatus(acct ? 'ready' : 'none')
      } catch (e) {
        if (!live) return
        setError(e as Error)
        setStatus('error')
      }
    })()
    return () => {
      live = false
    }
  }, [store, id])

  const install = useCallback(
    async (key: `0x${string}`) => {
      const acct = privateKeyToAccount(key)
      await store.put(await seal(id, key, acct.address))
      setAccount(acct)
      setStatus('ready')
      setError(null)
      return acct
    },
    [store, id],
  )

  /** Create a brand-new wallet. Refuses to overwrite an existing one. */
  const create = useCallback(async () => {
    if (await store.get(id)) throw new Error('a wallet already exists; export and forget it first')
    return install(generatePrivateKey())
  }, [store, id, install])

  /** Import an existing key (e.g. restoring a backup). Refuses to overwrite. */
  const importKey = useCallback(
    async (key: `0x${string}`) => {
      if (await store.get(id)) throw new Error('a wallet already exists; export and forget it first')
      return install(key)
    },
    [store, id, install],
  )

  /** Reveal the private key for backup. Call only after an explicit user confirmation. */
  const exportKey = useCallback(async () => {
    const rec = await store.get(id)
    if (!rec) throw new Error('no wallet')
    return unseal(rec)
  }, [store, id])

  /** Delete the wallet from this browser. Irreversible without a backup. */
  const forget = useCallback(async () => {
    await store.delete(id)
    setAccount(undefined)
    setStatus('none')
  }, [store, id])

  return { status, account, address: account?.address, error, create, importKey, exportKey, forget }
}
