import assert from 'node:assert/strict'
import { readFile, stat } from 'node:fs/promises'
import { pathToFileURL } from 'node:url'
const file = process.env.KEEL_AUTH_BROWSER_CREDENTIALS_FILE
assert(file?.startsWith('/tmp/keel-models-')); assert.equal((await stat(file)).mode & 0o077, 0)
const c = JSON.parse(await readFile(file, 'utf8')); assert.equal(c.base_url, 'http://127.0.0.1:3102')
const { chromium } = await import(pathToFileURL('/tmp/keel-browser-check/node_modules/playwright/index.mjs').href)
const browser = await chromium.launch({ headless: true }), ctx = await browser.newContext({ baseURL: c.base_url, viewport: { width: 1440, height: 1080 }, reducedMotion: 'reduce' }), page = await ctx.newPage(), errors = []
page.on('pageerror', e => errors.push(e.message))
page.on('console', m => { if (m.type() === 'error' && !/status of 40[01349]/.test(m.text())) errors.push(m.text()) })
try {
 const before = await (await ctx.request.get('/api/v1/auth/csrf')).json()
 let r = await ctx.request.post('/api/v1/auth/login', { headers: { Origin: c.base_url, 'X-CSRF-Token': before.csrf_token }, data: { method: 'local', username: c.username, password: c.password } }); assert.equal(r.status(), 200)
 const csrf = (await r.json()).csrf_token
 const suffix = crypto.randomUUID().slice(0, 8), name = 'GB300-tray-fixture-' + suffix
 r = await ctx.request.post('/api/v1/projects', { headers: { Origin: c.base_url, 'X-CSRF-Token': csrf, 'Idempotency-Key': crypto.randomUUID() }, data: { name: 'Compute tray fixture', slug: 'tray-' + suffix } }); assert.equal(r.status(), 201)
 const project = await r.json(), base = '/projects/' + project.id + '/models'
 await page.goto(`/p/${project.id}/models`)
 await page.getByRole('button', { name: '新建型号', exact: true }).click()
 assert.equal(await page.locator('#model-kind option[value="compute-tray"]').textContent(), '计算托盘')
 await page.locator('#model-name').fill(name); await page.locator('#model-kind').selectOption('compute-tray')
 let response = page.waitForResponse(r => r.request().method() === 'POST' && r.url().endsWith(base))
 await page.getByRole('dialog').getByRole('button', { name: '新建型号', exact: true }).click(); r = await response; assert.equal(r.status(), 201)
 const model = await r.json(); assert.equal(model.kind, 'compute-tray')
 const row = () => page.getByRole('row').filter({ has: page.getByText(name, { exact: true }) }).first()
 await row().getByRole('cell', { name: '计算托盘', exact: true }).waitFor()
 const scan = { source_host: 'independent-tray-fixture', hw_spec: { model: name, kind: 'compute-tray', cpu_arch: 'arm64', gpu_count: 2, gpu: 'Fixture-GPU' }, bom_expect: { nic_count: 1, mac_prefixes: ['aa:bb:cc'] }, slots: [{ slot_no: 1, pcie_slot: 'PCIe-1', expect_nic_model: 'Fixture-NIC', panel_port: 'p1', default_role: 'compute' }] }
 await page.getByRole('button', { name: '扫描导入', exact: true }).click(); await page.locator('#scan-manifest').fill(JSON.stringify(scan))
 response = page.waitForResponse(r => r.request().method() === 'POST' && r.url().endsWith(base + '/import'))
 await page.getByRole('dialog').getByRole('button', { name: '扫描导入', exact: true }).click(); r = await response; assert.equal(r.status(), 201)
 const draft = await r.json(); assert.equal(draft.model_id, model.id)
 await row().getByRole('button', { name: '版本历史', exact: true }).click()
 await page.getByRole('button', { name: '核对后发布', exact: true }).click()
 assert.equal(JSON.parse(await page.locator('#hw-spec').inputValue()).gpu_count, 2)
 response = page.waitForResponse(r => r.request().method() === 'POST' && r.url().endsWith(base + '/' + model.id + '/versions'))
 await page.getByRole('dialog').locator('button[type=submit]').click(); r = await response; assert.equal(r.status(), 201); const version = await r.json()
 await page.goto(`/p/${project.id}/assets`); await page.getByRole('button', { name: '宽表 BOM 兼容导入', exact: true }).click()
 await page.locator('#bom-legacy-batch').fill(`BOM-BATCH-${project.slug}-20261025-1`)
 await page.locator('#bom-legacy-file').setInputFiles({ name: 'tray-wide.csv', mimeType: 'text/csv', buffer: Buffer.from(`SN,hostname,model_name,MAC,port_name,rail\nTRAY-UI-1,tray-node-1,${name},aa:bb:cc:44:55:66,p1,0\n`) })
 response = page.waitForResponse(r => r.request().method() === 'POST' && r.url().includes('/bom/legacy/preview'))
 await page.getByRole('dialog').getByRole('button', { name: '预检', exact: true }).click(); r = await response; assert.equal(r.status(), 200)
 const preview = await r.json(); assert.equal(preview.can_commit, false); assert(!preview.preview_token)
 assert(!preview.issues.some(i => ['model_version_not_found', 'model_kind_mismatch'].includes(i.code)))
 for (const code of ['legacy_gpu_inventory_unverifiable', 'legacy_nic_inventory_unverifiable']) {
  const issue = preview.issues.find(i => i.code === code); assert(issue); assert.equal(issue.model_version_ref, version.id); assert.equal(issue.bom_line_no, 2)
 }
 assert(await page.getByRole('button', { name: '确认导入', exact: true }).isDisabled())
 const download = page.waitForEvent('download'); await page.getByRole('button', { name: '导出问题 CSV', exact: true }).click()
 const csv = await readFile(await (await download).path(), 'utf8'); assert(csv.includes(version.id)); assert(csv.includes('legacy_gpu_inventory_unverifiable')); assert(csv.includes('legacy_nic_inventory_unverifiable'))
 await page.screenshot({ path: process.env.KEEL_WORKSPACE_EVIDENCE_DIR + '/compute-tray-bom-difference.png', fullPage: true })
 await page.getByRole('dialog').getByRole('button', { name: '取消', exact: true }).click()
 await page.goto(`/p/${project.id}/models`); await page.getByRole('button', { name: 'Switch to English', exact: true }).click()
 await row().getByRole('cell', { name: 'Compute tray', exact: true }).waitFor()
 assert.deepEqual(errors, [])
 console.log('PASS compute-tray UI identity/import/review/publication and bilingual kind labels; legacy BOM resolves the immutable tray version and blocks unverifiable GPU/NIC inventory with source lines and CSV details')
} finally { await browser.close() }
