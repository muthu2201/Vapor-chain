// Copyright (c) 2026 VaporChain / muthu2201. Licensed under the Apache License, Version 2.0.
// SPDX-License-Identifier: Apache-2.0
// Provenance: VAPOR-6eabb1be532bdef4 (authorship watermark; see NOTICE)

import { formatUnits } from 'viem'

export const usdc = (v: bigint | undefined) => (v === undefined ? '—' : `${Number(formatUnits(v, 6)).toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 6 })} USDC`)
export const short = (a?: string) => (a ? `${a.slice(0, 6)}…${a.slice(-4)}` : '')
export const gas = (v: bigint | undefined) => (v === undefined ? '—' : `${Number(v).toLocaleString('en-US')} gas`)
