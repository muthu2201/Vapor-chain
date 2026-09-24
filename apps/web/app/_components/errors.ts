// Copyright (c) 2026 VaporChain / muthu2201. Licensed under the Apache License, Version 2.0.
// SPDX-License-Identifier: Apache-2.0
// Provenance: VAPOR-6eabb1be532bdef4 (authorship watermark; see NOTICE)

import { VaporError } from '@vaporchain/sdk'

/** Human-readable error text; never dumps request bodies or stack traces into the UI. */
export function explain(e: unknown): string {
  if (e instanceof VaporError) {
    switch (e.code) {
      case 'QUOTA_EXHAUSTED':
        return 'This app has used its free gas for now. Try again later.'
      case 'SPONSORSHIP_DENIED':
        return `The app declined to sponsor this action (${e.message.replace(/^sponsorship denied:\s*/, '')}).`
      case 'NOT_REGISTERED':
        return 'This checkout is not registered to an app yet.'
      case 'TIMEOUT':
        return 'The network took too long to answer. Please retry.'
      default:
        return e.message
    }
  }
  const m = (e as { shortMessage?: string; message?: string })?.shortMessage ?? (e as Error)?.message ?? String(e)
  if (/transfer amount exceeds balance|insufficient/i.test(m)) return 'Not enough USDC for this purchase.'
  return m.split('\n')[0]!.slice(0, 200)
}
