// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Provenance: VAPOR-6eabb1be532bdef4
//
// Localnet-only helper for the live e2e suites. Protocol-sponsored base gas
// is bought with capital bonded behind the app (x/apps; linear in the bond,
// no identity checks, no gatekeeper), so a test app whose users should be
// gasless bonds some test USDC first.
import { appsRest, type VaporClient, vaporLocalnet } from '@vaporchain/sdk'

export const TEST_BOND = 10_000_000_000n // 10,000 USDC

export async function bondApp(owner: VaporClient, appId: bigint, amount = TEST_BOND): Promise<bigint> {
  await owner.apps.bond(appId, amount)
  const rest = appsRest(vaporLocalnet.restUrl)
  for (let i = 0; i < 40; i++) {
    const q = await rest.quota(appId)
    if (q.gas > 0n) return q.gas
    await new Promise((r) => setTimeout(r, 500))
  }
  throw new Error(`app ${appId} has no quota after bonding`)
}
