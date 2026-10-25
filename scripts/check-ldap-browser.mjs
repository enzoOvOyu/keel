import assert from 'node:assert/strict'
import { lstat, readFile } from 'node:fs/promises'
import { pathToFileURL } from 'node:url'

const file = process.env.KEEL_LDAP_BROWSER_FIXTURE_FILE
assert(file)
const info = await lstat(file)
assert(info.isFile() && !info.isSymbolicLink() && (info.mode & 0o077) === 0 && info.uid === process.geteuid())
const fixture = JSON.parse(await readFile(file, 'utf8')), base = new URL(fixture.base_url)
assert.equal(base.hostname, '127.0.0.1'); assert.equal(base.protocol, 'http:'); assert(!['3000', '3001', '5173'].includes(base.port))
const { chromium } = await import(pathToFileURL(process.env.KEEL_PLAYWRIGHT_MODULE || '/tmp/keel-browser-check/node_modules/playwright/index.mjs').href)
const browser = await chromium.launch({ headless: true }), errors = []
const context = await browser.newContext({ baseURL: base.origin, viewport: { width: 1440, height: 960 } })
try {
  const page = await context.newPage(); page.on('pageerror', error => errors.push(error.message))
  const credentialsSentTo = []
  page.on('request', request => { if (request.postData()?.includes(fixture.password)) credentialsSentTo.push(new URL(request.url()).origin + new URL(request.url()).pathname) })
  await page.goto('/login'); await page.locator('#identity-method').waitFor()
  assert.equal(await page.locator('#identity-method').inputValue(), 'local')
  await page.locator('#password').fill('discard-on-method-change'); await page.locator('#identity-method').selectOption('ldap')
  assert.equal(await page.locator('#password').inputValue(), '')
  await page.locator('#username').fill(fixture.username); await page.locator('#password').fill('wrong-directory-password')
  const failure = page.waitForResponse(response => response.url().endsWith('/auth/login') && response.request().method() === 'POST')
  await page.locator('button[type=submit]').click(); assert.equal((await failure).status(), 401)
  await page.locator('#login-error').waitFor(); assert.equal(await page.locator('#password').inputValue(), '')
  await page.locator('#password').fill(fixture.password); await page.locator('button[type=submit]').click(); await page.waitForURL('**/projects')
  const response = await context.request.get('/api/v1/auth/session'); assert.equal(response.status(), 200)
  const session = await response.json(); assert.equal(session.user.username, fixture.username); assert.equal(session.user.is_admin, false)
  const cookie = (await context.cookies()).find(value => value.name === 'keel_session'); assert(cookie?.httpOnly); assert.equal(cookie.sameSite, 'Lax')
  const storage = await page.evaluate(() => JSON.stringify({ ...localStorage, ...sessionStorage }))
  assert(!storage.includes(fixture.password)); assert(!storage.includes(fixture.local_password))
  assert(credentialsSentTo.length > 0 && credentialsSentTo.every(destination => destination === base.origin + '/api/v1/auth/login'))
  await page.getByRole('button', { name: '退出登录', exact: true }).click(); await page.waitForURL('**/login')
  assert.equal((await context.request.get('/api/v1/auth/session')).status(), 401)
  console.log('PASS configured directory method, password clear on method/failure, real LDAPS login, current local roles, HttpOnly cookie and logout')
  await page.getByRole('button', { name: 'Switch to English', exact: true }).click(); await page.setViewportSize({ width: 390, height: 844 })
  await page.getByRole('option', { name: 'Fixture Directory organization account', exact: true }).waitFor({ state: 'attached' })
  assert(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth + 1))
  await page.locator('#identity-method').selectOption('ldap'); await page.locator('#username').fill(fixture.username); await page.locator('#password').fill(fixture.password)
  await page.locator('button[type=submit]').click(); await page.waitForURL('**/projects')
  await page.getByRole('button', { name: 'Sign out', exact: true }).click(); await page.waitForURL('**/login')
  await page.locator('#identity-method').selectOption('local'); await page.locator('#username').fill(fixture.username); await page.locator('#password').fill(fixture.local_password)
  await page.locator('button[type=submit]').click(); await page.waitForURL('**/projects')
  await page.getByRole('button', { name: 'Sign out', exact: true }).click(); await page.waitForURL('**/login')
  console.log('PASS English 390px directory and explicit local login coexist; no credentials persisted or sent to another origin')
  assert.deepEqual(errors, [])
} finally {
  try { const response = await context.request.get('/api/v1/auth/session'); if (response.ok()) await context.request.post('/api/v1/auth/logout', { headers: { Origin: base.origin, 'X-CSRF-Token': (await response.json()).csrf_token } }) } catch { /* fixture API cleanup follows */ }
  await browser.close()
}
