import assert from 'node:assert/strict'
import { lstat, readFile } from 'node:fs/promises'
import { pathToFileURL } from 'node:url'

const file = process.env.KEEL_SSH_DOMAIN_BROWSER_CREDENTIALS_FILE
assert(file, 'Private disposable browser credentials required')
const info = await lstat(file)
assert(info.isFile() && !info.isSymbolicLink() && (info.mode & 0o077) === 0 && info.uid === process.geteuid())
const input = JSON.parse(await readFile(file, 'utf8')), base = new URL(input.base_url)
assert.equal(base.protocol, 'http:'); assert.equal(base.hostname, '127.0.0.1')
assert(!['3000', '3001', '5173'].includes(base.port))
const { chromium } = await import(pathToFileURL(process.env.KEEL_PLAYWRIGHT_MODULE || '/tmp/keel-browser-check/node_modules/playwright/index.mjs').href)
const browser = await chromium.launch({ headless: true })
let context, csrf
try {
  context = await browser.newContext({ baseURL: base.origin, viewport: { width: 1440, height: 1000 } })
  const page = await context.newPage(), errors = []
  page.on('pageerror', error => errors.push(error.message))
  await page.goto('/login'); await page.locator('#username').fill(input.username); await page.locator('#password').fill(input.password)
  await page.locator('button[type=submit]').click(); await page.waitForURL('**/projects')
  csrf = (await (await context.request.get('/api/v1/auth/session')).json()).csrf_token
  await page.goto(`/p/${input.project_id}/assets`)
  const panel = page.locator('section[aria-labelledby="ssh-domain-keys-heading"]')
  await panel.getByRole('heading', { name: 'SSH 项目域公钥', exact: true }).waitFor()
  const active = panel.locator(`[data-ssh-domain-key="${input.active_id}"]`), revoked = panel.locator(`[data-ssh-domain-key="${input.revoked_id}"]`)
  await active.getByText('有效', { exact: true }).waitFor(); await revoked.getByText('控制面已撤销', { exact: true }).waitFor()
  await active.locator('summary').click(); assert.equal((await active.locator('code').textContent()).trim(), input.public_key.trim())
  assert.equal(await panel.getByRole('button', { name: /私钥|Private key|撤销密钥|Revoke key/ }).count(), 0)
  await page.getByRole('button', { name: 'Switch to English', exact: true }).click()
  await panel.getByRole('heading', { name: 'SSH project domain public keys', exact: true }).waitFor()
  await active.getByText('Active', { exact: true }).waitFor(); await revoked.getByText('Revoked on control plane', { exact: true }).waitFor()
  const route = `**/api/v1/projects/${input.project_id}/ssh-domain-keys?*`
  // Explicit UI failure injection, not a claim of actual backend revocation.
  await page.route(route, handle => handle.fulfill({ status: 403, contentType: 'application/json', body: JSON.stringify({ code: 'FORBIDDEN', message: 'Permission denied' }) }))
  await panel.getByRole('button', { name: 'Refresh', exact: true }).click()
  await panel.getByRole('alert').waitFor(); assert.equal(await panel.locator('[data-ssh-domain-key]').count(), 0)
  await page.unroute(route); await panel.getByRole('button', { name: 'Refresh', exact: true }).click(); await active.waitFor()
  await page.setViewportSize({ width: 390, height: 844 }); await active.locator('summary').click()
  assert(await panel.isVisible()); assert.deepEqual(errors, [])
  console.log('PASS real public-key UI: active/revoked versions, exact public key, Chinese/English, 390px, injected403 clears cached rows and actual API refresh restores')
} finally {
  if (context && csrf) await context.request.post('/api/v1/auth/logout', { headers: { Origin: base.origin, 'X-CSRF-Token': csrf } }).catch(() => {})
  await browser.close()
}
