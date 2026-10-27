/** Current BOM package/CSV UI against an owned PostgreSQL/API fixture. */
import assert from 'node:assert/strict'
import { createHash, randomBytes } from 'node:crypto'
import { mkdtemp, mkdir, readFile, rm, stat, writeFile } from 'node:fs/promises'
import { dirname, join } from 'node:path'
import { tmpdir } from 'node:os'
import { pathToFileURL } from 'node:url'

const file = process.env.KEEL_AUTH_BROWSER_CREDENTIALS_FILE
assert(file); assert.equal((await stat(file)).mode & 0o077, 0)
const c = JSON.parse(await readFile(file, 'utf8'))
assert(['http://127.0.0.1:3102', 'http://localhost:3102'].includes(c.base_url))
const { chromium } = await import(pathToFileURL(process.env.KEEL_PLAYWRIGHT_MODULE || '/tmp/keel-browser-check/node_modules/playwright/index.mjs').href)
const browser = await chromium.launch({ headless: true }), scratch = await mkdtemp(join(tmpdir(), 'keel-bom-browser-'))
const context = await browser.newContext({ baseURL: c.base_url, viewport: { width: 1440, height: 960 }, reducedMotion: 'reduce' })
const page = await context.newPage(), errors = [], passed = []
page.setDefaultTimeout(15_000); page.on('pageerror', e => errors.push(e.message))
function pass(name) { passed.push(name); console.log('PASS ' + name) }
try {
  await page.goto('/login'); await page.locator('#username').fill(c.username); await page.locator('#password').fill(c.password)
  await page.locator('#login-form button[type=submit]').click(); await page.waitForURL('**/projects')
  const csrf = (await (await context.request.get('/api/v1/auth/csrf')).json()).csrf_token
  async function call(method, path, data, expected = 200) {
    const r = await context.request.fetch('/api/v1' + path, { method, data, headers: { Origin: c.base_url, 'X-CSRF-Token': csrf, 'Idempotency-Key': crypto.randomUUID() } })
    const out = await r.json(); assert.equal(r.status(), expected, JSON.stringify(out)); return out
  }
  const suffix = randomBytes(4).toString('hex'), project = await call('POST', '/projects', { name: 'BOM browser ' + suffix, slug: 'bom-ui-' + suffix }, 201)
  const base = '/projects/' + project.id, versions = {}
  for (const kind of ['server', 'nic', 'switch', 'optic_module']) {
    const model = await call('POST', base + '/models', { name: 'BOM-' + kind + '-' + suffix, kind }, 201)
    const spec = { hw_spec: { name: kind, cpu_arch: 'amd64' } }
    if (kind === 'nic' || kind === 'switch') spec.capability_matrix = { ports: { 1: { speed: '200G', sub_type: kind === 'nic' ? 'nic-200g' : 'ethernet-200g' } } }
    versions[kind] = await call('POST', base + '/models/' + model.id + '/versions', spec, 201)
    versions[kind].name = model.name
  }
  const csv = {
    'server/servers.csv': `bom_line_no,sn,hostname,model_ref,rack,u,sub_type,source_of_record\n1,UI-SN-${suffix},bom-node-01,${versions.server.id},R01,1,B300,design_bom\n`,
    'server/server_components.csv': `bom_line_no,parent_sn,category,sub_type,slot,serial,model_ref,mac\n2,UI-SN-${suffix},nic,CX8,1,UI-NIC-${suffix},${versions.nic.id},aa:bb:cc:dd:ee:01\n`,
    'server/server_ports.csv': `bom_line_no,parent_ref,port_index,port_role,mac,rail\n3,UI-NIC-${suffix},1,compute,aa:bb:cc:dd:ee:01,0\n`,
    'fabric/switches.csv': `bom_line_no,sn,hostname,model_ref,role,rack,idx,dc,pod,source_of_record,status\n4,UI-SW-${suffix},leaf-1,${versions.switch.id},leaf,R01,1,DC,P1,design_bom,planned\n`,
    'fabric/switch_ports.csv': `bom_line_no,parent_ref,port_index,port_name\n5,UI-SW-${suffix},1,swp1s0\n`,
    'fabric/optic_modules.csv': `bom_line_no,parent_ref,serial,sub_type,model_ref\n6,5,UI-OPT-${suffix},DR8,${versions.optic_module.id}\n`,
    'fabric/cables.csv': 'bom_line_no,endpoint_a,endpoint_b,cable_type,sub_type\n7,3,5,DAC,DAC200\n',
  }
  const entities = ['server', 'component', 'port', 'switch', 'port', 'optic_module', 'cable']
  const profile = { version_ref: 'profile-v1', server_columns: {}, fabric_columns: {} }
  for (const name of Object.keys(csv)) profile[name.startsWith('server/') ? 'server_columns' : 'fabric_columns'][name] = {}
  await call('POST', base + '/bom/mapping-profiles', profile, 201)
  await call('POST', base + '/bom/mapping-profiles/profile-v1/approve', {})
  const batch = `BOM-BATCH-${project.slug}-20261027-1`
  async function directory(label, bad = false) {
    const files = { ...csv }
    if (bad) files['server/servers.csv'] = files['server/servers.csv'].replace(versions.server.id, 'missing-version')
    const hash = createHash('sha256')
    for (const name of Object.keys(files).sort()) { const raw = Buffer.from(files[name]); hash.update(`${name}\0${raw.length}\0${createHash('sha256').update(raw).digest('hex')}\n`) }
    files['manifest.json'] = JSON.stringify({ schema_version: 'bom-import/v1', bom_import_batch_id: batch, project_code: project.slug, project_id: project.id, batch_date: '2026-10-27', seq: 1, declared_by: 'design_institute', profile_version_ref: 'profile-v1', domain_files: Object.keys(csv).map((name, i) => ({ domain: name.split('/')[0] === 'server' ? 'server' : 'fabric', file: name, entity: entities[i], columns: csv[name].split('\n')[0].split(',') })), row_count: 7, content_hash: 'sha256:' + hash.digest('hex') })
    const target = join(scratch, label)
    for (const [name, data] of Object.entries(files)) { const path = join(target, name); await mkdir(dirname(path), { recursive: true, mode: 0o700 }); await writeFile(path, data, { mode: 0o600 }) }
    return target
  }
  const good = await directory('good'), bad = await directory('bad', true)
  const dialog = page.getByRole('dialog')
  async function previewPackage(path) {
    await page.locator('#bom-package-directory').setInputFiles(path)
    const response = page.waitForResponse(r => r.request().method() === 'POST' && r.url().endsWith('/bom/packages/preview'))
    await dialog.getByRole('button', { name: '预检', exact: true }).click()
    const r = await response; assert.equal(r.status(), 200); return r.json()
  }
  async function commitPackage() {
    const response = page.waitForResponse(r => r.request().method() === 'POST' && r.url().endsWith('/bom/packages/commit'))
    await dialog.getByRole('button', { name: '确认导入', exact: true }).click()
    const r = await response; assert.equal(r.status(), 201); await dialog.waitFor({ state: 'hidden' }); return r.json()
  }
  await page.goto(`/p/${project.id}/assets`)
  await page.getByRole('button', { name: '标准 BOM 包导入', exact: true }).click()
  let preview = await previewPackage(bad)
  assert.equal(preview.can_commit, false)
  const issue = preview.issues.find(i => i.code === 'model_version_not_found')
  assert(issue); assert.equal(issue.file, 'server/servers.csv'); assert.equal(issue.bom_line_no, 1); assert.equal(issue.field, 'model_ref')
  assert(await dialog.getByRole('button', { name: '确认导入', exact: true }).isDisabled())
  const issueRow = dialog.getByRole('row').filter({ hasText: 'model_version_not_found' }); await issueRow.getByText('server/servers.csv', { exact: true }).waitFor()
  const downloaded = page.waitForEvent('download'); await dialog.getByRole('button', { name: '导出问题 CSV', exact: true }).click()
  const stream = await (await downloaded).createReadStream(), chunks = []; for await (const chunk of stream) chunks.push(chunk)
  const exported = Buffer.concat(chunks).toString('utf8'); assert(exported.includes('bom_line_no')); assert(exported.includes('"server/servers.csv","1","model_ref","model_version_not_found"'))
  assert.equal((await call('GET', base + '/assets')).total, 0)
  pass('Standard directory preview blocks invalid models, exports file/line/field CSV and writes no assets')
  preview = await previewPackage(good); assert.equal(preview.can_commit, true, JSON.stringify(preview.issues))
  const committed = await commitPackage(); assert.equal(committed.item_count, 7)
  await page.getByRole('row').filter({ hasText: 'bom-node-01' }).waitFor()
  const original = (await call('GET', base + '/assets')).items[0]; assert.equal(original.model_version_id, versions.server.id)
  pass('One standard package UI commit materializes server and network material with a frozen model version')
  await page.getByRole('button', { name: '标准 BOM 包导入', exact: true }).click()
  preview = await previewPackage(good); assert.equal(preview.idempotent, true)
  assert.equal(preview.can_commit, false)
  assert(await dialog.getByRole('button', { name: '确认导入', exact: true }).isDisabled())
  assert.deepEqual((await call('GET', base + '/assets')).items[0], original)
  await dialog.getByRole('button', { name: '取消', exact: true }).click()
  pass('An identical package is recognized and cannot create duplicate assets or versions')
  await page.getByRole('button', { name: '标准 BOM 包导入', exact: true }).click()
  preview = await previewPackage(bad); assert(preview.issues.some(i => i.code === 'batch_hash_conflict')); assert.equal(preview.can_commit, false)
  await dialog.getByRole('button', { name: '取消', exact: true }).click()
  pass('A changed package cannot reuse the committed batch ID')
  await page.getByRole('button', { name: '宽表 BOM 兼容导入', exact: true }).click()
  await page.locator('#bom-legacy-batch').fill(`BOM-BATCH-${project.slug}-20261027-2`)
  await page.locator('#bom-legacy-file').setInputFiles({ name: 'wide.csv', mimeType: 'text/csv', buffer: Buffer.from(`SN,hostname,model_name,MAC,port_name,rail\nCSV-${suffix},csv-node-01,${versions.server.name},aa:bb:cc:11:22:01,p1,0\nCSV-${suffix},csv-node-01,${versions.server.name},aa:bb:cc:11:22:02,p2,1\n`) })
  let response = page.waitForResponse(r => r.request().method() === 'POST' && r.url().endsWith('/bom/legacy/preview'))
  await dialog.getByRole('button', { name: '预检', exact: true }).click(); preview = await (await response).json(); assert.equal(preview.can_commit, true, JSON.stringify(preview.issues)); assert.equal(preview.machines, 1); assert.equal(preview.ports, 2)
  response = page.waitForResponse(r => r.request().method() === 'POST' && r.url().endsWith('/bom/legacy/commit'))
  await dialog.getByRole('button', { name: '确认导入', exact: true }).click(); assert.equal((await response).status(), 201); await dialog.waitFor({ state: 'hidden' })
  await page.getByRole('row').filter({ hasText: 'csv-node-01' }).waitFor(); assert.equal((await call('GET', base + '/assets')).total, 2)
  pass('CSV file UI folds MAC rows by serial into one stable model-bound asset')
  await page.getByRole('button', { name: 'Switch to English', exact: true }).click()
  await page.getByRole('button', { name: 'Import standard BOM package', exact: true }).waitFor()
  await page.getByRole('button', { name: 'Legacy wide BOM import', exact: true }).waitFor()
  assert.deepEqual(errors, []); pass('Both current BOM entry points render in English without runtime errors')
  console.log(JSON.stringify({ status: 'passed', groups: passed.length, scope: 'isolated PostgreSQL + actual API + Chromium; no hardware observations' }))
} finally { await browser.close(); await rm(scratch, { recursive: true, force: true }) }
