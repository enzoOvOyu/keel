import assert from 'node:assert/strict'
import { lstat, readFile } from 'node:fs/promises'
import { pathToFileURL } from 'node:url'

const file = process.env.KEEL_OIDC_BROWSER_FIXTURE_FILE
assert(file)
const info = await lstat(file)
assert(info.isFile() && !info.isSymbolicLink() && (info.mode & 0o077) === 0 && info.uid === process.geteuid())
const fixture = JSON.parse(await readFile(file, 'utf8')), base = new URL(fixture.base_url)
assert.equal(base.hostname, '127.0.0.1'); assert.equal(base.protocol, 'http:'); assert(!['3000', '3001', '5173'].includes(base.port))
const { chromium } = await import(pathToFileURL(process.env.KEEL_PLAYWRIGHT_MODULE || '/tmp/keel-browser-check/node_modules/playwright/index.mjs').href)
const browser = await chromium.launch({ headless: true }), errors = [], contexts = []
try {
  const context = await browser.newContext({ baseURL: base.origin, viewport: { width: 1440, height: 960 } }); contexts.push(context)
  const page = await context.newPage(); page.on('pageerror', error => errors.push(error.message))
  await page.goto('/login?next=%2F%2Fattacker.invalid')
  const provider = page.getByRole('link', { name: '使用 Fixture Organization 登录', exact: true })
  await provider.waitFor(); assert.equal(await page.locator('.oidc-login').count(), 1)
  await provider.click(); await page.waitForURL('**/projects')
  const session = await context.request.get('/api/v1/auth/session'); assert.equal(session.status(), 200)
  const identity = await session.json(); assert.equal(identity.user.username, fixture.username); assert.equal(identity.user.is_admin, false)
  const cookie = (await context.cookies()).find(value => value.name === 'keel_session')
  assert(cookie?.httpOnly); assert.equal(cookie.sameSite, 'Lax')
  const storage = await page.evaluate(() => JSON.stringify({ ...localStorage, ...sessionStorage }))
  for (const secret of ['id_token', 'access_token', 'code_verifier', fixture.password, 'private-fixture-client-secret']) assert(!storage.includes(secret))
  await page.getByRole('button', { name: '退出登录', exact: true }).click(); await page.waitForURL('**/login')
  assert.equal((await context.request.get('/api/v1/auth/session')).status(), 401)
  console.log('PASS actual signed OIDC/PKCE roundtrip, safe destination, local roles, HttpOnly cookie, logout and no browser token storage')
  await page.getByRole('button', { name: 'Switch to English', exact: true }).click()
  await page.setViewportSize({ width: 390, height: 844 })
  await page.getByRole('link', { name: 'Sign in with Fixture Organization', exact: true }).waitFor()
  assert(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth + 1))
  await page.getByRole('link', { name: 'Sign in with Fixture Organization', exact: true }).click(); await page.waitForURL('**/projects')
  await page.getByRole('button', { name: 'Sign out', exact: true }).click(); await page.waitForURL('**/login')
  await page.goto('/auth/callback?status=failed')
  await page.getByRole('alert').filter({ hasText: 'Organization sign-in failed.' }).waitFor()
  assert.equal((await context.request.get('/api/v1/auth/session')).status(), 401)
  console.log('PASS English mobile provider entry, callback error result and repeat sign-in/logout')
  await page.goto('/login'); await page.locator('#username').fill(fixture.username); await page.locator('#password').fill(fixture.password)
  await page.locator('button[type=submit]').click(); await page.waitForURL('**/projects')
  await page.getByRole('button', { name: 'Sign out', exact: true }).click(); await page.waitForURL('**/login')
  console.log('PASS existing local login remains usable beside configured OIDC')
  assert.deepEqual(errors, [])
} finally {
  for (const context of contexts) {
    try { const response = await context.request.get('/api/v1/auth/session'); if (response.ok()) await context.request.post('/api/v1/auth/logout', { headers: { Origin: base.origin, 'X-CSRF-Token': (await response.json()).csrf_token } }) } catch { /* disposable API shuts down after test */ }
  }
  await browser.close()
}
