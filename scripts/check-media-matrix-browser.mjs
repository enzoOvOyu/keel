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
 const suffix = crypto.randomUUID().slice(0, 8), project = await call('POST', '/projects', { name: 'Imported compatibility', slug: 'matrix-' + suffix }, 201), other = await call('POST', '/projects', { name: 'Other compatibility', slug: 'matrix-other-' + suffix }, 201)
 const reviewerName = 'matrix-reviewer-' + suffix, viewerName = 'matrix-viewer-' + suffix, password = crypto.randomUUID() + '-Password!'
 for (const [username, is_admin] of [[reviewerName, true], [viewerName, false]]) await call('POST', '/admin/users', { username, display_name: username, password, is_admin }, 201)
 await call('POST', `/projects/${project.id}/members`, { username: viewerName, role: 'Viewer' }, 201)
 const reviewer = await browser.newContext({ baseURL: c.base_url }), viewer = await browser.newContext({ baseURL: c.base_url }), anonymous = await browser.newContext({ baseURL: c.base_url }), reviewToken = await login(reviewer, reviewerName, password), viewToken = await login(viewer, viewerName, password)
 const matrix = { source: 'operator-qualified-report-' + suffix, version: 'matrix-v1', report_ref: 'https://report.invalid/' + suffix, requires: { 'cpu.arch': ['amd64', 'arm64'] }, combinations: [{ 'cuda.version': '12.4', 'driver.version': '550.54' }, { 'cuda.version': '12.6', 'driver.version': '560.35' }] }
 const file = value => ({ name: 'compatibility.json', mimeType: 'application/json', buffer: Buffer.from(JSON.stringify(value)) })
 for (const scope of ['project', 'global']) {
  const base = scope === 'global' ? '/admin/media' : `/projects/${project.id}/media`, route = scope === 'global' ? '/admin/media' : `/p/${project.id}/media`, endpoint = base + '/compatibility/validate'
  const before = await call('GET', base)
  assert.deepEqual(await call('POST', endpoint, matrix), matrix)
  assert.deepEqual(await call('POST', endpoint, matrix), matrix)
  assert.equal((await call('GET', base)).total, before.total, 'preview must not create media')
  for (const invalid of [null, {}, { ...matrix, report_ref: '' }, { ...matrix, unknown: true }, { ...matrix, combinations: [matrix.combinations[0], matrix.combinations[0]] }, { ...matrix, combinations: [{ 'cuda.version': '12.4' }, matrix.combinations[1]] }, { ...matrix, combinations: Array.from({ length: 129 }, (_, i) => ({ 'cuda.version': String(i) })) }, { ...matrix, requires: { 'cpu.arch': ['amd64', 'amd64'] } }]) await call('POST', endpoint, invalid, 400)
  await call('POST', endpoint, { ...matrix, source: 'x'.repeat(66000) }, 413)
  assert.equal((await anonymous.request.post('/api/v1' + endpoint, { data: matrix })).status(), 401)
  await call('POST', endpoint, matrix, 403, viewer, viewToken)
  assert.equal((await admin.request.post('/api/v1' + endpoint, { headers: { Origin: c.base_url }, data: matrix })).status(), 403)
  assert.equal((await admin.request.post('/api/v1' + endpoint, { headers: { Origin: 'https://untrusted.invalid', 'X-CSRF-Token': csrf }, data: matrix })).status(), 403)
  if (scope === 'project') await call('POST', `/projects/${other.id}/media/compatibility/validate`, matrix, 404, viewer, viewToken)
  await page.goto(route, { waitUntil: 'networkidle' }); await page.getByRole('button', { name: '上传新介质', exact: true }).click()
  await page.locator('#media-name').fill('matrix-' + scope + '-' + suffix); await page.locator('#media-version').fill('v1'); await page.locator('#media-arch').selectOption('noarch')
  const payload = Buffer.from('matrix-bound-artifact-' + scope); await page.locator('#media-file').setInputFiles({ name: 'artifact.bin', mimeType: 'application/octet-stream', buffer: payload })
  const submit = page.locator('.action-dialog button[type=submit]'), input = page.locator('#media-matrix-file')
  await input.setInputFiles({ name: 'broken.json', mimeType: 'application/json', buffer: Buffer.from('{') }); await page.getByText('兼容矩阵无效，请修正文件或明确移除后再登记。', { exact: true }).waitFor(); assert(await submit.isDisabled())
  await page.getByRole('button', { name: '移除兼容矩阵', exact: true }).click(); assert(await submit.isEnabled())
  await input.setInputFiles({ name: 'large.json', mimeType: 'application/json', buffer: Buffer.alloc(65537, 32) }); await page.getByText('兼容矩阵无效，请修正文件或明确移除后再登记。', { exact: true }).waitFor(); assert(await submit.isDisabled())
  // Hold a valid first response; the new selection must supersede it even if it arrives last.
  let releaseFirst, firstStarted
  const held = new Promise(resolve => { releaseFirst = resolve }), started = new Promise(resolve => { firstStarted = resolve })
  await page.route('**/compatibility/validate', async handler => {
   if (handler.request().postDataJSON().source === 'stale-matrix') { firstStarted(); await held; await handler.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ ...matrix, source: 'stale-matrix' }) }).catch(() => {}) }
   else await handler.continue()
  })
  await input.setInputFiles(file({ ...matrix, source: 'stale-matrix' })); await started; assert(await submit.isDisabled())
  await input.setInputFiles(file(matrix)); await page.locator('.action-dialog').getByText(matrix.source, { exact: true }).waitFor(); releaseFirst(); await page.unrouteAll({ behavior: 'wait' })
  assert(await submit.isEnabled()); assert.equal(await page.locator('.action-dialog').getByText('stale-matrix', { exact: true }).count(), 0)
  const rows = page.getByRole('region', { name: '允许的完整组合', exact: true }); assert.equal(await rows.getByRole('row').count(), 3); await rows.getByRole('row').filter({ hasText: '12.4' }).getByText('550.54', { exact: true }).waitFor()
  let response = page.waitForResponse(r => r.url().endsWith(base + '/upload') && r.request().method() === 'POST'); await submit.click(); let out = await response; assert.equal(out.status(), 201); const item = await out.json(); assert.deepEqual(item.deps.compatibility, matrix); assert.equal(item.state, 'uploaded')
  const row = page.getByRole('row').filter({ hasText: item.name }); await row.getByRole('button', { name: '校验', exact: true }).click(); await page.locator('.action-dialog').getByText(matrix.report_ref, { exact: true }).waitFor()
  response = page.waitForResponse(r => r.url().endsWith('/transition')); await page.locator('.action-dialog button[type=submit]').click(); out = await response; assert.equal(out.status(), 200)
  // Review the same immutable matrix from an independent approver session.
  const reviewPage = await reviewer.newPage(); await reviewPage.goto(route, { waitUntil: 'networkidle' }); await reviewPage.getByRole('row').filter({ hasText: item.name }).getByRole('button', { name: '审批', exact: true }).click(); await reviewPage.locator('.action-dialog').getByText(matrix.source, { exact: true }).waitFor(); response = reviewPage.waitForResponse(r => r.url().endsWith('/transition')); await reviewPage.locator('.action-dialog button[type=submit]').click(); out = await response; assert.equal(out.status(), 200); await reviewPage.close()
  const download = await viewer.request.get(`/api/v1/projects/${project.id}/media/${item.id}/content`); assert.equal(download.status(), 200); assert.deepEqual(await download.body(), payload)
  const manifest = await call('GET', base + '/' + item.id + '/manifest'); const node = manifest.payload.artifacts.find(x => x.media_id === item.id); assert.deepEqual(node.compatibility, matrix)
  const viewPage = await viewer.newPage(); await viewPage.goto(`/p/${project.id}/media`, { waitUntil: 'networkidle' }); const readRow = viewPage.getByRole('row').filter({ hasText: item.name }); await readRow.getByRole('button', { name: '查看兼容矩阵', exact: true }).click(); await viewPage.locator('.action-dialog').getByText(matrix.source, { exact: true }).waitFor(); assert.equal(await viewPage.getByRole('button', { name: '上传新介质', exact: true }).count(), 0); await viewPage.locator('.action-dialog button[type=submit]').click(); await viewPage.close()
  await page.reload({ waitUntil: 'networkidle' }); await page.getByRole('button', { name: 'Switch to English' }).click(); await page.getByRole('row').filter({ hasText: item.name }).getByRole('button', { name: 'View compatibility matrix', exact: true }).click(); await page.getByRole('region', { name: 'Allowed complete combinations', exact: true }).waitFor(); assert(await page.locator('.action-dialog').evaluate(e => e.scrollWidth <= e.clientWidth + 1)); await page.screenshot({ path: work + '/matrix-' + scope + '.png', fullPage: true }); await page.locator('.action-dialog button[type=submit]').click(); await page.getByRole('button', { name: '切换为中文' }).click()
 }
 assert.deepEqual(errors, [])
 console.log('PASS pure scoped/CSRF protected matrix preview, strict shape/size/exact rows, no registration side effects, anonymous/Viewer/cross-project denial')
 console.log('PASS invalid/oversize import blocks registration; explicit removal and out-of-order file validation retain only latest reviewed matrix')
 console.log('PASS project/global real MinIO upload -> verification -> independent approval -> Viewer bytes/read-only matrix -> signed metadata; bilingual provenance and full correlated rows')
} finally { await browser.close() }
