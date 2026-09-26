// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 VaporChain / muthu2201
// Provenance: VAPOR-6eabb1be532bdef4

import { erc20Abi, type Address, type PublicClient } from 'viem'

/**
 * One symbol = one asset. Users see "USDC", never ibc/<hash>. Each entry maps
 * the symbol to its VaporChain bank denom, its ERC-20 address (x/erc20 single
 * token representation) and where it can be brought in from (Skip:Go).
 */
export interface Asset {
  symbol: string
  name: string
  decimals: number
  denom: string
  erc20: Address
  /** Skip:Go source assets the SDK can "bring in" from. */
  sources: Array<{ chainId: string; denom: string }>
}

export type AssetRegistry = Record<string, Asset>

export function localnetAssets(tusdc: Address): AssetRegistry {
  return {
    USDC: { symbol: 'USDC', name: 'Testnet USDC', decimals: 6, denom: 'uusdc', erc20: tusdc, sources: [] },
  }
}

export interface UnifiedBalance {
  total: bigint
  local: bigint
  remote: Array<{ chainId: string; amount: bigint }>
}

/** Local balance (VaporChain) plus balances elsewhere supplied by the caller's wallet/connectors. */
export async function unifiedBalance(
  client: PublicClient,
  asset: Asset,
  owner: Address,
  remoteLookups: Array<() => Promise<{ chainId: string; amount: bigint }>> = [],
): Promise<UnifiedBalance> {
  const [local, ...remote] = await Promise.all([
    client.readContract({ address: asset.erc20, abi: erc20Abi, functionName: 'balanceOf', args: [owner] }),
    ...remoteLookups.map((f) => f().catch(() => null)),
  ])
  const rem = remote.filter((r): r is { chainId: string; amount: bigint } => r !== null)
  return { local, remote: rem, total: rem.reduce((s, r) => s + r.amount, local) }
}
