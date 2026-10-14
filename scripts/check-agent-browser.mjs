/** Browser acceptance for the agent installer page (WP-3.5, F-5.4): the plan
 * form with approved agent media, the G3+/arch blocked-target rendering, the
 * target table with credential fingerprints and the abort lifecycle. */
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
}, `ag-ui-${suffix}`)
const pid = project.id

// Seed: a model with arch=amd64, two machines (one mapped = G3+, one
// unmapped), and an approved amd64 agent media (a second reviewer approves).
const seed = await page.evaluate(async ([projectId, runSuffix]) => {
  const csrf = await (await fetch('/api/v1/auth/csrf', { credentials: 'same-origin' })).json()
  const send = (method, path, body) => fetch(`/api/v1${path}`, { method, credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf.csrf_token, 'Idempotency-Key': crypto.randomUUID() }, body: JSON.stringify(body) })
  const post = (path, body) => send('POST', path, body)
  const model = await (await post(`/projects/${projectId}/models`, { name: 'AG300-8U', kind: 'server' })).json()
  await post(`/projects/${projectId}/models/${model.id}/versions`, {
    hw_spec: { gpus: 8, arch: 'amd64' },
    slots: [
      { slot_no: 1, pcie_slot: '01', expect_nic_model: 'cx7', panel_port: 'port1', default_role: 'management' },
      { slot_no: 2, pcie_slot: '02', expect_nic_model: 'x710', panel_port: 'port2', default_role: 'storage' },
    ],
  })
  const machine = (serial, hostname, baseMac) => ({
    serial_number: serial, rack: '', unit_position: '', bmc_address: '', hostname, ip_reference: '', model_name: 'AG300-8U',
    ports: [1, 2].map((index) => ({ mac_address: `${baseMac}:00:0${index}`, port_name: `port${index}`, rail: 0 })),
  })
  // MACs derive from the run suffix: globally unique, never colliding with a
  // previous run's leftovers in the shared test database.
  const macOf = (n) => `${n === 0 ? 'aa' : 'ab'}:${runSuffix.slice(0, 2)}:${runSuffix.slice(2, 4)}:${runSuffix.slice(4, 6)}`
  await post(`/projects/${projectId}/bom/commit`, { file_version: 'v1', machines: [
    machine(`SN-AG-${runSuffix}-1`, 'ag-node-1', macOf(0)), machine(`SN-AG-${runSuffix}-2`, 'ag-node-2', macOf(1)),
  ] })
  await send('PUT', `/projects/${projectId}/roles`, {
    roles: { management: { enabled: true, required: true }, storage: { enabled: true, required: true }, oob: { enabled: true, required: false } }, slot_overrides: {},
  })
  const assets = await (await fetch(`/api/v1/projects/${projectId}/assets?page=1&page_size=50`, { credentials: 'same-origin' })).json()
  const byHost = Object.fromEntries(assets.items.map((a) => [a.hostname, a.machine_id]))
  const m1 = byHost['ag-node-1']
  // Publish a mapping for machine 1 only (G3 evidence for it, none for 2).
  const ports = (await (await fetch(`/api/v1/projects/${projectId}/machines/${m1}/ports`, { credentials: 'same-origin' })).json()).items
  const byPort = Object.fromEntries(ports.map((p) => [p.port_name, p.id]))
  await post(`/projects/${projectId}/collections`, { level: 'L1', machine_ids: [m1] })
  await post(`/projects/${projectId}/collections`, { level: 'L2', machine_ids: [m1] })
  await post(`/projects/${projectId}/gates/evaluate`, { gate: 'G1', machine_ids: [m1] })
  await post(`/projects/${projectId}/lldp/snapshots`, { snapshots: [{ machine_id: m1, ports: {
    port1: { peer: 'sw1', peer_port: 'Eth1/1', link_up: true, dom_warning: false, flapping: false },
    port2: { peer: 'sw1', peer_port: 'Eth1/2', link_up: true, dom_warning: false, flapping: false },
  } }] })
  await post(`/projects/${projectId}/mappings/publish`, { machine_id: m1, entries: [
    { port_id: byPort['port1'], os_interface: 'nic-r0-p1', mac_address: `${macOf(0)}:00:01`, bdf: '18:00.0', switch_port: 'sw-a Eth1/1', gpu_affinity: 'gpu-0' },
    { port_id: byPort['port2'], os_interface: 'nic-r0-p2', mac_address: `${macOf(0)}:00:02`, bdf: '18:00.1', switch_port: 'sw-a Eth1/2', gpu_affinity: 'gpu-4' },
  ], note: 'agent fixture' })
  const media = await (await post(`/projects/${projectId}/media`, { type: 'agent', name: 'keel-agent', version: runSuffix, sha256: 'ab'.repeat(32), size: 1024, arch: 'amd64', manifest_ref: 'minio://media/agent.tar.gz' })).json()
  return { m1, m2: byHost['ag-node-2'], mediaId: media.id }
}, [pid, suffix])

const reviewerName = `ag-appr-${suffix}`
const reviewerPassword = randomBytes(12).toString('base64url')
await page.evaluate(async ([projectId, name, password]) => {
  const csrf = await (await fetch('/api/v1/auth/csrf', { credentials: 'same-origin' })).json()
  const send = (method, path, body) => fetch(`/api/v1${path}`, { method, credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf.csrf_token, 'Idempotency-Key': crypto.randomUUID() }, body: JSON.stringify(body) })
  await send('POST', '/admin/users', { username: name, display_name: 'AG reviewer', password, is_admin: false })
  await send('POST', `/projects/${projectId}/members`, { username: name, role: 'Admin' })
}, [pid, reviewerName, reviewerPassword])
const reviewerContext = await browser.newContext({ baseURL: credentials.base_url, locale: 'zh-CN', reducedMotion: 'reduce' })
const reviewerPage = await reviewerContext.newPage()
await reviewerPage.goto('/login')
await reviewerPage.locator('#username').fill(reviewerName)
await reviewerPage.locator('#password').fill(reviewerPassword)
await reviewerPage.locator('#login-form button[type=submit]').click()
await reviewerPage.waitForURL('**/projects')
await reviewerPage.evaluate(async ([projectId, mediaId]) => {
  const csrf = await (await fetch('/api/v1/auth/csrf', { credentials: 'same-origin' })).json()
  const send = (method, path, body) => fetch(`/api/v1${path}`, { method, credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf.csrf_token, 'Idempotency-Key': crypto.randomUUID() }, body: JSON.stringify(body) })
  await send('POST', `/projects/${projectId}/media/${mediaId}/transition`, { state: 'verified' })
  await send('POST', `/projects/${projectId}/media/${mediaId}/transition`, { state: 'approved' })
}, [pid, seed.mediaId])
await reviewerContext.close()

// 1. The page renders the plan form with the approved agent media dropdown.
await page.goto(`/p/${pid}/agent`)
await page.waitForSelector('#agent-plan-name')
const heading = await page.locator('h1').textContent()
assert.match(heading, /Agent 批量安装|Agent Batch Install/)
await page.waitForSelector('#agent-media')
const options = await page.locator('#agent-media option').allTextContents()
assert.ok(options.some((text) => text.includes('keel-agent')), 'the approved agent media appears in the dropdown')
pass('The agent page renders with the approved agent media dropdown')

// 2. Submitting a plan for both machines creates it: machine 1 is pending,
// machine 2 (unmapped) is blocked with the G3 reason.
await page.locator('#agent-plan-name').fill(`ag-plan-${suffix}`)
await page.locator('#agent-media').selectOption(seed.mediaId)
await page.locator('button.primary-button', { hasText: /发起安装计划|Plan an install/ }).click()
await page.waitForFunction((name) => document.body.innerText.includes(name), `ag-plan-${suffix}`)
pass('The plan form submits and the plan appears in the list')

// 3. Opening the plan shows the blocked target with its reason and the
// pending target; the media://agent URI is rendered.
try {
  await page.waitForSelector('.nettest-run-head', { timeout: 5_000 })
} catch {
  await page.locator('button.link-button', { hasText: `ag-plan-${suffix}` }).click()
  await page.waitForSelector('.nettest-run-head')
}
const targetsPanel = page.locator('.management-panel', { has: page.locator('.nettest-run-head') })
await targetsPanel.locator('tbody tr').first().waitFor({ timeout: 15_000 })
const rows = await targetsPanel.locator('tbody tr').allTextContents()
assert.equal(rows.length, 2, 'both machines are plan targets')
assert.ok(rows.some((row) => /G3|mapping/i.test(row)), `the unmapped machine is blocked with a G3 reason: ${rows.join(' | ')}`)
const bodyText = await page.locator('body').innerText()
assert.match(bodyText, /media:\/\/agent\/keel-agent@/, 'the media://agent URI is visible')
pass('The plan detail shows the blocked G3 target and the media://agent URI')

// 4. Abort moves the running plan to the aborted state.
await page.locator('button.secondary-button', { hasText: /中止计划|Abort/ }).click()
await page.waitForFunction(() => {
  const el = document.querySelector('.nettest-run-head .status-label')
  return el && /已中止|aborted/.test(el.textContent || '')
}, null, { timeout: 15_000 })
pass('Abort moves the plan to the aborted state')

assert.equal(errors.length, 0, `no runtime/console errors: ${errors.join(' | ')}`)
console.log(JSON.stringify({ status: 'passed', groups: passed }))
await browser.close()
