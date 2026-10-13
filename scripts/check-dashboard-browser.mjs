/** Browser acceptance for the gate board dashboard against an isolated test DB. */
import assert from 'node:assert/strict'
import { randomBytes } from 'node:crypto'
import { readFile, stat } from 'node:fs/promises'
import { pathToFileURL } from 'node:url'

const file = process.env.KEEL_AUTH_BROWSER_CREDENTIALS_FILE || '/tmp/keel-auth-browser-credentials.json'
assert(file.startsWith('/tmp/keel-'), 'Use an isolated test credential file')
assert.equal((await stat(file)).mode & 0o077, 0, 'Test credentials must be private')
const credentials = JSON.parse(await readFile(file, 'utf8'))
assert(['http://127.0.0.1:3102', 'http://localhost:3102'].includes(credentials.base_url), 'Only the isolated test API is allowed')
const { chromium } = await import(pathToFileURL(process.env.KEEL_PLAYWRIGHT_MODULE || '/tmp/keel-browser-check/node_modules/playwright/index.mjs').href)
const browser = await chromium.launch({ headless: true })
const context = await browser.newContext({ baseURL: credentials.base_url, viewport: { width: 1440, height: 960 }, locale: 'zh-CN', reducedMotion: 'reduce' })
const page = await context.newPage(); page.setDefaultTimeout(15_000)
const errors = [], passed = []
const suffix = randomBytes(4).toString('hex')
page.on('pageerror', (error) => errors.push(`Runtime: ${error.message}`))
page.on('console', (message) => {
  if (message.type() !== 'error') return
  const url = message.location().url ?? '', text = message.text()
  if (!((/40[1349]/.test(text) && /\/(auth|projects)\//.test(url)))) errors.push(`Console: ${text}`)
})
function pass(name) { passed.push(name); console.log(`PASS ${name}`) }

await page.goto('/login')
await page.locator('#username').fill(credentials.username)
await page.locator('#password').fill(credentials.password)
await page.locator('#login-form button[type=submit]').click()
await page.waitForURL('**/projects')
const project = await page.evaluate(async (name) => {
  const csrf = await (await fetch('/api/v1/auth/csrf', { credentials: 'same-origin' })).json()
  const response = await fetch('/api/v1/projects', { method: 'POST', credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf.csrf_token, 'Idempotency-Key': crypto.randomUUID() }, body: JSON.stringify({ name, slug: name }) })
  return (await response.json())
}, `dash-ui-${suffix}`)
const pid = project.id
// Seed: model, BOM, collections and a passing G1, so the board shows a pass card.
await page.evaluate(async ([projectId, runSuffix]) => {
  const csrf = await (await fetch('/api/v1/auth/csrf', { credentials: 'same-origin' })).json()
  const post = (path, body) => fetch(`/api/v1${path}`, { method: 'POST', credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf.csrf_token, 'Idempotency-Key': crypto.randomUUID() }, body: JSON.stringify(body) })
  await post(`/projects/${projectId}/models`, { name: 'B300-8U', kind: 'server' })
  await post(`/projects/${projectId}/bom/commit`, { file_version: 'v1', machines: [{ serial_number: `SN-DASH-${runSuffix}`, rack: '', unit_position: '', bmc_address: '', hostname: 'dash-node-1', ip_reference: '', model_name: 'B300-8U', ports: [{ mac_address: 'aa:bb:cc:a0:00:01', port_name: 'port1', rail: 0 }] }] })
  const assets = await (await fetch(`/api/v1/projects/${projectId}/assets?page=1&page_size=20`, { credentials: 'same-origin' })).json()
  const mid = assets.items[0].machine_id
  await post(`/projects/${projectId}/collections`, { level: 'L1', machine_ids: [mid] })
  await post(`/projects/${projectId}/collections`, { level: 'L2', machine_ids: [mid] })
  await post(`/projects/${projectId}/gates/evaluate`, { gate: 'G1', machine_ids: [mid] })
}, [pid, suffix])

await page.goto(`/p/${pid}/dashboard`)
await page.waitForSelector('.gate-card')

// 1. Six cards render; G1 passes, the rest stay honest.
const cards = await page.locator('.gate-card').all()
assert(cards.length === 6, `expected 6 gate cards, saw ${cards.length}`)
const g1 = page.locator('.gate-card', { hasText: 'G1' })
await g1.locator('.status-label:has-text("通过")').waitFor()
const g3 = page.locator('.gate-card', { hasText: 'G3' })
await g3.locator('.status-label:has-text("未实现")').waitFor()
pass('The board renders six cards with honest states')

// 2. The KPI block shows the seeded asset and trust counts.
await page.waitForSelector('.kpi-grid')
const kpis = await page.evaluate(() => ({
  assets: [...document.querySelectorAll('.kpi-grid dt')].map((n) => n.textContent),
  values: [...document.querySelectorAll('.kpi-grid dd')].map((n) => n.textContent),
}))
assert(kpis.values.some((v) => v && v.includes('未验证 1')), JSON.stringify(kpis))
assert(kpis.values.length >= 4, JSON.stringify(kpis))
pass('KPIs reflect the seeded trust state')

// 3. The waiver dialog refuses an empty reason client-side.
await g1.locator('button:has-text("豁免")').click()
await page.fill('#waive-reason', '')
await page.click('.action-dialog button[type=submit]')
await page.waitForSelector('.field-error')
await page.click('.action-dialog button:has-text("取消"), .action-dialog button:has-text("Cancel")')
pass('The waiver dialog refuses an empty reason client-side')

// A card-level waiver needs a target set; recording one via the API (the
// dialog itself defers until target selection exists) flips the card.
const mid = await page.evaluate(async (projectId) => {
  const assets = await (await fetch(`/api/v1/projects/${projectId}/assets?page=1&page_size=20`, { credentials: 'same-origin' })).json()
  return assets.items[0].machine_id
}, pid)
await page.evaluate(async ([projectId, machineId]) => {
  const csrf = await (await fetch('/api/v1/auth/csrf', { credentials: 'same-origin' })).json()
  await fetch(`/api/v1/projects/${projectId}/gates/waive`, { method: 'POST', credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf.csrf_token, 'Idempotency-Key': crypto.randomUUID() },
    body: JSON.stringify({ gate: 'G1', machine_ids: [machineId], reason: 'DOM unreadable on port1, RMA scheduled', scope: 'dash-node-1 port1', waiver_expires_hours: 24 }) })
}, [pid, mid])
await page.reload()
await page.waitForSelector('.gate-card')
await g1.locator('.status-label:has-text("已豁免")').waitFor()
pass('A recorded waiver flips the card to waived')

// 4. English page renders after the language switch.
await page.click('.language-button')
await page.goto(`/p/${pid}/dashboard`)
await page.waitForSelector('h1:has-text("Gate board")')
await page.locator('.gate-card', { hasText: 'G3' }).locator('.status-label:has-text("Not implemented")').waitFor()
pass('English dashboard renders with honest states')

await context.close()
await browser.close()
if (errors.length) { console.error(errors.join('\n')); process.exit(1) }
console.log(JSON.stringify({ status: 'passed', groups: passed.length, scope: 'isolated PostgreSQL + real API + Chromium' }))
