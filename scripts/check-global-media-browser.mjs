import assert from 'node:assert/strict'
import { readFile, stat } from 'node:fs/promises'
import { createHash } from 'node:crypto'
import { pathToFileURL } from 'node:url'
const file = process.env.KEEL_AUTH_BROWSER_CREDENTIALS_FILE
assert(file?.startsWith('/tmp/keel-media-')); assert.equal((await stat(file)).mode & 0o077, 0)
const c = JSON.parse(await readFile(file, 'utf8')); assert.equal(c.base_url, 'http://127.0.0.1:3102')
const { chromium } = await import(pathToFileURL('/tmp/keel-browser-check/node_modules/playwright/index.mjs').href)
const browser = await chromium.launch({ headless: true }), admin = await browser.newContext({ baseURL: c.base_url, viewport: { width: 1440, height: 1080 } }), page = await admin.newPage(), errors = []
page.setDefaultTimeout(15000)
page.on('pageerror', e => errors.push(e.message))
page.on('console', m => { if (m.type() === 'error' && !/status of 40[1349]/.test(m.text())) errors.push(m.text()) })
async function login(ctx, username, password) {
 const before = await (await ctx.request.get('/api/v1/auth/csrf')).json()
 const r = await ctx.request.post('/api/v1/auth/login', { headers: { Origin: c.base_url, 'X-CSRF-Token': before.csrf_token }, data: { method: 'local', username, password } }); assert.equal(r.status(), 200)
 return (await r.json()).csrf_token
}
try {
 const csrf = await login(admin, c.username, c.password)
 async function call(method, path, data, expected = 200, ctx = admin, token = csrf) {
  const r = await ctx.request.fetch('/api/v1' + path, { method, headers: { Origin: c.base_url, 'X-CSRF-Token': token, 'Idempotency-Key': crypto.randomUUID() }, ...(data === undefined ? {} : { data }) })
  const out = await r.json(); assert.equal(r.status(), expected, `${method} ${path}: ${JSON.stringify(out)}`); return out
 }
 const suffix = crypto.randomUUID().slice(0, 8), project = await call('POST', '/projects', { name: 'Global media consumers', slug: 'global-media-' + suffix }, 201)
 const users = {}
 for (const role of ['Reviewer', 'Admin', 'SRE', 'Viewer']) {
  const username = 'gm-' + role.toLowerCase() + '-' + suffix, password = crypto.randomUUID() + '-Password!'
  await call('POST', '/admin/users', { username, display_name: role, password, is_admin: role === 'Reviewer' }, 201)
  if (role !== 'Reviewer') await call('POST', `/projects/${project.id}/members`, { username, role }, 201)
  const context = await browser.newContext({ baseURL: c.base_url, viewport: { width: 1440, height: 1080 } })
  users[role] = { context, csrf: await login(context, username, password) }
 }
 const payload = Buffer.from('global media immutable artifact fixture\n'), sha = createHash('sha256').update(payload).digest('hex'), base = '/admin/media'
 await page.goto('/admin/media', { waitUntil: 'networkidle' }); await page.getByRole('heading', { name: '全局介质库', exact: true }).waitFor()
 await page.getByRole('button', { name: '上传新介质', exact: true }).click()
 await page.locator('#media-name').fill('global-driver-' + suffix); await page.locator('#media-version').fill('global-v1-' + suffix)
 await page.locator('#media-file').setInputFiles({ name: 'driver.bin', mimeType: 'application/octet-stream', buffer: payload })
 await page.locator('.action-dialog button[type=submit]').click(); await page.getByText('介质已登记。', { exact: true }).waitFor()
 const media = (await call('GET', base)).items.find(v => v.name === 'global-driver-' + suffix)
 assert(media); assert.equal(media.scope_key, 'global'); assert.equal(media.sha256, sha); assert.equal(media.size, payload.length); assert(media.artifact_stored)
 let row = page.getByRole('row').filter({ hasText: media.name })
 await row.getByRole('button', { name: '校验', exact: true }).click(); await page.locator('.action-dialog button[type=submit]').click(); await row.getByText('已校验', { exact: true }).waitFor()
 assert.equal((await call('POST', base + '/' + media.id + '/transition', { state: 'approved' }, 409)).code, 'MEDIA_SELF_APPROVAL')
 const reviewer = users.Reviewer, reviewPage = await reviewer.context.newPage()
 await reviewPage.goto('/admin/media'); row = reviewPage.getByRole('row').filter({ hasText: media.name })
 await row.getByRole('button', { name: '审批', exact: true }).click(); await reviewPage.locator('.action-dialog button[type=submit]').click(); await row.getByText('已审批', { exact: true }).waitFor()
 await row.getByRole('link', { name: '下载已批准文件', exact: true }).waitFor()
 let response = await admin.request.get('/api/v1' + base + '/' + media.id + '/content'); assert.equal(response.status(), 200); assert.deepEqual(await response.body(), payload); assert.equal(response.headers()['x-content-sha256'], sha)
 response = await admin.request.get('/api/v1' + base + '/' + media.id + '/content', { headers: { Range: 'bytes=2-8' } }); assert.equal(response.status(), 206); assert.deepEqual(await response.body(), payload.subarray(2, 9))
 const metadata = { type: 'driver', name: media.name, version: media.version, arch: 'amd64', sha256: sha, size: payload.length }
 await call('POST', base, metadata, 409)
 await call('POST', base, { ...metadata, name: 'invalid-scope', scope_key: 'project:' + project.id }, 400)
 const bare = await call('POST', base, { ...metadata, name: 'global-attach-' + suffix }, 201)
 assert.equal((await call('POST', base + '/' + bare.id + '/transition', { state: 'verified' }, 409)).code, 'MEDIA_ARTIFACT_MISSING')
 for (const [ctx, token, bytes, expected] of [[reviewer.context, reviewer.csrf, payload, 403], [admin, csrf, Buffer.from('wrong'), 422], [admin, csrf, payload, 200], [admin, csrf, payload, 409]]) {
  response = await ctx.request.put('/api/v1' + base + '/' + bare.id + '/content', { headers: { Origin: c.base_url, 'X-CSRF-Token': token, 'Content-Type': 'application/octet-stream' }, data: bytes }); assert.equal(response.status(), expected)
 }
 const local = await call('POST', `/projects/${project.id}/media`, { ...metadata, name: 'project-only-' + suffix }, 201)
 await call('POST', base + '/' + local.id + '/transition', { state: 'retired' }, 404)
 response = await admin.request.get('/api/v1' + base + '/' + local.id + '/content'); assert.equal(response.status(), 404)
 await call('POST', `/projects/${project.id}/media/${media.id}/transition`, { state: 'retired' }, 403)
 for (const role of ['Admin', 'SRE', 'Viewer']) {
  const user = users[role]
  assert((await call('GET', `/projects/${project.id}/media`, undefined, 200, user.context, user.csrf)).items.some(v => v.id === media.id))
  response = await user.context.request.get(`/api/v1/projects/${project.id}/media/${media.id}/content`); assert.equal(response.status(), 200); assert.deepEqual(await response.body(), payload)
  await call('GET', base, undefined, 403, user.context, user.csrf)
  await call('POST', base, metadata, 403, user.context, user.csrf)
  await call('POST', base + '/' + media.id + '/transition', { state: 'retired' }, 403, user.context, user.csrf)
  for (const method of ['GET', 'PUT']) { response = await user.context.request.fetch('/api/v1' + base + '/' + media.id + '/content', { method, headers: { Origin: c.base_url, 'X-CSRF-Token': user.csrf, 'Content-Type': 'application/octet-stream' } }); assert.equal(response.status(), 403) }
  response = await user.context.request.post('/api/v1' + base + '/upload', { headers: { Origin: c.base_url, 'X-CSRF-Token': user.csrf }, multipart: { metadata: JSON.stringify(metadata), file: { name: 'x', mimeType: 'application/octet-stream', buffer: payload } } }); assert.equal(response.status(), 403)
 }
 const localPage = await users.Admin.context.newPage(); await localPage.goto(`/p/${project.id}/media`)
 const inheritedRow = localPage.getByRole('row').filter({ hasText: media.name }); await inheritedRow.getByText('全局 · 只读', { exact: true }).waitFor(); assert.equal(await inheritedRow.getByRole('button', { name: '导出签名清单', exact: true }).count(), 1); for (const name of ['校验','批准','退役']) assert.equal(await inheritedRow.getByRole('button', { name, exact: true }).count(), 0)
 await localPage.goto('/admin/media'); await localPage.getByRole('heading', { name: '你没有此操作的权限', exact: true }).waitFor(); assert.equal(await localPage.getByRole('link', { name: '全局介质库', exact: true }).count(), 0)
 const definition = await call('PUT', '/admin/params', { key: 'globaltest.driver.version', value_type: 'version', source_media_type: 'driver', default_value: media.version, create_only: true }, 201)
 await call('PUT', `/projects/${project.id}/params`, { key: 'cpu.arch', value_type: 'enum', options: ['amd64', 'arm64'] }, 201)
 const values = { 'globaltest.driver.version': media.version, 'cpu.arch': 'amd64' }
 assert.equal((await call('POST', `/projects/${project.id}/params/validate`, { values })).valid, true)
 await page.goto('/admin/params'); row = page.getByRole('row').filter({ hasText: definition.key }); await row.getByRole('button', { name: '编辑参数', exact: true }).click()
 await page.locator('#catalog-default:not([disabled]) option[value="' + media.version + '"]:not([disabled])').waitFor({ state: 'attached' }); assert.equal(await page.locator('#catalog-default').inputValue(), media.version)
 const registerLink = page.locator('.action-dialog a[href="/admin/media"]'); await registerLink.click(); await page.getByRole('heading', { name: '全局介质库', exact: true }).waitFor()
 await page.screenshot({ path: process.env.KEEL_WORKSPACE_EVIDENCE_DIR + '/global-media-approved.png', fullPage: true })
 await page.getByRole('button', { name: 'Switch to English' }).click(); await page.getByRole('heading', { name: 'Global media library', exact: true }).waitFor()
 await page.getByRole('button', { name: '切换为中文' }).click()
 row = page.getByRole('row').filter({ hasText: media.name }); await row.getByRole('button', { name: '退役', exact: true }).click(); await page.locator('.action-dialog button[type=submit]').click(); await row.getByText('已退役', { exact: true }).waitFor()
 assert.equal((await call('POST', `/projects/${project.id}/params/validate`, { values })).valid, false)
 response = await users.Viewer.context.request.get(`/api/v1/projects/${project.id}/media/${media.id}/content`); assert.equal(response.status(), 409)
 await call('POST', base + '/' + media.id + '/transition', { state: 'approved' }, 409)
 assert((await call('GET', base)).items.every(v => v.scope_key === 'global'))
 const anon = await browser.newContext({ baseURL: c.base_url }); assert.equal((await anon.request.get('/api/v1' + base)).status(), 401)
 response = await admin.request.post('/api/v1' + base, { headers: { Origin: c.base_url }, data: metadata }); assert.equal(response.status(), 403)
 response = await admin.request.put('/api/v1' + base + '/' + bare.id + '/content', { headers: { Origin: c.base_url, 'Content-Type': 'application/octet-stream' }, data: payload }); assert.equal(response.status(), 403)
 assert.deepEqual(errors, [])
 console.log('PASS global media UI upload/verify/independent approval/download/retirement, actual MinIO bytes and Range, duplicate/metadata/digest/attachment checks and uploader-only binding')
 console.log('PASS global catalog approved defaults and registration link, inherited project read/download with read-only controls, global/project isolation, nonplatform roles/anonymous/CSRF refusal and bilingual navigation')
} finally { await browser.close() }
