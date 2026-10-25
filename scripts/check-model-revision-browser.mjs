import assert from 'node:assert/strict'
import { readFile, stat } from 'node:fs/promises'
import { pathToFileURL } from 'node:url'
const file = process.env.KEEL_AUTH_BROWSER_CREDENTIALS_FILE
assert(file?.startsWith('/tmp/keel-models-')); assert.equal((await stat(file)).mode & 0o077, 0)
const c = JSON.parse(await readFile(file, 'utf8')); assert.equal(c.base_url, 'http://127.0.0.1:3102')
const { chromium } = await import(pathToFileURL('/tmp/keel-browser-check/node_modules/playwright/index.mjs').href)
const browser = await chromium.launch({ headless: true }), admin = await browser.newContext({ baseURL: c.base_url, viewport: { width: 1440, height: 1080 } }), page = await admin.newPage(), errors = []
page.on('pageerror', e => errors.push(e.message))
async function login(ctx, username, password) {
 const before = await (await ctx.request.get('/api/v1/auth/csrf')).json()
 const r = await ctx.request.post('/api/v1/auth/login', { headers: { Origin: c.base_url, 'X-CSRF-Token': before.csrf_token }, data: { method: 'local', username, password } }); assert.equal(r.status(), 200)
 return (await r.json()).csrf_token
}
try {
 const csrf = await login(admin, c.username, c.password)
 async function call(method, path, data, expected = 200) {
  const r = await admin.request.fetch('/api/v1' + path, { method, headers: { Origin: c.base_url, 'X-CSRF-Token': csrf, 'Idempotency-Key': crypto.randomUUID() }, data })
  const out = await r.json(); assert.equal(r.status(), expected, `${method} ${path}: ${JSON.stringify(out)}`); return out
 }
 const suffix = crypto.randomUUID().slice(0, 8), project = await call('POST', '/projects', { name: 'Model revision fixtures', slug: 'model-revision-' + suffix }, 201)
 const payload = {
  hw_spec: { kind: 'compute-tray', gpu_count: 2, gpu: 'fixture-gpu', metadata: { source: 'independent-spec' } },
  expected_topology: { numa: ['fixture-a'] }, capability_matrix: { diagnostics: ['fixture-tool'] },
  panel_layout: { metadata: { preserve: true }, layout_v1: { rows: 1, columns: 2, ports: [{ id: 'p1', label: 'Original port', row: 1, column: 2, slot_no: 1 }] } },
  bom_expect: { nic_count: 1 }, default_role_plan: { management: 'slot-1' },
  slots: [{ slot_no: 1, pcie_slot: 'PCIe-1', expect_nic_model: 'fixture-nic', panel_port: 'p1', default_role: 'unused' }]
 }
 const username = 'revision-viewer-' + suffix, password = crypto.randomUUID() + '-Viewer!'
 await call('POST', '/admin/users', { username, display_name: 'Revision Viewer', password }, 201)
 await call('POST', '/projects/' + project.id + '/members', { username, role: 'Viewer' }, 201)
 const viewer = await browser.newContext({ baseURL: c.base_url }); await login(viewer, username, password)
 for (const global of [false, true]) {
  const base = global ? '/admin/models' : '/projects/' + project.id + '/models', route = global ? '/admin/models' : `/p/${project.id}/models`
  const name = 'Revision-' + suffix + (global ? '-global' : '-private'), model = await call('POST', base, { name, kind: 'compute-tray' }, 201), path = base + '/' + model.id
  const v1 = await call('POST', path + '/versions', { ...payload, expected_version: 0 }, 201)
  const v2 = await call('POST', path + '/versions', { ...payload, expected_version: 1, hw_spec: { ...payload.hw_spec, gpu_count: 4 }, panel_layout: { ...payload.panel_layout, layout_v1: { ...payload.panel_layout.layout_v1, ports: [{ ...payload.panel_layout.layout_v1.ports[0], label: 'Latest port' }] } } }, 201)
  const row = () => page.getByRole('row').filter({ has: page.getByText(name, { exact: true }) }).first()
  async function openSource() {
   await page.goto(route); await row().getByRole('button', { name: '版本历史', exact: true }).click()
   await page.getByRole('button', { name: '基于 v1 修订', exact: true }).click()
   for (const key of ['hw_spec', 'expected_topology', 'capability_matrix', 'panel_layout', 'bom_expect', 'default_role_plan']) assert.deepEqual(JSON.parse(await page.locator('#' + key.replaceAll('_', '-')).inputValue()), payload[key])
   assert.equal(await page.locator('.slot-row').count(), 1)
   assert.equal(await page.getByRole('combobox', { name: '默认角色', exact: true }).inputValue(), 'unused')
   await page.getByText('v1 · ' + v1.id, { exact: true }).waitFor()
   await page.getByText('已载入 v1 的完整规格。', { exact: false }).waitFor()
  }
  await openSource()
  const changed = { ...payload.hw_spec, revision: 'reviewed revision of original' }
  await page.locator('#hw-spec').fill(JSON.stringify(changed))
  let requestBody
  const response = page.waitForResponse(r => r.request().method() === 'POST' && r.url().endsWith(path + '/versions'))
  const capture = r => { if (r.method() === 'POST' && r.url().endsWith(path + '/versions')) requestBody = r.postDataJSON() }
  page.on('request', capture); await page.getByRole('dialog').locator('button[type=submit]').click()
  const r = await response; assert.equal(r.status(), 201); const v3 = await r.json(); page.off('request', capture)
  assert.equal(v3.version, 3); assert.deepEqual(v3.hw_spec, changed)
  for (const key of ['expected_topology', 'capability_matrix', 'panel_layout', 'bom_expect', 'default_role_plan', 'slots']) assert.deepEqual(v3[key], payload[key])
  assert.equal(requestBody.expected_version, 2); assert(!('source_draft_id' in requestBody))
  const before = await call('GET', path + '/versions'); assert.deepEqual(before.items.find(v => v.id === v1.id), v1); assert.deepEqual(before.items.find(v => v.id === v2.id), v2)
  await openSource()
  const v4 = await call('POST', path + '/versions', { ...payload, expected_version: 3, hw_spec: { ...payload.hw_spec, revision: 'concurrent editor' } }, 201)
  const conflict = page.waitForResponse(r => r.request().method() === 'POST' && r.url().endsWith(path + '/versions'))
  await page.getByRole('dialog').locator('button[type=submit]').click(); assert.equal((await conflict).status(), 409)
  await page.getByRole('alert').filter({ hasText: '型号已有新版本' }).waitFor()
  assert.equal((await call('GET', path + '/versions')).total, 4)
  await page.getByRole('dialog').getByRole('button', { name: '取消', exact: true }).click()
  if (global) {
   await page.goto(`/p/${project.id}/models`); await row().getByRole('button', { name: '版本历史', exact: true }).click()
   await page.getByText(v1.content_hash.slice(0, 16) + '…', { exact: false }).first().waitFor()
   assert.equal(await page.getByRole('button', { name: /基于 v\d+ 修订/ }).count(), 0)
  } else {
   const vp = await viewer.newPage(); await vp.goto(route)
   await vp.getByRole('row').filter({ has: vp.getByText(name, { exact: true }) }).getByRole('button', { name: '版本历史', exact: true }).click()
   await vp.getByText(v1.content_hash.slice(0, 16) + '…', { exact: false }).first().waitFor()
   assert.equal(await vp.getByRole('button', { name: /基于 v\d+ 修订/ }).count(), 0); await vp.close()
  }
  await page.goto(route); await row().getByRole('button', { name: '版本历史', exact: true }).click()
  await page.getByRole('button', { name: 'Switch to English', exact: true }).click()
  await page.getByRole('button', { name: 'Revise from v1', exact: true }).click()
  await page.getByText('Loaded the complete specification from v1.', { exact: false }).waitFor()
  await page.screenshot({ path: process.env.KEEL_WORKSPACE_EVIDENCE_DIR + `/model-revision-${global ? 'global' : 'project'}.png`, fullPage: true })
  await page.getByRole('dialog').getByRole('button', { name: 'Cancel', exact: true }).click()
  await page.getByRole('button', { name: '切换为中文', exact: true }).click()
  await call('POST', path + '/retire', {})
  await page.reload(); await row().getByRole('button', { name: '版本历史', exact: true }).click()
  await page.getByText(v4.content_hash.slice(0, 16) + '…', { exact: false }).first().waitFor()
  assert.equal(await page.getByRole('button', { name: /基于 v\d+ 修订/ }).count(), 0)
 }
 await viewer.close(); assert.deepEqual(errors, [])
 console.log('PASS project/global revisions copy complete selected historical payloads, preserve prior versions, create sequential versions without draft IDs, reject stale publication, and restrict Viewer/shared/retired edits; bilingual UI verified')
} finally { await browser.close() }
