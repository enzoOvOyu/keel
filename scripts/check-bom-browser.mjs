/** Browser acceptance for BOM import against an isolated test DB. */
import assert from 'node:assert/strict'
import { randomBytes } from 'node:crypto'
import { readFile, stat } from 'node:fs/promises'
import { pathToFileURL } from 'node:url'

const file = process.env.KEEL_AUTH_BROWSER_CREDENTIALS_FILE || '/tmp/keel-auth-browser-credentials.json'
assert(file.startsWith('/tmp/keel-'), 'Use an isolated test credential file')
assert.equal((await stat(file)).mode & 0o077, 0, 'Test credentials must be private')
const credentials = JSON.parse(await readFile(file, 'utf8'))
assert(['http://127.0.0.1:3102', 'http://localhost:3102'].includes(credentials.base_url), 'Only the isolated test API is allowed')
const { chromium } = await import(pathToFileURL(process.env.KEEL_PLAYWRIGHT_MODULE || '/tmp/keel-browser-check/node_modules/playwright/index.mjs').href)
const browser = await chromium.launch({ headless: true })
const context = await browser.newContext({ baseURL: credentials.base_url, viewport: { width: 1440, height: 960 }, locale: 'zh-CN', reducedMotion: 'reduce' })
const page = await context.newPage(); page.setDefaultTimeout(15_000)
const errors = [], passed = []
const suffix = randomBytes(4).toString('hex')
page.on('pageerror', (error) => errors.push(`Runtime: ${error.message}`))
page.on('console', (message) => {
  if (message.type() !== 'error') return
  const url = message.location().url ?? '', text = message.text()
  if (!((/40[1349]/.test(text) && /\/(auth|projects)\//.test(url)))) errors.push(`Console: ${text}`)
})
function pass(name) { passed.push(name); console.log(`PASS ${name}`) }

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
}, `bom-ui-${suffix}`)
// Register the model the BOM binds to.
await page.evaluate(async ([projectId, modelName]) => {
  const csrf = await (await fetch('/api/v1/auth/csrf', { credentials: 'same-origin' })).json()
  await fetch(`/api/v1/projects/${projectId}/models`, { method: 'POST', credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf.csrf_token, 'Idempotency-Key': crypto.randomUUID() }, body: JSON.stringify({ name: modelName, kind: 'server' }) })
}, [project.id, `BomUI-${suffix}`])

const pageURL = `/p/${project.id}/assets`
await page.goto(pageURL)
await page.waitForSelector('table.data-table')

const goodBom = {
  file_version: 'ui-v1',
  machines: [
    { serial_number: `SN-UI-${suffix}-1`, rack: 'R05', unit_position: 'U12', bmc_address: '10.0.0.9', hostname: 'bom-node-01', ip_reference: '10.1.1.10', model_name: `BomUI-${suffix}`,
      ports: [{ mac_address: 'aa:bb:cc:10:00:01', port_name: 'port1', rail: 0 }, { mac_address: 'aa:bb:cc:10:00:02', port_name: 'port2', rail: 1 }] },
  ],
}
const badBom = {
  file_version: 'ui-v2',
  machines: [
    { serial_number: `SN-UI-${suffix}-2`, rack: 'R05', unit_position: 'U13', bmc_address: '10.0.0.10', hostname: 'Bom_Node!', ip_reference: '', model_name: 'Nope-Model', ports: [{ mac_address: 'not-a-mac', port_name: 'port1', rail: 0 }] },
  ],
}

// 1. Preview classifies conflicts and blocks the commit button.
await page.click('button:has-text("导入 BOM"), button:has-text("Import BOM")')
await page.fill('#bom-raw', JSON.stringify(badBom, null, 2))
await page.click('.action-dialog button[type=submit]')
await page.waitForSelector('.impact-notice')
await page.waitForSelector('text=/已规划|非法|重复|Planned|Invalid|Duplicate|unknown/')
const commitBlocked = await page.locator('.action-dialog button:has-text("确认导入"), .action-dialog button:has-text("Commit import")').isDisabled()
assert(commitBlocked, 'commit stays disabled while conflicts exist')
pass('Preview classifies conflicts and blocks committing a bad batch')

// 2. A clean batch previews, commits and the asset appears in the list.
await page.fill('#bom-raw', JSON.stringify(goodBom, null, 2))
await page.click('.action-dialog button[type=submit]')
await page.waitForSelector('.impact-notice')
await page.click('.action-dialog button:has-text("确认导入"), .action-dialog button:has-text("Commit import")')
await page.waitForSelector('.success-notice')
await page.waitForSelector('tr:has-text("bom-node-01")')
const modelName = await page.locator('tr:has-text("bom-node-01") td').nth(2).textContent()
assert((modelName ?? '').includes(`BomUI-${suffix}`), `model binding visible: ${modelName}`)
pass('Commit materializes the asset with its model binding in the list')

// 3. Re-committing the same file version is rejected with a friendly notice.
await page.click('button:has-text("导入 BOM"), button:has-text("Import BOM")')
await page.fill('#bom-raw', JSON.stringify(goodBom, null, 2))
console.log('DIALOG-OPEN', await page.locator('.action-dialog').count())
await page.click('.action-dialog button[type=submit]')
await page.waitForSelector('.impact-notice[role="alert"]')
const notice = await page.locator('.impact-notice[role="alert"]').first().textContent()
assert(/已导入过|already imported/.test(notice ?? ''), `unexpected notice: ${notice}`)
pass('Duplicate file versions are rejected with a friendly notice')

// 4. CSV import: Chinese headers fold MAC rows into one machine per SN.
// The ALREADY_CURRENT dialog from group 3 is modal and blocks page clicks.
await page.click('.action-dialog button:has-text("取消"), .action-dialog button:has-text("Cancel")')
const csvBom = [
  'SN,主机名,型号,MAC 地址,端口,rail',
  `SN-CS-${suffix}-1,csv-node-01,BomUI-${suffix},aa:bb:cc:20:00:01,port1,0`,
  `SN-CS-${suffix}-1,csv-node-01,BomUI-${suffix},aa:bb:cc:20:00:02,port2,1`,
  `SN-CS-${suffix}-2,csv-node-02,BomUI-${suffix},aa:bb:cc:20:00:03,port1,0`,
].join('\n')
await page.click('button:has-text("导入 BOM"), button:has-text("Import BOM")')
await page.selectOption('#bom-format', 'csv')
await page.fill('#bom-version', `csv-v-${suffix}`)
await page.fill('#bom-raw', csvBom)
await page.click('.action-dialog button[type=submit]')
await page.waitForSelector('.impact-notice')
await page.click('.action-dialog button:has-text("确认导入"), .action-dialog button:has-text("Commit import")')
await page.waitForSelector('.success-notice')
await page.waitForSelector('tr:has-text("csv-node-01")')
await page.waitForSelector('tr:has-text("csv-node-02")')
const csvRow = await page.locator('tr:has-text("csv-node-01")').first().textContent()
assert((csvRow ?? '').includes(`BomUI-${suffix}`), 'CSV machine binds the model')
pass('CSV rows fold per SN and commit through the same pipeline')

// 5. A CSV missing the MAC column is rejected client-side with a message.
await page.click('button:has-text("导入 BOM"), button:has-text("Import BOM")')
await page.selectOption('#bom-format', 'csv')
await page.fill('#bom-version', 'csv-bad')
await page.fill('#bom-raw', 'SN,hostname\nSN-1,node-1')
await page.click('.action-dialog button[type=submit]')
await page.waitForSelector('.field-error')
const csvError = await page.locator('.field-error').first().textContent()
assert(/mac/i.test(csvError ?? ''), `expected MAC header error: ${csvError}`)
await page.click('.action-dialog button:has-text("取消"), .action-dialog button:has-text("Cancel")')
pass('Malformed CSV headers surface a field error before any request')

// 6. English page renders after the language switch.
await page.click('.language-button')
await page.goto(pageURL)
await page.waitForSelector('h1:has-text("Device assets")')
pass('English assets page renders')

await context.close()
await browser.close()
if (errors.length) { console.error(errors.join('\n')); process.exit(1) }
console.log(JSON.stringify({ status: 'passed', groups: passed.length, scope: 'isolated PostgreSQL + real API + Chromium' }))
