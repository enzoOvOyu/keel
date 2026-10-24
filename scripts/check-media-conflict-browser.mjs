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
page.on('pageerror', e => errors.push(e.message)); page.on('console', m => { if (m.type() === 'error' && !/status of 40[1349]/.test(m.text())) errors.push(m.text()) })
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
 const suffix = crypto.randomUUID().slice(0, 8), project = await call('POST', '/projects', { name: 'Media conflict review', slug: 'media-conflict-' + suffix }, 201)
 const username = 'mc-reviewer-' + suffix, password = crypto.randomUUID() + '-Password!'
 await call('POST', '/admin/users', { username, display_name: 'Independent reviewer', password, is_admin: true }, 201)
 const reviewer = await browser.newContext({ baseURL: c.base_url }), reviewToken = await login(reviewer, username, password)
 const payload = Buffer.from('conditional immutable media fixture\n'), sha = createHash('sha256').update(payload).digest('hex')
 for (const scope of ['project', 'global']) {
  const base = scope === 'global' ? '/admin/media' : `/projects/${project.id}/media`, route = scope === 'global' ? '/admin/media' : `/p/${project.id}/media`
  const metadata = { type: 'tool', name: 'conditional-' + scope + '-' + suffix, version: 'v1', arch: 'noarch' }
  async function upload(meta) {
   const r = await admin.request.post('/api/v1' + base + '/upload', { headers: { Origin: c.base_url, 'X-CSRF-Token': csrf }, multipart: { metadata: JSON.stringify(meta), file: { name: 'tool.bin', mimeType: 'application/octet-stream', buffer: payload } } })
   const out = await r.json(); assert.equal(r.status(), 201, JSON.stringify(out)); return out
  }
  const original = await upload(metadata)
  await page.goto(route, { waitUntil: 'networkidle' })
  const row = page.getByRole('row').filter({ hasText: metadata.name })
  await row.getByRole('button', { name: '退役', exact: true }).click()
  const verified = await call('POST', base + '/' + original.id + '/transition', { state: 'verified', expected_updated_at: original.updated_at }, 200, reviewer, reviewToken)
  assert.notEqual(verified.updated_at, original.updated_at); assert.equal(verified.created_at, original.created_at)
  async function staleConfirmation(expectedVersion) {
   const response = page.waitForResponse(r => r.url().endsWith(base + '/' + original.id + '/transition') && r.request().method() === 'POST')
   await page.locator('.action-dialog button[type=submit]').click()
   const r = await response; assert.equal(r.status(), 409); assert.equal((await r.json()).code, 'MEDIA_CONFLICT')
   assert.equal(r.request().postDataJSON().expected_updated_at, expectedVersion)
   await page.getByText('介质已变化，本次确认未生效。请取消并刷新列表，核对当前状态后重新确认。', { exact: true }).waitFor()
   assert.equal(await page.locator('.action-dialog').count(), 1)
  }
  await staleConfirmation(original.updated_at)
  await page.screenshot({ path: process.env.KEEL_WORKSPACE_EVIDENCE_DIR + '/media-' + scope + '-stale.png', fullPage: true })
  assert.equal((await call('GET', base)).items.find(v => v.id === original.id).state, 'verified')
  await page.getByRole('button', { name: '取消', exact: true }).click(); await row.getByText('已校验', { exact: true }).waitFor()
  await row.getByRole('button', { name: '退役', exact: true }).click()
  const approved = await call('POST', base + '/' + original.id + '/transition', { state: 'approved', expected_updated_at: verified.updated_at }, 200, reviewer, reviewToken)
  assert.notEqual(approved.updated_at, verified.updated_at)
  await staleConfirmation(verified.updated_at)
  assert.equal((await call('GET', base)).items.find(v => v.id === original.id).updated_at, approved.updated_at)
  await page.getByRole('button', { name: '取消', exact: true }).click(); await row.getByText('已审批', { exact: true }).waitFor()
  await row.getByRole('button', { name: '退役', exact: true }).click()
  const retiredResponse = page.waitForResponse(r => r.url().endsWith(base + '/' + original.id + '/transition') && r.request().method() === 'POST')
  await page.locator('.action-dialog button[type=submit]').click(); const response = await retiredResponse
  assert.equal(response.status(), 200); assert.equal(response.request().postDataJSON().expected_updated_at, approved.updated_at)
  const retired = await response.json(); assert.notEqual(retired.updated_at, approved.updated_at)
  await row.getByText('已退役', { exact: true }).waitFor(); assert.equal(await row.getByRole('button').count(), 0)
  // Timestamp round trips and legacy state-only clients retain the one-way gate.
  assert.equal((await call('GET', base)).items.find(v => v.id === original.id).updated_at, retired.updated_at)
  await call('POST', base + '/' + original.id + '/transition', { state: 'approved' }, 409, reviewer, reviewToken)
  for (const value of ['not-a-time', '0001-01-01T00:00:00Z']) await call('POST', base + '/' + original.id + '/transition', { state: 'retired', expected_updated_at: value }, 400)
  const legacy = await upload({ ...metadata, name: metadata.name + '-legacy' })
  const legacyVerified = await call('POST', base + '/' + legacy.id + '/transition', { state: 'verified' })
  assert.notEqual(legacyVerified.updated_at, legacy.updated_at)
  assert.equal((await call('GET', base)).items.find(v => v.id === legacy.id).updated_at, legacyVerified.updated_at)
  // File attachment is a revision even while the lifecycle state remains uploaded.
  const bare = await call('POST', base, { ...metadata, name: metadata.name + '-attach', sha256: sha, size: payload.length }, 201)
  const attach = await admin.request.put('/api/v1' + base + '/' + bare.id + '/content', { headers: { Origin: c.base_url, 'X-CSRF-Token': csrf, 'Content-Type': 'application/octet-stream' }, data: payload })
  assert.equal(attach.status(), 200); const attached = await attach.json(); assert.notEqual(attached.updated_at, bare.updated_at)
  assert.equal((await call('POST', base + '/' + bare.id + '/transition', { state: 'retired', expected_updated_at: bare.updated_at }, 409)).code, 'MEDIA_CONFLICT')
  assert.equal((await call('GET', base)).items.find(v => v.id === bare.id).state, 'uploaded')
 }
 assert.deepEqual(errors, [])
 console.log('PASS project/global stale retirement confirmations reject changes from upload to verified and verified to approved, retain dialogs, refresh on cancellation, and require a fresh reviewed timestamp')
 console.log('PASS file attachment invalidates old confirmations, response timestamps match persistence, malformed/zero timestamps are refused, legacy transitions remain compatible and retirement stays irreversible')
} finally { await browser.close() }
