import assert from 'node:assert/strict'
import { lstat, mkdir, readFile } from 'node:fs/promises'
import { pathToFileURL } from 'node:url'
import path from 'node:path'

const file = process.argv[2]
assert(file, 'private fixture file required')
const info = await lstat(file)
assert(info.isFile() && !info.isSymbolicLink() && (info.mode & 0o077) === 0 && info.uid === process.geteuid())
const fixture = JSON.parse(await readFile(file, 'utf8')), base = new URL(fixture.base_url)
assert.equal(base.hostname, '127.0.0.1'); assert.equal(base.protocol, 'http:')
assert(!['3000', '3001', '5173'].includes(base.port))
const evidence = process.env.KEEL_TEST_AGENT_BROWSER_EVIDENCE_DIR || fixture.evidence_directory
await mkdir(evidence, { recursive: true, mode: 0o700 })
const { chromium } = await import(pathToFileURL(process.env.KEEL_PLAYWRIGHT_MODULE || '/tmp/keel-browser-check/node_modules/playwright/index.mjs').href)
const browser = await chromium.launch({ headless: true }), contexts = [], errors = []
async function login(username) {
  const context = await browser.newContext({ baseURL: base.origin, viewport: { width: 1440, height: 1000 } }); contexts.push(context)
  const page = await context.newPage(); page.on('pageerror', error => errors.push(error.message))
  await page.goto('/login'); await page.locator('#username').fill(username); await page.locator('#password').fill(fixture.password)
  await page.locator('button[type=submit]').click(); await page.waitForURL('**/projects')
  await page.goto(`/p/${fixture.project_id}/assets`)
  await page.getByRole('heading', { name: '设备资产', exact: true }).waitFor()
  return { context, page }
}
async function detail(page, id) {
  const response = await page.request.get(`/api/v1/projects/${fixture.project_id}/executions/${id}`)
  assert.equal(response.status(), 200)
  return response.json()
}
async function finished(page, id) {
  // Poll the actual record and inspect that same response; navigation and a
  // separate page-side poll must not stand in for a persisted terminal proof.
  const deadline = Date.now() + 15_000
  let value
  do {
    value = await detail(page, id)
    if (value.execution.state === 'success') break
    await new Promise(resolve => setTimeout(resolve, 100))
  } while (Date.now() < deadline)
  assert.equal(value.execution.state, 'success')
  assert.equal(value.execution.mode, 'agent')
  assert.equal(value.attempts.length, 1)
  assert(value.attempts[0].receipt, `Terminal execution is missing a durable receipt: ${JSON.stringify(value)}`)
  assert.equal(value.attempts[0].receipt.output.transport, 'agent')
  assert.equal(value.attempts[0].receipt.output.hardware_evidence, false)
  return value
}
try {
  const { page } = await login(fixture.username)
  if (fixture.unknown_execution_id) {
    await page.goto(`/p/${fixture.project_id}/executions?execution=${fixture.unknown_execution_id}&tab=targets&attempt=${fixture.unknown_attempt_id}`)
    const original = await detail(page, fixture.unknown_execution_id)
    assert.equal(original.attempts[0].state, 'unknown')
    assert(original.attempts[0].receipt, 'worker must have queried an actual durable terminal receipt')
    const reconciliation = page.getByRole('dialog')
    await reconciliation.locator('#execution-reconcile-evidence').fill('Reviewed original Agent command, fence and durable terminal receipt after the real stream interruption.')
    const reconciled = page.waitForResponse(response => response.url().endsWith('/reconcile') && response.request().method() === 'POST')
    await reconciliation.getByRole('button', { name: '按回执对账', exact: true }).click()
    assert.equal((await reconciled).status(), 200)
    const result = await detail(page, fixture.unknown_execution_id)
    assert.notEqual(result.attempts[0].state, 'unknown')
    assert.equal(result.attempts[0].command_id, original.attempts[0].command_id)
    assert.equal(result.attempts.length, 1)
    console.log('PASS actual lost-stream unknown remains quarantined until operator consumes the worker-queried original receipt')
  }

  await page.goto(`/p/${fixture.project_id}/assets`)
  const launch = '新建只读 Agent 执行', confirm = '创建并执行'
  await page.getByRole('button', { name: launch, exact: true }).click()
  let dialog = page.getByRole('dialog')
  assert(await dialog.getByRole('button', { name: confirm, exact: true }).isDisabled())
  await dialog.getByRole('group', { name: '只读 Agent 目标', exact: true }).getByRole('button').click()
  let lostID, key, body, calls = 0
  const pattern = `**/api/v1/projects/${fixture.project_id}/agent-executions`
  await page.route(pattern, async route => {
    if (route.request().method() !== 'POST') return route.continue()
    calls++
    if (calls === 1) {
      key = route.request().headers()['idempotency-key']; body = route.request().postData()
      const result = await route.fetch(); assert.equal(result.status(), 201); lostID = (await result.json()).id
      await route.abort('failed')
    } else {
      assert.equal(route.request().headers()['idempotency-key'], key)
      assert.equal(route.request().postData(), body)
      await route.continue()
    }
  })
  await dialog.getByRole('button', { name: confirm, exact: true }).click()
  await dialog.getByRole('alert').waitFor(); assert(lostID)
  const replay = page.waitForResponse(response => response.url().endsWith('/agent-executions') && response.request().method() === 'POST')
  await dialog.getByRole('button', { name: confirm, exact: true }).click()
  assert.equal((await (await replay).json()).id, lostID)
  await page.waitForURL(url => url.searchParams.get('execution') === lostID)
  await finished(page, lostID); await page.unroute(pattern)
  for (const [tab, label] of [['targets', '目标'], ['logs', '日志'], ['outputs', '输出'], ['relations', '关联'], ['overview', '概览']]) {
    await page.getByRole('tab', { name: label, exact: true }).click()
    await page.waitForURL(url => url.searchParams.get('tab') === tab)
  }
  await page.screenshot({ path: path.join(evidence, 'agent-execution.png'), fullPage: true })
  console.log('PASS real authenticated Agent identity execution, ambiguous POST idempotent retry, durable transport proof and five URL tabs')


  await page.goto(`/p/${fixture.project_id}/assets`)
  await page.getByRole('button', { name: launch, exact: true }).click(); dialog = page.getByRole('dialog')
  await dialog.getByRole('group', { name: '只读 Agent 目标', exact: true }).getByRole('button').click()
  // Explicit UI error injection; actual RBAC denial is verified by PG/HTTP tests.
  await page.route(pattern, route => route.fulfill({ status: 403, contentType: 'application/json', body: JSON.stringify({ code: 'FORBIDDEN', message: 'fixture UI denial' }) }))
  await dialog.getByRole('button', { name: confirm, exact: true }).click(); await dialog.waitFor({ state: 'hidden' })
  assert(await page.getByRole('button', { name: launch, exact: true }).isDisabled())
  await page.unroute(pattern); await page.reload()
  await page.getByRole('button', { name: 'Switch to English', exact: true }).click(); await page.setViewportSize({ width: 390, height: 844 })
  await page.getByRole('button', { name: 'New read-only Agent execution', exact: true }).click(); dialog = page.getByRole('dialog')
  await dialog.getByRole('group', { name: 'Read-only Agent targets', exact: true }).getByRole('button').click()
  await dialog.locator('#agent-launch-operation').selectOption('host.wait'); await dialog.locator('#agent-launch-wait').fill('50')
  const bounds = await dialog.evaluate(element => { const box = element.getBoundingClientRect(); return { left: box.left, right: box.right, viewport: innerWidth, scroll: element.scrollWidth, width: element.clientWidth } })
  assert(bounds.left >= 0 && bounds.right <= bounds.viewport + 1 && bounds.scroll <= bounds.width + 1, JSON.stringify(bounds))
  await page.screenshot({ path: path.join(evidence, 'agent-mobile.png'), fullPage: true })
  const waited = page.waitForResponse(response => response.url().endsWith('/agent-executions') && response.request().method() === 'POST')
  await dialog.getByRole('button', { name: 'Create and execute', exact: true }).click()
  const waitID = (await (await waited).json()).id
  await page.waitForURL(url => url.searchParams.get('execution') === waitID); await finished(page, waitID)
  if (fixture.viewer_username) {
    const viewer = await login(fixture.viewer_username)
    await viewer.page.locator(`[data-agent-target="${fixture.machine_id}"]`).waitFor()
    assert.equal(await viewer.page.getByRole('button', { name: launch, exact: true }).count(), 0)
  }
  assert.deepEqual(errors, [])
  console.log('PASS UI403 closes stale form, English mobile actual Agent wait, read-only Viewer and no browser JavaScript errors')
} finally {
  for (const context of contexts) {
    try {
      const response = await context.request.get('/api/v1/auth/session')
      if (response.ok()) await context.request.post('/api/v1/auth/logout', { headers: { Origin: base.origin, 'X-CSRF-Token': (await response.json()).csrf_token } })
    } catch { /* isolated fixture teardown follows */ }
  }
  await browser.close()
}
