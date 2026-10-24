/** Browser acceptance for the media library page against an isolated test DB. */
import assert from 'node:assert/strict'
import { randomBytes, randomUUID, createHash } from 'node:crypto'
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
let credentialLeak = false
const suffix = randomBytes(5).toString('hex')
const reviewerPassword = randomBytes(24).toString('base64url')
const secrets = [credentials.password, reviewerPassword]
function pass(name) { passed.push(name); console.log(`PASS ${name}`) }
async function createPage() {
  const context = await browser.newContext({ baseURL: credentials.base_url, viewport: { width: 1440, height: 960 }, locale: 'zh-CN', reducedMotion: 'reduce' })
  contexts.push(context)
  const page = await context.newPage(); page.setDefaultTimeout(15_000)
  page.on('pageerror', (error) => errors.push(`Runtime: ${error.message}`))
  page.on('console', (message) => {
    if (message.type() !== 'error') return
    const url = message.location().url ?? '', text = message.text()
    const expected = (/40[134]/.test(text) && /\/(auth|projects)\//.test(url)) || (/409/.test(text) && /\/(media|projects)\//.test(url)) || (/42[29]/.test(text) && url.includes('/media'))
    if (!expected) errors.push(`Console: ${text}`)
  })
  page.on('request', (request) => { if (request.url().includes(credentials.password)) credentialLeak = true })
  await page.goto('/login')
  await page.locator('#username').fill(credentials.username)
  await page.locator('#password').fill(credentials.password)
  await page.locator('#login-form button[type=submit]').click()
  await page.waitForURL('**/projects')
  return page
}

const admin = await createPage()
const project = await admin.evaluate(async (name) => {
  const csrf = await (await fetch('/api/v1/auth/csrf', { credentials: 'same-origin' })).json()
  const response = await fetch('/api/v1/projects', { method: 'POST', credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf.csrf_token, 'Idempotency-Key': crypto.randomUUID() }, body: JSON.stringify({ name, slug: name }) })
  return (await response.json())
}, `media-ui-${suffix}`)
const projectURL = `/p/${project.id}/media`
await admin.goto(projectURL)
await admin.waitForSelector('table.data-table')

// Register a driver from the UI dialog with a strict digest.
await admin.click('button:has-text("登记介质"), button:has-text("Register media")')
const uploadBytes=Buffer.from('keel-ui-file-fixture'),digest=createHash('sha256').update(uploadBytes).digest('hex')
await admin.fill('#media-name', `ui-driver-${suffix}`)
await admin.fill('#media-version', '1.0.0')
await admin.fill('#media-sha', digest)
await admin.fill('#media-size', String(uploadBytes.length))
await admin.fill('#media-manifest', `minio://media/ui-driver-${suffix}.tar.gz`)
await admin.click('.action-dialog button[type=submit]')
await admin.waitForSelector('.success-notice')
await admin.waitForSelector(`text=${`ui-driver-${suffix}`}`)
pass('Media registration dialog registers an item with digest validation')
await admin.getByRole('button',{name:'上传登记文件',exact:true}).click()
await admin.locator('#media-attach-file').setInputFiles({name:'fixture.bin',mimeType:'application/octet-stream',buffer:uploadBytes})
await admin.locator('.action-dialog button[type=submit]').click()
await admin.getByText('文件已上传。',{exact:true}).waitFor()


// Register a reviewer, then approve through the UI from a different browser context.
const reviewerName = `ui-media-rev-${suffix}`
await admin.evaluate(async ([name, password, projectId]) => {
  const csrf = await (await fetch('/api/v1/auth/csrf', { credentials: 'same-origin' })).json()
  const headers = { 'Content-Type': 'application/json', 'X-CSRF-Token': csrf.csrf_token, 'Idempotency-Key': crypto.randomUUID() }
  await fetch('/api/v1/admin/users', { method: 'POST', credentials: 'same-origin', headers, body: JSON.stringify({ username: name, display_name: 'Media reviewer', password, is_admin: false }) })
  await fetch(`/api/v1/projects/${projectId}/members`, { method: 'POST', credentials: 'same-origin', headers, body: JSON.stringify({ username: name, role: 'Admin' }) })
}, [reviewerName, reviewerPassword, project.id])
const reviewerContext = await browser.newContext({ baseURL: credentials.base_url, viewport: { width: 1440, height: 960 }, locale: 'zh-CN' })
contexts.push(reviewerContext)
const reviewer = await reviewerContext.newPage(); reviewer.setDefaultTimeout(15_000)
await reviewer.goto('/login')
await reviewer.locator('#username').fill(reviewerName)
await reviewer.locator('#password').fill(reviewerPassword)
await reviewer.locator('#login-form button[type=submit]').click()
await reviewer.waitForURL('**/projects')
await reviewer.goto(projectURL)
await reviewer.waitForSelector('table.data-table')

// Verify then approve from the reviewer's page; the uploader's own approve is blocked.
await admin.click(`tr:has-text("ui-driver-${suffix}") button:has-text("校验"), tr:has-text("ui-driver-${suffix}") button:has-text("Verify")`)
await admin.click('.action-dialog button[type=submit]')
await admin.waitForSelector(`tr:has-text("ui-driver-${suffix}") >> text=/已校验|Verified/`)
pass('Verification advances from the uploader page after manifest registration')
await admin.click(`tr:has-text("ui-driver-${suffix}") button:has-text("审批"), tr:has-text("ui-driver-${suffix}") button:has-text("Approve")`)
await admin.click('.action-dialog button[type=submit]')
await admin.waitForSelector(`tr:has-text("ui-driver-${suffix}") >> text=/已校验|Verified/`)
pass('Self-approval stays rejected with the item still verified')
await reviewer.goto(projectURL)
await reviewer.waitForSelector('table.data-table')
await reviewer.waitForSelector(`tr:has-text("ui-driver-${suffix}") >> text=/已校验|Verified/`)
await reviewer.click(`tr:has-text("ui-driver-${suffix}") button:has-text("审批"), tr:has-text("ui-driver-${suffix}") button:has-text("Approve")`)
await reviewer.click('.action-dialog button[type=submit]')
await reviewer.waitForSelector(`tr:has-text("ui-driver-${suffix}") >> text=/已审批|Approved/`)
pass('A second reviewer approves through the UI and the state is visible to both')

// Retire with the explicit impact notice, after refreshing the admin's view.
await admin.goto(projectURL)
await admin.waitForSelector('table.data-table')
await admin.waitForSelector(`tr:has-text("ui-driver-${suffix}") >> text=/已审批|Approved/`)
await admin.click(`tr:has-text("ui-driver-${suffix}") button:has-text("退役"), tr:has-text("ui-driver-${suffix}") button:has-text("Retire")`)
await admin.waitForSelector('.impact-notice')
await admin.click('.action-dialog button[type=submit]')
await admin.waitForSelector(`tr:has-text("ui-driver-${suffix}") >> text=/已退役|Retired/`)
const actions = await admin.locator(`tr:has-text("ui-driver-${suffix}") .table-actions button`).count()
assert.equal(actions, 0, 'retired media offers no further transitions')
pass('Retirement shows the impact notice and removes further actions')

// The page renders in English as well.
await admin.getByRole('button',{name:'Switch to English',exact:true}).click()
await admin.goto(projectURL)
await admin.waitForSelector('table.data-table')
const english = await admin.locator('h1:has-text("Media library")').count()
assert.equal(english, 1, 'English heading renders')
pass('English media page renders after language switch')

assert(!credentialLeak, 'No credential values appear in requests')
for (const context of contexts) await context.close()
await browser.close()
if (errors.length) { console.error(errors.join('\n')); process.exit(1) }
console.log(JSON.stringify({ status: 'passed', groups: passed.length, scope: 'isolated PostgreSQL + real API + Chromium' }))
