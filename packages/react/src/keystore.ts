// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Proprietary. See LICENSE. Provenance: VAPOR-6eabb1be532bdef4

/**
 * Browser key storage for the embedded (no-seed-phrase) wallet.
 *
 * Threat model, stated plainly so integrators can decide:
 * - The secp256k1 key is encrypted with AES-256-GCM under a WebCrypto key
 *   generated with `extractable: false` and stored in IndexedDB. Script on the
 *   page can ASK the browser to decrypt, but can never read the AES key bytes,
 *   so a copied IndexedDB/disk image is useless without this origin's browser
 *   profile.
 * - It does not stop code already running on the page (XSS, a malicious
 *   dependency) from using the wallet while the page is open. Ship a strict
 *   CSP (the reference app does) and keep balances consumer-sized, which is
 *   what this wallet is for. Users can export the key to move to a hardware or
 *   extension wallet at any time.
 * - Clearing site data deletes the wallet. The UI must offer export first.
 */

const DB = 'vaporchain-wallet'
const STORE = 'keys'
const VERSION = 1

export interface SealedKey {
  id: string
  wrapKey: CryptoKey
  iv: Uint8Array<ArrayBuffer>
  ciphertext: ArrayBuffer
  address: string
  createdAt: number
}

export interface KeyStore {
  get(id: string): Promise<SealedKey | undefined>
  put(rec: SealedKey): Promise<void>
  delete(id: string): Promise<void>
}

function req<T>(r: IDBRequest<T>): Promise<T> {
  return new Promise((resolve, reject) => {
    r.onsuccess = () => resolve(r.result)
    r.onerror = () => reject(r.error)
  })
}

/** IndexedDB-backed store (structured clone keeps CryptoKey non-extractable). */
export function indexedDbKeyStore(factory: IDBFactory = globalThis.indexedDB): KeyStore {
  let dbp: Promise<IDBDatabase> | undefined
  const open = () =>
    (dbp ??= new Promise<IDBDatabase>((resolve, reject) => {
      if (!factory) return reject(new Error('IndexedDB is not available in this environment'))
      const o = factory.open(DB, VERSION)
      o.onupgradeneeded = () => {
        if (!o.result.objectStoreNames.contains(STORE)) o.result.createObjectStore(STORE, { keyPath: 'id' })
      }
      o.onsuccess = () => resolve(o.result)
      o.onerror = () => reject(o.error)
    }))
  const tx = async (mode: IDBTransactionMode) => (await open()).transaction(STORE, mode).objectStore(STORE)
  return {
    async get(id) {
      return (await req((await tx('readonly')).get(id))) as SealedKey | undefined
    },
    async put(rec) {
      await req((await tx('readwrite')).put(rec))
    },
    async delete(id) {
      await req((await tx('readwrite')).delete(id))
    },
  }
}

const subtle = () => {
  const s = globalThis.crypto?.subtle
  if (!s) throw new Error('WebCrypto is unavailable (a secure context — https or localhost — is required)')
  return s
}

const hexToBytes = (hex: string) => {
  const h = hex.startsWith('0x') ? hex.slice(2) : hex
  if (!/^[0-9a-fA-F]{64}$/.test(h)) throw new Error('private key must be 32 bytes of hex')
  const out = new Uint8Array(32)
  for (let i = 0; i < 32; i++) out[i] = parseInt(h.slice(i * 2, i * 2 + 2), 16)
  return out
}
const bytesToHex = (b: Uint8Array) => `0x${Array.from(b, (x) => x.toString(16).padStart(2, '0')).join('')}` as const

/** Encrypt a private key under a fresh non-extractable AES-GCM key. */
export async function seal(id: string, privateKey: `0x${string}`, address: string): Promise<SealedKey> {
  const wrapKey = await subtle().generateKey({ name: 'AES-GCM', length: 256 }, false, ['encrypt', 'decrypt'])
  const iv = globalThis.crypto.getRandomValues(new Uint8Array(12))
  const pt = hexToBytes(privateKey)
  // bind the ciphertext to its record id and address so records cannot be swapped
  const ciphertext = await subtle().encrypt({ name: 'AES-GCM', iv, additionalData: new TextEncoder().encode(`${id}|${address.toLowerCase()}`) }, wrapKey, pt)
  pt.fill(0)
  return { id, wrapKey, iv, ciphertext, address, createdAt: Date.now() }
}

export async function unseal(rec: SealedKey): Promise<`0x${string}`> {
  const pt = new Uint8Array(
    await subtle().decrypt(
      { name: 'AES-GCM', iv: rec.iv, additionalData: new TextEncoder().encode(`${rec.id}|${rec.address.toLowerCase()}`) },
      rec.wrapKey,
      rec.ciphertext,
    ),
  )
  const hex = bytesToHex(pt)
  pt.fill(0)
  return hex
}
