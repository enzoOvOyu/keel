/** Browser acceptance for identity/project control against an isolated test DB. */
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
const contexts = [], errors = [], credentialRequests = [], passed = []
const suffix = randomBytes(5).toString('hex'), username = `ui-control-${suffix}`
const initialPassword = randomBytes(24).toString('base64url'), resetPassword = randomBytes(24).toString('base64url'), finalPassword = randomBytes(24).toString('base64url')
const secrets = [credentials.password, initialPassword, resetPassword, finalPassword]
function pass(name) { passed.push(name); console.log(`PASS ${name}`) }
async function createPage() {
  const context = await browser.newContext({ baseURL: credentials.base_url, viewport: { width: 1440, height: 960 }, locale: 'zh-CN', reducedMotion: 'reduce' })
  contexts.push(context)
  const page = await context.newPage(); page.setDefaultTimeout(15_000)
  page.on('pageerror', (error) => errors.push(`Runtime: ${error.message}`))
  page.on('console', (message) => {
    if (message.type() !== 'error') return
    const url = message.location().url ?? '', text = message.text()
    const expected = (/401/.test(text) && /\/auth\/(session|login)/.test(url))
      || (/403/.test(text) && url.includes('/auth/password'))
      || (/409/.test(text) && /\/admin\/users\//.test(url))
      || (/404/.test(text) && /\/projects\//.test(url))
    if (!expected) errors.push(`Console: ${text}`)
  })
  page.on('request', (request) => {
    if (secrets.some((secret) => request.postData()?.includes(secret))) credentialRequests.push({ url: request.url(), method: request.method() })
  })
  return { context, page }
}
async function signIn(page, account, password, target = '/projects') {
  await page.goto('/login', { waitUntil: 'networkidle' })
  await page.locator('#username').fill(account); await page.locator('#password').fill(password)
  await page.locator('#login-form button[type=submit]').click()
  await page.waitForURL((url) => url.pathname === target)
}
async function noOverflow(page) { assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1), 'Unexpected page overflow') }
async function refreshSession(page) {
  const response = page.waitForResponse((r) => new URL(r.url()).pathname === '/api/v1/auth/session')
  await page.evaluate(() => {
    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'hidden' }); window.dispatchEvent(new Event('visibilitychange'))
    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' }); window.dispatchEvent(new Event('visibilitychange'))
  })
  return response
}
async function session(context) { const result = await context.request.get('/api/v1/auth/session'); assert.equal(result.status(), 200); return result.json() }
async function mutate(context, method, path, data) {
  const auth = await session(context)
  return context.request.fetch(path, { method, data, headers: { Origin: credentials.base_url, 'X-CSRF-Token': auth.csrf_token, 'Idempotency-Key': randomUUID() } })
}
async function tableUser(page) {
  await page.locator('#user-search').fill(username); await page.getByRole('button', { name: '搜索', exact: true }).click()
  const row = page.getByRole('row').filter({ hasText: username }); await row.waitFor(); return row
}
async function confirm(page, method, path, status = 200) {
  const pending = page.waitForResponse((response) => new URL(response.url()).pathname === path && response.request().method() === method)
  await page.getByRole('dialog').getByRole('button', { name: '确认变更', exact: true }).click()
  const response = await pending; assert.equal(response.status(), status)
  return response
}

try {
  const { context, page } = await createPage()
  await signIn(page, credentials.username, credentials.password)
  await page.getByRole('link', { name: '账号管理', exact: true }).click()
  await page.getByRole('button', { name: '创建账号', exact: true }).click()
  await page.locator('#new-username').fill(username); await page.locator('#display-name').fill(`Browser control ${suffix}`)
  assert.equal((await refreshSession(page)).status(), 200)
  assert((await page.locator('#new-username').inputValue()) === username, 'A session recheck must preserve the draft')
  await page.locator('#initial-password').fill(initialPassword)
  assert(!(await page.getByLabel('平台管理员', { exact: true }).isChecked()), 'New accounts must default to standard access')
  const creation = page.waitForResponse((r) => new URL(r.url()).pathname === '/api/v1/admin/users' && r.request().method() === 'POST')
  await page.getByRole('dialog').getByRole('button', { name: '创建账号', exact: true }).click()
  const createdResponse = await creation; assert.equal(createdResponse.status(), 201); const user = await createdResponse.json()
  await page.getByText('变更已保存并记录审计。', { exact: true }).waitFor()
  let row = await tableUser(page)
  await noOverflow(page); await page.screenshot({ path: '/tmp/keel-control-users.png', fullPage: true })
  pass('Real account creation, standard default, and background session recheck preserves draft')

  await row.getByRole('button', { name: '禁用账号', exact: true }).click()
  assert((await page.getByRole('dialog').innerText()).includes(username), 'Confirmation must name the frozen target')
  await confirm(page, 'PATCH', `/api/v1/admin/users/${user.id}`)
  await row.getByText('已禁用', { exact: true }).waitFor()
  const target = await createPage()
  await target.page.goto('/login'); await target.page.locator('#username').fill(username); await target.page.locator('#password').fill(initialPassword)
  await target.page.locator('#login-form button[type=submit]').click(); await target.page.getByText('账号或密码不正确', { exact: true }).waitFor()
  assert((await target.page.locator('#password').inputValue()) === '', 'Failed login must clear the password')
  await row.getByRole('button', { name: '启用账号', exact: true }).click(); await confirm(page, 'PATCH', `/api/v1/admin/users/${user.id}`)
  await row.getByText('已启用', { exact: true }).waitFor()
  await row.getByRole('button', { name: '重置密码', exact: true }).click(); await page.locator('#reset-password').fill(resetPassword)
  await confirm(page, 'POST', `/api/v1/admin/users/${user.id}/password`)
  await page.getByRole('dialog').waitFor({ state: 'hidden' })
  await row.getByRole('button', { name: '解除锁定', exact: true }).click()
  assert(!(await page.getByLabel('同时解除指定来源IP的限流', { exact: true }).isChecked()), 'IP unlock must be opt-in')
  await confirm(page, 'POST', `/api/v1/admin/users/${user.id}/unlock`)
  await page.getByRole('dialog').waitFor({ state: 'hidden' })
  pass('Disable blocks real login; enable, password reset and account-only unlock succeed')

  await row.getByRole('button', { name: '禁用账号', exact: true }).click()
  const current = (await (await context.request.get(`/api/v1/admin/users?q=${username}`)).json()).items[0]
  assert.equal((await mutate(context, 'POST', `/api/v1/admin/users/${user.id}/unlock`, { version: current.version })).status(), 200)
  await confirm(page, 'PATCH', `/api/v1/admin/users/${user.id}`, 409)
  await page.getByText('记录已发生变化，当前修改未提交。', { exact: true }).waitFor()
  await page.getByRole('dialog').getByRole('button', { name: '取消', exact: true }).click()
  const stillEnabled = (await (await context.request.get(`/api/v1/admin/users?q=${username}`)).json()).items[0]
  assert(stillEnabled.enabled, 'Stale confirmation must not disable the account')
  pass('Stale record confirmation returns 409 and does not apply the intended mutation')

  // This is fixture setup only; the project-creation UI has its own acceptance script.
  const projectResponse = await mutate(context, 'POST', '/api/v1/projects', { name: `Control browser ${suffix}`, slug: `control-ui-${suffix}`, description: 'Isolated control-plane browser acceptance' })
  assert.equal(projectResponse.status(), 201); const project = await projectResponse.json()
  await page.goto(`/p/${project.id}/members`)
  await page.getByRole('button', { name: '添加成员', exact: true }).click(); await page.locator('#member-username').fill(username)
  await page.locator('#member-role').selectOption('Viewer')
  await confirm(page, 'POST', `/api/v1/projects/${project.id}/members`, 201)
  let memberRow = page.getByRole('row').filter({ hasText: username }); await memberRow.waitFor()
  await memberRow.getByRole('button', { name: '修改角色', exact: true }).click(); await page.locator('#member-role').selectOption('DE')
  await confirm(page, 'PATCH', `/api/v1/projects/${project.id}/members/${user.id}`)
  await memberRow.getByText('DE', { exact: true }).waitFor()
  await page.screenshot({ path: '/tmp/keel-control-members.png', fullPage: true })
  pass('Project membership creation and versioned role change use the real scoped API')

  await page.getByRole('link', { name: '审计日志', exact: true }).click()
  await page.locator('#audit-action').fill('membership.grant'); await page.getByRole('button', { name: '应用筛选', exact: true }).click()
  await page.getByText('membership.grant', { exact: true }).waitFor()
  const rows = await page.locator('tbody tr').count(); assert(rows >= 1)
  assert(await page.locator('tbody tr').evaluateAll((items) => items.every((item) => item.textContent.includes('membership.grant'))))
  await page.setViewportSize({ width: 390, height: 844 }); await noOverflow(page)
  await page.getByRole('button', { name: '切换到深色主题', exact: true }).click(); await noOverflow(page)
  await page.screenshot({ path: '/tmp/keel-control-audit-mobile.png', fullPage: true })
  await page.getByRole('button', { name: 'Switch to English', exact: true }).click(); await noOverflow(page)
  await page.getByRole('heading', { name: 'Audit log', exact: true }).waitFor()
  await page.getByRole('button', { name: '切换为中文', exact: true }).click()
  pass('Project audit exact filtering, English/Chinese and dark 390px layout')

  await signIn(target.page, username, resetPassword)
  await target.page.goto('/admin/users'); await target.page.getByRole('heading', { name: '你没有此操作的权限', exact: true }).waitFor()
  assert(!(await target.page.locator('body').innerText()).includes(credentials.username), 'A standard account cannot see the account directory')
  await target.page.goto(`/p/${project.id}/members`); await target.page.getByRole('heading', { name: '你没有此操作的权限', exact: true }).waitFor()
  pass('Standard account is denied platform accounts and project-admin membership pages')

  await target.page.goto('/account/security'); await target.page.getByRole('button', { name: '修改密码', exact: true }).click()
  await target.page.locator('#current-password').fill('intentionally-incorrect-password'); await target.page.locator('#new-password').fill(finalPassword); await target.page.locator('#confirm-password').fill(finalPassword)
  await confirm(target.page, 'POST', '/api/v1/auth/password', 403)
  await target.page.getByText('当前密码不正确。', { exact: true }).waitFor()
  assert((await target.page.locator('#current-password').inputValue()) === '' && (await target.page.locator('#new-password').inputValue()) === '', 'Password error must clear sensitive fields')
  await target.page.locator('#current-password').fill(resetPassword); await target.page.locator('#new-password').fill(finalPassword); await target.page.locator('#confirm-password').fill(finalPassword)
  await confirm(target.page, 'POST', '/api/v1/auth/password', 204)
  await target.page.waitForURL('**/login?**'); await target.page.getByText('密码已修改。请使用新密码重新登录。', { exact: true }).waitFor()
  assert(!(await target.context.cookies()).some((cookie) => cookie.name === 'keel_session'), 'Password change must revoke the cookie')
  await signIn(target.page, username, finalPassword)
  pass('Own password rejects wrong current password, clears fields, revokes sessions and accepts the new password')

  await page.goto(`/p/${project.id}/members`); memberRow = page.getByRole('row').filter({ hasText: username })
  await memberRow.getByRole('button', { name: '撤销成员', exact: true }).click(); await confirm(page, 'DELETE', `/api/v1/projects/${project.id}/members/${user.id}`, 204)
  assert.equal((await refreshSession(target.page)).status(), 401)
  await target.page.waitForURL('**/login?**')
  assert(!(await target.page.locator('body').innerText()).includes(project.name), 'Revoked sessions cannot retain private project content')
  pass('Revoking membership expires the affected live browser session and removes private content')

  await page.goto('/admin/audits'); await page.getByRole('heading', { name: '平台审计', exact: true }).waitFor()
  await noOverflow(page)
  const next = page.getByRole('button', { name: '下一页', exact: true })
  assert(await next.isEnabled(), 'Real audit activity should span more than one page')
  await next.click(); await page.waitForURL((url) => url.searchParams.get('page') === '2')
  await page.locator('tbody tr').first().waitFor(); await noOverflow(page)
  await page.getByRole('button', { name: '上一页', exact: true }).click(); await page.waitForURL((url) => url.searchParams.get('page') === '1')
  pass('Platform audit pagination uses real backend pages')

  for (const browserPage of [page, target.page]) {
    const storage = await browserPage.evaluate(() => JSON.stringify({ ...localStorage, ...sessionStorage }))
    assert(!secrets.some((secret) => storage.includes(secret)), 'Passwords must not be persisted in browser storage')
  }
  assert(credentialRequests.every(({ method, url }) => method === 'POST' && new URL(url).origin === credentials.base_url
    && /^\/api\/v1\/(auth\/(login|password)|admin\/users(?:\/[a-f0-9]{32}\/password)?)$/.test(new URL(url).pathname)), 'Unexpected credential destination')
  assert.deepEqual(errors, [], 'Unexpected browser errors')
  pass('No secret persistence, unexpected credential destination, or browser runtime error')
  console.log(JSON.stringify({ status: 'passed', groups: passed.length, scope: 'isolated PostgreSQL + real API + Chromium' }))
} finally {
  for (const context of contexts) await context.close()
  await browser.close()
}
