// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Proprietary. See LICENSE. Provenance: VAPOR-6eabb1be532bdef4

import { encodeFunctionData, type Address, type Hex, type PublicClient } from 'viem'
import { settleAbi } from './abi/index.js'
import { SETTLE_ADDRESS } from './constants.js'

/** Quote the protocol fee exactly as the chain will charge it. */
export async function quoteFee(client: PublicClient, token: Address, amount: bigint): Promise<{ fee: bigint; net: bigint }> {
  const [fee, net] = await client.readContract({ address: SETTLE_ADDRESS, abi: settleAbi, functionName: 'quoteFee', args: [token, amount] })
  return { fee, net }
}

export async function appOf(client: PublicClient, contract: Address): Promise<{ appId: bigint; active: boolean }> {
  const [appId, active] = await client.readContract({ address: SETTLE_ADDRESS, abi: settleAbi, functionName: 'appOf', args: [contract] })
  return { appId: BigInt(appId), active }
}

export async function claimable(client: PublicClient, appId: bigint, token: Address): Promise<bigint> {
  return client.readContract({ address: SETTLE_ADDRESS, abi: settleAbi, functionName: 'claimable', args: [appId, token] })
}

export async function appAllowance(client: PublicClient, owner: Address, appId: bigint, token: Address): Promise<bigint> {
  return client.readContract({ address: SETTLE_ADDRESS, abi: settleAbi, functionName: 'appAllowance', args: [owner, appId, token] })
}

export async function quoteCredits(client: PublicClient, token: Address, amount: bigint): Promise<bigint> {
  return client.readContract({ address: SETTLE_ADDRESS, abi: settleAbi, functionName: 'quoteCredits', args: [token, amount] })
}

export async function provenance(client: PublicClient): Promise<Hex> {
  return client.readContract({ address: SETTLE_ADDRESS, abi: settleAbi, functionName: 'provenance' })
}

/** Calldata builders — usable in a single tx, a 7702 batch or a UserOp. */
export const settleCalls = {
  approveApp: (appId: bigint, token: Address, amount: bigint) => ({
    to: SETTLE_ADDRESS,
    data: encodeFunctionData({ abi: settleAbi, functionName: 'approveApp', args: [appId, token, amount] }),
  }),
  pay: (token: Address, amount: bigint, payee: Address, referrer: Address = '0x0000000000000000000000000000000000000000') => ({
    to: SETTLE_ADDRESS,
    data: encodeFunctionData({ abi: settleAbi, functionName: 'pay', args: [token, amount, payee, referrer] }),
  }),
  claim: (appId: bigint, token: Address) => ({
    to: SETTLE_ADDRESS,
    data: encodeFunctionData({ abi: settleAbi, functionName: 'claim', args: [appId, token] }),
  }),
  registerApp: (recipient: Address, metadataUri: string, referrerBps: number) => ({
    to: SETTLE_ADDRESS,
    data: encodeFunctionData({ abi: settleAbi, functionName: 'registerApp', args: [recipient, metadataUri, referrerBps] }),
  }),
  acceptContractClaim: (appId: bigint, contract: Address) => ({
    to: SETTLE_ADDRESS,
    data: encodeFunctionData({ abi: settleAbi, functionName: 'acceptContractClaim', args: [appId, contract] }),
  }),
  buyCredits: (token: Address, amount: bigint) => ({
    to: SETTLE_ADDRESS,
    data: encodeFunctionData({ abi: settleAbi, functionName: 'buyCredits', args: [token, amount] }),
  }),
}
