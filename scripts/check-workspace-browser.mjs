/** Shared shell against a real isolated API; no mocked auth/project endpoints. */
import assert from 'node:assert/strict'
import { readFile, stat, mkdir } from 'node:fs/promises'
import { pathToFileURL } from 'node:url'
const file = process.env.KEEL_AUTH_BROWSER_CREDENTIALS_FILE
assert(file?.startsWith('/tmp/keel-'), 'Private isolated credentials required')
assert.equal((await stat(file)).mode & 0o077, 0)
const credentials = JSON.parse(await readFile(file, 'utf8'))
assert.equal(credentials.base_url, 'http://127.0.0.1:3102')
const evidence = process.env.KEEL_WORKSPACE_EVIDENCE_DIR
assert(evidence?.startsWith('/tmp/keel-'))
await mkdir(evidence, { recursive: true, mode: 0o700 })
const { chromium } = await import(pathToFileURL(process.env.KEEL_PLAYWRIGHT_MODULE || '/tmp/keel-browser-check/node_modules/playwright/index.mjs').href)
const browser = await chromium.launch({ headless: true })
const context = await browser.newContext({ baseURL: credentials.base_url, viewport: { width: 1920, height: 1080 }, reducedMotion: 'reduce' })
const page = await context.newPage(), errors = []
page.on('pageerror', error => errors.push(error.message))
const pass = message => console.log('PASS ' + message)
async function ready() { await page.locator('.workspace-content').waitFor(); await page.locator('.workspace-content [role="status"]').waitFor({ state: 'hidden' }).catch(() => {}) }
async function size(selector) { return page.locator(selector).evaluate(element => ({ width: element.getBoundingClientRect().width, height: element.getBoundingClientRect().height })) }
try {
  await page.goto('/login')
  await page.evaluate(async () => {
    await Promise.all([400, 500, 700].map(weight => document.fonts.load(`${weight} 14px "Intel One Mono"`)))
  })
  for (const selector of ['body', '#username', '#password', '#login-form button[type="submit"]']) {
    assert.match(await page.locator(selector).evaluate(element => getComputedStyle(element).fontFamily), /^"Intel One Mono"/)
  }
  assert(await page.evaluate(() => [400, 500, 700].every(weight => [...document.fonts].some(font => font.family.replaceAll('"', '') === 'Intel One Mono' && font.weight === String(weight) && font.status === 'loaded'))))
  pass('Bundled Intel One Mono 400/500/700 loaded; login and controls share the font')
  await page.locator('#username').fill(credentials.username)
  await page.locator('#password').fill(credentials.password)
  await page.locator('#login-form button[type="submit"]').click()
  await page.waitForURL('**/projects')
  const sessionResponse = await context.request.get('/api/v1/auth/session')
  assert.equal(sessionResponse.status(), 200)
  const session = await sessionResponse.json(), projects = []
  for (const name of ['Shell Alpha', 'Shell Beta']) {
    const response = await context.request.post('/api/v1/projects', { headers: { Origin: credentials.base_url, 'X-CSRF-Token': session.csrf_token, 'Idempotency-Key': crypto.randomUUID() }, data: { name, slug: 'shell-' + crypto.randomUUID(), description: 'Isolated workspace acceptance' } })
    assert.equal(response.status(), 201); projects.push(await response.json())
  }
  const [alpha, beta] = projects
  await page.goto(`/p/${alpha.id}/ztp`); await ready()
  for (const selector of ['.workspace-header', '.workspace-project-switcher select', '.workspace-sidebar-nav', '.workspace-content code']) {
    assert.match(await page.locator(selector).first().evaluate(element => getComputedStyle(element).fontFamily), /^"Intel One Mono"/)
  }
  assert.equal((await size('.workspace-header')).height, 56)
  assert.equal((await size('.workspace-sidebar')).width, 240)
  assert.equal(await page.locator('.workspace-content').evaluate(element => getComputedStyle(element).paddingLeft), '24px')
  assert(await page.locator('.workspace-content').evaluate(element => element.getBoundingClientRect().right >= innerWidth - 1))
  await page.getByRole('button', { name: '折叠导航', exact: true }).click()
  assert.equal((await size('.workspace-sidebar')).width, 56)
  await page.getByRole('button', { name: '展开导航', exact: true }).click()
  pass('56px topbar, 240/56px sidebar, full-width content with 24px padding')
  await page.getByRole('combobox', { name: '切换项目', exact: true }).selectOption(beta.id)
  await page.waitForURL(`**/p/${beta.id}/ztp`); await ready()
  await page.getByRole('link', { name: `进入项目: ${alpha.name}`, exact: true }).click()
  await page.waitForURL(`**/p/${alpha.id}/ztp`); await ready()
  assert.equal(await page.getByRole('combobox', { name: '切换项目', exact: true }).inputValue(), alpha.id)
  const other = await context.newPage(); await other.goto(`/p/${alpha.id}/assets`); await other.locator('.workspace-content').waitFor()
  await page.getByRole('combobox', { name: '切换项目', exact: true }).selectOption(beta.id)
  await page.waitForURL(`**/p/${beta.id}/ztp`)
  assert.equal(new URL(other.url()).pathname, `/p/${alpha.id}/assets`)
  await other.close()
  assert(!(await page.evaluate(() => JSON.stringify({ ...localStorage, ...sessionStorage }))).includes(alpha.id))
  pass('Both project entries preserve the active tool; switching one tab leaves the other project unchanged')
  await page.screenshot({ path: evidence + '/workspace-light.png', fullPage: true })
  await page.getByRole('button', { name: '切换到深色主题', exact: true }).click()
  await page.screenshot({ path: evidence + '/workspace-dark.png', fullPage: true })
  assert.equal(await page.evaluate(() => document.documentElement.dataset.theme), 'dark')
  await page.getByRole('button', { name: 'Switch to English', exact: true }).click()
  await page.getByRole('combobox', { name: 'Switch project', exact: true }).waitFor()
  pass('Light/dark themes and bilingual navigation')
  for (const width of [1440, 1280]) {
    await page.setViewportSize({ width, height: 900 })
    assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1), `Overflow at ${width}`)
  }
  await page.goto(`/p/${alpha.id}`); await ready()
  await page.setViewportSize({ width: 390, height: 844 })
  await page.waitForFunction(() => document.querySelector('.sidebar-toggle')?.getAttribute('aria-expanded') === 'false')
  assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1))
  await page.screenshot({ path: evidence + '/workspace-mobile.png', fullPage: true })
  pass('1280/1440 business layouts and P1 mobile project layout')
  await page.getByRole('button', { name: 'Sign out', exact: true }).click()
  await page.waitForURL('**/login')
  assert(!(await context.cookies()).some(cookie => cookie.name === 'keel_session'))
  assert.deepEqual(errors, [])
  pass('Logout revokes the real session; no browser runtime errors')
} finally { await browser.close() }
