// Copyright (c) 2026 VaporChain / muthu2201. All rights reserved.
// Proprietary. See LICENSE. Provenance: VAPOR-6eabb1be532bdef4
//
// Wires the reference app to a running localnet (scripts/localnet/*.sh):
//   1. "Vapor Demo Shop" app + a VaporCheckout contract attributed to it
//   2. "Vapor Launchpad" app owning the token factory (CREATE-nonce proof), so
//      token launches are sponsorable from the launchpad's quota
//   3. the faucet key exported for the server-side /api/faucet route
//   4. apps/web/.env.local
// Idempotent: existing apps/bindings are reused.
//
//   node apps/web/scripts/bootstrap-localnet.ts
import { execFileSync } from 'node:child_process'
import { existsSync, readFileSync, writeFileSync, mkdirSync, chmodSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { createPublicClient, createWalletClient, getContractAddress, http, type Address, type Hex } from 'viem'
import { privateKeyToAccount } from 'viem/accounts'
import { appsRest, createVaporClient, vaporLocalnet } from '@vaporchain/sdk'

const here = dirname(fileURLToPath(import.meta.url))
const root = resolve(here, '../../..')
const net = resolve(root, '.localnet')
const bin = resolve(root, 'chain/build/vaporchaind')
const secrets = resolve(net, 'secrets')
const envFile = resolve(here, '../.env.local')

const readKey = (f: string): Hex => `0x${readFileSync(f, 'utf8').trim().replace(/^0x/, '')}`
const dep = JSON.parse(readFileSync(resolve(root, 'contracts/deployments/779700.json'), 'utf8')) as Record<string, Address>
const accounts = JSON.parse(readFileSync(resolve(net, 'accounts.json'), 'utf8')) as Record<string, string>
const deployer = privateKeyToAccount(readKey(resolve(secrets, 'deployer')))
const chain = vaporLocalnet.chain
const pub = createPublicClient({ chain, transport: http() })
const wallet = createWalletClient({ account: deployer, chain, transport: http() })
const client = createVaporClient({ network: vaporLocalnet, account: deployer, contracts: { tokenFactory: dep.tokenFactory!, usdc: dep.usdc! } })
const rest = appsRest(vaporLocalnet.restUrl)

const previous: Record<string, string> = existsSync(envFile)
  ? Object.fromEntries(
      readFileSync(envFile, 'utf8')
        .split('\n')
        .filter((l) => l.includes('=') && !l.startsWith('#'))
        .map((l) => [l.slice(0, l.indexOf('=')), l.slice(l.indexOf('=') + 1)]),
    )
  : {}

async function appExists(id?: string): Promise<boolean> {
  if (!id) return false
  return rest.get(BigInt(id)).then(() => true, () => false)
}

async function shop(): Promise<{ appId: bigint; checkout: Address }> {
  const prevApp = previous.NEXT_PUBLIC_SHOP_APP_ID
  const prevCheckout = previous.NEXT_PUBLIC_SHOP_CHECKOUT as Address | undefined
  if (prevApp && prevCheckout && (await appExists(prevApp)) && (await rest.appByContract(prevCheckout)) === BigInt(prevApp)) {
    console.log(`shop: reusing app ${prevApp}, checkout ${prevCheckout}`)
    return { appId: BigInt(prevApp), checkout: prevCheckout }
  }
  const { appId } = await client.apps.register({ recipient: deployer.address, metadataUri: 'ipfs://vapor-demo-shop', referrerBps: 500 })
  const art = JSON.parse(readFileSync(resolve(root, 'contracts/out/VaporCheckout.sol/VaporCheckout.json'), 'utf8'))
  const hash = await wallet.deployContract({ abi: art.abi, bytecode: art.bytecode.object, args: [appId, deployer.address] })
  const checkout = (await pub.waitForTransactionReceipt({ hash, confirmations: 2 })).contractAddress!
  await client.apps.acceptContract(appId, checkout)
  console.log(`shop: app ${appId}, checkout ${checkout}`)
  return { appId, checkout }
}

async function launchpad(): Promise<bigint> {
  const factory = dep.tokenFactory!
  const bound = await rest.appByContract(factory)
  if (bound !== null) {
    console.log(`launchpad: factory already attributed to app ${bound}`)
    return bound
  }
  const { appId } = await client.apps.register({ recipient: deployer.address, metadataUri: 'ipfs://vapor-launchpad', referrerBps: 0 })
  // prove we deployed the factory: find the CREATE nonce that produced it
  const current = await pub.getTransactionCount({ address: deployer.address })
  let nonce = -1
  for (let n = 0; n < current; n++) {
    if (getContractAddress({ from: deployer.address, nonce: BigInt(n) }).toLowerCase() === factory.toLowerCase()) {
      nonce = n
      break
    }
  }
  if (nonce < 0) throw new Error(`factory ${factory} was not deployed by ${deployer.address} with CREATE`)
  const out = execFileSync(
    bin,
    [
      'tx', 'apps', 'add-contract', appId.toString(), factory,
      '--proof', JSON.stringify({ type: 'PROOF_TYPE_DEPLOYER_CREATE', nonce: String(nonce) }),
      '--from', 'dev', '--keyring-backend', 'test', '--home', resolve(net, 'node0'),
      '--chain-id', 'vapor-local-1', '--node', 'tcp://127.0.0.1:26657',
      '--gas', 'auto', '--gas-adjustment', '1.5', '--gas-prices', '1000000000acredit', '-y', '-o', 'json',
    ],
    { encoding: 'utf8' },
  )
  const { code, raw_log } = JSON.parse(out) as { code: number; raw_log: string }
  if (code !== 0) throw new Error(`add-contract failed: ${raw_log}`)
  for (let i = 0; i < 30 && (await rest.appByContract(factory)) !== appId; i++) await new Promise((r) => setTimeout(r, 500))
  if ((await rest.appByContract(factory)) !== appId) throw new Error('factory attribution did not land')
  console.log(`launchpad: app ${appId} owns factory ${factory} (CREATE nonce ${nonce})`)
  return appId
}

function faucetKey(): string {
  const f = resolve(secrets, 'faucet')
  if (!existsSync(f)) {
    mkdirSync(secrets, { recursive: true })
    const key = execFileSync(bin, ['keys', 'unsafe-export-eth-key', 'faucet', '--keyring-backend', 'test', '--home', resolve(net, 'node0')], { encoding: 'utf8' }).trim()
    writeFileSync(f, key + '\n', { mode: 0o600 })
    chmodSync(f, 0o600)
  }
  return f
}

const s = await shop()
const lp = await launchpad()
const env = {
  NEXT_PUBLIC_VAPOR_NETWORK_NAME: 'VaporChain Localnet',
  NEXT_PUBLIC_VAPOR_EVM_CHAIN_ID: String(chain.id),
  NEXT_PUBLIC_VAPOR_COSMOS_CHAIN_ID: vaporLocalnet.cosmosChainId,
  NEXT_PUBLIC_VAPOR_RPC: chain.rpcUrls.default.http[0]!,
  NEXT_PUBLIC_VAPOR_REST: vaporLocalnet.restUrl,
  NEXT_PUBLIC_VAPOR_COMET: vaporLocalnet.cometUrl,
  NEXT_PUBLIC_VAPOR_BUNDLER: vaporLocalnet.bundlerUrl,
  NEXT_PUBLIC_VAPOR_SPONSOR: vaporLocalnet.sponsorUrl,
  NEXT_PUBLIC_VAPOR_INDEXER: vaporLocalnet.indexerUrl,
  NEXT_PUBLIC_VAPOR_EXPLORER: '',
  NEXT_PUBLIC_VAPOR_USDC: dep.usdc!,
  NEXT_PUBLIC_VAPOR_TOKEN_FACTORY: dep.tokenFactory!,
  NEXT_PUBLIC_SHOP_APP_ID: s.appId.toString(),
  NEXT_PUBLIC_SHOP_CHECKOUT: s.checkout,
  NEXT_PUBLIC_LAUNCHPAD_APP_ID: lp.toString(),
  NEXT_PUBLIC_FAUCET_ENABLED: '1',
  VAPOR_FAUCET_KEY_FILE: faucetKey(),
  VAPOR_FAUCET_AMOUNT: '25000000',
}
writeFileSync(envFile, `# generated by scripts/bootstrap-localnet.ts (${accounts.faucet ? 'localnet' : ''})\n` + Object.entries(env).map(([k, v]) => `${k}=${v}`).join('\n') + '\n', { mode: 0o600 })
console.log(`wrote ${envFile}`)
