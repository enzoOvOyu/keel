import assert from 'node:assert/strict'
import { readFile, stat } from 'node:fs/promises'
import { pathToFileURL } from 'node:url'
const file = process.env.KEEL_AUTH_BROWSER_CREDENTIALS_FILE
assert(file?.startsWith('/tmp/keel-media-')); assert.equal((await stat(file)).mode & 0o077, 0)
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
 const suffix = crypto.randomUUID().slice(0, 8), project = await call('POST', '/projects', { name: 'Catalog management verification', slug: 'catalog-manage-' + suffix }, 201), base = '/projects/' + project.id
 const rows = () => page.getByRole('table', { name: '参数目录', exact: true }).getByRole('row')
 const row = key => rows().filter({ has: page.getByText(key, { exact: true }) })
 const deps = { compatibility: { source: 'reviewed-fixture', version: 'matrix-3', report_ref: 'report:fixture-3', requires: { 'cpu.arch': ['amd64'] } } }
 await page.goto(`/p/${project.id}/params`)
 await page.getByRole('heading', { name: '参数目录', exact: true }).waitFor()
 await page.getByRole('button', { name: '新增参数', exact: true }).click()
 await page.locator('#catalog-key').fill('runtime.profile'); await page.locator('#catalog-options').fill('tool-a\ntool-b')
 await page.locator('#catalog-default').selectOption('tool-a')
 await page.locator('#catalog-deps').fill('[]')
 await page.getByRole('button', { name: '保存参数', exact: true }).click()
 await page.getByRole('alert').filter({ hasText: '兼容依赖须为' }).waitFor()
 await page.locator('#catalog-deps').fill(JSON.stringify(deps))
 let response = page.waitForResponse(r => r.request().method() === 'PUT' && r.url().endsWith(base + '/params'))
 await page.getByRole('button', { name: '保存参数', exact: true }).click()
 const created = await (await response).json(); assert.equal(created.default_value, 'tool-a')
 await row('runtime.profile').waitFor(); await row('runtime.profile').getByText('兼容依赖', { exact: true }).click()
 await row('runtime.profile').getByText('report:fixture-3', { exact: false }).waitFor()
 // Another editor changes this definition while the UI is open.
 await row('runtime.profile').getByRole('button', { name: '编辑参数', exact: true }).click()
 const update = { key: created.key, value_type: 'enum', options: ['tool-a', 'tool-b'], default_value: 'tool-b', deps, expected_updated_at: created.updated_at }
 const changed = await call('PUT', base + '/params', update, 201)
 assert.equal(changed.created_at, created.created_at)
 response = page.waitForResponse(r => r.request().method() === 'PUT' && r.url().endsWith(base + '/params'))
 await page.getByRole('button', { name: '保存参数', exact: true }).click(); assert.equal((await response).status(), 409)
 await page.getByRole('alert').filter({ hasText: '参数定义已变化' }).waitFor()
 assert.equal(await page.locator('#catalog-default').inputValue(), 'tool-a')
 await page.getByRole('button', { name: '取消', exact: true }).click()
 await page.getByRole('button', { name: '刷新目录', exact: true }).click(); await row('runtime.profile').getByRole('cell', { name: 'tool-b', exact: true }).waitFor()
 // Approve real bytes independently, alongside an unapproved upload.
 const reviewerName = 'catalog-sre-' + suffix, password = crypto.randomUUID() + '-Review!'
 await call('POST', '/admin/users', { username: reviewerName, display_name: 'Catalog Reviewer', password }, 201)
 await call('POST', base + '/members', { username: reviewerName, role: 'SRE' }, 201)
 const reviewer = await browser.newContext({ baseURL: c.base_url }), reviewToken = await login(reviewer, reviewerName, password)
 async function upload(version) {
  const r = await admin.request.post('/api/v1' + base + '/media/upload', { headers, multipart: { metadata: JSON.stringify({ type: 'driver', name: 'management-fixture', version, arch: 'amd64' }), file: { name: 'driver.bin', mimeType: 'application/octet-stream', buffer: Buffer.from('catalog management artifact fixture') } } }); assert.equal(r.status(), 201); return r.json()
 }
 const approved = await upload('approved-1'); await upload('unapproved-2')
 await call('POST', base + '/media/' + approved.id + '/transition', { state: 'verified' })
 await call('POST', base + '/media/' + approved.id + '/transition', { state: 'approved' }, 200, reviewer, reviewToken)
 await page.getByRole('button', { name: '新增参数', exact: true }).click()
 await page.locator('#catalog-key').fill('driver.version'); await page.locator('#catalog-type').selectOption('version')
 await page.locator('#catalog-default option[value="approved-1"]').waitFor({ state: 'attached' })
 assert.equal(await page.locator('#catalog-default option[value="unapproved-2"]').count(), 0)
 assert.equal(await page.locator('#catalog-options').count(), 0)
 assert.equal(await page.locator('#catalog-default').evaluate(e => e.tagName), 'SELECT')
 await page.locator('#catalog-default').selectOption('approved-1')
 response = page.waitForResponse(r => r.request().method() === 'PUT' && r.url().endsWith(base + '/params'))
 await page.getByRole('button', { name: '保存参数', exact: true }).click(); assert.equal((await response).status(), 201)
 await row('driver.version').waitFor()
 // Retirement confirmation also detects a stale definition.
 await row('runtime.profile').getByRole('button', { name: '退役参数', exact: true }).click()
 const newer = await call('PUT', base + '/params', { ...update, default_value: 'tool-a', expected_updated_at: changed.updated_at }, 201)
 response = page.waitForResponse(r => r.request().method() === 'POST' && r.url().endsWith('/retire'))
 await page.getByRole('dialog').getByRole('button', { name: '退役参数', exact: true }).click(); assert.equal((await response).status(), 409)
 await page.getByRole('alert').filter({ hasText: '参数定义已变化' }).waitFor()
 await page.getByRole('button', { name: '取消', exact: true }).click()
 await page.getByRole('button', { name: '刷新目录', exact: true }).click(); await row('runtime.profile').getByRole('cell', { name: 'tool-a', exact: true }).waitFor()
 await row('runtime.profile').getByRole('button', { name: '退役参数', exact: true }).click()
 response = page.waitForResponse(r => r.request().method() === 'POST' && r.url().endsWith('/retire'))
 await page.getByRole('dialog').getByRole('button', { name: '退役参数', exact: true }).click(); assert.equal((await response).status(), 200)
 await row('runtime.profile').getByRole('cell', { name: '已退役', exact: true }).waitFor()
 assert.equal(await row('runtime.profile').getByRole('button').count(), 0)
 assert(!(await call('GET', base + '/params')).items.some(k => k.id === created.id))
 assert((await call('GET', base + '/params?include_retired=true')).items.some(k => k.id === created.id && k.state === 'retired'))
 await call('GET', base + '/params?include_retired=invalid', undefined, 400)
 await call('PUT', base + '/params', { ...update, expected_updated_at: undefined }, 409)
 await call('PUT', base + '/params', { key: 'driver.version', value_type: 'version', source_media_type: 'driver', create_only: true }, 409)
 // DS §6: DL reads management, Admin manages; other roles use active options only.
 const leaderName = 'catalog-leader-' + suffix
 await call('POST', '/admin/users', { username: leaderName, display_name: 'Catalog Leader', password }, 201); await call('POST', base + '/members', { username: leaderName, role: 'DL' }, 201)
 const leader = await browser.newContext({ baseURL: c.base_url }), leaderToken = await login(leader, leaderName, password), leaderPage = await leader.newPage()
 await leaderPage.goto(`/p/${project.id}/params`); await leaderPage.getByRole('cell', { name: 'driver.version', exact: false }).waitFor()
 assert.equal(await leaderPage.getByRole('button', { name: '新增参数', exact: true }).count(), 0)
 assert.equal(await leaderPage.getByRole('button', { name: '编辑参数', exact: true }).count(), 0)
 await call('GET', base + '/params?include_retired=true', undefined, 200, leader, leaderToken)
 await call('PUT', base + '/params', { key: 'forbidden', value_type: 'enum', options: ['x'] }, 403, leader, leaderToken)
 const viewerName = 'catalog-viewer-' + suffix
 await call('POST', '/admin/users', { username: viewerName, display_name: 'Catalog Viewer', password }, 201); await call('POST', base + '/members', { username: viewerName, role: 'Viewer' }, 201)
 const viewer = await browser.newContext({ baseURL: c.base_url }), viewerToken = await login(viewer, viewerName, password), viewerPage = await viewer.newPage()
 await viewerPage.goto(`/p/${project.id}/params`); await viewerPage.getByRole('alert').waitFor()
 await call('GET', base + '/params?include_retired=true', undefined, 403, viewer, viewerToken)
 await call('GET', base + '/params', undefined, 200, viewer, viewerToken)
 assert.equal(await viewerPage.getByRole('button', { name: '新增参数', exact: true }).count(), 0)
 assert.equal(await viewerPage.getByRole('button', { name: '编辑参数', exact: true }).count(), 0)
 await call('PUT', base + '/params', { key: 'forbidden', value_type: 'enum', options: ['x'] }, 403, viewer, viewerToken)
 await call('POST', base + '/params/' + created.id + '/retire', {}, 403, viewer, viewerToken)
 const other = await call('POST', '/projects', { name: 'Other catalog scope', slug: 'catalog-other-' + suffix }, 201)
 await call('POST', '/projects/' + other.id + '/params/' + created.id + '/retire', { expected_updated_at: newer.updated_at }, 404)
 await call('GET', '/projects/' + other.id + '/params?include_retired=true', undefined, 404, viewer, viewerToken)
 await page.goto(`/p/${project.id}/params`)
 await page.getByRole('button', { name: '新增参数', exact: true }).click(); await page.locator('#catalog-key').fill('unsaved.original')
 const otherPage = await admin.newPage(); await otherPage.goto(`/p/${other.id}/params`); await otherPage.getByRole('heading', { name: '参数目录', exact: true }).waitFor()
 assert.equal(await otherPage.getByRole('dialog').count(), 0); assert.equal(await page.locator('#catalog-key').inputValue(), 'unsaved.original')
 await page.getByRole('button', { name: '取消', exact: true }).click()
 await page.screenshot({ path: process.env.KEEL_WORKSPACE_EVIDENCE_DIR + '/catalog-management.png', fullPage: true })
 await page.getByRole('button', { name: 'Switch to English', exact: true }).click(); await page.getByRole('heading', { name: 'Parameter catalog', exact: true }).waitFor()
 const anonymous = await browser.newContext({ baseURL: c.base_url }); assert.equal((await anonymous.request.get('/api/v1' + base + '/params?include_retired=true')).status(), 401)
 assert.equal((await admin.request.put('/api/v1' + base + '/params', { headers: { Origin: c.base_url }, data: update })).status(), 403)
 assert.deepEqual(errors, [])
 console.log('PASS catalog management create/default/dependency review, stale edit and retirement refusal, approved real artifact defaults without manual versions, visible retired definitions excluded from execution options')
 console.log('PASS DL read-only management, Viewer management denied with active consumer reads preserved, create-only conflicts, scope/anonymous/CSRF denial, independent project tabs and English catalog')
} catch (error) { await page.screenshot({ path: process.env.KEEL_WORKSPACE_EVIDENCE_DIR + '/catalog-management-failure.png', fullPage: true }).catch(() => {}); throw error } finally { await browser.close() }
