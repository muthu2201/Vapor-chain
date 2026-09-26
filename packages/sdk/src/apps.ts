// Copyright (c) 2026 VaporChain / muthu2201. Licensed under the Apache License, Version 2.0.
// SPDX-License-Identifier: Apache-2.0
// Provenance: VAPOR-6eabb1be532bdef4 (authorship watermark; see NOTICE)

import { decodeEventLog, type Address, type PublicClient, type WalletClient, type Hash } from 'viem'
import { settleAbi } from './abi/index.js'
import { SETTLE_ADDRESS } from './constants.js'
import { VaporError, fetchJson } from './errors.js'

export interface AppRecord {
  appId: bigint
  owner: string
  revenueRecipient: string
  metadataUri: string
  domain: string
  referrerBps: number
  status: 'APP_STATUS_ACTIVE' | 'APP_STATUS_PAUSED' | 'APP_STATUS_REVOKED' | string
  contractCount: number
}

export interface AppBond {
  denom: string
  /** capital locked behind the app (base units of `denom`) */
  bonded: bigint
  /** base sponsorship quota it buys, in gas per epoch */
  baseGas: bigint
  unbonding: { owner: string; amount: bigint; releaseHeight: bigint }[]
}

export interface Quota {
  epoch: bigint
  gas: bigint
  uniquePayersEstimate: bigint
  diversityBps: number
  epochEndsAtHeight: bigint
}

/** Read-side of x/apps over the Cosmos REST gateway. */
export function appsRest(restUrl: string) {
  const base = restUrl.replace(/\/$/, '')
  return {
    async get(appId: bigint): Promise<AppRecord> {
      const r = await fetchJson<{ app: Record<string, unknown> }>(`${base}/vaporchain/apps/v1/apps/${appId}`)
      const a = r.app
      return {
        appId: BigInt(a.app_id as string),
        owner: a.owner as string,
        revenueRecipient: a.revenue_recipient as string,
        metadataUri: (a.metadata_uri as string) ?? '',
        domain: (a.domain as string) ?? '',
        referrerBps: Number(a.referrer_bps ?? 0),
        status: a.status as string,
        contractCount: Number(a.contract_count ?? 0),
      }
    },
    async bond(appId: bigint): Promise<AppBond> {
      const r = await fetchJson<{ denom: string; bonded: string; base_gas: string; unbonding?: { owner: string; amount: string; release_height: string }[] }>(
        `${base}/vaporchain/apps/v1/apps/${appId}/bond`,
      )
      return {
        denom: r.denom,
        bonded: BigInt(r.bonded ?? 0),
        baseGas: BigInt(r.base_gas ?? 0),
        unbonding: (r.unbonding ?? []).map((u) => ({ owner: u.owner, amount: BigInt(u.amount), releaseHeight: BigInt(u.release_height) })),
      }
    },
    async quota(appId: bigint): Promise<Quota> {
      const r = await fetchJson<{ quota: Record<string, string | number>; epoch_ends_at_height: string }>(`${base}/vaporchain/apps/v1/apps/${appId}/quota`)
      return {
        epoch: BigInt(r.quota.epoch ?? 0),
        gas: BigInt(r.quota.gas ?? 0),
        uniquePayersEstimate: BigInt(r.quota.unique_payers_estimate ?? 0),
        diversityBps: Number(r.quota.diversity_bps ?? 0),
        epochEndsAtHeight: BigInt(r.epoch_ends_at_height ?? 0),
      }
    },
    async appByContract(contract: Address): Promise<bigint | null> {
      try {
        const r = await fetchJson<{ binding: { app_id: string } }>(`${base}/vaporchain/apps/v1/contracts/${contract.toLowerCase()}`)
        return BigInt(r.binding.app_id)
      } catch (e) {
        if (e instanceof VaporError && e.message.includes('-> 404')) return null
        if (e instanceof VaporError && /not (bound|attributed|found)/i.test(e.message)) return null
        throw e
      }
    },
  }
}

/** Register an app from an EVM wallet; returns the new app id. */
export async function registerApp(
  wallet: WalletClient,
  client: PublicClient,
  p: { recipient: Address; metadataUri: string; referrerBps?: number },
): Promise<{ appId: bigint; hash: Hash }> {
  const account = wallet.account
  if (!account) throw new VaporError('wallet has no account', 'CONFIG')
  const hash = await wallet.writeContract({
    account, chain: wallet.chain, address: SETTLE_ADDRESS, abi: settleAbi,
    functionName: 'registerApp', args: [p.recipient, p.metadataUri, p.referrerBps ?? 0],
  })
  // confirmations: 2 -> the receipt is indexed at FinalizeBlock but state is
  // queryable only after Commit; waiting one more block removes the race.
  const rcpt = await client.waitForTransactionReceipt({ hash, confirmations: 2 })
  for (const log of rcpt.logs) {
    if (log.address.toLowerCase() !== SETTLE_ADDRESS.toLowerCase()) continue
    try {
      const ev = decodeEventLog({ abi: settleAbi, data: log.data, topics: log.topics })
      if (ev.eventName === 'AppRegistered') return { appId: BigInt(ev.args.appId), hash }
    } catch {
      /* not ours */
    }
  }
  throw new VaporError('AppRegistered event not found (did the tx revert?)', 'NOT_REGISTERED')
}
