import assert from 'node:assert/strict'
import { readFile, stat } from 'node:fs/promises'
import { pathToFileURL } from 'node:url'
const credentials = process.env.KEEL_AUTH_BROWSER_CREDENTIALS_FILE
assert(credentials?.startsWith('/tmp/keel-media-')); assert.equal((await stat(credentials)).mode & 0o077, 0)
const c = JSON.parse(await readFile(credentials, 'utf8')), work = process.env.KEEL_WORKSPACE_EVIDENCE_DIR
assert.equal(c.base_url, 'http://127.0.0.1:3102')
const { chromium } = await import(pathToFileURL('/tmp/keel-browser-check/node_modules/playwright/index.mjs').href)
const browser = await chromium.launch({ headless: true }), admin = await browser.newContext({ baseURL: c.base_url, viewport: { width: 1440, height: 1080 } }), page = await admin.newPage(), errors = []
page.setDefaultTimeout(15000); page.on('pageerror', e => errors.push(e.message))
async function login(ctx, username, password) {
 const before = await (await ctx.request.get('/api/v1/auth/csrf')).json()
 const response = await ctx.request.post('/api/v1/auth/login', { headers: { Origin: c.base_url, 'X-CSRF-Token': before.csrf_token }, data: { method: 'local', username, password } }); assert.equal(response.status(), 200)
 return (await response.json()).csrf_token
}
try {
 const csrf = await login(admin, c.username, c.password)
 async function call(method, path, data, expected = 200, ctx = admin, token = csrf) {
  const response = await ctx.request.fetch('/api/v1' + path, { method, headers: { Origin: c.base_url, 'X-CSRF-Token': token, 'Idempotency-Key': crypto.randomUUID() }, ...(data === undefined ? {} : { data }) })
  const out = await response.json(); assert.equal(response.status(), expected, `${method} ${path}: ${JSON.stringify(out)}`); return out
 }
 const suffix = crypto.randomUUID().slice(0, 8), project = await call('POST', '/projects', { name: 'Filtered media', slug: 'filtered-' + suffix }, 201), other = await call('POST', '/projects', { name: 'Other filtered media', slug: 'filtered-other-' + suffix }, 201)
 const reviewerName = 'filter-reviewer-' + suffix, viewerName = 'filter-viewer-' + suffix, password = crypto.randomUUID() + '-Password!'
 for (const [username, is_admin] of [[reviewerName, true], [viewerName, false]]) await call('POST', '/admin/users', { username, display_name: username, password, is_admin }, 201)
 await call('POST', `/projects/${project.id}/members`, { username: viewerName, role: 'Viewer' }, 201)
 const reviewer = await browser.newContext({ baseURL: c.base_url }), viewer = await browser.newContext({ baseURL: c.base_url }), anonymous = await browser.newContext({ baseURL: c.base_url }), reviewToken = await login(reviewer, reviewerName, password), viewToken = await login(viewer, viewerName, password)
 const base = `/projects/${project.id}/media`, otherBase = `/projects/${other.id}/media`, globalBase = '/admin/media'
 const register = (scope, name, version = 'v1', type = 'config', arch = 'arm64') => call('POST', scope, { type, name, version, arch, sha256: '0'.repeat(64), size: 0 }, 201)
 for (let index = 0; index < 25; index++) await register(base, `aaa-draft-${suffix}-${String(index).padStart(2, '0')}`)
 const literal = await register(base, 'literal-%_\\-' + suffix, 'ExactVersion-' + suffix), global = await register(globalBase, 'global-' + suffix), foreign = await register(otherBase, 'foreign-' + suffix)
 const query = (scope, filters) => scope + '?' + new URLSearchParams(filters)
 assert.equal((await call('GET', query(base, { search: '%_\\' }))).items[0].id, literal.id)
 assert.equal((await call('GET', query(base, { search: 'exactversion-' + suffix }))).items[0].id, literal.id)
 assert.equal((await call('GET', query(base, { search: "' OR 1=1 --" }))).total, 0)
 const scoped = await call('GET', query(base, { search: suffix, page_size: '100' })); assert(scoped.items.some(x => x.id === global.id)); assert(!scoped.items.some(x => x.id === foreign.id)); assert(scoped.items.every(x => x.scope_key === 'global' || x.scope_key === 'project:' + project.id))
 const onlyGlobal = await call('GET', query(globalBase, { search: suffix })); assert(onlyGlobal.items.every(x => x.scope_key === 'global'))
 const second = await call('GET', query(base, { search: 'aaa-draft-' + suffix, type: 'config', state: 'uploaded', compatible_arch: 'arm64', page_size: '5', page: '2' })); assert.equal(second.total, 25); assert.equal(second.items.length, 5); assert(second.items.every(x => x.arch === 'arm64')); assert(second.items[0].name.endsWith('-05'))
 for (const params of ['state=anything', 'type=anything', 'compatible_arch=x86', 'stored_only=yes', 'state=approved&state=retired', 'search=a&search=b', 'search=%FF', 'search=%00', 'search=' + encodeURIComponent('界'.repeat(121)), 'search=' + encodeURIComponent(' '.repeat(121))]) await call('GET', base + '?' + params, undefined, 400)
 assert.equal((await anonymous.request.get('/api/v1' + base + '?state=approved')).status(), 401)
 await call('GET', otherBase + '?search=' + suffix, undefined, 404, viewer, viewToken); await call('GET', globalBase + '?search=' + suffix, undefined, 403, viewer, viewToken)
 async function upload(scope, name, arch) {
  const buffer = Buffer.from('filtered dependency ' + name)
  const response = await admin.request.post('/api/v1' + scope + '/upload', { headers: { Origin: c.base_url, 'X-CSRF-Token': csrf }, multipart: { metadata: JSON.stringify({ type: 'tool', name, version: 'v1', arch }), file: { name: 'artifact.bin', mimeType: 'application/octet-stream', buffer } } }); const item = await response.json(); assert.equal(response.status(), 201)
  for (const state of ['verified', 'approved']) await call('POST', scope + '/' + item.id + '/transition', { state }, 200, reviewer, reviewToken)
  return item
 }
 const amd = await upload(base, 'zz-amd-' + suffix, 'amd64'), arm = await upload(base, 'zz-arm-' + suffix, 'arm64'), neutral = await upload(globalBase, 'zz-noarch-' + suffix, 'noarch')
 const catalogSources = await call('GET', '/admin/params/media-options?page_size=100'); assert(catalogSources.items.some(x => x.id === neutral.id)); assert(!catalogSources.items.some(x => x.id === global.id || x.id === amd.id)); assert(catalogSources.items.every(x => x.state === 'approved' && x.artifact_stored && !x.artifact_quarantined && x.scope_key === 'global'))
 const ready = await call('GET', query(base, { search: suffix, state: 'approved', stored_only: 'true', compatible_arch: 'amd64' })); assert.deepEqual(ready.items.map(x => x.id).sort(), [amd.id, neutral.id].sort()); assert.equal(ready.total, 2)
 assert.deepEqual((await call('GET', query(base, { search: suffix, state: 'approved', stored_only: 'true', compatible_arch: 'noarch' }))).items.map(x => x.id), [neutral.id])
 assert.equal((await call('GET', query(base, { search: suffix, stored_only: 'true', state: 'uploaded' }))).total, 0)
 // User-facing search is scoped and literal; clearing restores all page counts.
 for (const [scope, route, item] of [[base, `/p/${project.id}/media`, literal], [globalBase, '/admin/media', global]]) {
  await page.goto(route, { waitUntil: 'networkidle' }); await page.locator('#media-search').fill(item.name)
  await page.getByRole('search', { name: '筛选介质' }).getByRole('button', { name: '查询', exact: true }).click(); await page.getByRole('row').filter({ hasText: item.name }).waitFor()
  await page.waitForFunction(() => document.querySelectorAll('.data-table tbody tr').length === 1)
  await page.locator('#media-filter-state').selectOption('approved'); await page.getByText('没有符合条件的记录。', { exact: true }).waitFor()
  await page.getByRole('button', { name: '清空筛选', exact: true }).click(); await page.getByRole('row').filter({ hasText: scope === base ? 'aaa-draft-' : global.name }).first().waitFor()
 }
 // Approved stored source is on the first eligible page even though 25 drafts sort before it.
 await page.goto(`/p/${project.id}/media`, { waitUntil: 'networkidle' }); await page.getByRole('button', { name: '登记介质', exact: true }).click()
 await page.locator('#media-arch').selectOption('amd64'); const picker = page.locator('#media-dependency')
 await picker.locator(`option[value="${amd.id}"]`).waitFor({ state: 'attached' }); await picker.locator(`option[value="${neutral.id}"]`).waitFor({ state: 'attached' }); assert.equal(await picker.locator(`option[value="${arm.id}"]`).count(), 0); assert(!(await picker.locator('option').allTextContents()).some(value => value.includes('aaa-draft-' + suffix)))
 await picker.selectOption(amd.id); await page.getByRole('list', { name: '已固定的制品依赖' }).getByRole('listitem').filter({ hasText: amd.name }).waitFor()
 await page.locator('#media-arch').selectOption('arm64'); await picker.locator(`option[value="${arm.id}"]`).waitFor({ state: 'attached' }); assert.equal(await picker.locator(`option[value="${amd.id}"]`).count(), 0); await page.getByText('依赖架构与当前介质不兼容，请调整架构或移除该依赖。', { exact: true }).waitFor()
 await page.getByRole('button', { name: '取消', exact: true }).click()
 const viewerPage = await viewer.newPage(); await viewerPage.goto(`/p/${project.id}/media`, { waitUntil: 'networkidle' }); await viewerPage.locator('#media-search').fill(amd.name); await viewerPage.getByRole('search').getByRole('button', { name: '查询', exact: true }).click(); await viewerPage.getByRole('row').filter({ hasText: amd.name }).waitFor(); assert.equal(await viewerPage.getByRole('button', { name: '登记介质', exact: true }).count(), 0); await viewerPage.close()
 await page.getByRole('button', { name: 'Switch to English' }).click(); await page.getByRole('search', { name: 'Filter media' }).waitFor(); await page.getByLabel('Name or version', { exact: true }).fill(amd.name); await page.getByRole('search').getByRole('button', { name: 'Search', exact: true }).click(); await page.getByRole('row').filter({ hasText: amd.name }).waitFor(); await page.screenshot({ path: work + '/media-filter-search.png', fullPage: true })
 assert.deepEqual(errors, [])
 console.log('PASS scoped literal name/version filters, escaped wildcards, safe SQL parameters, exact counts/pages, global inheritance and cross-project/anonymous/Viewer boundaries')
 console.log('PASS approved/stored/nonquarantined compatible root sources are filtered before pagination; amd64/arm64 include noarch, source query/cache responds to architecture changes')
 console.log('PASS project/global search/state/reset and Viewer read-only search, unknown/duplicate/invalid UTF-8/oversize query rejection, bilingual UI')
} finally { await browser.close() }
