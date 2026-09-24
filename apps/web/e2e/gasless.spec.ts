// Copyright (c) 2026 VaporChain / muthu2201. Licensed under the Apache License, Version 2.0 (see LICENSE).
// Provenance: VAPOR-6eabb1be532bdef4
//
// Real browser, real chain: a first-time visitor creates a wallet, gets test
// USDC, buys an item and launches a token without ever holding gas. Runs
// against `next start` + the localnet stack (scripts/localnet/*.sh and
// scripts/bootstrap-localnet.ts).
import { readFileSync } from 'node:fs'
import { expect, test, type Page } from '@playwright/test'

const env = Object.fromEntries(
  readFileSync(new URL('../.env.local', import.meta.url), 'utf8')
    .split('\n')
    .filter((l) => l.includes('='))
    .map((l) => [l.slice(0, l.indexOf('=')), l.slice(l.indexOf('=') + 1)]),
) as Record<string, string>

/** Fail the test on any CSP violation or uncaught page error. */
function watch(page: Page) {
  const problems: string[] = []
  page.on('console', (m) => {
    if (m.type() === 'error' && /Content Security Policy|Refused to/i.test(m.text())) problems.push(m.text())
  })
  page.on('pageerror', (e) => problems.push(`pageerror: ${e.message}`))
  return problems
}

async function newWallet(page: Page) {
  await page.getByTestId('create-wallet').click()
  await expect(page.getByTestId('wallet-chip')).toBeVisible()
}

test('security headers and strict nonce CSP', async ({ request }) => {
  const r = await request.get('/')
  const h = r.headers()
  expect(h['content-security-policy']).toMatch(/script-src 'self' 'nonce-[A-Za-z0-9+/=]+' 'strict-dynamic'/)
  expect(h['content-security-policy']).toContain("frame-ancestors 'none'")
  expect(h['content-security-policy']).not.toContain('unsafe-eval')
  expect(h['x-frame-options']).toBe('DENY')
  expect(h['x-content-type-options']).toBe('nosniff')
  expect(h['x-powered-by']).toBeUndefined()
  // a fresh nonce per response
  const again = (await request.get('/')).headers()['content-security-policy']
  expect(again).not.toBe(h['content-security-policy'])
})

test('faucet validates input', async ({ request }) => {
  expect((await request.post('/api/faucet', { data: { address: 'not-an-address' } })).status()).toBe(400)
})

test('first visit: wallet, test USDC, gasless purchase, survives reload', async ({ page }) => {
  const problems = watch(page)
  await page.goto('/')
  await newWallet(page)

  await page.getByTestId('faucet').click()
  await expect(page.getByTestId('faucet-result')).toContainText('Received 25.00 USDC')
  await expect(page.getByTestId('usdc-balance')).toHaveText('25.00 USDC')
  // a second drip for the same address is refused
  await page.getByTestId('faucet').click()
  await expect(page.getByTestId('faucet-result')).toContainText('already funded today')

  await page.getByTestId('buy-coffee').click()
  await expect(page.getByTestId('paid-coffee')).toBeVisible({ timeout: 120_000 })
  await expect(page.getByTestId('usdc-balance')).toHaveText('23.50 USDC')

  await page.getByRole('link', { name: 'Wallet' }).click()
  await expect(page.getByTestId('credits')).toHaveText('0') // never held gas
  const address = await page.getByTestId('wallet-address').textContent()
  await page.reload()
  await expect(page.getByTestId('wallet-address')).toHaveText(address!)
  expect(problems).toEqual([])
})

test('gasless token launch at the predicted address', async ({ page }) => {
  const problems = watch(page)
  await page.goto('/launch')
  await newWallet(page)
  await page.getByTestId('token-name').fill(`Gem ${Date.now()}`)
  await page.getByTestId('token-symbol').fill('GEM')
  const predicted = page.getByTestId('predicted-address')
  await expect(predicted).toHaveText(/^0x[0-9a-fA-F]{40}$/)
  const address = (await predicted.textContent())!
  await page.getByTestId('launch').click()
  await expect(page.getByTestId('launched')).toContainText(address, { timeout: 120_000 })
  expect(problems).toEqual([])
})

test('dashboard reflects indexed purchases', async ({ page }) => {
  const id = env.NEXT_PUBLIC_SHOP_APP_ID
  await expect
    .poll(
      async () => {
        await page.goto(`/apps/${id}`)
        return Number((await page.getByTestId('payments').textContent())?.replace(/,/g, '') ?? '0')
      },
      { timeout: 60_000, intervals: [2_000] },
    )
    .toBeGreaterThan(0)
  await expect(page.getByTestId('settlements').locator('tr').first()).toBeVisible()
  expect(Number(await page.getByTestId('sponsored-ops').textContent())).toBeGreaterThan(0)
  await page.goto('/apps/99999999')
  await expect(page.getByText('Not found')).toBeVisible()
})
