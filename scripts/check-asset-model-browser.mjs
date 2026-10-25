import assert from 'node:assert/strict'
import { readFile, stat } from 'node:fs/promises'
import { pathToFileURL } from 'node:url'
const file = process.env.KEEL_AUTH_BROWSER_CREDENTIALS_FILE
assert(file?.startsWith('/tmp/keel-models-')); assert.equal((await stat(file)).mode & 0o077, 0)
const c = JSON.parse(await readFile(file, 'utf8')); assert.equal(c.base_url, 'http://127.0.0.1:3102')
const { chromium } = await import(pathToFileURL('/tmp/keel-browser-check/node_modules/playwright/index.mjs').href)
const browser = await chromium.launch({ headless: true }), context = await browser.newContext({ baseURL: c.base_url, viewport: { width: 1440, height: 1080 }, reducedMotion: 'reduce' }), page = await context.newPage(), errors = []
page.on('pageerror', error => errors.push(error.message)); page.on('console', message => { if (message.type() === 'error' && !/status of 40[1349]/.test(message.text())) errors.push(message.text()) })
async function login(ctx, username, password) {
 const before = await (await ctx.request.get('/api/v1/auth/csrf')).json()
 const response = await ctx.request.post('/api/v1/auth/login', { headers: { Origin: c.base_url, 'X-CSRF-Token': before.csrf_token }, data: { method: 'local', username, password } }); assert.equal(response.status(), 200)
 return (await response.json()).csrf_token
}
try {
 const csrf = await login(context, c.username, c.password)
 async function call(method, path, data, expected = 200, ctx = context, token = csrf) {
  const response = await ctx.request.fetch('/api/v1' + path, { method, headers: { Origin: c.base_url, 'X-CSRF-Token': token, 'Idempotency-Key': crypto.randomUUID() }, data })
  const value = await response.json(); assert.equal(response.status(), expected, `${method} ${path}: ${JSON.stringify(value)}`); return value
 }
 const suffix = crypto.randomUUID().slice(0, 8), project = await call('POST', '/projects', { name: 'Pinned model assets', slug: 'asset-model-' + suffix }, 201), base = '/projects/' + project.id
 const model = await call('POST', base + '/models', { name: 'Pinned-model-' + suffix, kind: 'compute-tray' }, 201)
 const spec = { hw_spec: { source: 'independent-asset-fixture', cpu_arch: 'arm64' }, panel_layout: { layout_v1: { rows: 1, columns: 2, ports: [{ id: 'bound-port', label: 'Bound layout v1', row: 1, column: 1 }, { id: 'oob', label: 'OOB', row: 1, column: 2 }] } }, expected_version: 0 }
 const v1 = await call('POST', base + '/models/' + model.id + '/versions', spec, 201)
 const batch = `BOM-BATCH-${project.slug}-20261025-1`, csv = Buffer.from(`SN,hostname,model_name,MAC,port_name,rail\nPINNED-${suffix},pinned-node,${model.name},aa:bb:cc:55:66:77,p1,0\n`)
 const form = () => ({ file: { name: 'pinned.csv', mimeType: 'text/csv', buffer: csv }, bom_import_batch_id: batch })
 let response = await context.request.post('/api/v1' + base + '/bom/legacy/preview', { headers: { Origin: c.base_url, 'X-CSRF-Token': csrf }, multipart: form() }); assert.equal(response.status(), 200)
 const preview = await response.json(); assert.equal(preview.can_commit, true)
 response = await context.request.post('/api/v1' + base + '/bom/legacy/commit', { headers: { Origin: c.base_url, 'X-CSRF-Token': csrf }, multipart: { ...form(), preview_token: preview.preview_token } }); assert.equal(response.status(), 201)
 const asset = (await call('GET', base + '/assets')).items.find(asset => asset.hostname === 'pinned-node')
 assert(asset); assert.equal(asset.model_version_id, v1.id); assert.equal(asset.model_version, 1); assert.equal(asset.model_id, model.id)
 const v2 = await call('POST', base + '/models/' + model.id + '/versions', { ...spec, expected_version: 1, panel_layout: { layout_v1: { rows: 1, columns: 1, ports: [{ id: 'new-port', label: 'Unbound layout v2', row: 1, column: 1 }] } } }, 201)
 await call('POST', base + '/models/' + model.id + '/retire', {})
 const bound = await call('GET', base + '/assets/' + asset.id + '/model'); assert.equal(bound.model_version.id, v1.id); assert.equal(bound.model.latest_version, 2); assert.equal(bound.model.state, 'retired'); assert.notEqual(bound.model_version.id, v2.id)
 const foreign = await call('POST', '/projects', { name: 'Other pinned assets', slug: 'other-asset-model-' + suffix }, 201)
 await call('GET', '/projects/' + foreign.id + '/assets/' + asset.id + '/model', undefined, 404)
 const viewerName = 'asset-viewer-' + suffix, password = crypto.randomUUID() + '-Viewer!'
 await call('POST', '/admin/users', { username: viewerName, display_name: 'Asset Viewer', password }, 201); await call('POST', base + '/members', { username: viewerName, role: 'Viewer' }, 201)
 const viewer = await browser.newContext({ baseURL: c.base_url, viewport: { width: 1440, height: 1080 }, reducedMotion: 'reduce' }), token = await login(viewer, viewerName, password)
 const viewerBinding = await call('GET', base + '/assets/' + asset.id + '/model', undefined, 200, viewer, token); assert.deepEqual(viewerBinding.model_version, v1)
 await call('GET', '/projects/' + foreign.id + '/assets/' + asset.id + '/model', undefined, 404, viewer, token)
 const viewerPage = await viewer.newPage(); await viewerPage.goto(`/p/${project.id}/assets`)
 await viewerPage.getByRole('button', { name: '查看绑定型号', exact: true }).click()
 const dialog = viewerPage.getByRole('dialog'); await dialog.getByText(v1.id, { exact: true }).waitFor()
 await dialog.getByText('型号库已有 v2', { exact: false }).waitFor()
 assert.equal(await dialog.getByRole('button', { name: 'Unbound layout v2', exact: false }).count(), 0)
 await dialog.getByRole('button', { name: 'Bound layout v1 未关联槽位', exact: true }).click()
 await dialog.getByText('bound-port', { exact: true }).waitFor()
 await dialog.screenshot({ path: process.env.KEEL_WORKSPACE_EVIDENCE_DIR + '/asset-bound-model.png' })
 assert.equal(await dialog.getByRole('button', { name: '发布新版本', exact: true }).count(), 0)
 await dialog.getByRole('button', { name: '关闭', exact: true }).click(); await viewerPage.getByRole('button', { name: 'Switch to English', exact: true }).click()
 await viewerPage.getByRole('button', { name: 'View bound model', exact: true }).click(); await viewerPage.getByRole('dialog').getByText('Bound model version', { exact: true }).waitFor()
 await viewerPage.getByRole('dialog').getByRole('button', { name: 'Close', exact: true }).click()
 const anonymous = await browser.newContext({ baseURL: c.base_url }); assert.equal((await anonymous.request.get('/api/v1' + base + '/assets/' + asset.id + '/model')).status(), 401)
 await page.goto(`/p/${project.id}/assets`); await page.getByRole('button', { name: '查看绑定型号', exact: true }).click(); await page.getByRole('dialog').getByText(v1.id, { exact: true }).waitFor()
 await page.getByRole('dialog').getByRole('button', { name: '关闭', exact: true }).click()
 assert.deepEqual(errors, [])
 console.log('PASS asset model API and Viewer/admin UI read the exact immutable tray version after new publication and retirement; historical layout, version IDs, bilingual inspection, scope isolation and anonymous denial')
} finally { await browser.close() }
