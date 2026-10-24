import assert from 'node:assert/strict'
import { readFile, stat } from 'node:fs/promises'
import { pathToFileURL } from 'node:url'
const file = process.env.KEEL_AUTH_BROWSER_CREDENTIALS_FILE
assert(file?.startsWith('/tmp/keel-models-')); assert.equal((await stat(file)).mode & 0o077, 0)
const c = JSON.parse(await readFile(file, 'utf8')); assert.equal(c.base_url, 'http://127.0.0.1:3102')
const { chromium } = await import(pathToFileURL('/tmp/keel-browser-check/node_modules/playwright/index.mjs').href)
const browser = await chromium.launch({ headless: true }), admin = await browser.newContext({ baseURL: c.base_url, viewport: { width: 1440, height: 1080 }, reducedMotion: 'reduce' }), page = await admin.newPage(), errors = []
page.on('pageerror', e => errors.push(e.message)); page.on('console', m => { if (m.type() === 'error' && !/status of 40[01349]/.test(m.text())) errors.push(m.text()) })
async function login(ctx, username, password) {
 const before = await (await ctx.request.get('/api/v1/auth/csrf')).json()
 const r = await ctx.request.post('/api/v1/auth/login', { headers: { Origin: c.base_url, 'X-CSRF-Token': before.csrf_token }, data: { method: 'local', username, password } }); assert.equal(r.status(), 200)
 return (await r.json()).csrf_token
}
try {
 const csrf = await login(admin, c.username, c.password)
 async function call(method, path, data, expected = 200, ctx = admin, token = csrf) {
  const r = await ctx.request.fetch('/api/v1' + path, { method, headers: { Origin: c.base_url, 'X-CSRF-Token': token, 'Idempotency-Key': crypto.randomUUID() }, data })
  const out = await r.json(); assert.equal(r.status(), expected, `${method} ${path}: ${JSON.stringify(out)}`); return out
 }
 const suffix = crypto.randomUUID().slice(0, 8), name = 'Shared-model-' + suffix, base = '/admin/models'
 const project = await call('POST', '/projects', { name: 'Global model adoption', slug: 'shared-models-' + suffix }, 201), projectBase = '/projects/' + project.id + '/models'
 await page.goto('/admin/models'); await page.getByRole('heading', { name: '全局型号库', exact: true }).waitFor()
 assert(await page.getByRole('link', { name: '全局型号库', exact: true }).count() > 0)
 // Identity creation is performed through the shared dialog.
 await page.getByRole('button', { name: '新建型号', exact: true }).click()
 await page.locator('#model-name').fill('Identity-' + suffix); await page.locator('#model-kind').selectOption('gpu')
 let response = page.waitForResponse(r => r.request().method() === 'POST' && r.url().endsWith(base))
 await page.getByRole('dialog').getByRole('button', { name: '新建型号', exact: true }).click()
 let r = await response; assert.equal(r.status(), 201); const identity = await r.json(); assert.equal(identity.scope_key, 'global')
 await call('POST', base, { name: identity.name, kind: 'gpu' }, 409)
 await call('POST', base, { name: 'invalid', kind: 'gpu', scope_key: 'project:' + project.id }, 400)
 const scan = { source_host: 'independent-global-fixture', hw_spec: { model: name, kind: 'server', cpu_arch: 'amd64' }, bom_expect: { mac_prefixes: ['aa:bb:cc'] }, panel_layout: { source: 'fixture' }, slots: [] }
 await page.getByRole('button', { name: '扫描导入', exact: true }).click(); await page.locator('#scan-manifest').fill(JSON.stringify(scan))
 response = page.waitForResponse(r => r.request().method() === 'POST' && r.url().endsWith(base + '/import'))
 await page.getByRole('dialog').getByRole('button', { name: '扫描导入', exact: true }).click()
 r = await response; assert.equal(r.status(), 201); const draft = await r.json(), modelPath = base + '/' + draft.model_id
 assert.equal((await call('POST', base + '/import', scan, 201)).id, draft.id)
 const row = () => page.getByRole('row').filter({ has: page.getByText(name, { exact: true }) }).first()
 await row().getByRole('button', { name: '版本历史', exact: true }).click()
 await page.getByText(scan.source_host, { exact: true }).waitFor()
 await page.getByRole('button', { name: '核对后发布', exact: true }).click()
 assert.deepEqual(JSON.parse(await page.locator('#hw-spec').inputValue()), scan.hw_spec)
 assert.deepEqual(JSON.parse(await page.locator('#bom-expect').inputValue()), scan.bom_expect)
 response = page.waitForResponse(r => r.request().method() === 'POST' && r.url().endsWith(modelPath + '/versions'))
 await page.getByRole('dialog').locator('button[type=submit]').click(); r = await response; assert.equal(r.status(), 201); const v1 = await r.json()
 assert.equal(v1.version, 1)
 // Revisions use the same observed-version condition as project publication.
 await row().getByRole('button', { name: '发布新版本', exact: true }).click()
 await page.getByRole('dialog').getByRole('button', { name: '移除此槽位', exact: true }).click()
 const v2 = await call('POST', modelPath + '/versions', { ...scan, source_host: undefined, expected_version: 1, hw_spec: { ...scan.hw_spec, revision: 'second independent fixture' } }, 201)
 response = page.waitForResponse(r => r.request().method() === 'POST' && r.url().endsWith(modelPath + '/versions'))
 await page.getByRole('dialog').locator('button[type=submit]').click(); assert.equal((await response).status(), 409)
 await page.getByRole('alert').filter({ hasText: '型号已有新版本' }).waitFor()
 await page.getByRole('dialog').getByRole('button', { name: '取消', exact: true }).click()
 const versions = await call('GET', modelPath + '/versions'); assert.deepEqual(versions.items.find(v => v.id === v1.id), v1); assert.equal(v2.version, 2)
 await call('POST', modelPath + '/drafts/' + draft.id + '/discard', {})
 assert.equal((await call('GET', modelPath + '/drafts')).items.length, 0)
 const privateModel = await call('POST', projectBase, { name, kind: 'server' }, 201)
 assert.notEqual(privateModel.id, draft.model_id)
 assert((await call('GET', base + '?project_id=' + project.id)).items.every(m => m.scope_key === 'global'))
 for (const path of [base + '/' + privateModel.id + '/versions', base + '/' + privateModel.id + '/drafts']) await call('GET', path, undefined, 404)
 await call('POST', base + '/' + privateModel.id + '/versions', {}, 404)
 await call('POST', projectBase + '/' + draft.model_id + '/versions', {}, 404)
 await call('POST', projectBase + '/' + draft.model_id + '/retire', {}, 404)
 await page.goto(`/p/${project.id}/models`)
 const inherited = page.getByRole('row').filter({ has: page.getByText('全局共享 · 仅可读取', { exact: true }) }).filter({ has: page.getByText(name, { exact: true }) })
 await inherited.waitFor(); assert.equal(await inherited.getByRole('button', { name: '发布新版本', exact: true }).count(), 0); assert.equal(await inherited.getByRole('button', { name: '退役型号', exact: true }).count(), 0)
 await inherited.getByRole('button', { name: '版本历史', exact: true }).click()
 assert.equal(await page.getByRole('button', { name: '核对后发布', exact: true }).count(), 0)
 assert.equal((await call('GET', projectBase + '/' + draft.model_id + '/versions')).items.length, 2)
 await page.goto('/admin/models'); await row().getByRole('button', { name: '版本历史', exact: true }).click()
 await page.screenshot({ path: process.env.KEEL_WORKSPACE_EVIDENCE_DIR + '/global-models-history.png', fullPage: true })
 await row().getByRole('button', { name: '退役型号', exact: true }).click()
 response = page.waitForResponse(r => r.request().method() === 'POST' && r.url().endsWith(modelPath + '/retire'))
 await page.getByRole('dialog').getByRole('button', { name: '退役型号', exact: true }).click(); assert.equal((await response).status(), 200)
 await call('POST', modelPath + '/versions', {}, 409); await call('POST', base + '/import', scan, 409)
 assert.equal((await call('GET', projectBase + '/' + draft.model_id + '/versions')).items.length, 2)
 for (const role of ['Admin', 'SRE', 'Viewer']) {
  const username = 'global-model-' + role.toLowerCase() + '-' + suffix, password = crypto.randomUUID() + '-Role!'
  await call('POST', '/admin/users', { username, display_name: role, password }, 201)
  await call('POST', '/projects/' + project.id + '/members', { username, role }, 201)
  const ctx = await browser.newContext({ baseURL: c.base_url }), token = await login(ctx, username, password)
  for (const [method, path, data] of [['GET', base], ['GET', '/models'], ['GET', modelPath + '/versions'], ['GET', modelPath + '/drafts'], ['POST', base, { name: 'Denied', kind: 'gpu' }], ['POST', base + '/import', scan], ['POST', modelPath + '/versions', {}], ['POST', modelPath + '/retire', {}], ['POST', modelPath + '/drafts/' + draft.id + '/discard', {}]]) await call(method, path, data, 403, ctx, token)
  assert.equal((await call('GET', projectBase + '/' + draft.model_id + '/versions', undefined, 200, ctx, token)).items.length, 2)
  const denied = await ctx.newPage(); await denied.goto('/admin/models'); await denied.getByRole('alert').waitFor(); assert.equal(await denied.getByRole('heading', { name: '全局型号库', exact: true }).count(), 0)
  assert.equal(await denied.getByRole('button', { name: '新建型号', exact: true }).count(), 0)
  assert.equal(await denied.getByRole('link', { name: '全局型号库', exact: true }).count(), 0)
  await ctx.close()
 }
 await page.getByRole('button', { name: 'Switch to English', exact: true }).click(); await page.getByRole('heading', { name: 'Global models', exact: true }).waitFor()
 const anonymous = await browser.newContext({ baseURL: c.base_url }); assert.equal((await anonymous.request.get('/api/v1' + base)).status(), 401)
 assert.equal((await admin.request.post('/api/v1' + base, { headers: { Origin: c.base_url }, data: { name: 'No CSRF', kind: 'gpu' } })).status(), 403)
 assert.deepEqual(errors, [])
 console.log('PASS global model UI identity/import/review/publication/history/stale confirmation/retirement; shared project versions remain read-only and immutable')
 console.log('PASS global/project scope isolation, uniqueness, Admin/SRE/Viewer UI/API denial, anonymous/CSRF protection and bilingual navigation')
} finally { await browser.close() }
