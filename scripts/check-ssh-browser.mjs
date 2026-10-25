import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { pathToFileURL } from 'node:url'

const credentialsFile = process.env.KEEL_SSH_BROWSER_CREDENTIALS_FILE
assert(credentialsFile, 'Private disposable browser credentials required')
const input = JSON.parse(await readFile(credentialsFile, 'utf8'))
const base = new URL(input.base_url)
assert.equal(base.protocol, 'http:')
assert.equal(base.hostname, '127.0.0.1')
assert(!['3000', '3001', '5173'].includes(base.port))
const { chromium } = await import(pathToFileURL(process.env.KEEL_PLAYWRIGHT_MODULE || '/tmp/keel-browser-check/node_modules/playwright/index.mjs').href)
const browser = await chromium.launch({ headless: true })
try {
 const context = await browser.newContext({ baseURL: base.origin, viewport: { width: 1440, height: 1000 } })
 const page = await context.newPage()
 const errors = []
 page.on('pageerror', error => errors.push(error.message))
 await page.goto('/login')
 await page.locator('#username').fill(input.username)
 await page.locator('#password').fill(input.password)
 await page.locator('button[type=submit]').click()
 await page.waitForURL('**/projects')
 await page.goto(`/p/${input.project_id}/executions?execution=${input.execution_id}&tab=overview`)
 await page.getByText(/受限 SSH 只读任务|Bounded read-only SSH/).waitFor()
 assert.equal(await page.getByText(/尚未连接真实设备/).count(), 0)
 await page.goto(`/p/${input.project_id}/executions?execution=${input.execution_id}&tab=outputs`)
 await page.getByText(/受限 SSH 只读任务|Bounded read-only SSH/).waitFor()
 await page.getByText('host.identity', { exact: false }).first().waitFor()
 assert.deepEqual(errors, [])
 console.log('PASS actual SSH execution workspace renders transport boundary and persisted outputs without browser errors')
 const session = await (await context.request.get('/api/v1/auth/session')).json()
 assert.equal((await context.request.post('/api/v1/auth/logout', { headers: { Origin: base.origin, 'X-CSRF-Token': session.csrf_token } })).status(), 204)
} finally { await browser.close() }
