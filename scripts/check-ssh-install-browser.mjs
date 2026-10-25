import assert from 'node:assert/strict'
import { lstat, readFile } from 'node:fs/promises'
import { pathToFileURL } from 'node:url'
const file = process.env.KEEL_SSH_INSTALL_BROWSER_CREDENTIALS_FILE
assert(file)
const info = await lstat(file)
assert(info.isFile() && !info.isSymbolicLink() && (info.mode & 0o077) === 0 && info.uid === process.geteuid())
const input = JSON.parse(await readFile(file, 'utf8')), base = new URL(input.base_url)
assert.equal(base.hostname, '127.0.0.1'); assert.equal(base.protocol, 'http:'); assert(!['3000', '3001', '5173'].includes(base.port))
const { chromium } = await import(pathToFileURL(process.env.KEEL_PLAYWRIGHT_MODULE || '/tmp/keel-browser-check/node_modules/playwright/index.mjs').href)
const browser = await chromium.launch({ headless: true }), contexts = [], errors = []
async function login(username) {
  const context = await browser.newContext({ baseURL: base.origin, viewport: { width: 1440, height: 1000 } }); contexts.push(context)
  const page = await context.newPage(); page.on('pageerror', error => errors.push(error.message))
  await page.goto('/login'); await page.locator('#username').fill(username); await page.locator('#password').fill(input.password)
  await page.locator('button[type=submit]').click(); await page.waitForURL('**/projects'); await page.goto(`/p/${input.project_id}/assets`)
  await page.getByRole('heading', { name: 'SSH 主机信任验证', exact: true }).waitFor()
  return { context, page }
}
try {
  const creator = await login(input.username)
  await creator.page.getByRole('button', { name: '新建验证请求', exact: true }).click()
  const form = creator.page.getByRole('dialog')
  await form.locator('#ssh-trust-machine').selectOption(input.machine_id); await form.locator('#ssh-trust-host-key').fill(input.host_key)
  await form.locator('#ssh-trust-reason').fill('Disposable browser approval: pinned target and selected domain key')
  await form.locator('#ssh-trust-minutes').fill('5'); await form.locator('#ssh-trust-install-key').selectOption(input.key_id)
  assert.equal((await form.locator('code').textContent()).trim(), input.public_key.trim())
  await form.getByText(/通过运维已配置的临时非特权 SSH 通道/).waitFor()
  const created = creator.page.waitForResponse(response => response.url().endsWith('/ssh-trust-requests') && response.request().method() === 'POST')
  await form.getByRole('button', { name: '新建验证请求', exact: true }).click()
  const createResponse = await created; assert.equal(createResponse.status(), 201)
  const request = await createResponse.json(); assert.equal(request.install_key.id, input.key_id); assert.equal(request.install_key.public_key, input.public_key)
  const selfRow = creator.page.locator(`[data-ssh-trust-request="${request.id}"]`); await selfRow.waitFor()
  await selfRow.getByText('安装并验证域公钥', { exact: true }).waitFor(); assert.equal(await selfRow.getByRole('button', { name: '批准验证', exact: true }).count(), 0)
  const approver = await login(input.approver_username), row = approver.page.locator(`[data-ssh-trust-request="${request.id}"]`)
  await row.getByRole('button', { name: '批准验证', exact: true }).click()
  let review = approver.page.getByRole('dialog'); await review.getByText(input.public_key.trim(), { exact: true }).waitFor()
  await review.getByRole('button', { name: '批准验证', exact: true }).click(); await review.waitFor({ state: 'hidden' })
  await row.getByRole('button', { name: '执行验证', exact: true }).click()
  review = approver.page.getByRole('dialog'); await review.getByText(input.public_key.trim(), { exact: true }).waitFor()
  const accepted = approver.page.waitForResponse(response => response.url().endsWith(`/ssh-trust-requests/${request.id}/execute`) && response.request().method() === 'POST')
  await review.getByRole('button', { name: '执行验证', exact: true }).click()
  const executionResponse = await accepted; assert.equal(executionResponse.status(), 201)
  const execution = await executionResponse.json()
  await approver.page.waitForURL(url => url.pathname.endsWith('/executions') && url.searchParams.get('execution') === execution.id)
  await creator.page.goto(`/p/${input.project_id}/assets`)
  const completed = creator.page.locator(`[data-ssh-trust-request="${request.id}"]`)
  await completed.getByText(/验证成功|Verified/, { exact: true }).waitFor()
  assert((await completed.getByRole('link', { name: /查看执行|View execution/ }).getAttribute('href')).includes(execution.id))
  await creator.page.getByRole('button', { name: 'Switch to English', exact: true }).click()
  await completed.getByText('Install and verify domain key', { exact: true }).waitFor()
  assert.deepEqual(errors, [])
  console.log('PASS real two-user UI: select frozen public key, prevent self-approval, independently approve exact key, execute one install/new-key proof, retain actual Execution evidence and English action label')
} finally {
  for (const context of contexts) {
    const session = await context.request.get('/api/v1/auth/session').catch(() => null)
    if (session?.ok()) { const body = await session.json(); await context.request.post('/api/v1/auth/logout', { headers: { Origin: base.origin, 'X-CSRF-Token': body.csrf_token } }).catch(() => {}) }
  }
  await browser.close()
}
