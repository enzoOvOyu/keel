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
  hw_spec: { model: 'Reviewed-' + suffix, kind: 'server', cpu_arch: 'amd64' },
  expected_topology: { source: 'independent-fixture', numa: ['fixture-a'] },
  capability_matrix: { diagnostics: ['fixture-tool'] }, panel_layout: { source: 'fixture-layout', ports: ['p1', 'p2'] },
  bom_expect: { nic_count: 2 }, default_role_plan: { management: 'slot-2' },
  slots: [1, 2].map(n => ({ slot_no: n, pcie_slot: 'PCIe-' + n, expect_nic_model: 'fixture-nic', panel_port: 'p' + n, default_role: n === 1 ? 'compute' : 'management' }))
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
 }
 await openReview()
 // Another editor publishes while this review is open; the stale form cannot append.
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
 await page.getByText('legacy_nic_inventory_unverifiable', { exact: false }).waitFor()
 const differenceCell = page.locator('td[colspan="5"]').filter({ hasText: `型号 ${v2.id}` })
 await differenceCell.waitFor(); assert((await differenceCell.boundingBox()).width > 400, 'difference must span the issue table instead of squeezing into the code column')
 assert(await page.getByRole('button', { name: '确认导入', exact: true }).isDisabled())
 const downloadPromise = page.waitForEvent('download')
 await page.getByRole('button', { name: '导出问题 CSV', exact: true }).click()
 const exported = await readFile(await (await downloadPromise).path(), 'utf8')
 assert(exported.includes('model_version_ref,expected,actual')); assert(exported.includes(v2.id)); assert(exported.includes(difference.expected)); assert(exported.includes(difference.actual))
 await page.screenshot({ path: process.env.KEEL_WORKSPACE_EVIDENCE_DIR + '/bom-hardware-difference.png', fullPage: true })
 await page.getByRole('dialog').getByRole('button', { name: '取消', exact: true }).click()
 console.log('PASS real legacy BOM precheck refuses MAC-derived NIC inventory; UI shows version/expected/actual, disables commit and exports the full difference CSV')
 await call('POST', modelPath + '/retire', {})
 const retired = await call('POST', base + '/import', scan, 409); assert.equal(retired.code, 'MODEL_RETIRED')
 assert.equal((await call('GET', modelPath + '/versions')).items.length, 2)
 const anonymous = await browser.newContext({ baseURL: c.base_url }); assert.equal((await anonymous.request.post('/api/v1' + base + '/import', { data: scan })).status(), 401)
 assert.equal((await admin.request.post('/api/v1' + modelPath + '/versions', { headers: { Origin: c.base_url }, data: payload })).status(), 403)
 assert.deepEqual(errors, [])
 console.log('PASS full draft payload and slots survive import/reuse/review/publication; stale editor and deleted/wrong-model draft publication rejected; prior immutable version preserved')
 console.log('PASS switch kind/capability import, retired identity protection, Viewer UI/API/anonymous/CSRF denial and scoped draft discard')
} finally { await browser.close() }
