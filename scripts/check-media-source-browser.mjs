import assert from 'node:assert/strict'
import { readFile, writeFile, stat } from 'node:fs/promises'
import { createHash } from 'node:crypto'
import { pathToFileURL } from 'node:url'
const credentials = process.env.KEEL_AUTH_BROWSER_CREDENTIALS_FILE
assert(credentials?.startsWith('/tmp/keel-media-')); assert.equal((await stat(credentials)).mode & 0o077, 0)
const c = JSON.parse(await readFile(credentials, 'utf8')), work = process.env.KEEL_WORKSPACE_EVIDENCE_DIR, origin = process.env.KEEL_MEDIA_SOURCE_FIXTURE_ORIGIN
assert.equal(c.base_url, 'http://127.0.0.1:3102'); assert.match(origin, /^http:\/\/127\.0\.0\.1:\d+$/)
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
 const suffix = crypto.randomUUID().slice(0, 8), project = await call('POST', '/projects', { name: 'External sources', slug: 'source-' + suffix }, 201), other = await call('POST', '/projects', { name: 'Other sources', slug: 'source-other-' + suffix }, 201)
 const reviewerName = 'source-reviewer-' + suffix, viewerName = 'source-viewer-' + suffix, password = crypto.randomUUID() + '-Password!'
 for (const [username, is_admin] of [[reviewerName, true], [viewerName, false]]) await call('POST', '/admin/users', { username, display_name: username, password, is_admin }, 201)
 await call('POST', `/projects/${project.id}/members`, { username: viewerName, role: 'Viewer' }, 201)
 const reviewer = await browser.newContext({ baseURL: c.base_url }), viewer = await browser.newContext({ baseURL: c.base_url }), anonymous = await browser.newContext({ baseURL: c.base_url }), reviewToken = await login(reviewer, reviewerName, password), viewToken = await login(viewer, viewerName, password)
 for (const scope of ['project', 'global']) {
  const base = scope === 'global' ? '/admin/media' : `/projects/${project.id}/media`, route = scope === 'global' ? '/admin/media' : `/p/${project.id}/media`, identifier = crypto.randomUUID().replaceAll('-', ''), payload = Buffer.from('Controlled external source ' + identifier), sha = createHash('sha256').update(payload).digest('hex'), source = origin + '/redirect/' + identifier
  await writeFile(work + '/source-fixtures/' + identifier + '.bin', payload, { mode: 0o600 })
  await page.goto(route, { waitUntil: 'networkidle' }); await page.getByRole('button', { name: '登记介质', exact: true }).click()
  await page.locator('#media-name').fill('source-' + scope + '-' + suffix); await page.locator('#media-version').fill('source-' + scope + '-v1'); await page.locator('#media-arch').selectOption('noarch')
  await page.locator('#media-sha').fill(sha); await page.locator('#media-size').fill(String(payload.length)); await page.locator('#media-manifest').fill(source)
  let response = page.waitForResponse(r => r.url().endsWith(base) && r.request().method() === 'POST'); await page.locator('.action-dialog button[type=submit]').click(); let out = await response; assert.equal(out.status(), 201); const root = await out.json(), endpoint = base + '/' + root.id + '/fetch'
  assert.equal((await anonymous.request.post('/api/v1' + endpoint)).status(), 401)
  await call('POST', endpoint, undefined, 403, viewer, viewToken); await call('POST', endpoint, undefined, 403, reviewer, reviewToken)
  assert.equal((await admin.request.post('/api/v1' + endpoint, { headers: { Origin: c.base_url } })).status(), 403)
  await call('POST', endpoint, { source_url: origin + '/artifact/' + identifier }, 400)
  if (scope === 'project') await call('POST', `/projects/${other.id}/media/${root.id}/fetch`, undefined, 404)
  else await call('POST', `/projects/${project.id}/media/${root.id}/fetch`, undefined, 403)
  const row = page.getByRole('row').filter({ hasText: root.name }); assert.equal(await row.getByRole('button', { name: '校验', exact: true }).isDisabled(), true)
  await row.getByRole('button', { name: '从来源获取文件', exact: true }).click(); await page.locator('.action-dialog').getByText(source, { exact: true }).waitFor(); await page.locator('.action-dialog').getByText(sha, { exact: true }).waitFor()
  response = page.waitForResponse(r => r.url().endsWith(endpoint) && r.request().method() === 'POST'); await page.locator('.action-dialog button[type=submit]').click(); out = await response; assert.equal(out.status(), 200); const fetched = await out.json(); assert.equal(fetched.state, 'uploaded'); assert.equal(fetched.artifact_stored, true)
  await row.getByText('文件已存储', { exact: true }).waitFor(); assert.equal(await row.getByRole('button', { name: '校验', exact: true }).isDisabled(), false); assert.equal(await row.getByRole('button', { name: '从来源获取文件', exact: true }).count(), 0)
  for (const state of ['verified', 'approved']) await call('POST', base + '/' + root.id + '/transition', { state }, 200, reviewer, reviewToken)
  // A source can change later; distribution must continue reading the pinned storage version.
  await writeFile(work + '/source-fixtures/' + identifier + '.bin', 'changed remote source')
  const download = await admin.request.get('/api/v1' + base + '/' + root.id + '/content'); assert.equal(download.status(), 200); assert.deepEqual(await download.body(), payload)
  await call('POST', endpoint, undefined, 409)
  const bad = await call('POST', base, { type: 'tool', name: 'bad-source-' + scope + '-' + suffix, version: 'v1', arch: 'noarch', sha256: '0'.repeat(64), size: payload.length, manifest_ref: origin + '/artifact/' + identifier }, 201)
  await page.reload({ waitUntil: 'networkidle' }); const badRow = page.getByRole('row').filter({ hasText: bad.name }); await badRow.getByRole('button', { name: '从来源获取文件', exact: true }).click(); await page.locator('.action-dialog button[type=submit]').click()
  await page.getByText('来源文件摘要或大小与登记不符；未绑定文件，请核对来源并登记正确版本。', { exact: true }).waitFor(); await page.getByRole('button', { name: '取消', exact: true }).click()
  const afterBad = (await call('GET', base)).items.find(x => x.id === bad.id); assert.equal(afterBad.artifact_stored, false); assert.equal(afterBad.artifact_quarantined, false)
  const escape = await call('POST', base, { type: 'tool', name: 'escape-source-' + scope + '-' + suffix, version: 'v1', arch: 'noarch', sha256: sha, size: payload.length, manifest_ref: origin + '/escape/' + identifier }, 201)
  assert.equal((await call('POST', base + '/' + escape.id + '/fetch', undefined, 422)).code, 'MEDIA_SOURCE_REJECTED')
  for (const url of [origin + '/artifact/' + identifier + '?token=secret', origin.replace('http://', 'http://user:secret@') + '/artifact/' + identifier]) await call('POST', base, { type: 'tool', name: 'unsafe-url-' + crypto.randomUUID(), version: 'v1', arch: 'noarch', sha256: sha, size: payload.length, manifest_ref: url }, 400)
  await page.getByRole('button', { name: 'Switch to English' }).click(); await page.getByRole('row').filter({ hasText: bad.name }).getByRole('button', { name: 'Fetch source file', exact: true }).click(); await page.locator('.action-dialog .impact-notice').getByText(/independent approval/i).waitFor()
  await page.screenshot({ path: work + '/source-review-' + scope + '.png', fullPage: true }); await page.getByRole('button', { name: 'Cancel', exact: true }).click(); await page.getByRole('button', { name: '切换为中文' }).click()
  for (const item of [root, bad, escape]) await call('POST', base + '/' + item.id + '/transition', { state: 'retired' }, 200, reviewer, reviewToken)
 }
 const requests = (await readFile(work + '/source-requests.jsonl', 'utf8')).trim().split('\n').map(x => JSON.parse(x)); assert(requests.length > 0); assert(requests.every(x => !x.credentials_received))
 assert.deepEqual(errors, [])
 console.log('PASS real controlled HTTP origin/redirect -> digest/size check -> immutable MinIO binding -> independent approval -> original bytes after remote source changes, project and global UI')
 console.log('PASS source policy, no request URL override, anonymous/Viewer/non-uploader/CSRF/cross-scope denial; mismatches leave no binding/quarantine, escaped redirects and credential/query URLs rejected')
 console.log('PASS reviewed bilingual source acquisition retains uploaded state, disables duplicate fetch and forwards no session credentials; controlled fixtures are not vendor provenance evidence')
} finally { await browser.close() }
