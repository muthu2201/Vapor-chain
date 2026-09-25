// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Provenance: VAPOR-6eabb1be532bdef4
//
// Localnet-only helper for the live e2e suites. Protocol-sponsored base gas
// is granted only to domain-verified apps, so a test app that should have
// gasless users gets its domain set by the owner (Settle.setAppDomain) and
// confirmed by the localnet's attestor key (x/apps MsgAttestDomain).
import { execFileSync } from 'node:child_process'
import { resolve } from 'node:path'
import { appsRest, type VaporClient, vaporLocalnet } from '@vaporchain/sdk'

// vitest runs from packages/react; under happy-dom import.meta.url is a Vite
// /@fs/ URL, so derive the repo root from the working directory instead
const root = `${resolve(process.cwd(), '../..')}/`
const bin = `${root}chain/build/vaporchaind`
const home = `${root}.localnet/node0`

export async function verifyApp(owner: VaporClient, appId: bigint, domain = `app${appId}.e2e.vapor.test`): Promise<string> {
  await owner.apps.setDomain(appId, domain)
  const out = execFileSync(
    bin,
    ['tx', 'apps', 'attest-domain', appId.toString(), domain, '--from', 'attestor', '--keyring-backend', 'test', '--home', home,
      '--chain-id', vaporLocalnet.cosmosChainId, '--gas', 'auto', '--gas-adjustment', '1.5', '--gas-prices', '2000000000acredit', '-y', '-o', 'json'],
    { encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'] },
  )
  const res = JSON.parse(out.slice(out.indexOf('{'))) as { code: number; raw_log: string }
  if (res.code !== 0) throw new Error(`attest-domain rejected: ${res.raw_log}`)
  const rest = appsRest(vaporLocalnet.restUrl)
  for (let i = 0; i < 40; i++) {
    if ((await rest.get(appId)).domainVerified) return domain
    await new Promise((r) => setTimeout(r, 500))
  }
  throw new Error(`app ${appId} not verified after attestation`)
}
