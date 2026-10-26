/** Deferred P1 milestone check: real isolated API/MinIO, no PXE or hardware. */
import assert from 'node:assert/strict'
import { execFile as execFileCallback } from 'node:child_process'
import { lstat, readFile } from 'node:fs/promises'
import { pathToFileURL } from 'node:url'
import { promisify } from 'node:util'
const execFile = promisify(execFileCallback)
const credentialFile = process.env.KEEL_AUTH_BROWSER_CREDENTIALS_FILE
assert(credentialFile?.startsWith('/tmp/keel-media-'))
const info = await lstat(credentialFile)
assert(info.isFile() && !info.isSymbolicLink() && (info.mode & 0o077) === 0 && info.uid === process.geteuid())
const credentials = JSON.parse(await readFile(credentialFile, 'utf8'))
assert.equal(credentials.base_url, 'http://127.0.0.1:3102')
const dsn = new URL(process.env.KEEL_DATABASE_URL)
assert.equal(dsn.hostname, '127.0.0.1'); assert.equal(dsn.port, '15433'); assert.match(dsn.pathname, /^\/keel_test_media_[a-f0-9]+$/)
const work = credentialFile.slice(0, credentialFile.lastIndexOf('/'))
assert.equal(process.env.KEEL_PROVISION_TEST_MASTER_FILE, work + '/provision-master')
assert.equal(process.env.KEEL_ADMIN_BINARY, work + '/keel-admin')
const { chromium } = await import(pathToFileURL(process.env.KEEL_PLAYWRIGHT_MODULE || '/tmp/keel-browser-check/node_modules/playwright/index.mjs').href)
const browser = await chromium.launch({ headless: true })
const admin = await browser.newContext({ baseURL: credentials.base_url, viewport: { width: 1440, height: 1080 } })
const page = await admin.newPage(), errors = []
page.setDefaultTimeout(15_000); page.on('pageerror', error => errors.push(error.message))
async function login(context, username, password) {
  const before = await (await context.request.get('/api/v1/auth/csrf')).json()
  const response = await context.request.post('/api/v1/auth/login', { headers: { Origin: credentials.base_url, 'X-CSRF-Token': before.csrf_token }, data: { method: 'local', username, password } })
  assert.equal(response.status(), 200)
  return (await response.json()).csrf_token
}
try {
  const csrf = await login(admin, credentials.username, credentials.password)
  async function call(method, path, data, expected = 200, context = admin, token = csrf) {
    const response = await context.request.fetch('/api/v1' + path, { method, headers: { Origin: credentials.base_url, 'X-CSRF-Token': token, 'Idempotency-Key': crypto.randomUUID() }, ...(data === undefined ? {} : { data }) })
    const result = await response.json(); assert.equal(response.status(), expected, `${method} ${path}: ${JSON.stringify(result)}`)
    return result
  }
  async function owner(command, args) {
    const env = Object.fromEntries(Object.entries(process.env).filter(([key]) => !key.startsWith('KEEL_')))
    env.KEEL_DATABASE_URL = process.env.KEEL_DATABASE_URL
    env.KEEL_CREDENTIAL_MASTER_KEY_FILE = process.env.KEEL_PROVISION_TEST_MASTER_FILE
    const { stdout } = await execFile(process.env.KEEL_ADMIN_BINARY, [command, ...args], { env, timeout: 30_000 })
    return JSON.parse(stdout)
  }
  const suffix = crypto.randomUUID().slice(0, 8)
  const project = await call('POST', '/projects', { name: 'Provision preparation ' + suffix, slug: 'provision-' + suffix }, 201)
  const projectBase = `/projects/${project.id}`
  const reviewUser = 'provision-review-' + suffix, password = crypto.randomUUID() + '-Password!'
  await call('POST', '/admin/users', { username: reviewUser, display_name: 'Independent reviewer', password, is_admin: true }, 201)
  const reviewer = await browser.newContext({ baseURL: credentials.base_url }), reviewToken = await login(reviewer, reviewUser, password)
  const roots = []
  for (const scope of ['project', 'global']) {
    const base = scope === 'global' ? '/admin/media' : projectBase + '/media'
    const route = scope === 'global' ? '/admin/media' : `/p/${project.id}/media`
    async function upload(label, type, arch, payload) {
      const response = await admin.request.post('/api/v1' + base + '/upload', { headers: { Origin: credentials.base_url, 'X-CSRF-Token': csrf }, multipart: { metadata: JSON.stringify({ name: `provision-${scope}-${label}-${suffix}`, type, arch, version: 'v1' }), file: { name: label + '.bin', mimeType: 'application/octet-stream', buffer: Buffer.from(payload) } } })
      const result = await response.json(); assert.equal(response.status(), 201, JSON.stringify(result))
      for (const state of ['verified', 'approved']) await call('POST', base + '/' + result.id + '/transition', { state }, 200, reviewer, reviewToken)
      return result
    }
    const kernel = await upload('kernel', 'os', 'amd64', 'fixture kernel')
    const initrd = await upload('initrd', 'os', 'amd64', 'fixture initrd')
    const template = await upload('template', 'config', 'noarch', '#cloud-config\nlocale: en_US.UTF-8\npackage_update: false\n')
    const incompatible = await upload('otherarch', 'os', 'arm64', 'arm fixture')
    await page.goto(route, { waitUntil: 'networkidle' })
    await page.getByRole('button', { name: '上传新介质', exact: true }).click()
    await page.locator('#media-type').selectOption('os')
    await page.locator('#media-name').fill(`provision-${scope}-rootfs-${suffix}`)
    await page.locator('#media-version').fill('v1'); await page.locator('#media-arch').selectOption('amd64')
    const picker = page.locator('#media-dependency')
    for (const dependency of [kernel, initrd, template]) {
      await picker.locator(`option[value="${dependency.id}"]`).waitFor({ state: 'attached' })
      await picker.selectOption(dependency.id)
    }
    assert.equal(await picker.locator(`option[value="${incompatible.id}"]`).count(), 0)
    await page.locator('#media-boot-profile').selectOption('cloud-init')
    await page.locator('#media-boot-kernel_media_id').selectOption(kernel.id)
    assert.equal(await page.locator(`#media-boot-initrd_media_id option[value="${kernel.id}"]`).isDisabled(), true)
    assert.equal(await page.locator(`#media-boot-kernel_media_id option[value="${template.id}"]`).count(), 0)
    await page.locator('#media-boot-initrd_media_id').selectOption(initrd.id)
    await page.locator('#media-boot-template_media_id').selectOption(template.id)
    await page.locator('#media-file').setInputFiles({ name: 'rootfs.img', mimeType: 'application/octet-stream', buffer: Buffer.from('fixture rootfs, not bootable hardware evidence') })
    const uploaded = page.waitForResponse(response => response.url().endsWith(base + '/upload') && response.request().method() === 'POST')
    await page.locator('.action-dialog button[type=submit]').click()
    const uploadResponse = await uploaded; assert.equal(uploadResponse.status(), 201)
    const root = await uploadResponse.json()
    assert.deepEqual(root.deps.os_boot, { schema: 'keel.os-boot/v1', installer: 'cloud-init', kernel_media_id: kernel.id, initrd_media_id: initrd.id, template_media_id: template.id })
    assert.deepEqual(root.deps.artifacts.map(item => item.media_id).sort(), [kernel.id, initrd.id, template.id].sort())
    for (const state of ['verified', 'approved']) await call('POST', base + '/' + root.id + '/transition', { state }, 200, reviewer, reviewToken)
    await page.reload({ waitUntil: 'networkidle' })
    const row = page.getByRole('row').filter({ hasText: root.name })
    await row.getByRole('button', { name: '核对 OS 引导文件', exact: true }).click()
    const dialog = page.locator('.action-dialog')
    for (const item of [root, kernel, initrd, template]) await dialog.getByText(item.name + ' · v1 · ' + item.arch, { exact: true }).waitFor()
    // Trigger a real refresh with an injected permission failure; stale role metadata must disappear.
    const endpoint = '**/api/v1' + base + '/' + root.id + '/boot-profile'
    await page.route(endpoint, handle => handle.fulfill({ status: 403, contentType: 'application/json', body: JSON.stringify({ code: 'FORBIDDEN', message: 'Permission denied' }) }))
    await dialog.locator('button[type=submit]').click()
    await row.getByRole('button', { name: '核对 OS 引导文件', exact: true }).click()
    await dialog.getByRole('alert').waitFor()
    assert.equal(await dialog.getByText(kernel.name + ' · v1 · amd64', { exact: true }).count(), 0)
    await page.unroute(endpoint); await dialog.locator('button[type=submit]').click()
    roots.push({ root, kernel, initrd, template, incompatible, base })
  }
  const model = await call('POST', projectBase + '/models', { name: 'Provision-model-' + suffix, kind: 'server' }, 201)
  await call('POST', projectBase + '/models/' + model.id + '/versions', { hw_spec: { cpu_arch: 'amd64' }, slots: [{ slot_no: 1, pcie_slot: 'PCIe-1', expect_nic_model: 'Fixture-NIC', panel_port: 'p1', default_role: 'management' }] }, 201)
  await call('POST', projectBase + '/bom/commit', { file_version: 'provision-' + suffix, machines: [{ serial_number: 'PREVIEW-' + suffix, hostname: 'preview-' + suffix, model_name: model.name, ports: [{ mac_address: 'aa:bb:cc:dd:ee:82', port_name: 'p1', rail: 0 }] }] }, 201)
  const assetsBefore = await call('GET', projectBase + '/assets')
  const asset = assetsBefore.items.find(item => item.serial_number === 'PREVIEW-' + suffix); assert(asset)
  const executionsBefore = (await call('GET', projectBase + '/executions')).total
  const key = await owner('ssh-key-create', ['--project', project.id, '--ref', 'preview-' + suffix, '--version', 'v1', '--expires-at', new Date(Date.now() + 7_200_000).toISOString()])
  await page.goto(`/p/${project.id}/assets`, { waitUntil: 'networkidle' })
  await page.getByRole('row').filter({ hasText: asset.hostname }).getByRole('button', { name: '装机配置预览', exact: true }).click()
  const dialog = page.locator('.action-dialog')
  const port = page.locator('#provision-boot-port'); await port.locator('option').nth(1).waitFor({ state: 'attached' })
  await port.selectOption({ index: 1 })
  const image = page.locator('#provision-image')
  await image.locator(`option[value="${roots[0].root.id}"]`).waitFor({ state: 'attached' })
  assert.equal(await image.locator(`option[value="${roots[0].kernel.id}"]`).count(), 0)
  await image.selectOption(roots[0].root.id)
  await page.locator('#provision-domain-key').selectOption(key.id)
  const previewResponse = page.waitForResponse(response => response.url().endsWith('/provision/preview') && response.request().method() === 'POST')
  await dialog.locator('button[type=submit]').click()
  const response = await previewResponse; assert.equal(response.status(), 200)
  const preview = await response.json()
  assert.equal(preview.target.asset_id, asset.id); assert.equal(preview.domain_key.id, key.id)
  assert(preview.seed.user_data.includes(key.public_key.trim())); assert(preview.seed.user_data.includes('restrict,command='))
  assert(!JSON.stringify(preview).match(/PRIVATE KEY|ciphertext|callback_token/))
  await dialog.getByText('本次配置摘要: ' + preview.snapshot_sha256, { exact: true }).waitFor()
  // Changing the selected image clears the previous result rather than relabeling it.
  await image.selectOption(roots[1].root.id)
  assert.equal(await dialog.getByText('本次配置摘要: ' + preview.snapshot_sha256, { exact: true }).count(), 0)
  const nextResponse = page.waitForResponse(result => result.url().endsWith('/provision/preview') && result.request().method() === 'POST')
  await dialog.locator('button[type=submit]').click(); const next = await (await nextResponse).json()
  assert.notEqual(next.snapshot_sha256, preview.snapshot_sha256)
  // Persist consent from the actual BOM preview dialog, then independently
  // approve and execute source verification. No bootable image/hardware is used.
  await dialog.locator('#provision-request-reason').fill('Isolated software preparation only')
  const creation = page.waitForResponse(result => result.url().endsWith('/provision/requests') && result.request().method() === 'POST')
  await dialog.getByRole('button', { name: '提交装机准备申请', exact: true }).click()
  const createdResponse = await creation; assert.equal(createdResponse.status(), 201)
  const created = await createdResponse.json()
  assert(created.snapshot.seed.meta_data.includes(created.id))
  assert.notEqual(created.snapshot_sha256, next.snapshot_sha256)
  const requestBase = projectBase + '/provision/requests/' + created.id
  await call('POST', requestBase + '/approve', { expected_version: 1, reason: 'Self approval must fail' }, 409)
  const reviewPage = await reviewer.newPage()
  await reviewPage.goto(`/p/${project.id}/deploy/provision`, { waitUntil: 'networkidle' })
  const reviewRow = reviewPage.locator(`[data-provision-request="${created.id}"]`)
  await reviewRow.getByRole('button', { name: '审批准备申请', exact: true }).click()
  const reviewDialog = reviewPage.locator('.action-dialog')
  await reviewDialog.locator('#provision-action-reason').fill('Independent fixture media review')
  const approvalResponse = reviewPage.waitForResponse(result => result.url().endsWith('/' + created.id + '/approve'))
  await reviewDialog.locator('button[type=submit]').click()
  assert.equal((await approvalResponse).status(), 200)
  const boardPage = await admin.newPage()
  await boardPage.goto(`/p/${project.id}/deploy/provision`, { waitUntil: 'networkidle' })
  const boardPanel = boardPage.locator('section.management-page').filter({ has: boardPage.getByRole('heading', { name: '服务器装机准备', exact: true }) })
  const boardRow = boardPage.locator(`[data-provision-request="${created.id}"]`)
  await boardRow.getByRole('button', { name: '执行准备校验', exact: true }).click()
  const preparationResponse = boardPage.waitForResponse(result => result.url().endsWith('/' + created.id + '/prepare'))
  await boardPage.locator('.action-dialog button[type=submit]').click()
  const executionResponse = await preparationResponse; assert.equal(executionResponse.status(), 201)
  const preparation = await executionResponse.json(); assert.equal(preparation.mode, 'provision')
  const deadline = Date.now() + 60_000
  let prepared
  do {
    prepared = await call('GET', requestBase)
    if (prepared.state === 'prepared') break
    assert(!['failed', 'canceled', 'expired'].includes(prepared.state), JSON.stringify(prepared))
    await new Promise(resolve => setTimeout(resolve, 150))
  } while (Date.now() < deadline)
  assert.equal(prepared.state, 'prepared'); assert.equal(prepared.execution_id, preparation.id)
  await boardPanel.getByRole('button', { name: '刷新', exact: true }).click()
  await boardRow.getByText('准备完成', { exact: true }).waitFor()
  // Session UI never guesses a domain or enables submission without one.
  await boardRow.getByRole('button', { name: '发起安装会话', exact: true }).click()
  const startDialog = boardPage.locator('.action-dialog')
  await startDialog.getByText('本页没有已批准网络域，可翻页或在网络域面板登记并完成独立审批。', { exact: true }).waitFor()
  await startDialog.locator('#install-start-reason').fill('No approved domain must block admission')
  await startDialog.locator('#install-start-confirm').check()
  assert.equal(await startDialog.locator('button[type=submit]').isEnabled(), false)
  await startDialog.getByRole('button', { name: '取消', exact: true }).click()

  await boardPage.screenshot({ path: work + '/provision-prepared.png', fullPage: true })
  // A failed current read must remove cached approval/action rows.
  const boardPattern = `**/api/v1/projects/${project.id}/provision/requests?**`
  await boardPage.route(boardPattern, route => route.fulfill({ status: 503, contentType: 'application/json', body: JSON.stringify({ error: { code: 'TEMPORARY_FAILURE', message: 'fixture unavailable' } }) }))
  await boardPanel.getByRole('button', { name: '刷新', exact: true }).click()
  await boardRow.waitFor({ state: 'detached' })
  await boardPage.unroute(boardPattern)
  await boardPanel.getByRole('button', { name: '刷新', exact: true }).click()
  await boardRow.waitFor({ state: 'visible' })
  await call('POST', requestBase + '/cancel', { expected_version: prepared.state_version, reason: 'Retire isolated preparation fixture' })
  await boardPage.close(); await reviewPage.close()
  await owner('ssh-key-revoke', ['--project', project.id, '--id', key.id, '--expected-version', String(key.state_version), '--reason', 'isolated preview revocation check'])
  const revokedResponse = page.waitForResponse(result => result.url().endsWith('/provision/preview') && result.request().method() === 'POST')
  await dialog.locator('button[type=submit]').click(); assert.equal((await revokedResponse).status(), 409)
  await dialog.getByRole('alert').waitFor()
  assert.equal(await dialog.getByText('本次配置摘要: ' + next.snapshot_sha256, { exact: true }).count(), 0)
  await page.screenshot({ path: work + '/provision-preview-revoked.png', fullPage: true })
  await dialog.getByRole('button', { name: '取消', exact: true }).click()
  const assetsAfter = await call('GET', projectBase + '/assets')
  assert.deepEqual(assetsAfter.items.find(item => item.id === asset.id), asset)
  assert.equal((await call('GET', projectBase + '/executions')).total, executionsBefore + 1)
  const viewerName = 'provision-viewer-' + suffix
  await call('POST', '/admin/users', { username: viewerName, display_name: 'Viewer', password, is_admin: false }, 201)
  await call('POST', projectBase + '/members', { username: viewerName, role: 'Viewer' }, 201)
  const viewer = await browser.newContext({ baseURL: credentials.base_url }), viewerToken = await login(viewer, viewerName, password)
  await call('GET', projectBase + '/assets/' + asset.id + '/provision/target', undefined, 403, viewer, viewerToken)
  const viewerPage = await viewer.newPage(); await viewerPage.goto(`/p/${project.id}/assets`, { waitUntil: 'networkidle' })
  assert.equal(await viewerPage.getByRole('button', { name: '装机配置预览', exact: true }).count(), 0)
  // Retire fixtures through the real lifecycle so subsequent catalog checks stay isolated.
  for (const group of roots) for (const item of [group.root, group.kernel, group.initrd, group.template, group.incompatible]) await call('POST', group.base + '/' + item.id + '/transition', { state: 'retired' }, 200, reviewer, reviewToken)
  assert.deepEqual(errors, [])
  console.log('PASS durable independent provisioning approval, native source verification and unchanged asset/trust state; actual project/global OS upload UI pins exact roles/digests; readback checks real stored bytes and permission failure hides stale metadata')
  console.log('PASS actual BOM/model/vault key preview, image change clears result, owner revocation rejects refresh, no installation/asset-state mutation, Viewer denied')
  console.log('No PXE, installation, host trust or hardware acceptance is claimed')
} finally { await browser.close() }
