/** Browser acceptance for the driver upgrade page (WP-3.4): the capability
 *  matrix, the plan form with approved-media dropdowns, the target/transfer
 *  rendering, and the precheck -> abort lifecycle. */
import assert from 'node:assert/strict'
import { randomBytes } from 'node:crypto'
import { readFile, stat } from 'node:fs/promises'
import { pathToFileURL } from 'node:url'

const file = process.env.KEEL_AUTH_BROWSER_CREDENTIALS_FILE || '/tmp/keel-auth-browser-credentials.json'
assert(file.startsWith('/tmp/keel-') || file.startsWith('/opt/keel-tools/'), 'Isolated test credentials')
assert.equal((await stat(file)).mode & 0o077, 0, 'Credentials must be private')
const credentials = JSON.parse(await readFile(file, 'utf8'))
assert(['http://127.0.0.1:3102', 'http://localhost:3102'].includes(credentials.base_url), 'Isolated API only')
const { chromium } = await import(pathToFileURL(process.env.KEEL_PLAYWRIGHT_MODULE || '/tmp/keel-browser-check/node_modules/playwright/index.mjs').href)
const browser = await chromium.launch({ headless: true })
const context = await browser.newContext({ baseURL: credentials.base_url, viewport: { width: 1440, height: 960 }, locale: 'zh-CN', reducedMotion: 'reduce' })
const page = await context.newPage(); page.setDefaultTimeout(20_000)
const errors = [], passed = []
const suffix = randomBytes(4).toString('hex')
const apiCalls = []
page.on('response', (res) => { if (/\/driver\//.test(res.url())) apiCalls.push(`${res.status()} ${res.request().method()} ${res.url().split('/api/v1')[1]}`) })
page.on('pageerror', (error) => errors.push(`Runtime: ${error.message}`))
page.on('console', (message) => {
  if (message.type() !== 'error') return
  const url = message.location().url ?? '', text = message.text()
  if (!(/40[1349]/.test(text) && /\/(auth|projects)\//.test(url))) errors.push(`Console: ${text}`)
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
  return await response.json()
}, `drv-ui-${suffix}`)
const pid = project.id

// Seed: a B300 model, two machines, and two approved driver media (a second
// reviewer must approve: the uploader cannot self-approve).
const seed = await page.evaluate(async ([projectId, runSuffix]) => {
  const csrf = await (await fetch('/api/v1/auth/csrf', { credentials: 'same-origin' })).json()
  const send = (method, path, body) => fetch(`/api/v1${path}`, { method, credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf.csrf_token, 'Idempotency-Key': crypto.randomUUID() }, body: JSON.stringify(body) })
  const post = (path, body) => send('POST', path, body)
  const model = await (await post(`/projects/${projectId}/models`, { name: 'B300-8U', kind: 'server' })).json()
  await post(`/projects/${projectId}/models/${model.id}/versions`, {
    hw_spec: { gpus: 8 }, slots: [
      { slot_no: 1, pcie_slot: '01', expect_nic_model: 'cx7', panel_port: 'port1', default_role: 'management' },
      { slot_no: 2, pcie_slot: '02', expect_nic_model: 'x710', panel_port: 'port2', default_role: 'storage' },
    ],
  })
  const machine = (serial, hostname, baseMac) => ({
    serial_number: serial, rack: '', unit_position: '', bmc_address: '', hostname, ip_reference: '', model_name: 'B300-8U',
    ports: [1, 2].map((index) => ({ mac_address: `${baseMac}:00:0${index}`, port_name: `port${index}`, rail: 0 })),
  })
  // MACs are globally unique across projects and must stay 6-octet: the base
  // prefix is 4 octets (ports append the last 2), derived from the random run
  // suffix so repeated runs never collide with previous leftovers.
  const macOf = (n) => `${n === 0 ? 'aa' : 'ab'}:${runSuffix.slice(0, 2)}:${runSuffix.slice(2, 4)}:${runSuffix.slice(4, 6)}`
  const bom = await post(`/projects/${projectId}/bom/commit`, { file_version: 'v1', machines: [
    machine(`SN-DU-${runSuffix}-1`, 'drv-node-1', macOf(0)), machine(`SN-DU-${runSuffix}-2`, 'drv-node-2', macOf(1)),
  ] })
  const roles = await send('PUT', `/projects/${projectId}/roles`, {
    roles: { management: { enabled: true, required: true }, storage: { enabled: true, required: true }, oob: { enabled: true, required: false } }, slot_overrides: {},
  })
  const driverMedia = await (await post(`/projects/${projectId}/media`, { type: 'driver', name: 'nvidia-gpu-driver', version: 'R580', sha256: 'd1'.repeat(32), size: 1, arch: 'amd64', manifest_ref: 'minio://media/driver.tar.gz' })).json()
  const cudaMedia = await (await post(`/projects/${projectId}/media`, { type: 'driver', name: 'cuda-toolkit', version: '13.4', sha256: 'c1'.repeat(32), size: 1, arch: 'amd64', manifest_ref: 'minio://media/cuda.tar.gz' })).json()
  return { driverMediaId: driverMedia.id, cudaMediaId: cudaMedia.id }
}, [pid, suffix])

const reviewerName = `drv-appr-${suffix}`
const reviewerPassword = randomBytes(12).toString('base64url')
await page.evaluate(async ([projectId, name, password]) => {
  const csrf = await (await fetch('/api/v1/auth/csrf', { credentials: 'same-origin' })).json()
  const send = (method, path, body) => fetch(`/api/v1${path}`, { method, credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf.csrf_token, 'Idempotency-Key': crypto.randomUUID() }, body: JSON.stringify(body) })
  await send('POST', '/admin/users', { username: name, display_name: 'Driver reviewer', password, is_admin: false })
  await send('POST', `/projects/${projectId}/members`, { username: name, role: 'Admin' })
}, [pid, reviewerName, reviewerPassword])
// A second reviewer approves the media: the uploader cannot self-approve.
const reviewerContext = await browser.newContext({ baseURL: credentials.base_url, locale: 'zh-CN', reducedMotion: 'reduce' })
const reviewerPage = await reviewerContext.newPage()
await reviewerPage.goto('/login')
await reviewerPage.locator('#username').fill(reviewerName)
await reviewerPage.locator('#password').fill(reviewerPassword)
await reviewerPage.locator('#login-form button[type=submit]').click()
await reviewerPage.waitForURL('**/projects')
await reviewerPage.evaluate(async ([projectId, driverMediaId, cudaMediaId]) => {
  const csrf = await (await fetch('/api/v1/auth/csrf', { credentials: 'same-origin' })).json()
  const send = (method, path, body) => fetch(`/api/v1${path}`, { method, credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf.csrf_token, 'Idempotency-Key': crypto.randomUUID() }, body: JSON.stringify(body) })
  for (const mid of [driverMediaId, cudaMediaId]) {
    await send('POST', `/projects/${projectId}/media/${mid}/transition`, { state: 'verified' })
    await send('POST', `/projects/${projectId}/media/${mid}/transition`, { state: 'approved' })
  }
}, [pid, seed.driverMediaId, seed.cudaMediaId])
await reviewerContext.close()

// 1. The page renders the capability matrix with the six platform components.
await page.goto(`/p/${pid}/driver`)
await page.waitForSelector('.data-table')
const heading = await page.locator('h1').textContent()
assert.match(heading, /驱动|Driver/)
const matrixRows = await page.locator('.management-panel', { hasText: /组件能力矩阵|Component Capability/ }).locator('tbody tr').count()
assert.equal(matrixRows, 6, 'six components in the capability matrix')
pass('The driver page renders with the six-component capability matrix')

// 2. The plan form exposes approved-media dropdowns and accepts a plan.
// The capability-matrix component order is not guaranteed, so select the two
// driver-type components (gpu_driver, cuda) that have seeded approved media by
// their key rather than by position.
await page.waitForSelector('.driver-component-toggle input[type=checkbox]')
const toggleFor = (key) => page.locator('.driver-component-toggle', { hasText: key }).locator('input[type=checkbox]')
await toggleFor('gpu_driver').check()
await toggleFor('cuda').check()
await page.waitForSelector('#driver-media-gpu_driver')
async function selectMedia(selectId, text) {
  const value = await page.locator(`#${selectId} option`, { hasText: text }).getAttribute('value')
  await page.locator(`#${selectId}`).selectOption(value)
}
await selectMedia('driver-media-gpu_driver', /R580/)
await page.waitForSelector('#driver-media-cuda')
await selectMedia('driver-media-cuda', /13\.4/)
await page.locator('#driver-plan-name').fill(`drv-plan-${suffix}`)
await page.locator('#driver-baseline-gpu_driver').fill('R550')
await page.locator('#driver-baseline-cuda').fill('13.1')
await page.locator('#driver-backup').fill('snapshot + backup taken before upgrade')
await page.locator('button.primary-button', { hasText: /发起计划|Create plan/ }).click()
await page.waitForSelector('.data-table')
await page.waitForFunction((name) => document.body.innerText.includes(name), `drv-plan-${suffix}`)
pass('The plan form submits a plan with approved media and the plan appears in the list')

// 3. Opening the plan shows the target machines and the distribution summary.
// The created plan is auto-selected on creation; the link-button toggles the
// selection, so only click it when the detail did not open on its own.
try {
  await page.waitForSelector('.nettest-run-head', { timeout: 5_000 })
} catch {
  await page.locator('button.link-button', { hasText: `drv-plan-${suffix}` }).click()
  await page.waitForSelector('.nettest-run-head')
}
// 3. Precheck passes (baseline + companion rule satisfied); the plan becomes
// prechecked. Targets materialise at start, so the detail assertions run
// after the canary start below.
await page.locator('button.primary-button', { hasText: /预检|Precheck/ }).click()
await page.waitForFunction(() => {
  const el = document.querySelector('.nettest-run-head .status-label')
  return el && /已预检|prechecked/.test(el.textContent || '')
}, null, { timeout: 15_000 })
pass('Precheck passes and the plan becomes prechecked')

// 4. Starting the plan materialises the target machines and the distribution
// summary (canary first).
await page.locator('button.primary-button', { hasText: /启动|Start/ }).click()
const targetsPanel = page.locator('.management-panel', { hasText: /目标设备进度|Target progress/ })
await targetsPanel.locator('tbody tr').first().waitFor({ timeout: 15_000 }).catch(() => {
  console.log('DIAG apiCalls:', JSON.stringify(apiCalls))
  throw new Error('targets table did not render')
})
const targets = await targetsPanel.locator('tbody tr').count()
assert.equal(targets, 2, 'both machines are upgrade targets')
const summaryText = await page.locator('.management-panel', { hasText: /分发汇总|Distribution summary/ }).textContent()
assert.match(summaryText, /缓存命中|cache hits/)
pass('The started plan shows both targets and the distribution summary')

// 5. Abort moves the plan to the aborted state.
await page.locator('button.secondary-button', { hasText: /中止|Abort/ }).click()
await page.waitForFunction(() => {
  const el = document.querySelector('.nettest-run-head .status-label')
  return el && /已中止|aborted/.test(el.textContent || '')
}, null, { timeout: 15_000 })
pass('Abort moves the plan to the aborted state')

assert.equal(errors.length, 0, `no runtime/console errors: ${errors.join(' | ')}`)
console.log(JSON.stringify({ status: 'passed', groups: passed }))
await browser.close()
