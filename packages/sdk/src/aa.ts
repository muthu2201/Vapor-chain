// Copyright (c) 2026 VaporChain / muthu2201. Licensed under the Apache License, Version 2.0.
// SPDX-License-Identifier: Apache-2.0
// Provenance: VAPOR-6eabb1be532bdef4 (authorship watermark; see NOTICE)

import { http, type Account, type Address, type Chain, type Hex, type PublicClient, type Transport } from 'viem'
import type { PrivateKeyAccount } from 'viem/accounts'
import {
  createBundlerClient,
  createPaymasterClient,
  toSimple7702SmartAccount,
  type SmartAccount,
  type UserOperationReceipt,
} from 'viem/account-abstraction'
import { ENTRY_POINT_ADDRESS, SIMPLE_7702_IMPLEMENTATION } from './constants.js'
import { VaporError } from './errors.js'

export interface Call {
  to: Address
  data?: Hex
  value?: bigint
}

/**
 * The user's own EOA, upgraded with EIP-7702 to the canonical Simple7702Account:
 * same address in MetaMask/Rabby, batching, and sponsored gas.
 */
export async function toVaporAccount(client: PublicClient, owner: PrivateKeyAccount): Promise<SmartAccount> {
  return toSimple7702SmartAccount({
    client,
    owner,
    implementation: SIMPLE_7702_IMPLEMENTATION,
    entryPoint: { abi: (await import('viem/account-abstraction')).entryPoint08Abi, address: ENTRY_POINT_ADDRESS, version: '0.8' },
  })
}

export interface SponsoredSender {
  send(calls: Call[]): Promise<{ userOpHash: Hex; receipt: UserOperationReceipt }>
}

/**
 * Sends calls as a UserOperation whose gas is paid by the app's earned quota
 * (VaporVerifyingPaymaster via the ERC-7677 sponsor service). The first op
 * also carries the EIP-7702 authorization.
 */
export function sponsoredSender(p: {
  client: PublicClient
  chain: Chain
  account: SmartAccount
  owner: PrivateKeyAccount
  appId: bigint
  bundlerUrl: string
  sponsorUrl: string
  transport?: Transport
}): SponsoredSender {
  const paymaster = createPaymasterClient({ transport: http(p.sponsorUrl) })
  const bundler = createBundlerClient({
    client: p.client,
    chain: p.chain,
    account: p.account,
    paymaster,
    paymasterContext: { appId: Number(p.appId) },
    transport: p.transport ?? http(p.bundlerUrl),
  })
  return {
    async send(calls) {
      const delegated = await isDelegated(p.client, p.account.address)
      const authorization = delegated
        ? undefined
        : await p.owner.signAuthorization({
            chainId: p.chain.id,
            contractAddress: SIMPLE_7702_IMPLEMENTATION,
            nonce: await p.client.getTransactionCount({ address: p.account.address }),
          })
      try {
        const userOpHash = await bundler.sendUserOperation({
          calls: calls.map((c) => ({ to: c.to, data: c.data ?? '0x', value: c.value ?? 0n })),
          ...(authorization ? { authorization } : {}),
        })
        const receipt = await bundler.waitForUserOperationReceipt({ hash: userOpHash, timeout: 60_000 })
        if (!receipt.success) throw new VaporError(`UserOperation reverted: ${receipt.reason ?? 'unknown'}`, 'SPONSORSHIP_DENIED')
        return { userOpHash, receipt }
      } catch (e) {
        const msg = (e as Error).message ?? String(e)
        if (msg.includes('sponsorship denied')) {
          throw new VaporError(msg.slice(msg.indexOf('sponsorship denied')), msg.includes('quota') ? 'QUOTA_EXHAUSTED' : 'SPONSORSHIP_DENIED', e)
        }
        throw e
      }
    },
  }
}

/** True if the EOA already delegates (EIP-7702) to the VaporChain account implementation. */
export async function isDelegated(client: PublicClient, address: Address): Promise<boolean> {
  const code = await client.getCode({ address })
  return !!code && code.toLowerCase() === `0xef0100${SIMPLE_7702_IMPLEMENTATION.slice(2).toLowerCase()}`
}

export type { Account }
