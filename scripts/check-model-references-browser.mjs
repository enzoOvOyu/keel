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
 const csrf = await login(admin, c.username, c.password), suffix = crypto.randomUUID().slice(0, 8)
 async function call(method, path, data, expected = 200, ctx = admin, token = csrf) {
  const r = await ctx.request.fetch('/api/v1' + path, { method, headers: { Origin: c.base_url, 'X-CSRF-Token': token, 'Idempotency-Key': crypto.randomUUID() }, data })
  const out = await r.json(); assert.equal(r.status(), expected, `${method} ${path}: ${JSON.stringify(out)}`); return out
 }
 const a = await call('POST', '/projects', { name: 'Reference A', slug: 'reference-a-' + suffix }, 201), b = await call('POST', '/projects', { name: 'Reference B', slug: 'reference-b-' + suffix }, 201)
 const shared = await call('POST', '/admin/models', { name: 'Shared-count-' + suffix, kind: 'server' }, 201), privateModel = await call('POST', `/projects/${a.id}/models`, { name: 'Private-count-' + suffix, kind: 'server' }, 201)
 const globalPath = '/admin/models/' + shared.id, privatePath = `/projects/${a.id}/models/${privateModel.id}`
 const v1 = await call('POST', globalPath + '/versions', { hw_spec: { source: 'independent-reference-fixture' } }, 201)
 await call('POST', privatePath + '/versions', { hw_spec: { source: 'private-reference-fixture' } }, 201)
 async function bind(project, model, seq, mac) {
  const csv = Buffer.from(`SN,hostname,model_name,MAC,port_name,rail\nREF-${suffix}-${project.id}-${seq},ref-${seq},${model.name},${mac},p1,0\n`)
  const form = () => ({ file: { name: 'reference.csv', mimeType: 'text/csv', buffer: csv }, bom_import_batch_id: `BOM-BATCH-${project.slug}-20261025-${seq}` })
  const headers = { Origin: c.base_url, 'X-CSRF-Token': csrf }
  let response = await admin.request.post(`/api/v1/projects/${project.id}/bom/legacy/preview`, { headers, multipart: form() }); assert.equal(response.status(), 200)
  const preview = await response.json(); assert.equal(preview.can_commit, true)
  response = await admin.request.post(`/api/v1/projects/${project.id}/bom/legacy/commit`, { headers, multipart: { ...form(), preview_token: preview.preview_token } }); assert.equal(response.status(), 201)
 }
 assert.equal((await call('GET', `/projects/${a.id}/models`)).items.find(m => m.id === shared.id).reference_count, 0)
 await bind(a, shared, 1, 'aa:bb:cc:01:01:01')
 const v2 = await call('POST', globalPath + '/versions', { hw_spec: { revision: 'second-independent-spec' }, expected_version: 1 }, 201)
 await bind(b, shared, 1, 'aa:bb:cc:01:01:02')
 await bind(a, privateModel, 2, 'aa:bb:cc:01:01:03')
 const asset = (await call('GET', `/projects/${a.id}/assets`)).items.find(a => a.model_id === shared.id)
 assert.equal(asset.model_version_id, v1.id); assert.notEqual(asset.model_version_id, v2.id)
 const username = 'reference-viewer-' + suffix, password = crypto.randomUUID() + '-Viewer!'
 await call('POST', '/admin/users', { username, display_name: 'Reference Viewer', password }, 201); await call('POST', `/projects/${a.id}/members`, { username, role: 'Viewer' }, 201)
 const viewer = await browser.newContext({ baseURL: c.base_url }), token = await login(viewer, username, password)
 const scoped = await call('GET', `/projects/${a.id}/models`, undefined, 200, viewer, token)
 assert.equal(scoped.items.find(m => m.id === shared.id).reference_count, 1); assert.equal(scoped.items.find(m => m.id === privateModel.id).reference_count, 1)
 await call('GET', `/projects/${b.id}/models`, undefined, 404, viewer, token); await call('GET', '/admin/models', undefined, 403, viewer, token)
 const vp = await viewer.newPage(); await vp.goto(`/p/${a.id}/models`)
 await vp.getByRole('columnheader', { name: '绑定资产数', exact: true }).waitFor()
 for (const model of [shared, privateModel]) assert.equal(await vp.getByRole('row').filter({ has: vp.getByText(model.name, { exact: true }) }).getByRole('cell').nth(4).textContent(), '1')
 await vp.getByText('绑定资产数仅统计当前项目', { exact: false }).waitFor()
 await page.goto('/admin/models')
 const row = () => page.getByRole('row').filter({ has: page.getByText(shared.name, { exact: true }) })
 assert.equal(await row().getByRole('cell').nth(4).textContent(), '2')
 assert.equal((await call('GET', '/admin/models')).items.find(m => m.id === shared.id).reference_count, 2)
 await call('POST', globalPath + '/retire', {})
 await page.reload(); await row().getByText('已退役', { exact: true }).waitFor(); assert.equal(await row().getByRole('cell').nth(4).textContent(), '2')
 await page.getByRole('button', { name: 'Switch to English', exact: true }).click()
 await page.getByRole('columnheader', { name: 'Bound assets', exact: true }).waitFor()
 await page.getByText('Bound assets includes all projects', { exact: false }).waitFor()
 await page.screenshot({ path: process.env.KEEL_WORKSPACE_EVIDENCE_DIR + '/model-reference-counts.png', fullPage: true })
 await viewer.close(); assert.deepEqual(errors, [])
 console.log('PASS model reference counts are scoped per project, aggregate global bindings only for platform admins, retain pinned old versions and retired references, and display bilingual UI; actual BOM commits and Viewer denial verified')
} finally { await browser.close() }
