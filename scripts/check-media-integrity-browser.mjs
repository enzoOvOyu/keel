import assert from 'node:assert/strict'
import { readFile, writeFile, rename, stat } from 'node:fs/promises'
import { pathToFileURL } from 'node:url'
const credentials = process.env.KEEL_AUTH_BROWSER_CREDENTIALS_FILE
assert(credentials?.startsWith('/tmp/keel-media-')); assert.equal((await stat(credentials)).mode & 0o077, 0)
const c = JSON.parse(await readFile(credentials, 'utf8')), work = process.env.KEEL_WORKSPACE_EVIDENCE_DIR
assert.equal(c.base_url, 'http://127.0.0.1:3102')
const { chromium } = await import(pathToFileURL('/tmp/keel-browser-check/node_modules/playwright/index.mjs').href)
const browser = await chromium.launch({ headless: true }), admin = await browser.newContext({ baseURL: c.base_url, viewport: { width: 1440, height: 1000 } }), page = await admin.newPage(), errors = []
page.setDefaultTimeout(15000); page.on('pageerror', e => errors.push(e.message))
async function login(ctx, username, password) {
 const before = await (await ctx.request.get('/api/v1/auth/csrf')).json()
 const response = await ctx.request.post('/api/v1/auth/login', { headers: { Origin: c.base_url, 'X-CSRF-Token': before.csrf_token }, data: { method: 'local', username, password } }); assert.equal(response.status(), 200)
 return (await response.json()).csrf_token
}
async function inject(id, remaining) {
 await writeFile(work + '/integrity-fault.next', JSON.stringify({ media_id: id, remaining, reads: 0 }), { mode: 0o600 }); await rename(work + '/integrity-fault.next', work + '/integrity-fault.json')
}
const fault = async () => JSON.parse(await readFile(work + '/integrity-fault.json', 'utf8'))
try {
 const csrf = await login(admin, c.username, c.password)
 async function call(method, path, data, expected = 200, ctx = admin, token = csrf) {
  const response = await ctx.request.fetch('/api/v1' + path, { method, headers: { Origin: c.base_url, 'X-CSRF-Token': token, 'Idempotency-Key': crypto.randomUUID() }, ...(data === undefined ? {} : { data }) })
  const out = await response.json(); assert.equal(response.status(), expected, `${method} ${path}: ${JSON.stringify(out)}`); return out
 }
 const suffix = crypto.randomUUID().slice(0, 8), project = await call('POST', '/projects', { name: 'Media integrity', slug: 'integrity-' + suffix }, 201), projectBase = `/projects/${project.id}`
 const username = 'integrity-reviewer-' + suffix, password = crypto.randomUUID() + '-Password!'
 await call('POST', '/admin/users', { username, display_name: username, password, is_admin: true }, 201)
 const reviewer = await browser.newContext({ baseURL: c.base_url }), reviewToken = await login(reviewer, username, password)
 await call('PUT', projectBase + '/params', { key: 'driver.version', value_type: 'version', source_media_type: 'driver' }, 201)
 const targets = await call('POST', projectBase + '/sim-targets', { prefix: 'integrity-' + suffix, count: 1, seed: 42 }, 201)
 const yaml = `api_version: keel.sim/v1\ninputs:\n  cpu.arch: {type: enum, required: true, enum: [amd64]}\n  driver.version: {type: string, required: true}\ntasks:\n  - id: inspect\n    type: sim.read\n    retry: 0\n    idempotent: true\n    timeout_ms: 1000\n    resources: [machine]\n    sim: {outcome: success, duration_ms: 10, seed: 42}\n`
 const flow = await call('POST', projectBase + '/flows', { name: 'Integrity gate', yaml_source: yaml, expected_version: 0 }, 201)
 for (const scope of ['project', 'global']) {
  const base = scope === 'global' ? '/admin/media' : projectBase + '/media', route = scope === 'global' ? '/admin/media' : `/p/${project.id}/media`
  async function upload(name, version = scope + '-v1', deps, approved = true) {
   const buffer = Buffer.from(name + version + ' trusted bytes')
   const response = await admin.request.post('/api/v1' + base + '/upload', { headers: { Origin: c.base_url, 'X-CSRF-Token': csrf }, multipart: { metadata: JSON.stringify({ type: 'driver', name, version, arch: 'noarch', ...(deps ? { deps: { artifacts: deps.map(x => ({ media_id: x.id, sha256: x.sha256 })) } } : {}) }), file: { name: 'source.bin', mimeType: 'application/octet-stream', buffer } } })
   const out = await response.json(); assert.equal(response.status(), 201, JSON.stringify(out))
   if (approved) for (const state of ['verified', 'approved']) await call('POST', base + '/' + out.id + '/transition', { state }, 200, reviewer, reviewToken)
   return { ...out, buffer }
  }
  const root = await upload('root-' + scope + '-' + suffix)
  await inject(root.id, 2)
  let download = await admin.request.get('/api/v1' + base + '/' + root.id + '/content'); assert.equal(download.status(), 200, await download.text()); assert.deepEqual(await download.body(), root.buffer)
  assert.equal((await fault()).reads, 3, 'bounded recovery did not use exactly three complete object reads')
  const ready = (await call('GET', base)).items.find(x => x.id === root.id); assert.equal(ready.artifact_quarantined, false)
  // Fail during execution admission: business rows roll back, quarantine commits independently.
  await inject(root.id, 3)
  const before = (await call('GET', projectBase + '/executions')).total
  const input = { flow_version_id: flow.id, mode: 'sim', target_ids: [targets.items[0].id], parameters: { 'driver.version': root.version, 'cpu.arch': 'amd64' } }
  const failed = await call('POST', projectBase + '/executions', input, 422); assert.equal(failed.code, 'MEDIA_DIGEST_MISMATCH'); assert.equal((await fault()).reads, 3)
  assert.equal((await call('GET', projectBase + '/executions')).total, before)
  let item = (await call('GET', base)).items.find(x => x.id === root.id); assert.equal(item.state, 'approved'); assert.equal(item.artifact_quarantined, true)
  download = await admin.request.get('/api/v1' + base + '/' + root.id + '/content'); assert.equal(download.status(), 422); assert.equal((await download.json()).code, 'MEDIA_ARTIFACT_QUARANTINED'); assert.equal((await fault()).reads, 3, 'quarantine retried a source')
  assert.equal((await call('GET', base + '/' + root.id + '/manifest', undefined, 422)).code, 'MEDIA_ARTIFACT_QUARANTINED')
  const denied = await call('POST', projectBase + '/params/validate', { values: { 'driver.version': root.version, 'cpu.arch': 'amd64' } }); assert.equal(denied.valid, false)
  await page.goto(route, { waitUntil: 'networkidle' }); const row = page.getByRole('row').filter({ hasText: root.name })
  await row.getByText('已隔离：固定制品连续摘要不符，请退役并登记复核新版本。', { exact: true }).waitFor()
  assert.equal(await row.locator('a[href*="/content"]').count(), 0); assert.equal(await row.getByRole('button', { name: '导出签名清单', exact: true }).count(), 0)
  await page.getByRole('button', { name: '上传新介质', exact: true }).click(); await page.locator('#media-dependency').waitFor(); assert.equal(await page.locator(`#media-dependency option[value="${root.id}"]`).count(), 0); await page.getByRole('button', { name: '取消', exact: true }).click()
  await page.getByRole('button', { name: 'Switch to English' }).click(); await page.getByRole('row').filter({ hasText: root.name }).getByText('Quarantined: repeated pinned artifact digest mismatch. Retire and register a reviewed new version.', { exact: true }).waitFor()
  await page.screenshot({ path: work + '/integrity-quarantined-' + scope + '.png', fullPage: true }); await page.getByRole('button', { name: '切换为中文' }).click()
  await call('POST', base + '/' + root.id + '/transition', { state: 'retired' }, 200, reviewer, reviewToken)
  const replacement = await upload(root.name, scope + '-v2'); download = await admin.request.get('/api/v1' + base + '/' + replacement.id + '/content'); assert.equal(download.status(), 200); assert.deepEqual(await download.body(), replacement.buffer)
  // Healthy parent remains approved, but quarantined dependency blocks its reference.
  const leaf = await upload('leaf-' + scope + '-' + suffix, scope + '-leaf'), parent = await upload('parent-' + scope + '-' + suffix, scope + '-parent', [leaf])
  await inject(leaf.id, 3); download = await admin.request.get('/api/v1' + base + '/' + parent.id + '/content'); assert.equal(download.status(), 422); assert.equal((await download.json()).code, 'MEDIA_DIGEST_MISMATCH'); assert.equal((await fault()).reads, 3)
  const listed = (await call('GET', base)).items; assert.equal(listed.find(x => x.id === leaf.id).artifact_quarantined, true); assert.equal(listed.find(x => x.id === parent.id).artifact_quarantined, false)
  assert.equal((await call('GET', base + '/' + parent.id + '/manifest', undefined, 422)).code, 'MEDIA_DEPENDENCY_QUARANTINED')
  for (const item of [replacement, parent, leaf]) await call('POST', base + '/' + item.id + '/transition', { state: 'retired' }, 200, reviewer, reviewToken)
 }
 assert.deepEqual(errors, [])
 console.log('PASS actual PostgreSQL/MinIO/API with explicitly injected HTTP byte corruption: exactly three bounded reads, transient recovery, execution rollback with durable quarantine and no partial execution')
 console.log('PASS project/global and transitive-source isolation, no further source reads, blocked download/manifest/catalog/dependency options, unchanged approval state, retirement and independently approved replacement')
 console.log('PASS real shared UI quarantine status, hidden distribution actions and bilingual review; test protocol fault injection is not hardware or storage-disk corruption evidence')
} finally { await browser.close() }
