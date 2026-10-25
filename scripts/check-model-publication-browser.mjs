import assert from 'node:assert/strict'
import { readFile, stat } from 'node:fs/promises'
import { pathToFileURL } from 'node:url'
const file = process.env.KEEL_AUTH_BROWSER_CREDENTIALS_FILE
assert(file?.startsWith('/tmp/keel-models-')); assert.equal((await stat(file)).mode & 0o077, 0)
const c = JSON.parse(await readFile(file, 'utf8')); assert.equal(c.base_url, 'http://127.0.0.1:3102')
const { chromium } = await import(pathToFileURL('/tmp/keel-browser-check/node_modules/playwright/index.mjs').href)
const browser = await chromium.launch({ headless: true }), admin = await browser.newContext({ baseURL: c.base_url, viewport: { width: 1440, height: 1080 } }), page = await admin.newPage(), errors = []
page.on('pageerror', e => errors.push(e.message)); page.on('console', m => { if (m.type() === 'error' && !/status of 40[1349]/.test(m.text())) errors.push(m.text()) })
async function login(ctx, username, password) {
 const before = await (await ctx.request.get('/api/v1/auth/csrf')).json()
 const r = await ctx.request.post('/api/v1/auth/login', { headers: { Origin: c.base_url, 'X-CSRF-Token': before.csrf_token }, data: { method: 'local', username, password } }); assert.equal(r.status(), 200)
 return (await r.json()).csrf_token
}
try {
 const csrf = await login(admin, c.username, c.password), headers = { Origin: c.base_url, 'X-CSRF-Token': csrf }
 async function call(method, path, data, expected = 200, ctx = admin, token = csrf) {
  const r = await ctx.request.fetch('/api/v1' + path, { method, headers: { ...headers, 'X-CSRF-Token': token, 'Idempotency-Key': crypto.randomUUID() }, data }); const out = await r.json()
  assert.equal(r.status(), expected, `${method} ${path}: ${JSON.stringify(out)}`); return out
 }
 const suffix = crypto.randomUUID().slice(0, 8), project = await call('POST', '/projects', { name: 'Complete model review', slug: 'model-review-' + suffix }, 201), base = '/projects/' + project.id + '/models'
 const payload = {
  hw_spec: { model: 'Reviewed-' + suffix, kind: 'server', cpu_arch: 'amd64', gpu_count: 2, gpu: 'Independent-GPU-fixture' },
  expected_topology: { source: 'independent-fixture', numa: ['fixture-a'] },
  capability_matrix: { diagnostics: ['fixture-tool'] }, panel_layout: { source: 'independent-fixture-layout', layout_v1: { rows: 1, columns: 3, ports: [{ id: 'p1', label: 'NIC-1', row: 1, column: 1, slot_no: 1 }, { id: 'p2', label: 'NIC-2', row: 1, column: 2, slot_no: 2 }, { id: 'oob', label: 'OOB', row: 1, column: 3 }] } },
  bom_expect: { nic_count: 2 }, default_role_plan: { management: 'slot-2' },
  slots: [1, 2].map(n => ({ slot_no: n, pcie_slot: 'PCIe-' + n, expect_nic_model: 'fixture-nic', panel_port: 'p' + n, default_role: n === 1 ? 'compute' : 'management' }))
 }
 for (const invalid of [{ gpu_count: -1 }, { gpu_count: 1.5 }, { gpu_count: null }, { gpu_count: '2' }, { gpu: '' }, { gpu: null }, { gpu: ' invalid ' }, { gpu: ['fixture'] }]) {
  await call('POST', base + '/import', { source_host: 'invalid-gpu', hw_spec: { model: 'Invalid-GPU-' + suffix, kind: 'server', ...invalid } }, 400)
 }
 const scan = { source_host: 'fixture-node-' + suffix, ...payload }, draft = await call('POST', base + '/import', scan, 201)
 assert.equal(draft.slots.length, 2); assert.deepEqual(draft.capability_matrix, payload.capability_matrix)
 const reused = await call('POST', base + '/import', { ...scan, slots: [...scan.slots].reverse() }, 201)
 assert.equal(reused.id, draft.id); assert.deepEqual(reused.slots, payload.slots)
 const modelPath = base + '/' + draft.model_id
 async function openReview() {
  await page.goto(`/p/${project.id}/models`)
  const row = page.getByRole('row').filter({ has: page.getByText(payload.hw_spec.model, { exact: true }) }).first()
  await row.getByRole('button', { name: '版本历史', exact: true }).click()
  await page.getByRole('row').filter({ hasText: scan.source_host }).getByRole('button', { name: '核对后发布', exact: true }).click()
  for (const key of ['hw_spec', 'expected_topology', 'capability_matrix', 'panel_layout', 'bom_expect', 'default_role_plan']) {
   const id = key === 'hw_spec' ? 'hw-spec' : key.replaceAll('_', '-')
   assert.deepEqual(JSON.parse(await page.locator('#' + id).inputValue()), payload[key])
  }
  assert.equal(await page.locator('.slot-row').count(), 2)
  const panel = page.getByRole('region', { name: '型号面板预览', exact: true })
  await panel.getByRole('button', { name: 'NIC-1 计算', exact: true }).click()
  await panel.getByText('PCIe-1', { exact: true }).waitFor()
  assert.equal(await panel.getByRole('button', { name: 'OOB 未关联槽位', exact: true }).count(), 1)
 }
 await openReview()
 const invalidPanels = [null, { rows: 0, columns: 3, ports: payload.panel_layout.layout_v1.ports }, { ...payload.panel_layout.layout_v1, ports: [{ ...payload.panel_layout.layout_v1.ports[0], slot_no: 99 }] }, { ...payload.panel_layout.layout_v1, ports: [payload.panel_layout.layout_v1.ports[0], { ...payload.panel_layout.layout_v1.ports[1], column: 1 }] }]
 for (const grid of invalidPanels) {
  await call('POST', base + '/import', { ...scan, model_name: 'Invalid-panel-' + suffix, panel_layout: { layout_v1: grid } }, 400)
  await call('POST', modelPath + '/versions', { ...payload, panel_layout: { layout_v1: grid } }, 400)
 }
 await page.locator('#panel-layout').fill(JSON.stringify({ layout_v1: invalidPanels[2] }))
 await page.getByText('面板布局无效', { exact: false }).waitFor()
 await page.locator('.action-dialog button[type=submit]').click()
 await page.getByRole('alert').filter({ hasText: '面板布局无效' }).waitFor()
 await page.locator('#panel-layout').fill(JSON.stringify(payload.panel_layout))
 // Reject ambiguous PCIe/slot aliases before sending a publication request.
 let publicationRequests = 0
 const trackPublication = request => { if (request.method() === 'POST' && request.url().endsWith(modelPath + '/versions')) publicationRequests++ }
 page.on('request', trackPublication)
 for (const location of ['PCIe-2', '2']) {
  await page.getByRole('textbox', { name: 'PCIe 槽', exact: true }).first().fill(location)
  await page.locator('.action-dialog button[type=submit]').click()
  const conflict = page.getByRole('alert').filter({ hasText: '指向多个槽位' })
  await conflict.waitFor()
  assert(await conflict.evaluate(element => document.activeElement === element))
  const box = await conflict.boundingBox(); assert(box && box.y >= 0 && box.y + box.height <= 1080)
  assert.equal(publicationRequests, 0)
 }
 await page.screenshot({ path: process.env.KEEL_WORKSPACE_EVIDENCE_DIR + '/model-slot-conflict.png', fullPage: true })
 await page.getByRole('textbox', { name: 'PCIe 槽', exact: true }).first().fill('PCIe-1')
 page.off('request', trackPublication)
 for (const scopeBase of [base, '/admin/models']) {
  const invalidName = 'Invalid-slot-' + suffix + (scopeBase === base ? '-project' : '-global')
  const isolated = await call('POST', scopeBase, { name: invalidName, kind: 'server' }, 201)
  for (const location of ['PCIe-2', '2', ' PCIe-1 ']) {
   const invalid = { ...payload, slots: payload.slots.map((slot, i) => i === 0 ? { ...slot, pcie_slot: location } : slot) }
   await call('POST', scopeBase + '/import', { ...invalid, source_host: 'invalid-slot-fixture', model_name: invalidName }, 400)
   await call('POST', scopeBase + '/' + isolated.id + '/versions', { ...invalid, expected_version: 0 }, 400)
   await call('POST', scopeBase + '/import', { ...invalid, source_host: 'invalid-auto-create-fixture', model_name: invalidName + '-auto' }, 400)
  }
  assert.equal((await call('GET', scopeBase + '/' + isolated.id + '/drafts')).items.length, 0)
  assert.equal((await call('GET', scopeBase + '/' + isolated.id + '/versions')).items.length, 0)
  const identities = await call('GET', scopeBase)
  assert(!identities.items.some(model => model.name === invalidName + '-auto'))
 }
 console.log('PASS slot identity collisions blocked before UI publication and by project/global publish/import APIs; no automatic identity, draft or version left behind')
 // Another editor publishes while this review is open; the stale form cannot append.
 await call('POST', modelPath + '/versions', { ...payload, hw_spec: { ...payload.hw_spec, gpu_count: '2' }, expected_version: 0 }, 400)
 const v1 = await call('POST', modelPath + '/versions', { ...payload, expected_version: 0 }, 201)
 let response = page.waitForResponse(r => r.request().method() === 'POST' && r.url().endsWith(modelPath + '/versions'))
 await page.locator('.action-dialog button[type=submit]').click(); assert.equal((await response).status(), 409)
 await page.getByRole('alert').filter({ hasText: '型号已有新版本' }).waitFor()
 await page.getByRole('dialog').getByRole('button', { name: '取消', exact: true }).click()
 await openReview()
 await page.locator('#bom-expect').fill(JSON.stringify({ nic_count: 3, source: 'human-reviewed-fixture' }))
 await page.getByRole('dialog').evaluate(async dialog => { await Promise.all(dialog.getAnimations({ subtree: true }).filter(a => a.effect?.getTiming().iterations !== Infinity).map(a => a.finished.catch(() => {}))); dialog.scrollTop = 0; await new Promise(requestAnimationFrame) })
 await page.screenshot({ path: process.env.KEEL_WORKSPACE_EVIDENCE_DIR + '/model-complete-review.png', fullPage: true })
 response = page.waitForResponse(r => r.request().method() === 'POST' && r.url().endsWith(modelPath + '/versions'))
 await page.locator('.action-dialog button[type=submit]').click(); const r = await response; assert.equal(r.status(), 201); const v2 = await r.json(); assert.equal(v2.version, 2)
 const reviewed = { ...payload, bom_expect: { nic_count: 3, source: 'human-reviewed-fixture' } }
 for (const key of Object.keys(reviewed)) assert.deepEqual(v2[key], reviewed[key])
 const history = await call('GET', modelPath + '/versions'); assert.deepEqual(history.items.find(v => v.id === v1.id), v1)
 await page.getByRole('row').filter({ has: page.getByText(payload.hw_spec.model, { exact: true }) }).first().getByRole('button', { name: '版本历史', exact: true }).click()
 const oldRow = page.getByRole('row').filter({ has: page.getByText('v1', { exact: true }) }).last()
 await oldRow.locator('summary').filter({ hasText: '型号面板预览' }).click()
 const oldPanel = oldRow.getByRole('region', { name: '型号面板预览', exact: true })
 await oldPanel.getByRole('button', { name: 'NIC-2 管理', exact: true }).click()
 await oldPanel.getByText('PCIe-2', { exact: true }).waitFor()
 assert((await oldPanel.textContent()).includes('v1'))
 await oldPanel.screenshot({ path: process.env.KEEL_WORKSPACE_EVIDENCE_DIR + '/model-panel-history.png' })
 console.log('PASS typed model layout import/review/publication/history preview, physical coordinates and slot defaults; invalid layouts rejected without publication and legacy metadata remains compatible')

 await call('POST', base + '/import', { source_host: scan.source_host, ...reviewed }, 409)
 const other = await call('POST', base, { name: 'Other-' + suffix, kind: 'server' }, 201)
 await call('POST', base + '/' + other.id + '/drafts/' + draft.id + '/discard', {}, 404)
 assert((await call('GET', modelPath + '/drafts')).items.some(d => d.id === draft.id))
 await call('POST', base + '/' + other.id + '/versions', { ...payload, expected_version: 0, source_draft_id: draft.id }, 404)
 await call('POST', modelPath + '/drafts/' + draft.id + '/discard', {})
 const absent = await call('POST', modelPath + '/versions', { ...payload, expected_version: 2, source_draft_id: draft.id }, 404); assert.equal(absent.code, 'MODEL_DRAFT_NOT_FOUND')
 const switchDraft = await call('POST', base + '/import', { model_name: 'Switch-' + suffix, source_host: 'switch-fixture', hw_spec: { kind: 'switch' }, capability_matrix: { ports: { 1: { speeds: [100] } } } }, 201)
 assert.equal((await call('GET', base)).items.find(m => m.id === switchDraft.model_id).kind, 'switch')
 const badKind = await call('POST', base + '/import', { ...scan, hw_spec: { ...payload.hw_spec, kind: 'switch' } }, 409); assert.equal(badKind.code, 'MODEL_KIND_MISMATCH')
 const user = 'model-viewer-' + suffix, password = crypto.randomUUID() + '-Viewer!'
 await call('POST', '/admin/users', { username: user, display_name: 'Model Viewer', password }, 201); await call('POST', '/projects/' + project.id + '/members', { username: user, role: 'Viewer' }, 201)
 const viewer = await browser.newContext({ baseURL: c.base_url }), viewerToken = await login(viewer, user, password)
 await call('POST', modelPath + '/versions', { ...payload, expected_version: 2 }, 403, viewer, viewerToken)
 await call('POST', base + '/import', scan, 403, viewer, viewerToken)
 const viewerPage = await viewer.newPage(); await viewerPage.goto(`/p/${project.id}/models`)
 await viewerPage.getByRole('row').filter({ has: viewerPage.getByText('Switch-' + suffix, { exact: true }) }).getByRole('button', { name: '版本历史', exact: true }).click()
 await viewerPage.getByText('switch-fixture', { exact: true }).waitFor(); assert.equal(await viewerPage.getByRole('button', { name: '核对后发布', exact: true }).count(), 0)
 // The compatibility importer must not infer NIC hardware from MAC rows.
 await page.goto(`/p/${project.id}/assets`)
 await page.getByRole('button', { name: '宽表 BOM 兼容导入', exact: true }).click()
 await page.locator('#bom-legacy-batch').fill(`BOM-BATCH-${project.slug}-20261024-1`)
 await page.locator('#bom-legacy-file').setInputFiles({ name: 'wide.csv', mimeType: 'text/csv', buffer: Buffer.from(`SN,hostname,model_name,MAC,port_name,rail\nUI-NIC-1,ui-nic-1,${payload.hw_spec.model},aa:bb:cc:33:44:55,p1,0\n`) })
 const hardwareResponse = page.waitForResponse(r => r.request().method() === 'POST' && r.url().includes('/bom/legacy/preview'))
 await page.getByRole('dialog').getByRole('button', { name: '预检', exact: true }).click()
 const hardware = await (await hardwareResponse).json()
 assert.equal(hardware.can_commit, false)
 const difference = hardware.issues.find(i => i.code === 'legacy_nic_inventory_unverifiable')
 assert.equal(difference.model_version_ref, v2.id); assert.equal(difference.bom_line_no, 2)
 const gpuDifference = hardware.issues.find(i => i.code === 'legacy_gpu_inventory_unverifiable')
 assert.equal(gpuDifference.model_version_ref, v2.id); assert.equal(gpuDifference.bom_line_no, 2)
 assert(gpuDifference.expected.includes('physical GPU inventory')); assert(gpuDifference.actual.includes('MAC rows'))
 await page.getByText('legacy_nic_inventory_unverifiable', { exact: false }).waitFor()
 const differenceCell = page.locator('td[colspan="5"]').filter({ hasText: `型号 ${v2.id}` }).first()
 await differenceCell.waitFor(); assert((await differenceCell.boundingBox()).width > 400, 'difference must span the issue table instead of squeezing into the code column')
 assert(await page.getByRole('button', { name: '确认导入', exact: true }).isDisabled())
 const downloadPromise = page.waitForEvent('download')
 await page.getByRole('button', { name: '导出问题 CSV', exact: true }).click()
 const exported = await readFile(await (await downloadPromise).path(), 'utf8')
 assert(exported.includes('model_version_ref,expected,actual')); assert(exported.includes(v2.id)); assert(exported.includes(difference.expected)); assert(exported.includes(difference.actual))
 await page.screenshot({ path: process.env.KEEL_WORKSPACE_EVIDENCE_DIR + '/bom-hardware-difference.png', fullPage: true })
 await page.getByRole('dialog').getByRole('button', { name: '取消', exact: true }).click()
 console.log('PASS real legacy BOM precheck refuses MAC-derived NIC/GPU inventory; UI shows version/expected/actual, disables commit and exports the full difference CSV')

 // A separate specification checks MAC identity without claiming physical inventory.
 const macModel = await call('POST', base, { name: 'MAC-rule-' + suffix, kind: 'server' }, 201)
 const macPath = base + '/' + macModel.id
 for (const value of [null, [], ['aa:bb'], ['aa:bb:cc', 'AA:BB:CC']]) {
  await call('POST', macPath + '/versions', { bom_expect: { mac_prefixes: value } }, 400)
  await call('POST', base + '/import', { model_name: 'Invalid-' + suffix, source_host: 'fixture', hw_spec: { kind: 'server' }, bom_expect: { mac_prefixes: value } }, 400)
 }
 const macVersion = await call('POST', macPath + '/versions', { bom_expect: { mac_prefixes: ['00:11:22'] } }, 201)
 await page.getByRole('button', { name: '宽表 BOM 兼容导入', exact: true }).click()
 await page.locator('#bom-legacy-batch').fill(`BOM-BATCH-${project.slug}-20261024-2`)
 await page.locator('#bom-legacy-file').setInputFiles({ name: 'mac.csv', mimeType: 'text/csv', buffer: Buffer.from(`SN,hostname,model_name,MAC,port_name,rail\nUI-MAC-1,ui-mac-1,MAC-rule-${suffix},aa:bb:cc:33:44:55,p1,0\n`) })
 const macResponse = page.waitForResponse(r => r.request().method() === 'POST' && r.url().includes('/bom/legacy/preview'))
 await page.getByRole('dialog').getByRole('button', { name: '预检', exact: true }).click()
 const macPreview = await (await macResponse).json()
 assert.equal(macPreview.can_commit, false); assert(!macPreview.preview_token)
 const macIssues = macPreview.issues.filter(i => i.code === 'model_mac_prefix_mismatch')
 assert(macIssues.length >= 1)
 for (const issue of macIssues) {
  assert.equal(issue.model_version_ref, macVersion.id); assert.equal(issue.bom_line_no, 2)
  assert.equal(issue.expected, '00:11:22'); assert.equal(issue.actual, 'aa:bb:cc:33:44:55')
 }
 await page.getByText('model_mac_prefix_mismatch', { exact: false }).first().waitFor()
 assert(await page.getByRole('button', { name: '确认导入', exact: true }).isDisabled())
 const macDownload = page.waitForEvent('download')
 await page.getByRole('button', { name: '导出问题 CSV', exact: true }).click()
 const macCSV = await readFile(await (await macDownload).path(), 'utf8')
 for (const text of [macVersion.id, '00:11:22', 'aa:bb:cc:33:44:55']) assert(macCSV.includes(text))
 await page.screenshot({ path: process.env.KEEL_WORKSPACE_EVIDENCE_DIR + '/bom-mac-prefix-difference.png', fullPage: true })
 await page.getByRole('dialog').getByRole('button', { name: '取消', exact: true }).click()
 console.log('PASS MAC-prefix publish/import validation and real wide-BOM mismatch blocking with source line, immutable model version, disabled commit and exported CSV')
 await call('POST', modelPath + '/retire', {})
 const retired = await call('POST', base + '/import', scan, 409); assert.equal(retired.code, 'MODEL_RETIRED')
 assert.equal((await call('GET', modelPath + '/versions')).items.length, 2)
 const anonymous = await browser.newContext({ baseURL: c.base_url }); assert.equal((await anonymous.request.post('/api/v1' + base + '/import', { data: scan })).status(), 401)
 assert.equal((await admin.request.post('/api/v1' + modelPath + '/versions', { headers: { Origin: c.base_url }, data: payload })).status(), 403)
 assert.deepEqual(errors, [])
 console.log('PASS full draft payload and slots survive import/reuse/review/publication; stale editor and deleted/wrong-model draft publication rejected; prior immutable version preserved')
 console.log('PASS switch kind/capability import, retired identity protection, Viewer UI/API/anonymous/CSRF denial and scoped draft discard')
} finally { await browser.close() }
