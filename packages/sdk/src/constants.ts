// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2026 VaporChain / muthu2201
// Provenance: VAPOR-6eabb1be532bdef4

import type { Address, Hex } from 'viem'

/** Native Settle precompile (x/settle + x/apps). */
export const SETTLE_ADDRESS: Address = '0x0000000000000000000000000000000000000900'
/** Canonical ERC-4337 EntryPoint v0.8 (same address on every chain). */
export const ENTRY_POINT_ADDRESS: Address = '0x4337084D9E255Ff0702461CF8895CE9E3b5Ff108'
/** Canonical eth-infinitism Simple7702Account v0.8 (allowlisted by VaporChain paymasters). */
export const SIMPLE_7702_IMPLEMENTATION: Address = '0x4Cd241E8d1510e30b2076397afc7508Ae59C66c9'
export const CREATE2_DEPLOYER: Address = '0x4e59b44847b379578588920ca78fbf26c0b4956c'
export const MULTICALL3: Address = '0xcA11bde05977b3631167028862bE2a173976CA11'
export const PERMIT2: Address = '0x000000000022D473030F116dDEE9F6B43aC78BA3'
/** VaporChain authorship fingerprint (also returned by SETTLE.provenance()). */
export const PROVENANCE: Hex = '0x6eabb1be532bdef432109abc178d88669ab33aed940f18cd5169887d215c1fcf'
/** Infinite app approval (Settle never decrements >= 2^255). */
export const MAX_APPROVAL = 2n ** 256n - 1n
