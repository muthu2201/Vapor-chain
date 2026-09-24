// Copyright (c) 2026 VaporChain / muthu2201. Licensed under the Apache License, Version 2.0.
// SPDX-License-Identifier: Apache-2.0
// Provenance: VAPOR-6eabb1be532bdef4 (authorship watermark; see NOTICE)

import { decodeEventLog, encodeFunctionData, keccak256, stringToHex, type Address, type Hash, type Hex, type Log, type PublicClient, type WalletClient } from 'viem'
import { vaporTokenFactoryAbi } from './abi/index.js'
import { VaporError } from './errors.js'

export interface TokenLaunchParams {
  name: string
  symbol: string
  decimals?: number
  /** whole-token units are NOT applied: pass base units (e.g. 1_000_000n * 10n ** 18n) */
  initialSupply: bigint
  /** 0 = fixed supply forever; otherwise the owner may mint up to cap */
  cap?: bigint
  owner: Address
  metadataURI?: string
  /**
   * Any string; the factory mixes in the creator so it cannot be front-run.
   * Default: derived from (name, symbol, owner), so predictTokenAddress and
   * launchToken agree, and launching the exact same token twice reverts
   * instead of silently creating a duplicate. Pass a salt to launch again.
   */
  salt?: string
}

function toParams(p: TokenLaunchParams) {
  return {
    name: p.name,
    symbol: p.symbol,
    decimals: p.decimals ?? 18,
    initialSupply: p.initialSupply,
    cap: p.cap ?? 0n,
    owner: p.owner,
    metadataURI: p.metadataURI ?? '',
  }
}

/** The CREATE2 salt (before the factory mixes in the creator) for these params. */
export function tokenSalt(p: Pick<TokenLaunchParams, 'name' | 'symbol' | 'owner' | 'salt'>): Hex {
  return keccak256(stringToHex(p.salt ?? `vapor-token|${p.name}|${p.symbol}|${p.owner.toLowerCase()}`))
}

/** Predict the token address before launching (show it in your UI). */
export async function predictTokenAddress(client: PublicClient, factory: Address, creator: Address, p: TokenLaunchParams): Promise<Address> {
  return client.readContract({ address: factory, abi: vaporTokenFactoryAbi, functionName: 'predictAddress', args: [creator, toParams(p), tokenSalt(p)] })
}

/** Launch an ERC-20 (permit + burnable + optional capped mint) in one transaction. */
export async function launchToken(
  wallet: WalletClient,
  client: PublicClient,
  factory: Address,
  p: TokenLaunchParams,
): Promise<{ token: Address; hash: Hash }> {
  if (!wallet.account) throw new VaporError('wallet has no account', 'CONFIG')
  const hash = await wallet.writeContract({
    account: wallet.account, chain: wallet.chain, address: factory, abi: vaporTokenFactoryAbi,
    functionName: 'createToken', args: [toParams(p), tokenSalt(p)],
  })
  const rcpt = await client.waitForTransactionReceipt({ hash, confirmations: 2 })
  if (rcpt.status !== 'success') throw new VaporError('token launch reverted', 'CONFIG')
  const token = tokenFromLogs(rcpt.logs)
  if (!token) throw new VaporError('TokenCreated event not found', 'CONFIG')
  return { token, hash }
}

/** Calldata for factory.createToken, usable in a transaction, a 7702 batch or a UserOp. */
export function createTokenCall(factory: Address, p: TokenLaunchParams): { to: Address; data: Hex } {
  return { to: factory, data: encodeFunctionData({ abi: vaporTokenFactoryAbi, functionName: 'createToken', args: [toParams(p), tokenSalt(p)] }) }
}

/** The token address from a receipt's TokenCreated event, if present. */
export function tokenFromLogs(logs: ReadonlyArray<Pick<Log, 'data' | 'topics'>>): Address | undefined {
  for (const log of logs) {
    try {
      const ev = decodeEventLog({ abi: vaporTokenFactoryAbi, data: log.data, topics: log.topics })
      if (ev.eventName === 'TokenCreated') return ev.args.token
    } catch {
      /* other contracts' logs */
    }
  }
  return undefined
}
