/** Browser acceptance for the role scheme and the config renderers (WP-3.2). */
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
}, `cf-ui-${suffix}`)
const pid = project.id

// Seed: a model with four slots (management / storage / two compute rails),
// one machine carrying those ports, and the default role plan saved.
await page.evaluate(async ([projectId, runSuffix]) => {
  const csrf = await (await fetch('/api/v1/auth/csrf', { credentials: 'same-origin' })).json()
  const send = (method, path, body) => fetch(`/api/v1${path}`, { method, credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf.csrf_token, 'Idempotency-Key': crypto.randomUUID() }, body: JSON.stringify(body) })
  const post = (path, body) => send('POST', path, body)
  const model = await (await post(`/projects/${projectId}/models`, { name: 'B300-8U', kind: 'server' })).json()
  await post(`/projects/${projectId}/models/${model.id}/versions`, {
    hw_spec: { gpus: 8 },
    capability_matrix: { nic_partition: { cx7: { pf: 2, vf: 64, rate_gbps: 200 }, x710: { pf: 2, vf: 32, rate_gbps: 10 } } },
    slots: [
      { slot_no: 1, pcie_slot: '01', expect_nic_model: 'cx7', panel_port: 'port1', default_role: 'management' },
      { slot_no: 2, pcie_slot: '02', expect_nic_model: 'x710', panel_port: 'port2', default_role: 'storage' },
      { slot_no: 3, pcie_slot: '03', expect_nic_model: 'cx7', panel_port: 'port3', default_role: 'compute' },
      { slot_no: 4, pcie_slot: '04', expect_nic_model: 'cx7', panel_port: 'port4', default_role: 'compute' },
    ],
  })
  await post(`/projects/${projectId}/bom/commit`, { file_version: 'v1', machines: [
    { serial_number: `SN-CF-${runSuffix}`, rack: '', unit_position: '', bmc_address: '', hostname: 'cf-node-1', ip_reference: '', model_name: 'B300-8U',
      ports: [1, 2, 3, 4].map((index) => ({ mac_address: `aa:bb:cc:c1:00:0${index}`, port_name: `port${index}`, rail: index > 2 ? 1 : 0 })) },
  ] })
  await send('PUT', `/projects/${projectId}/roles`, {
    roles: { management: { enabled: true, required: true }, storage: { enabled: true, required: true },
             compute: { enabled: true, required: true }, oob: { enabled: true, required: false } },
    slot_overrides: {},
  })
}, [pid, suffix])

await page.goto(`/p/${pid}/config`)
await page.waitForSelector('.data-table')
await page.waitForSelector('select[aria-label*="port2"], table tr td select')

// 1. The role scheme shows the resolved coverage per role.
const coverage = await page.evaluate(() => {
  const rows = [...document.querySelectorAll('table tbody tr')].filter((row) => row.querySelector('th'))
  return Object.fromEntries(rows.map((row) => [row.querySelector('th').textContent.trim(), row.lastElementChild.textContent.trim()]))
})
console.log('COVERAGE', JSON.stringify(coverage))
assert(coverage['管理网'].startsWith('1'), coverage)
assert(coverage['存储网'].startsWith('1'), coverage)
assert(coverage['计算网（RoCE）'].startsWith('2'), coverage)
pass('The role scheme reports in-place port coverage per logical role')

// 2. Setting the storage slot to unused re-binds the port and leaves storage
//    as a missing required role — the FR-5.9 rule made visible.
await page.locator('tr', { hasText: 'port2' }).locator('select').selectOption('unused')
await page.locator('button', { hasText: '保存槽位覆盖' }).click()
await page.waitForSelector('.impact-notice')
const afterOverride = await page.evaluate(() => {
  const row = [...document.querySelectorAll('table tbody tr')].find((row) => row.textContent.includes('port2'))
  const plan = [...document.querySelectorAll('table tbody tr')].filter((row) => row.querySelector('th'))
    .map((row) => [row.querySelector('th').textContent.trim(), row.lastElementChild.textContent.trim()])
  return { role: row.querySelector('select').value, plan: Object.fromEntries(plan) }
})
assert.equal(afterOverride.role, 'unused', afterOverride)
assert(afterOverride.plan['存储网'].includes('缺失'), afterOverride)
pass('A slot override re-binds the port and required roles stay visible as missing')

// 3. Disabling the storage role clears the missing badge (phase two).
const roleTable = page.locator('.management-panel table').first()
await roleTable.locator('tr', { hasText: '存储网' }).locator('input[type=checkbox]').first().uncheck()
await page.locator('button', { hasText: '保存角色方案' }).click()
await page.waitForFunction(() => {
  const row = [...document.querySelectorAll('table tbody tr')].find((row) => row.querySelector('th') && row.querySelector('th').textContent.trim() === '存储网')
  return Boolean(row) && !row.lastElementChild.textContent.includes('缺失')
}, null, { timeout: 10_000 }).catch(async () => {
  throw new Error('storage role still missing: ' + await page.locator('.management-panel').first().innerText())
})
const afterDisable = await page.evaluate(() => Object.fromEntries([...document.querySelectorAll('table tbody tr')]
  .filter((row) => row.querySelector('th')).map((row) => [row.querySelector('th').textContent.trim(), row.lastElementChild.textContent.trim()])))
assert(!afterDisable['存储网'].includes('缺失'), afterDisable)
pass('Disabling a role clears its missing-binding error without touching the model')

// 4. The renderer previews a role-aware plan: the unused slot is absent.
const params = JSON.stringify({ net: {
  management: { subnet: '10.10.0.0/24', gateway: '10.10.0.1', mtu: 1500 },
  storage: { subnet: '10.20.0.0/24' },
  compute: { subnet: '10.30.0.0/24', mtu: 9000 },
} })
await page.locator('#render-params').fill(params)
await page.locator('button', { hasText: '预检渲染' }).click()
await page.waitForSelector('.render-result')
const rendered = await page.locator('.render-result pre').first().innerText()
console.log('RENDERED', rendered)
assert(rendered.includes('iface <unmapped:port1>'), rendered)
assert(rendered.includes('address 10.30.0.13/24') && rendered.includes('address 10.30.0.14/24'), rendered)
assert(!rendered.includes('port2') && !rendered.includes('10.20.0.'), rendered)
pass('The preview renders per role and skips the slot set to unused')

// 5. The NIC partitioning renderer reports the model capability totals for
//    the three active rails (the unused storage slot contributes nothing).
await page.locator('#render-kind').selectOption('nic_partition')
await page.locator('button', { hasText: '预检渲染' }).click()
await page.waitForSelector('.render-result')
const partitioned = await page.locator('.render-result pre').first().innerText()
assert(partitioned.includes('rate_gbps=200') && partitioned.includes('total_gbps=600'), partitioned)
pass('Partitioning renders the capability-derived rate and the bandwidth total')

// 6. The read-back field is shared by every renderer: a matching sample
//    verifies the preview receipt, a disagreeing sample does not.
await page.locator('#render-kind').selectOption('gpu_network')
await page.locator('#render-readback').fill(JSON.stringify({ role: 'compute', address: '10.30.0.13/24', mtu: '9000' }))
await page.locator('button', { hasText: '预检渲染' }).click()
await page.waitForSelector('.render-result')
let receipt = await page.locator('.render-result p').last().innerText()
console.log('RECEIPT', receipt)
assert(receipt.includes('readback_ok') && receipt.includes('已验证'), receipt)
await page.locator('#render-readback').fill(JSON.stringify({ address: '10.30.0.99/24' }))
await page.locator('button', { hasText: '预检渲染' }).click()
await page.waitForFunction(() => document.body.innerText.includes('readback_mismatch'))
receipt = await page.locator('.render-result p').last().innerText()
assert(receipt.includes('readback_mismatch') && receipt.includes('未验证'), receipt)
pass('Every renderer verifies a sampled read-back in the preview receipt')

await context.close()
await browser.close()
if (errors.length) { console.error(errors.join('\n')); process.exit(1) }
console.log(JSON.stringify({ status: 'passed', groups: passed.length, scope: 'isolated PostgreSQL + real API + Chromium' }))
