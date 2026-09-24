// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Proprietary. See LICENSE. Provenance: VAPOR-6eabb1be532bdef4

/** Demo shop items; prices in USDC base units (6 decimals). */
export interface Item {
  id: string
  name: string
  blurb: string
  price: bigint
}

export const catalog: Item[] = [
  { id: 'coffee', name: 'Espresso', blurb: 'A 1.50 USDC micro-purchase: the fee stays 1%.', price: 1_500_000n },
  { id: 'ticket', name: 'Concert ticket', blurb: 'One signature, no gas token, instant finality.', price: 12_000_000n },
  { id: 'sword', name: 'Legendary sword', blurb: 'In-game item paid straight from your wallet.', price: 4_990_000n },
]
