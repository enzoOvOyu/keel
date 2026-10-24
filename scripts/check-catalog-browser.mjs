import assert from 'node:assert/strict'
import { readFile, stat } from 'node:fs/promises'
import { pathToFileURL } from 'node:url'
const file = process.env.KEEL_AUTH_BROWSER_CREDENTIALS_FILE
assert(file?.startsWith('/tmp/keel-media-')); assert.equal((await stat(file)).mode & 0o077, 0)
const c = JSON.parse(await readFile(file, 'utf8')); assert.equal(c.base_url, 'http://127.0.0.1:3102')
const { chromium } = await import(pathToFileURL('/tmp/keel-browser-check/node_modules/playwright/index.mjs').href)
const browser = await chromium.launch({ headless: true })
const admin = await browser.newContext({ baseURL: c.base_url, viewport: { width: 1440, height: 1080 } })
const page = await admin.newPage(), errors = []
page.on('pageerror', e => errors.push(e.message)); page.on('console', m => { if (m.type() === 'error' && !m.text().includes('status of 4')) errors.push(m.text()) })
async function login(ctx, username, password) {
 const csrf = await (await ctx.request.get('/api/v1/auth/csrf')).json()
 const r = await ctx.request.post('/api/v1/auth/login', { headers: { Origin: c.base_url, 'X-CSRF-Token': csrf.csrf_token }, data: { method: 'local', username, password } })
 assert.equal(r.status(), 200); return (await r.json()).csrf_token
}
try {
 const csrf = await login(admin, c.username, c.password), headers = { Origin: c.base_url, 'X-CSRF-Token': csrf, 'Idempotency-Key': crypto.randomUUID() }
 async function call(method, path, data, expected = 200, ctx = admin, token = csrf) {
  const r = await ctx.request.fetch('/api/v1' + path, { method, headers: { ...headers, 'X-CSRF-Token': token, 'Idempotency-Key': crypto.randomUUID() }, data })
  const out = await r.json(); assert.equal(r.status(), expected, `${method} ${path}: ${JSON.stringify(out)}`); return out
 }
 const suffix = crypto.randomUUID().slice(0, 8)
 const project = await call('POST', '/projects', { name: 'Catalog dependency verification', slug: 'catalog-' + suffix }, 201), base = '/projects/' + project.id
 const username = 'catalog-review-' + suffix, password = crypto.randomUUID() + '-Review!'
 await call('POST', '/admin/users', { username, display_name: 'Catalog SRE', password }, 201)
 await call('POST', base + '/members', { username, role: 'SRE' }, 201)
 const reviewer = await browser.newContext({ baseURL: c.base_url }), reviewCSRF = await login(reviewer, username, password)
 const matrix = { compatibility: { source: 'fixture-vendor-report', version: 'reviewed-1', report_ref: 'report:fixture-1', requires: { 'cpu.arch': ['amd64'] }, combinations: [{ 'os.kernel': '6.8.0-52', 'runtime.profile': 'tool-a' }, { 'os.kernel': '6.8.0-53', 'runtime.profile': 'tool-b' }] } }
 async function upload(name, version, arch = 'amd64', deps = matrix) {
  const r = await admin.request.post('/api/v1' + base + '/media/upload', { headers, multipart: { metadata: JSON.stringify({ type: 'driver', name, version, arch, deps }), file: { name: 'driver.bin', mimeType: 'application/octet-stream', buffer: Buffer.from('registered driver fixture') } } })
  const media = await r.json(); assert.equal(r.status(), 201, JSON.stringify(media))
  await call('POST', base + '/media/' + media.id + '/transition', { state: 'verified' })
  await call('POST', base + '/media/' + media.id + '/transition', { state: 'approved' }, 200, reviewer, reviewCSRF)
  return media
 }
 await call('PUT', base + '/params', { key: 'driver.version', value_type: 'version', source_media_type: 'driver' }, 201)
 await call('PUT', base + '/params', { key: 'os.kernel', value_type: 'enum', options: ['6.8.0-52', '6.8.0-53'] }, 201)
 await call('PUT', base + '/params', { key: 'cpu.arch', value_type: 'enum', options: ['amd64', 'arm64'] }, 201)
 await call('PUT', base + '/params', { key: 'runtime.profile', value_type: 'enum', options: ['tool-a', 'tool-b'] }, 201)
 const media = await upload('registered-driver', 'exact-1')
 const values = { 'driver.version': 'exact-1', 'cpu.arch': 'amd64', 'os.kernel': '6.8.0-52', 'runtime.profile': 'tool-a' }
 let result = await call('POST', base + '/params/validate', { values }); assert.equal(result.valid, true)
 assert.equal(result.selections.find(s => s.key === 'driver.version').media.sha256, media.sha256)
 for (const [bad, code] of [[{ ...values, 'os.kernel': '6.8.0-53' }, 'PARAM_COMBINATION_REJECTED'], [{ ...values, 'cpu.arch': 'arm64' }, 'PARAM_VALUE_REJECTED'], [{ 'driver.version': 'exact-1' }, 'PARAM_ARCH_REQUIRED'], [{ 'unknown.version': 'v1' }, 'PARAM_KEY_NOT_FOUND'], [{ ...values, 'os.kernel': 1 }, 'PARAM_VALUE_REJECTED']]) {
  result = await call('POST', base + '/params/validate', { values: bad }); assert.equal(result.valid, false); assert(result.issues.some(i => i.code === code))
 }
 result = await call('POST', base + '/params/validate', { values: { ...values, 'os.kernel': '6.8.0-53' } })
 assert.equal(result.issues.find(i => i.matrix).matrix.report_ref, 'report:fixture-1')
 assert.deepEqual(result.issues.find(i => i.code === 'PARAM_COMBINATION_REJECTED').matrix.combinations, matrix.compatibility.combinations)
 result = await call('POST', base + '/params/validate', { values: { ...values, 'os.kernel': '6.8.0-53', 'runtime.profile': 'tool-b' } }); assert.equal(result.valid, true)
 const yaml = `api_version: keel.sim/v1\ninputs:\n  cpu.arch: {type: enum, required: true, enum: [amd64, arm64], default: amd64}\n  driver.version: {type: string, required: true, default: exact-1}\n  os.kernel: {type: enum, required: true, enum: [6.8.0-52, 6.8.0-53], default: 6.8.0-52}\n  runtime.profile: {type: enum, required: true, enum: [tool-a, tool-b], default: tool-a}\ntasks:\n  - id: inspect\n    type: sim.read\n    retry: 0\n    idempotent: true\n    timeout_ms: 1000\n    resources: [machine]\n    sim: {outcome: success, duration_ms: 10, seed: 7}\n`
 const flow = await call('POST', base + '/flows', { name: 'Catalog exact compatibility', yaml_source: yaml, expected_version: 0 }, 201)
 const targets = await call('POST', base + '/sim-targets', { prefix: 'catalog', count: 1, seed: 7 }, 201)
 const input = { flow_version_id: flow.id, mode: 'sim', target_ids: [targets.items[0].id], parameters: values }
 assert.equal((await call('GET', base + '/executions')).total, 0)
 const invalid = await call('POST', base + '/executions', { ...input, parameters: { ...values, 'os.kernel': '6.8.0-53' } }, 422); assert.equal(invalid.code, 'PARAM_COMBINATION_REJECTED')
 assert.equal((await call('GET', base + '/executions')).total, 0)
 const execution = await call('POST', base + '/executions', input, 201), frozen = execution.snapshot.catalog
 assert.equal(frozen.find(s => s.key === 'driver.version').media.object_version.length > 0, true)
 assert.deepEqual(frozen.find(s => s.key === 'driver.version').media.deps.compatibility.combinations, matrix.compatibility.combinations)
 await page.goto(`/p/${project.id}/executions`)
 await page.getByRole('button', { name: '发起执行', exact: true }).click()
 await page.locator('#execution-flow').selectOption(flow.id)
 await page.locator('[id="execution-driver.version"]').waitFor()
 assert.equal(await page.locator('[id="execution-driver.version"]').evaluate(e => e.tagName), 'SELECT')
 await page.locator('[id="execution-os.kernel"]').selectOption('6.8.0-53')
 await page.getByRole('alert').filter({ hasText: 'fixture-vendor-report@reviewed-1' }).waitFor()
 assert.equal(await page.locator('.action-dialog button[type=submit]').isDisabled(), true)
 const combinations = page.getByRole('table', { name: '允许的完整兼容组合', exact: true }); await combinations.waitFor()
 assert.equal(await combinations.getByRole('row').count(), 3)
 await page.getByText('须匹配下表完整一行；不能混用不同行的参数。', { exact: true }).waitFor()
 await page.screenshot({ path: process.env.KEEL_WORKSPACE_EVIDENCE_DIR + '/catalog-correlated-rejected.png', fullPage: true })
 await page.locator('[id="execution-runtime.profile"]').selectOption('tool-b')
 await page.locator('.action-dialog button[type=submit]:enabled').waitFor()
 await page.locator('[id="execution-runtime.profile"]').selectOption('tool-a')
 await page.locator('[id="execution-os.kernel"]').selectOption('6.8.0-52')
 await page.locator('.action-dialog button[type=submit]:enabled').waitFor()
 await page.screenshot({ path: process.env.KEEL_WORKSPACE_EVIDENCE_DIR + '/catalog-compatible.png', fullPage: true })
 const created = page.waitForResponse(r => r.request().method() === 'POST' && r.url().endsWith(base + '/executions'))
 await page.locator('.action-dialog button[type=submit]').click(); assert.equal((await created).status(), 201)
 // Current catalog definitions changing must not rewrite the stored historical snapshot.
 for (let i = 0; i < 150; i++) { const d = await call('GET', base + '/executions/' + execution.id); if (d.execution.state === 'success') break; if (i === 149) throw new Error('worker did not finish'); await page.waitForTimeout(100) }
 const preview = await call('POST', base + '/executions/' + execution.id + '/rerun/preview', { mode: 'targets', target_ids: input.target_ids })
 const rerunInput = { mode: 'targets', target_ids: input.target_ids, expected_version: preview.source_version, selection_hash: preview.selection_hash, change_reason: 'Verify immutable media selection' }
 await call('POST', base + '/executions/' + execution.id + '/rerun', rerunInput, 201)
 await call('PUT', base + '/params', { key: 'os.kernel', value_type: 'enum', options: ['6.8.0-52', '6.8.0-53'], default_value: '6.8.0-52' }, 201)
 await call('POST', base + '/executions/' + execution.id + '/rerun', rerunInput, 409)
 assert.deepEqual((await call('GET', base + '/executions/' + execution.id)).execution.snapshot.catalog, frozen)
 const duplicate = await upload('different-driver', 'exact-1')
 result = await call('POST', base + '/params/validate', { values }); assert(result.issues.some(i => i.code === 'PARAM_MEDIA_AMBIGUOUS'))
 await call('POST', base + '/media/' + duplicate.id + '/transition', { state: 'retired' })
 await call('POST', base + '/media/' + media.id + '/transition', { state: 'retired' })
 result = await call('POST', base + '/params/validate', { values }); assert.equal(result.valid, false)
 const foreign = await call('POST', '/projects', { name: 'Other scope', slug: 'catalog-other-' + suffix }, 201)
 await call('POST', '/projects/' + foreign.id + '/params/validate', { values }, 404, reviewer, reviewCSRF)
 const anonymous = await browser.newContext({ baseURL: c.base_url }); assert.equal((await anonymous.request.post('/api/v1' + base + '/params/validate', { data: { values } })).status(), 401)
 assert.equal((await admin.request.post('/api/v1' + base + '/params/validate', { headers: { Origin: c.base_url }, data: { values } })).status(), 403)
 assert.deepEqual(errors, [])
 console.log('PASS exact correlated rows reject cross-row mixtures, accept both complete rows, preserve matrix provenance/snapshot, and show the full allowed rows in the live form')
 console.log('PASS approved stored artifact catalog, exact architecture/dependency/provenance checks, execution admission and frozen digest/version/matrix, stable rerun and changed-catalog refusal')
 console.log('PASS real parameter dropdowns and live dependency errors, ambiguity/retirement/type/scope/anonymous/CSRF rejection; historical snapshots preserved')
} finally { await browser.close() }
