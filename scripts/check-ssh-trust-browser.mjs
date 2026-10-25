import assert from 'node:assert/strict'
import { lstat, readFile } from 'node:fs/promises'
import { pathToFileURL } from 'node:url'

const credentialsFile = process.env.KEEL_SSH_TRUST_BROWSER_CREDENTIALS_FILE
assert(credentialsFile, 'Private disposable browser credentials required')
const file = await lstat(credentialsFile)
assert(file.isFile() && !file.isSymbolicLink() && (file.mode & 0o077) === 0 && file.uid === process.geteuid())
const input = JSON.parse(await readFile(credentialsFile, 'utf8'))
const base = new URL(input.base_url)
assert.equal(base.protocol, 'http:')
assert.equal(base.hostname, '127.0.0.1')
assert(!['3000', '3001', '5173'].includes(base.port))
for (const id of [input.project_id, input.request_id, input.machine_id]) assert.match(id, /^[a-f0-9]{32}$/)
const { chromium } = await import(pathToFileURL(process.env.KEEL_PLAYWRIGHT_MODULE || '/tmp/keel-browser-check/node_modules/playwright/index.mjs').href)
const browser = await chromium.launch({ headless: true })
const contexts = [], errors = []
async function login(username, password) {
  const context = await browser.newContext({ baseURL: base.origin, viewport: { width: 1440, height: 1000 } })
  contexts.push(context)
  const page = await context.newPage()
  page.on('pageerror', error => errors.push(error.message))
  await page.goto('/login')
  await page.locator('#username').fill(username)
  await page.locator('#password').fill(password)
  await page.locator('button[type=submit]').click()
  await page.waitForURL('**/projects')
  await page.goto(`/p/${input.project_id}/assets`)
  await page.getByRole('heading', { name: /SSH 主机信任验证|SSH host trust verification/ }).waitFor()
  return { context, page }
}
try {
  const creator = await login(input.username, input.password)
  const creatorRow = creator.page.locator(`[data-ssh-trust-request="${input.request_id}"]`)
  await creatorRow.waitFor()
  await creatorRow.getByText(/待批准|Awaiting approval/, { exact: true }).waitFor()
  assert.equal(await creatorRow.getByRole('button', { name: /批准验证|Approve verification/ }).count(), 0)
  await creatorRow.locator('summary').click()
  await creatorRow.getByText(input.host_key ?? /^SHA256:/).waitFor()
  const legacy = creator.page.getByRole('button', { name: /模拟互信动作|Simulated trust actions/ })
  if (await legacy.count()) assert(await legacy.isVisible())
  const english = creator.page.getByRole('button', { name: 'Switch to English', exact: true })
  if (await english.count()) await english.click()
  await creator.page.getByRole('heading', { name: 'SSH host trust verification', exact: true }).waitFor()
  await creator.page.getByRole('button', { name: '切换为中文', exact: true }).click()
  await creator.page.getByRole('heading', { name: 'SSH 主机信任验证', exact: true }).waitFor()
  await creator.page.getByText(/验证只检查运维已安装的公钥和受限 helper/).waitFor()
  console.log('PASS real requester UI shows pending trusted binding and prevents self-approval; legacy trust is explicitly simulated')

  await creator.page.getByRole('button', { name: '新建验证请求', exact: true }).click()
  const createDialog = creator.page.getByRole('dialog')
  await createDialog.locator('#ssh-trust-machine').selectOption(input.machine_id)
  await createDialog.locator('#ssh-trust-host-key').fill(`SHA256:${'A'.repeat(43)}`)
  await createDialog.locator('#ssh-trust-reason').fill('Disposable negative check: independently supplied pin differs from the configured trusted pin')
  await createDialog.locator('#ssh-trust-minutes').fill('5')
  const rejectedPin = creator.page.waitForResponse(res => res.url().endsWith('/ssh-trust-requests') && res.request().method() === 'POST')
  await createDialog.getByRole('button', { name: '新建验证请求', exact: true }).click()
  const rejectedResponse = await rejectedPin
  assert.equal(rejectedResponse.status(), 409)
  assert.equal((await rejectedResponse.json()).code, 'SSH_TRUST_HOST_KEY_MISMATCH')
  await createDialog.getByText(/人工核验的主机密钥指纹与操作员配置不一致/).waitFor()
  creator.page.once('dialog', async prompt => { assert.equal(prompt.type(), 'confirm'); await prompt.accept() })
  await createDialog.getByRole('button', { name: '取消', exact: true }).click()
  await createDialog.waitFor({ state: 'hidden' })
  console.log('PASS actual create form rejects a mismatched trusted host pin with a translated explanation and preserves unsaved-close confirmation')

  const approver = await login(input.approver_username, input.approver_password)
  const approverRow = approver.page.locator(`[data-ssh-trust-request="${input.request_id}"]`)
  await approverRow.waitFor()
  await approverRow.getByRole('button', { name: /批准验证|Approve verification/ }).click()
  const dialog = approver.page.getByRole('dialog')
  await dialog.getByText(input.machine_id, { exact: true }).waitFor()
  await dialog.getByText(/批准人必须为另一位|A different Admin or DL must approve/).waitFor()
  const response = approver.page.waitForResponse(res => res.url().endsWith(`/ssh-trust-requests/${input.request_id}/approve`) && res.request().method() === 'POST')
  await dialog.getByRole('button', { name: /批准验证|Approve verification/ }).click()
  assert.equal((await response).status(), 200)
  await approverRow.getByText(/已批准|Approved/, { exact: true }).waitFor()
  await approverRow.getByRole('button', { name: /执行验证|Run verification/ }).waitFor()
  console.log('PASS independent approver confirms frozen target and actual API approval updates the request; execution requires a separate explicit action')

  // Explicit frontend failure injection: the actual authorization and mutation
  // checks above and below still use the disposable API without interception.
  await approverRow.getByRole('button', { name: /执行验证|Run verification/ }).click()
  const reviewDialog = approver.page.getByRole('dialog')
  await reviewDialog.getByText(input.machine_id, { exact: true }).waitFor()
  const deniedList = `**/api/v1/projects/${input.project_id}/ssh-trust-requests?*`
  await approver.page.route(deniedList, route => route.fulfill({ status: 403, contentType: 'application/json', body: JSON.stringify({ code: 'FORBIDDEN', trace_id: 'disposable-frontend-access-check' }) }))
  const deniedResponse = approver.page.waitForResponse(res => res.url().includes(`/projects/${input.project_id}/ssh-trust-requests?`) && res.status() === 403)
  // Simulate a background list refresh while the frozen-target modal is open.
  await approver.page.locator('section[aria-labelledby="ssh-trust-heading"]').getByRole('button', { name: /刷新|Refresh/, exact: true, includeHidden: true }).evaluate(button => button.click())
  await deniedResponse
  await reviewDialog.waitFor({ state: 'hidden' })
  assert.equal(await approver.page.locator('[data-ssh-trust-request]').count(), 0)
  await approver.page.unroute(deniedList)
  await approver.page.locator('section[aria-labelledby="ssh-trust-heading"]').getByRole('button', { name: /刷新|Refresh/, exact: true }).click()
  await approverRow.waitFor()
  assert.equal(await approver.page.getByRole('dialog').count(), 0)
  console.log('PASS explicitly injected list403 hides cached rows and open target details; an explicit refresh restores the actual API list without reopening the operation')

  if (input.success_request_id && input.execution_id) {
    for (const id of [input.success_request_id, input.execution_id]) assert.match(id, /^[a-f0-9]{32}$/)
    const row = approver.page.locator(`[data-ssh-trust-request="${input.success_request_id}"]`)
    await row.getByText(/验证成功|Verified/, { exact: true }).waitFor()
    const link = row.getByRole('link', { name: /查看执行|View execution/ })
    assert((await link.getAttribute('href')).includes(input.execution_id))
    await link.click()
    await approver.page.waitForURL(url => url.searchParams.get('execution') === input.execution_id)
    console.log('PASS successful trust verification retains a link to the actual execution evidence')
  }
  for (const result of [
    { request: input.failed_request_id, execution: input.failed_execution_id, state: /验证失败|Verification failed/, label: 'failed' },
    { request: input.unknown_request_id, execution: input.unknown_execution_id, state: /结果未知|Outcome unknown/, label: 'unknown' },
  ]) {
    assert.equal(Boolean(result.request), Boolean(result.execution), 'Evidence request and execution identifiers must be supplied together')
    if (!result.request) continue
    for (const id of [result.request, result.execution]) assert.match(id, /^[a-f0-9]{32}$/)
    await approver.page.goto(`/p/${input.project_id}/assets`)
    const row = approver.page.locator(`[data-ssh-trust-request="${result.request}"]`)
    await row.getByText(result.state, { exact: true }).waitFor()
    assert.equal(await row.getByRole('button', { name: /执行验证|批准验证|撤销请求|Run verification|Approve verification|Revoke request/ }).count(), 0)
    const link = row.getByRole('link', { name: /查看执行|View execution/ })
    assert((await link.getAttribute('href')).includes(result.execution))
    await link.click()
    await approver.page.waitForURL(url => url.searchParams.get('execution') === result.execution)
    await approver.page.getByText(/受限 SSH 主机信任验证|Restricted SSH host trust verification/).waitFor()
    assert.equal(await approver.page.getByRole('button', { name: /重新运行|重跑失败|Rerun failed|Rerun all/i }).count(), 0)
    console.log(`PASS actual ${result.label} trust verification preserves its execution evidence and offers no automatic rerun`)
  }
  assert.deepEqual(errors, [])
} finally {
  const cleanup = await Promise.allSettled(contexts.map(async context => {
    try {
      const response = await context.request.get('/api/v1/auth/session')
      if (response.ok()) {
        const session = await response.json()
        assert.equal((await context.request.post('/api/v1/auth/logout', { headers: { Origin: base.origin, 'X-CSRF-Token': session.csrf_token } })).status(), 204)
      }
    } finally { await context.close() }
  }))
  await browser.close()
  assert(cleanup.every(result => result.status === 'fulfilled'), 'Disposable browser sessions must log out cleanly')
}
