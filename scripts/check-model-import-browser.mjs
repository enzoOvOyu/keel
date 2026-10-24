/** Browser acceptance for scan-import of device models against an isolated test DB. */
import assert from 'node:assert/strict'
import { randomBytes, randomUUID } from 'node:crypto'
import { readFile, stat } from 'node:fs/promises'
import { pathToFileURL } from 'node:url'

const file = process.env.KEEL_AUTH_BROWSER_CREDENTIALS_FILE || '/tmp/keel-auth-browser-credentials.json'
assert(file.startsWith('/tmp/keel-'), 'Use an isolated test credential file')
assert.equal((await stat(file)).mode & 0o077, 0, 'Test credentials must be private')
const credentials = JSON.parse(await readFile(file, 'utf8'))
assert(['http://127.0.0.1:3102', 'http://localhost:3102'].includes(credentials.base_url), 'Only the isolated test API is allowed')
const { chromium } = await import(pathToFileURL(process.env.KEEL_PLAYWRIGHT_MODULE || '/tmp/keel-browser-check/node_modules/playwright/index.mjs').href)
const browser = await chromium.launch({ headless: true })
const contexts = [], errors = [], passed = []
const suffix = randomBytes(5).toString('hex')
const secrets = [credentials.password]
function pass(name) { passed.push(name); console.log(`PASS ${name}`) }

const context = await browser.newContext({ baseURL: credentials.base_url, viewport: { width: 1440, height: 960 }, locale: 'zh-CN', reducedMotion: 'reduce' })
contexts.push(context)
const page = await context.newPage(); page.setDefaultTimeout(15_000)
page.on('pageerror', (error) => errors.push(`Runtime: ${error.message}`))
page.on('console', (message) => {
  if (message.type() !== 'error') return
  const url = message.location().url ?? '', text = message.text()
  const expected = (/40[134]/.test(text) && /\/(auth|projects)\//.test(url)) || (/409/.test(text) && /\/(models|projects)\//.test(url))
  if (!expected) errors.push(`Console: ${text}`)
})

// Login and open the models page of a fresh project.
await page.goto('/login')
await page.locator('#username').fill(credentials.username)
await page.locator('#password').fill(credentials.password)
await page.locator('#login-form button[type=submit]').click()
await page.waitForURL('**/projects')
const project = await page.evaluate(async (name) => {
  const csrf = await (await fetch('/api/v1/auth/csrf', { credentials: 'same-origin' })).json()
  const response = await fetch('/api/v1/projects', { method: 'POST', credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf.csrf_token, 'Idempotency-Key': crypto.randomUUID() }, body: JSON.stringify({ name, slug: name }) })
  return (await response.json())
}, `scan-ui-${suffix}`)
const pageURL = `/p/${project.id}/models`
await page.goto(pageURL)
await page.waitForSelector('table.data-table')

// The manifest mirrors collect-model-inventory.py output (with _warnings stripped by the UI).
const manifest = {
  source_host: `poc-node-${suffix}`,
  hw_spec: { vendor: 'NVIDIA', model: `ScanUI-${suffix}`, kind: 'server', gpu_count: 8 },
  slots: [
    { slot_no: 1, pcie_slot: '01', expect_nic_model: 'cx7', panel_port: 'p1', default_role: 'compute' },
    { slot_no: 2, pcie_slot: '02', expect_nic_model: 'cx7', panel_port: 'p2', default_role: 'management' },
  ],
  _warnings: ['nvidia-smi unavailable in this environment'],
}

// 1. Scan import via the dialog: manifest pasted, draft created, model auto-appears.
await page.click('button:has-text("扫描导入"), button:has-text("Scan import")')
await page.fill('#scan-manifest', JSON.stringify(manifest, null, 2))
await page.click('.action-dialog button[type=submit]')
await page.waitForSelector('.success-notice')
await page.waitForSelector(`tr:has-text("ScanUI-${suffix}")`)
pass('Scan import creates the model row from the pasted manifest')

// 2. Expanding shows the draft with source host and content hash.
await page.click(`tr:has-text("ScanUI-${suffix}") button:has-text("版本历史"), tr:has-text("ScanUI-${suffix}") button:has-text("Version history")`)
await page.waitForSelector(`tr:has-text("poc-node-${suffix}")`)
const hash = await page.locator(`tr:has-text("poc-node-${suffix}") td.mono-value`).first().textContent()
assert(hash && hash.length >= 16, 'draft shows a content hash')
pass('The draft appears in the review area with source host and hash')

// 3. Re-importing the identical manifest hits the existing draft (idempotent through the UI).
await page.click(`tr:has-text("ScanUI-${suffix}") button:has-text("版本历史"), tr:has-text("ScanUI-${suffix}") button:has-text("Version history")`)
await page.click('button:has-text("扫描导入"), button:has-text("Scan import")')
await page.fill('#scan-manifest', JSON.stringify(manifest, null, 2))
await page.click('.action-dialog button[type=submit]')
await page.waitForSelector('.success-notice')
await page.click(`tr:has-text("ScanUI-${suffix}") button:has-text("版本历史"), tr:has-text("ScanUI-${suffix}") button:has-text("Version history")`)
await page.waitForSelector(`tr:has-text("poc-node-${suffix}")`)
const draftCount = await page.locator(`tr:has-text("poc-node-${suffix}")`).count()
assert.equal(draftCount, 1, 'identical re-import does not create a second draft')
pass('Re-importing an identical manifest reuses the draft (no duplicate)')

// 4. Publish from the draft: the form is prefilled with the draft content.
await page.click(`tr:has-text("poc-node-${suffix}") button:has-text("核对后发布"), tr:has-text("poc-node-${suffix}") button:has-text("Review and publish")`)
await page.waitForSelector('#hw-spec')
const prefilled = await page.locator('#hw-spec').inputValue()
assert(prefilled.includes(`ScanUI-${suffix}`), 'hw_spec is prefilled from the draft')
const slotRows = await page.locator('.slot-row').count()
assert.equal(slotRows, 2, 'slots are prefilled from the draft')
await page.click('.action-dialog button[type=submit]')
await page.waitForSelector('.success-notice')
await page.click(`tr:has-text("ScanUI-${suffix}") button:has-text("版本历史"), tr:has-text("ScanUI-${suffix}") button:has-text("Version history")`)
await page.waitForSelector(`tr:has-text("poc-node-${suffix}")`)  // the draft remains until discarded
pass('Publishing from a prefilled draft creates the immutable version')

// 5. Re-importing published content now answers ALREADY_CURRENT with a friendly notice.
await page.click(`tr:has-text("ScanUI-${suffix}") button:has-text("版本历史"), tr:has-text("ScanUI-${suffix}") button:has-text("Version history")`)
await page.click('button:has-text("扫描导入"), button:has-text("Scan import")')
await page.fill('#scan-manifest', JSON.stringify(manifest, null, 2))
await page.click('.action-dialog button[type=submit]')
await page.waitForSelector('.impact-notice[role="alert"]')
const notice = await page.locator('.impact-notice[role="alert"]').first().textContent()
assert(/最新已发布|latest published/.test(notice ?? ''), `unexpected notice: ${notice}`)
pass('Importing content identical to the published version shows the current notice')
await page.goto(pageURL)
await page.waitForSelector('table.data-table')

// 6. Discard the draft; the version history remains intact. The page reloaded in step 5, so one click expands.
await page.click(`tr:has-text("ScanUI-${suffix}") button:has-text("版本历史"), tr:has-text("ScanUI-${suffix}") button:has-text("Version history")`)
await page.click(`tr:has-text("poc-node-${suffix}") button:has-text("废弃草稿"), tr:has-text("poc-node-${suffix}") button:has-text("Discard draft")`)
await page.click('.action-dialog button[type=submit]')
await page.waitForSelector('.action-dialog', { state: 'detached' })
await page.waitForSelector(`tr:has-text("poc-node-${suffix}")`, { state: 'detached' })
const versionRow = await page.evaluate(() => {
  const rows = [...document.querySelectorAll('.model-versions table tbody tr')]
  return rows.filter((tr) => (tr.textContent ?? '').includes('v1')).length
})
assert(versionRow === 1, `published version still listed after discarding the draft (got ${versionRow})`)
pass('Discarding the draft leaves the published version untouched')

// 7. English rendering after the language switch.
await page.getByRole('button', {name: 'Switch to English', exact: true}).click()
await page.goto(pageURL)
await page.click(`tr:has-text("ScanUI-${suffix}") button:has-text("Version history")`)
await page.waitForSelector('h2:has-text("Review drafts")')
pass('English models page renders the draft area')

for (const ctx of contexts) await ctx.close()
await browser.close()
if (errors.length) { console.error(errors.join('\n')); process.exit(1) }
console.log(JSON.stringify({ status: 'passed', groups: passed.length, scope: 'isolated PostgreSQL + real API + Chromium' }))
