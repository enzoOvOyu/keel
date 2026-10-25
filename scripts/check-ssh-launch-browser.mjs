import assert from 'node:assert/strict'
import { lstat, readFile } from 'node:fs/promises'
import { pathToFileURL } from 'node:url'
const file = process.env.KEEL_SSH_LAUNCH_BROWSER_CREDENTIALS_FILE
assert(file)
const info = await lstat(file)
assert(info.isFile() && !info.isSymbolicLink() && (info.mode & 0o077) === 0 && info.uid === process.geteuid())
const fixture = JSON.parse(await readFile(file, 'utf8')), base = new URL(fixture.base_url)
assert.equal(base.hostname, '127.0.0.1'); assert.equal(base.protocol, 'http:'); assert(!['3000', '3001', '5173'].includes(base.port))
const { chromium } = await import(pathToFileURL(process.env.KEEL_PLAYWRIGHT_MODULE || '/tmp/keel-browser-check/node_modules/playwright/index.mjs').href)
const browser = await chromium.launch({ headless: true }), contexts = [], errors = []
async function login(username) {
  const context = await browser.newContext({ baseURL: base.origin, viewport: { width: 1440, height: 1000 } }); contexts.push(context)
  const page = await context.newPage(); page.on('pageerror', error => errors.push(error.message))
  await page.goto('/login'); await page.locator('#username').fill(username); await page.locator('#password').fill(fixture.password)
  await page.locator('button[type=submit]').click(); await page.waitForURL('**/projects'); await page.goto(`/p/${fixture.project_id}/assets`)
  await page.getByRole('heading', { name: '设备资产', exact: true }).waitFor()
  return { context, page }
}
try {
  const { context, page } = await login(fixture.username)
  await page.getByRole('button', { name: '新建只读 SSH 执行', exact: true }).click()
  let dialog = page.getByRole('dialog'); await dialog.getByRole('button', { name: '创建并执行', exact: true }).waitFor()
  assert(await dialog.getByRole('button', { name: '创建并执行', exact: true }).isDisabled())
  await dialog.getByRole('group', { name: '只读 SSH 目标', exact: true }).getByRole('button').click()
  await dialog.getByText(/服务端在提交和派发时复验当前权限/).waitFor()
  let lostID, key, body, calls = 0
  const routePattern = `**/api/v1/projects/${fixture.project_id}/ssh-executions`
  await page.route(routePattern, async route => {
    if (route.request().method() !== 'POST') return route.continue()
    calls++
    if (calls === 1) {
      key = route.request().headers()['idempotency-key']; body = route.request().postData()
      const result = await route.fetch(); assert.equal(result.status(), 201); lostID = (await result.json()).id
      await route.abort('failed')
    } else {
      assert.equal(route.request().headers()['idempotency-key'], key); assert.equal(route.request().postData(), body)
      await route.continue()
    }
  })
  await dialog.getByRole('button', { name: '创建并执行', exact: true }).click(); await dialog.getByRole('alert').waitFor()
  assert(lostID)
  const response = page.waitForResponse(result => result.url().endsWith('/ssh-executions') && result.request().method() === 'POST')
  await dialog.getByRole('button', { name: '创建并执行', exact: true }).click(); assert.equal((await (await response).json()).id, lostID)
  await page.waitForURL(url => url.pathname.endsWith('/executions') && url.searchParams.get('execution') === lostID)
  await page.getByText(/受限 SSH 只读任务|Bounded read-only SSH/).waitFor()
  await page.unroute(routePattern)
  await page.waitForFunction(async () => {
    const url = new URL(location.href), id = url.searchParams.get('execution')
    const response = await fetch(`/api/v1/projects/${url.pathname.split('/')[2]}/executions/${id}`, { credentials: 'same-origin' })
    return response.ok && (await response.json()).execution.state === 'success'
  })
  console.log('PASS project DE starts real domain-key SSH identity; ambiguous POST response retries same idempotency key and original execution')
  await page.goto(`/p/${fixture.project_id}/assets`)
  await page.getByRole('button', { name: '新建只读 SSH 执行', exact: true }).click(); dialog = page.getByRole('dialog')
  await dialog.getByRole('group', { name: '只读 SSH 目标', exact: true }).getByRole('button').click()
  // Explicit UI failure injection. Actual role enforcement is covered by PG/HTTP tests.
  await page.route(routePattern, route => route.fulfill({ status: 403, contentType: 'application/json', body: JSON.stringify({ code: 'FORBIDDEN', message: 'fixture UI denial' }) }))
  await dialog.getByRole('button', { name: '创建并执行', exact: true }).click(); await dialog.waitFor({ state: 'hidden' })
  assert(await page.getByRole('button', { name: '新建只读 SSH 执行', exact: true }).isDisabled())
  await page.unroute(routePattern); await page.reload()
  await page.getByRole('button', { name: 'Switch to English', exact: true }).click(); await page.setViewportSize({ width: 390, height: 844 })
  await page.getByRole('button', { name: 'New read-only SSH execution', exact: true }).click(); dialog = page.getByRole('dialog')
  await dialog.getByRole('group', { name: 'Read-only SSH targets', exact: true }).getByRole('button').click()
  await dialog.locator('#ssh-launch-operation').selectOption('host.wait'); await dialog.locator('#ssh-launch-wait').fill('50')
  // The project workspace retains its designed desktop minimum width. Verify
  // the viewport-bound dialog independently of that horizontal workspace.
  const bounds = await dialog.evaluate(element => {
    const box = element.getBoundingClientRect()
    return { left: box.left, right: box.right, viewport: innerWidth, scroll: element.scrollWidth, width: element.clientWidth }
  })
  assert(bounds.left >= 0 && bounds.right <= bounds.viewport + 1 && bounds.scroll <= bounds.width + 1, JSON.stringify(bounds))
  const waited = page.waitForResponse(result => result.url().endsWith('/ssh-executions') && result.request().method() === 'POST')
  await dialog.getByRole('button', { name: 'Create and execute', exact: true }).click(); const waitID = (await (await waited).json()).id
  await page.waitForURL(url => url.searchParams.get('execution') === waitID)
  await page.waitForFunction(async () => {
    const url = new URL(location.href), id = url.searchParams.get('execution')
    const response = await fetch(`/api/v1/projects/${url.pathname.split('/')[2]}/executions/${id}`, { credentials: 'same-origin' })
    return response.ok && (await response.json()).execution.state === 'success'
  })
  console.log('PASS explicit UI403 closes stale form; real refresh restores; English mobile bounded wait uses actual SSH execution')
  const viewer = await login(fixture.viewer_username)
  assert.equal(await viewer.page.getByRole('button', { name: '新建只读 SSH 执行', exact: true }).count(), 0)
  assert.deepEqual(errors, [])
  console.log('PASS Viewer has no launch action; no browser JavaScript errors')
} finally {
  for (const context of contexts) {
    try { const response = await context.request.get('/api/v1/auth/session'); if (response.ok()) await context.request.post('/api/v1/auth/logout', { headers: { Origin: base.origin, 'X-CSRF-Token': (await response.json()).csrf_token } }) } catch { /* isolated API cleanup follows */ }
  }
  await browser.close()
}
