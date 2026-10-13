/** Browser acceptance for the drawer + status pills (WP-2.11). */
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
  return await response.json()
}, `dw-ui-${suffix}`)
const pid = project.id
// Seed: model, BOM (2 machines), collections, G1 pass, healthy LLDP,
// mapping publish for machine 1, mon samples with one down port on machine 2.
await page.evaluate(async ([projectId, runSuffix]) => {
  const csrf = await (await fetch('/api/v1/auth/csrf', { credentials: 'same-origin' })).json()
  const post = (path, body) => fetch(`/api/v1${path}`, { method: 'POST', credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf.csrf_token, 'Idempotency-Key': crypto.randomUUID() }, body: JSON.stringify(body) })
  await post(`/projects/${projectId}/models`, { name: 'B300-8U', kind: 'server' })
  await post(`/projects/${projectId}/bom/commit`, { file_version: 'v1', machines: [
    { serial_number: `SN-DW-A-${runSuffix}`, rack: '', unit_position: '', bmc_address: '', hostname: 'dw-node-a', ip_reference: '', model_name: 'B300-8U', ports: [{ mac_address: 'aa:bb:cc:f0:00:01', port_name: 'port1', rail: 0 }] },
    { serial_number: `SN-DW-B-${runSuffix}`, rack: '', unit_position: '', bmc_address: '', hostname: 'dw-node-b', ip_reference: '', model_name: 'B300-8U', ports: [{ mac_address: 'aa:bb:cc:f0:00:02', port_name: 'port1', rail: 0 }] },
  ] })
  const assets = await (await fetch(`/api/v1/projects/${projectId}/assets?page=1&page_size=20`, { credentials: 'same-origin' })).json()
  const a = assets.items[0], b = assets.items[1]
  await post(`/projects/${projectId}/collections`, { level: 'L1', machine_ids: [a.machine_id, b.machine_id] })
  await post(`/projects/${projectId}/collections`, { level: 'L2', machine_ids: [a.machine_id, b.machine_id] })
  await post(`/projects/${projectId}/gates/evaluate`, { gate: 'G1', machine_ids: [a.machine_id, b.machine_id] })
  const healthy = { peer: 'sw1', peer_port: 'Eth1/1', link_up: true, dom_warning: false, flapping: false }
  await post(`/projects/${projectId}/lldp/snapshots`, { snapshots: [
    { machine_id: a.machine_id, ports: { port1: healthy } },
    { machine_id: b.machine_id, ports: { port1: healthy } },
  ] })
  const portsA = await (await fetch(`/api/v1/projects/${projectId}/machines/${a.machine_id}/ports`, { credentials: 'same-origin' })).json()
  await post(`/projects/${projectId}/mappings/publish`, { machine_id: a.machine_id, entries: [
    { port_id: portsA.items[0].id, os_interface: 'nic-r0-p1', mac_address: 'aa:bb:cc:f0:00:01', bdf: '18:00.0', switch_port: 'sw1 Eth1/1', gpu_affinity: 'gpu-0' },
  ], note: 'drawer fixture' })
  await post(`/projects/${projectId}/mon/ingest`, { machine_id: a.machine_id, stage: 'M2', changed: [{ port: 'port1', metric: 'link', value: { state: 'up' } }] })
  await post(`/projects/${projectId}/mon/ingest`, { machine_id: b.machine_id, stage: 'M2', changed: [{ port: 'port1', metric: 'link', value: { state: 'down' } }] })
}, [pid, suffix])

await page.goto(`/p/${pid}/assets`)
await page.waitForSelector('.pill-pair')

// 1. The pill pair renders per machine with the right semantics.
const pills = await page.evaluate(() => [...document.querySelectorAll('.pill-pair')].map((node) => ({
  text: node.textContent ?? '', machine: node.getAttribute('data-machine') ?? ''
})))
assert(pills.length === 2, pills)
const hostById = await page.evaluate(async (projectId) => {
  const assets = await (await fetch(`/api/v1/projects/${projectId}/assets?page=1&page_size=20`, { credentials: 'same-origin' })).json()
  return Object.fromEntries(assets.items.map((item) => [item.machine_id, item.hostname]))
}, pid)
const byHost = Object.fromEntries(pills.map((entry) => [hostById[entry.machine], entry.text]))
console.log('PILLS-BY-HOST', JSON.stringify(byHost))
assert(byHost['dw-node-a'] && byHost['dw-node-a'].includes('映射') && byHost['dw-node-a'].includes('✓'), byHost)
assert(byHost['dw-node-b'] && (byHost['dw-node-b'].includes('未建') || byHost['dw-node-b'].includes('○')), byHost)
pass('Status pills render mapping and network columns with the right values')

// 2. The network column stays independent: machine b (no mapping) still
//    shows a network error from its down port.
assert(byHost['dw-node-b'].includes('✕'), byHost)
pass('The network column does not depend on a mapping existing')

// 3. Clicking the mapping pill opens the drawer on the mapping tab.
await page.locator('tr', { hasText: 'dw-node-a' }).locator('.status-pill').first().click()
await page.waitForSelector('.server-drawer')
await page.waitForSelector('.drawer-tab.active')
const activeTab = await page.locator('.drawer-tab.active').innerText()
assert(activeTab.includes('映射'), activeTab)
await page.waitForSelector('td:has-text("nic-r0-p1")')
pass('The mapping pill opens the drawer on the mapping tab with entries')

// 4. Tab navigation: switch to the port panel and see the seeded link.
await page.locator('.drawer-tab', { hasText: '网口面板' }).click()
await page.waitForSelector('.drawer-body .status-label')
const panelText = await page.locator('.drawer-body').innerText()
assert(panelText.includes('up'), panelText)
pass('The port panel tab shows the seeded link state')

// 5. Esc closes the drawer and the list stays intact.
await page.keyboard.press('Escape')
await page.waitForSelector('.server-drawer', { state: 'detached' })
await page.locator('tr', { hasText: 'dw-node-a' }).first().waitFor()
pass('Esc closes the drawer and keeps the list context')

await context.close()
await browser.close()
if (errors.length) { console.error(errors.join('\n')); process.exit(1) }
console.log(JSON.stringify({ status: 'passed', groups: passed.length, scope: 'isolated PostgreSQL + real API + Chromium' }))
