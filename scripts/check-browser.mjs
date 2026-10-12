/**
 * Browser acceptance for the keel foundation, run on the development server.
 * Requires an existing Playwright install + Chromium; does not install anything.
 * node /tmp/keel-browser-check.mjs
 * Optional KEEL_PLAYWRIGHT_MODULE=/absolute/path/to/playwright/index.mjs
 * Optional KEEL_CHROMIUM_PATH=/absolute/path/to/chromium
 * Optional KEEL_BASE_URL=http://127.0.0.1:3000
 */
import assert from 'node:assert/strict'
import { createRequire } from 'node:module'
import { pathToFileURL } from 'node:url'

async function loadPlaywright() {
  if (process.env.KEEL_PLAYWRIGHT_MODULE) {
    return import(pathToFileURL(process.env.KEEL_PLAYWRIGHT_MODULE).href)
  }
  try { return await import('playwright') } catch (error) {
    if (error.code !== 'ERR_MODULE_NOT_FOUND') throw error
    return createRequire('/opt/keel/package.json')('playwright')
  }
}
const { chromium } = await loadPlaywright()
const baseURL = process.env.KEEL_BASE_URL ?? 'http://127.0.0.1:3000'
const base = new URL(baseURL)
assert(['http:', 'https:'].includes(base.protocol), 'Expected an HTTP(S) preview URL')
const browser = await chromium.launch({
  headless: true,
  ...(process.env.KEEL_CHROMIUM_PATH ? { executablePath: process.env.KEEL_CHROMIUM_PATH } : {}),
  args: ['--disable-dev-shm-usage'],
})
const report = { passed: [], screenshots: [], expectedFaultConsoleErrors: [] }
const contexts = []
const probeAccount = 'keel-browser-probe'
const probePassword = 'test-only-not-a-real-password'

function pass(name) { report.passed.push(name); console.log(`PASS ${name}`) }
async function createPage(options = {}, { fault = false } = {}) {
  const context = await browser.newContext({
    baseURL, viewport: { width: 1440, height: 960 }, colorScheme: 'light',
    locale: 'zh-CN', reducedMotion: 'reduce', ...options,
  })
  contexts.push(context)
  const page = await context.newPage()
  page.setDefaultTimeout(15_000)
  const errors = []
  const requests = []
  page.on('pageerror', (error) => errors.push(`Uncaught: ${error.message}`))
  page.on('console', (message) => {
    if (message.type() !== 'error') return
    const text = message.text()
    // Only the intentionally injected provider 503 may produce a browser resource error.
    const location = message.location().url ?? ''
    if (fault && /503/.test(text) && (location.includes('/api/v1/auth/providers') || !location)) {
      report.expectedFaultConsoleErrors.push(text)
    } else errors.push(`Console: ${text}`)
  })
  page.on('request', (request) => requests.push({
    method: request.method(), url: request.url(), body: request.postData() ?? '',
  }))
  return { context, page, errors, requests }
}
async function ready(page, language = 'zh') {
  await page.getByText(language === 'zh' ? '身份服务尚未启用' : 'Identity service is not enabled', { exact: true }).waitFor()
  assert(await page.locator('#login-form button[type="submit"]').isDisabled(), 'Login must remain disabled')
  assert.equal(await page.title(), language === 'zh' ? '登录 · keel' : 'Sign in · keel')
  assert.equal(await page.locator('html').getAttribute('lang'), language === 'zh' ? 'zh-CN' : 'en')
}
async function theme(page, target, language = 'zh') {
  const current = await page.locator('html').getAttribute('data-theme')
  if (current !== target) {
    const label = language === 'zh'
      ? target === 'dark' ? '切换到深色主题' : '切换到浅色主题'
      : target === 'dark' ? 'Switch to dark theme' : 'Switch to light theme'
    await page.getByRole('button', { name: label, exact: true }).click()
  }
  await page.waitForFunction((value) => document.documentElement.dataset.theme === value, target)
}
async function screenshot(page, name) {
  const path = `/tmp/keel-ui-${name}.png`
  await page.screenshot({ path, fullPage: true, animations: 'disabled' })
  report.screenshots.push(path)
}
async function noOverflow(page) {
  const width = await page.evaluate(() => ({
    viewport: window.innerWidth,
    html: document.documentElement.scrollWidth,
    body: document.body.scrollWidth,
  }))
  assert(width.html <= width.viewport + 1 && width.body <= width.viewport + 1,
    `Horizontal overflow: ${JSON.stringify(width)}`)
}
function assertSafeRequests(requests) {
  assert(requests.every(({ method }) => method === 'GET' || method === 'HEAD'), 'Unexpected mutating request from disabled login')
  assert(requests.every(({ url, body }) => ![probeAccount, probePassword].some((value) =>
    url.includes(value) || body.includes(value) || url.includes(encodeURIComponent(value)))), 'Credentials were included in a request')
  assert(requests.every(({ url }) => new URL(url).origin === base.origin), 'Unexpected external asset or API request')
}
function assertNoErrors(errors) { assert.deepEqual(errors, [], 'Unexpected browser console or runtime errors') }

try {
  const desktop = await createPage()
  const { page, context } = desktop
  const response = await page.goto('/login', { waitUntil: 'networkidle' })
  assert.equal(response?.status(), 200, 'Direct /login navigation must work')
  await ready(page)
  await noOverflow(page)
  await screenshot(page, 'zh-light')
  pass('Chinese /login, light theme, correct title, identity unavailable, disabled submit')

  await theme(page, 'dark')
  await screenshot(page, 'zh-dark')
  assert.equal(await page.evaluate(() => localStorage.getItem('keel.theme')), 'dark')
  pass('Dark theme and display preference persistence')

  await page.getByRole('button', { name: 'Switch to English', exact: true }).click()
  await ready(page, 'en')
  await screenshot(page, 'en-dark')
  await theme(page, 'light', 'en')
  await screenshot(page, 'en-light')
  pass('English labels, English title, light and dark screenshots')

  const username = page.getByLabel('Account', { exact: true })
  const password = page.getByLabel('Password', { exact: true })
  assert.equal(await username.getAttribute('autocomplete'), 'username')
  assert.equal(await password.getAttribute('autocomplete'), 'current-password')
  await username.fill(probeAccount)
  await password.fill(probePassword)
  assert.equal(await password.getAttribute('type'), 'password')
  await page.getByRole('button', { name: 'Show password', exact: true }).click()
  assert.equal(await password.getAttribute('type'), 'text')
  assert.equal(await page.getByRole('button', { name: 'Hide password', exact: true }).getAttribute('aria-pressed'), 'true')
  await page.getByRole('button', { name: 'Hide password', exact: true }).click()
  assert.equal(await password.getAttribute('type'), 'password')
  await password.press('Enter')
  // Also exercise native form submission explicitly: onSubmit must prevent navigation/POST.
  await page.locator('#login-form').evaluate((form) => form.requestSubmit())
  await page.waitForLoadState('networkidle')
  assert.equal(new URL(page.url()).pathname, '/login')
  assertSafeRequests(desktop.requests)
  const storage = await page.evaluate(() => ({ local: { ...localStorage }, session: { ...sessionStorage } }))
  assert(!JSON.stringify(storage).includes(probeAccount) && !JSON.stringify(storage).includes(probePassword), 'Credentials were persisted in browser storage')
  await username.clear()
  await password.clear()
  pass('Password visibility, password-manager fields, Enter/native submit blocked, no credential requests or storage')

  await page.reload({ waitUntil: 'networkidle' })
  await ready(page, 'en')
  assert.equal(await page.locator('html').getAttribute('data-theme'), 'light')
  assert.equal(await page.locator('#password').inputValue(), '')
  await page.goto('/', { waitUntil: 'networkidle' })
  await page.waitForURL('**/login')
  await ready(page, 'en')
  pass('Language/theme persist across reload; root route redirects to /login')

  const health = await context.request.get('/healthz')
  assert.equal(health.status(), 200, 'Public health endpoint must remain independently available')
  assert.equal(typeof await health.json(), 'object')
  assert.equal(await page.getByRole('link', { name: 'Service status', exact: true }).getAttribute('href'), '/healthz')
  assertNoErrors(desktop.errors)
  assertSafeRequests(desktop.requests)
  pass('Independent health endpoint; desktop has no console/runtime errors or external requests')

  const mobile = await createPage({ viewport: { width: 390, height: 844 }, isMobile: true, hasTouch: true, deviceScaleFactor: 1 })
  await mobile.page.goto('/login', { waitUntil: 'networkidle' })
  await ready(mobile.page)
  await noOverflow(mobile.page)
  assert(!(await mobile.page.locator('.brand-panel').isVisible()), 'Mobile should use a single-column form')
  assert(await mobile.page.locator('#login-heading').isVisible())
  for (const id of ['#username', '#password']) {
    const box = await mobile.page.locator(id).boundingBox()
    assert(box && box.height >= 44, 'Mobile field target must be at least 44px high')
    assert(await mobile.page.locator(id).evaluate((element) => parseFloat(getComputedStyle(element).fontSize)) >= 16)
  }
  await screenshot(mobile.page, 'mobile-zh-light')
  await theme(mobile.page, 'dark')
  await noOverflow(mobile.page)
  await screenshot(mobile.page, 'mobile-zh-dark')
  await mobile.page.getByRole('button', { name: 'Switch to English', exact: true }).click()
  await ready(mobile.page, 'en')
  await noOverflow(mobile.page)
  await screenshot(mobile.page, 'mobile-en-dark')
  assertNoErrors(mobile.errors)
  assertSafeRequests(mobile.requests)
  pass('390px mobile: Chinese/English, light/dark, no horizontal overflow, accessible control sizing')

  const failure = await createPage({}, { fault: true })
  let injectFailure = true
  let providerRequests = 0
  await failure.page.route('**/api/v1/auth/providers', async (route) => {
    providerRequests += 1
    if (injectFailure) {
      await route.fulfill({ status: 503, contentType: 'application/json', body: JSON.stringify({ error: 'test_service_unavailable' }) })
    } else await route.continue()
  })
  await failure.page.goto('/login', { waitUntil: 'networkidle' })
  await failure.page.getByText('暂时无法连接身份服务', { exact: true }).waitFor()
  assert(await failure.page.locator('#login-form button[type="submit"]').isDisabled())
  await screenshot(failure.page, 'provider-error')
  const previousRequests = providerRequests
  injectFailure = false
  await failure.page.getByRole('button', { name: '重新检查', exact: true }).click()
  await ready(failure.page)
  assert(providerRequests > previousRequests, 'Retry must issue a new provider request')
  assert(!(await failure.page.getByText('暂时无法连接身份服务', { exact: true }).isVisible()))
  assertNoErrors(failure.errors)
  assertSafeRequests(failure.requests)
  pass('Injected provider 503 fails closed; retry fetches real configuration and restores not-enabled state')

  console.log(JSON.stringify({ status: 'passed', ...report }, null, 2))
} catch (error) {
  console.error(`FAIL ${error instanceof Error ? error.message : String(error)}`)
  console.error(JSON.stringify({ status: 'failed', ...report }, null, 2))
  process.exitCode = 1
} finally {
  for (const context of contexts) await context.close()
  await browser.close()
}
