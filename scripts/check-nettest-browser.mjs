/** Browser acceptance for the RoCE connectivity page (WP-3.3): plan, five-layer
 *  progress, heat matrix, admission, and the G5 dashboard mini bar. */
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
}, `nt-ui-${suffix}`)
const pid = project.id

// Seed: two B300 machines with four slots each, the default role plan, a
// published mapping (G3), verified applies for every required renderer (G4),
// the platform handshake and a fully ingested all-pass run (G5).
const prepared = await page.evaluate(async ([projectId, runSuffix]) => {
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
  const machine = (serial, hostname, baseMac) => ({
    serial_number: serial, rack: '', unit_position: '', bmc_address: '', hostname, ip_reference: '', model_name: 'B300-8U',
    ports: [1, 2, 3, 4].map((index) => ({ mac_address: `${baseMac}:00:0${index}`, port_name: `port${index}`, rail: index > 3 ? 1 : 0 })),
  })
  await post(`/projects/${projectId}/bom/commit`, { file_version: 'v1', machines: [
    machine(`SN-NU-${runSuffix}`, 'ntu-node-1', 'aa:bb:cc:c2'), machine(`SN-NV-${runSuffix}`, 'ntu-node-2', 'aa:bb:cc:c3'),
  ] })
  await send('PUT', `/projects/${projectId}/roles`, {
    roles: { management: { enabled: true, required: true }, storage: { enabled: true, required: true },
             compute: { enabled: true, required: true }, oob: { enabled: true, required: false } },
    slot_overrides: {},
  })
  const assets = await (await (await fetch(`/api/v1/projects/${projectId}/assets?page=1&page_size=20`, { credentials: 'same-origin' })).json()).items
  const ids = { 'ntu-node-1': assets.find((a) => a.hostname === 'ntu-node-1').machine_id, 'ntu-node-2': assets.find((a) => a.hostname === 'ntu-node-2').machine_id }
  for (const mid of Object.values(ids)) {
    await post(`/projects/${projectId}/collections`, { level: 'L1', machine_ids: [mid] })
    await post(`/projects/${projectId}/collections`, { level: 'L2', machine_ids: [mid] })
    await post(`/projects/${projectId}/gates/evaluate`, { gate: 'G1', machine_ids: [mid] })
  }
  await post(`/projects/${projectId}/lldp/snapshots`, { snapshots: Object.entries(ids).map(([hostname, mid]) => ({
    machine_id: mid, ports: Object.fromEntries([1, 2, 3, 4].map((index) => [`port${index}`, { peer: 'sw1', peer_port: `Eth1/${index}`, link_up: true, dom_warning: false, flapping: false }])),
  })) })
  const entries = { port1: ['mgmt0', '', ''], port2: ['stor0', '', ''], port3: ['nic-r0-p1', '18:00.0', 'gpu-0'], port4: ['nic-r1-p1', '5a:00.0', 'gpu-1'] }
  for (const mid of Object.values(ids)) {
    const ports = await (await (await fetch(`/api/v1/projects/${projectId}/machines/${mid}/ports`, { credentials: 'same-origin' })).json()).items
    const byName = Object.fromEntries(ports.map((p) => [p.port_name, p]))
    await post(`/projects/${projectId}/mappings/publish`, { machine_id: mid, note: 'nettest ui fixture',
      entries: Object.entries(entries).map(([name, [iface, bdf, gpu]]) => ({ port_id: byName[name].id, os_interface: iface, mac_address: byName[name].mac_address, bdf, switch_port: 'sw1', gpu_affinity: gpu })) })
    await post(`/projects/${projectId}/pcie/snapshots`, { machine_id: mid, devices: [
      { role: 'gpu', index: 0, bdf: '01:00.0', upstream: ['slot-1'], numa_node: 0, lnk_cap_width: 16, lnk_sta_width: 16, slot: 'slot-1' },
      { role: 'gpu', index: 1, bdf: '02:00.0', upstream: ['slot-2'], numa_node: 1, lnk_cap_width: 16, lnk_sta_width: 16, slot: 'slot-2' },
      { role: 'nic', index: 0, bdf: '18:00.0', upstream: ['slot-3'], numa_node: 0, lnk_cap_width: 16, lnk_sta_width: 16, slot: 'slot-3' },
      { role: 'nic', index: 1, bdf: '5a:00.0', upstream: ['slot-4'], numa_node: 1, lnk_cap_width: 16, lnk_sta_width: 16, slot: 'slot-4' },
    ], expected: { slot_by_role: {}, gpu_numa_group: [0, 1], nvlink_pairs: [] }, nvlink_raw: '' })
  }
  const draft = await (await post(`/projects/${projectId}/config-sets`, { name: 'roce-ui', scope: 'model', model_name: 'B300-8U', template: 'mtu={{param.mtu}}', keys: JSON.stringify({ mtu: 9000 }) })).json()
  return { draftId: draft.id, machineIds: Object.values(ids) }
}, [pid, suffix])

// A ConfigSet is approved by a second project admin: the creator may not
// approve their own draft (self-approval is intercepted).
const approverName = `ntu-appr-${suffix}`
const approverPassword = randomBytes(12).toString('base64url')
await page.evaluate(async ([projectId, name, password]) => {
  const csrf = await (await fetch('/api/v1/auth/csrf', { credentials: 'same-origin' })).json()
  const send = (method, path, body) => fetch(`/api/v1${path}`, { method, credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf.csrf_token, 'Idempotency-Key': crypto.randomUUID() }, body: JSON.stringify(body) })
  await send('POST', '/admin/users', { username: name, display_name: 'NT approver', password, is_admin: false })
  await send('POST', `/projects/${projectId}/members`, { username: name, role: 'Admin' })
}, [pid, approverName, approverPassword])
const approverContext = await browser.newContext({ baseURL: credentials.base_url, locale: 'zh-CN', reducedMotion: 'reduce' })
const approverPage = await approverContext.newPage()
await approverPage.goto('/login')
await approverPage.locator('#username').fill(approverName)
await approverPage.locator('#password').fill(approverPassword)
await approverPage.locator('#login-form button[type=submit]').click()
await approverPage.waitForURL('**/projects')
const seed = await approverPage.evaluate(async ([projectId, draftId]) => {
  const csrf = await (await fetch('/api/v1/auth/csrf', { credentials: 'same-origin' })).json()
  const response = await fetch(`/api/v1/projects/${projectId}/config-sets/${draftId}/approve`, { method: 'POST', credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf.csrf_token }, body: '{}' })
  if (!response.ok) throw new Error(`approve failed with ${response.status}`)
  return await response.json()
}, [pid, prepared.draftId])
await approverContext.close()
assert.equal(seed.status ?? seed.state, 'approved', 'the ConfigSet reaches the approved state')

await page.evaluate(async ([projectId, draftId, machineIds]) => {
  const csrf = await (await fetch('/api/v1/auth/csrf', { credentials: 'same-origin' })).json()
  const send = (method, path, body) => fetch(`/api/v1${path}`, { method, credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf.csrf_token, 'Idempotency-Key': crypto.randomUUID() }, body: JSON.stringify(body) })
  const post = (path, body) => send('POST', path, body)
  const net = { net: { management: { subnet: '10.10.0.0/24', gateway: '10.10.0.1', mtu: 1500 }, storage: { subnet: '10.20.0.0/24' }, compute: { subnet: '10.30.0.0/24', mtu: 9000 } } }
  const readbacks = {
    gpu_network: { role: 'compute', address: '10.30.0.13/24', mtu: '9000' },
    nic_partition: { pf: '2', total_gbps: '610' },
    roce_import: { mtu: 9000 },
    gpu_nic_binding: { NCCL_NETDEVS_POLICY: 'GPU:0:nic-r0-p1,GPU:1:nic-r1-p1', gpu: '0', nic: 'nic-r0-p1' },
  }
  for (const mid of machineIds) {
    for (const [kind, readback] of Object.entries(readbacks)) {
      const body = { kind, machine_id: mid, config_set_id: draftId, params: kind === 'gpu_network' ? net : kind === 'roce_import' ? { mtu: 9000 } : {}, readback }
      const applied = await (await send('POST', `/projects/${projectId}/config/render?apply=1`, body)).json()
      if (!applied.verified) throw new Error(`render ${kind} not verified: ${JSON.stringify(applied)}`)
    }
  }
  await send('PUT', `/projects/${projectId}/nettest/platform`, { status: 'ready', detail: { switch: 'sw1', counters: 'pfc,ecn' } })
  const run = await (await post(`/projects/${projectId}/nettest/runs`, { machine_ids: machineIds, profile: 'quick' })).json()
  const byHost = { 'ntu-node-1': {}, 'ntu-node-2': {} }
  for (const mid of machineIds) {
    const ports = await (await (await fetch(`/api/v1/projects/${projectId}/machines/${mid}/ports`, { credentials: 'same-origin' })).json()).items
    const hostname = mid === machineIds[0] ? 'ntu-node-1' : 'ntu-node-2'
    // The port panel does not repeat machine_id per port; carry it explicitly
    // so every pair row references the machine it was measured on.
    byHost[hostname] = Object.fromEntries(ports.map((p) => [p.port_name, { ...p, machine_id: mid }]))
  }
  const results = []
  for (const hostname of Object.keys(byHost)) {
    for (const port of Object.values(byHost[hostname])) {
      results.push({ layer: 'L1', machine_id: port.machine_id, port_id: port.id, peer_machine_id: port.machine_id, peer_port_id: port.id, state: 'pass', detail: { link_up: true, rate: '200G' } })
    }
  }
  for (const layer of ['L2', 'L3', 'L4']) {
    for (const a of Object.values(byHost['ntu-node-1'])) {
      for (const b of Object.values(byHost['ntu-node-2'])) {
        if (a.rail !== b.rail) continue
        results.push({ layer, machine_id: a.machine_id, port_id: a.id, peer_machine_id: b.machine_id, peer_port_id: b.id, state: 'pass', bandwidth_gbps: 196.5, loss_ppm: 0, pfc_pause: 12, hotspot: layer === 'L4' })
      }
    }
  }
  for (const hostname of Object.keys(byHost)) {
    const rails = [...new Set(Object.values(byHost[hostname]).map((p) => p.rail))]
    for (const rail of rails) {
      const first = Object.values(byHost[hostname]).find((p) => p.rail === rail)
      results.push({ layer: 'L5', machine_id: first.machine_id, port_id: first.id, peer_machine_id: first.machine_id, peer_port_id: '', state: 'pass', pfc_pause: 3, detail: { rail, pfc_watchdog: 'ok', ecn_rate: 0.001 } })
    }
  }
  const ingested = await send('POST', `/projects/${projectId}/nettest/runs/${run.id}/results`, { results })
  if (!ingested.ok) throw new Error(`ingest failed with ${ingested.status}: ${await ingested.text()}`)
  const finished = await (await send('POST', `/projects/${projectId}/nettest/runs/${run.id}/finish`, {})).json()
  if (finished.conclusion !== 'PASS') throw new Error(`run conclusion ${finished.conclusion} coverage ${JSON.stringify(finished.coverage)}`)
}, [pid, prepared.draftId, prepared.machineIds])

// 1. The page renders the full workspace: heading, platform, plan form.
await page.goto(`/p/${pid}/nettest`)
await page.waitForSelector('.nettest-layers, .management-panel')
assert.match(await page.locator('h1').textContent(), /RoCE/)
assert.match(await page.locator('.management-panel', { hasText: '网络平台对接' }).textContent(), /就绪/)
pass('The NetTest page renders with a ready platform handshake')

// 2. The plan form defaults to every asset and the quick profile.
await page.waitForSelector('.nettest-targets input[type=checkbox]')
const checked = await page.locator('.nettest-targets input[type=checkbox]:checked').count()
assert.equal(checked, 2, 'both machines pre-selected')
assert.equal(await page.locator('#nettest-profile').inputValue(), 'quick')
pass('The plan form pre-selects the project assets and the quick profile')

// 3. The runs table lists the seeded run with all five layers covered.
await page.waitForSelector('.nettest-run-layers .nettest-chip.enabled')
const chips = await page.locator('.nettest-run-layers .nettest-chip').allTextContents()
assert.deepEqual(chips, ['L1', 'L2', 'L3', 'L4', 'L5'])
assert.equal(await page.locator('.nettest-run-layers .nettest-chip.enabled').count(), 5)
pass('The runs table shows every layer covered for the all-pass run')

// 4. Opening the run reveals the layer progress, coverage badges and pairs.
await page.locator('button.link-button').first().click()
await page.waitForSelector('.nettest-layer')
assert.equal(await page.locator('.nettest-layer').count(), 5)
assert.equal(await page.locator('.nettest-layer-icon.pass').count(), 5)
const summary = await page.locator('.management-panel:has(.nettest-run-head)').textContent()
assert.match(summary, /完整/)
assert.match(summary, /G5 证据/)
const pairRows = await page.locator('.data-table tbody tr').count()
assert(pairRows >= 42, `pair rows ${pairRows}`)
pass('The run detail renders five-layer progress, coverage badges and every pair')

// 5. The heat matrix draws an ECharts canvas with one cell per machine pair.
await page.waitForSelector('.nettest-matrix canvas')
const matrixCells = await page.evaluate(() => document.querySelectorAll('.nettest-matrix canvas').length)
assert.equal(matrixCells, 1)
pass('The rail heat matrix renders an ECharts canvas')

// 6. The admission view reports both machines admitted.
await page.waitForSelector('.management-panel', { hasText: 'NCCL 准入' })
const admitted = await page.locator('.management-panel', { hasText: 'NCCL 准入' }).locator('.status-label.enabled').count()
assert.equal(admitted, 2)
pass('The NCCL admission view admits both machines on the G5 PASS')

// 7. The dashboard G5 card carries the five-segment layer pass-rate bar.
await page.goto(`/p/${pid}/dashboard`)
await page.waitForSelector('.gate-layer-bars')
const rows = await page.locator('.gate-layer-bars .gate-layer-row').count()
assert.equal(rows, 5)
const g5 = await page.locator('.gate-card[data-state="pass"]', { hasText: 'RoCE' }).count()
assert.equal(g5, 1)
pass('The dashboard G5 card shows the L1-L5 layer pass-rate mini bar')

assert.deepEqual(errors, [], `unexpected page errors: ${errors.join(' | ')}`)
console.log(JSON.stringify({ status: 'passed', groups: passed }, null, 1))
await browser.close()
