import assert from 'node:assert/strict'
import { readFile, writeFile, mkdir, stat } from 'node:fs/promises'
import { spawnSync } from 'node:child_process'
import { pathToFileURL } from 'node:url'
const credentials = process.env.KEEL_AUTH_BROWSER_CREDENTIALS_FILE
assert(credentials?.startsWith('/tmp/keel-media-')); assert.equal((await stat(credentials)).mode & 0o077, 0)
const c = JSON.parse(await readFile(credentials, 'utf8')), work = process.env.KEEL_WORKSPACE_EVIDENCE_DIR, publicKey = process.env.KEEL_MEDIA_MANIFEST_PUBLIC_KEY_FILE, binary = process.env.KEEL_ADMIN_BINARY
assert.equal(c.base_url, 'http://127.0.0.1:3102'); assert(publicKey && binary)
const { chromium } = await import(pathToFileURL('/tmp/keel-browser-check/node_modules/playwright/index.mjs').href)
const browser = await chromium.launch({ headless: true }), admin = await browser.newContext({ baseURL: c.base_url, viewport: { width: 1280, height: 900 } }), page = await admin.newPage(), errors = []
page.setDefaultTimeout(15000); page.on('pageerror', e => errors.push(e.message))
function offline(file, directory, key = publicKey, expected = 0) {
 const result = spawnSync(binary, ['media-manifest-verify', '--manifest-file', file, '--public-key-file', key, ...(directory ? ['--artifact-directory', directory] : [])], { encoding: 'utf8', env: { PATH: process.env.PATH } })
 assert.equal(result.status, expected, result.stderr)
 if (expected === 0) { const out = JSON.parse(result.stdout); assert.equal(out.artifact_bytes_verified, !!directory); return out }
}
async function login(ctx, username, password) {
 const before = await (await ctx.request.get('/api/v1/auth/csrf')).json()
 const response = await ctx.request.post('/api/v1/auth/login', { headers: { Origin: c.base_url, 'X-CSRF-Token': before.csrf_token }, data: { method: 'local', username, password } }); assert.equal(response.status(), 200)
 return (await response.json()).csrf_token
}
try {
 const csrf = await login(admin, c.username, c.password)
 async function call(method, path, data, expected = 200, ctx = admin, token = csrf) {
  const response = await ctx.request.fetch('/api/v1' + path, { method, headers: { Origin: c.base_url, 'X-CSRF-Token': token, 'Idempotency-Key': crypto.randomUUID() }, ...(data === undefined ? {} : { data }) })
  const out = await response.json(); assert.equal(response.status(), expected, `${method} ${path}: ${JSON.stringify(out)}`); return out
 }
 const suffix = crypto.randomUUID().slice(0, 8), project = await call('POST', '/projects', { name: 'Signed manifests', slug: 'manifest-' + suffix }, 201)
 const reviewerName = 'manifest-reviewer-' + suffix, viewerName = 'manifest-viewer-' + suffix, password = crypto.randomUUID() + '-Password!'
 for (const [username, is_admin] of [[reviewerName, true], [viewerName, false]]) await call('POST', '/admin/users', { username, display_name: username, password, is_admin }, 201)
 await call('POST', `/projects/${project.id}/members`, { username: viewerName, role: 'Viewer' }, 201)
 const reviewer = await browser.newContext({ baseURL: c.base_url }), viewer = await browser.newContext({ baseURL: c.base_url }), anonymous = await browser.newContext({ baseURL: c.base_url })
 const reviewToken = await login(reviewer, reviewerName, password); await login(viewer, viewerName, password)
 const projectBase = `/projects/${project.id}/media`, globalBase = '/admin/media', contents = new Map()
 async function upload(base, name, version = 'v1', deps = undefined, approved = true) {
  const buffer = Buffer.from(name + version + ' payload')
  const response = await admin.request.post('/api/v1' + base + '/upload', { headers: { Origin: c.base_url, 'X-CSRF-Token': csrf }, multipart: { metadata: JSON.stringify({ type: 'tool', name, version, arch: 'noarch', ...(deps ? { deps: { artifacts: deps.map(x => ({ media_id: x.id, sha256: x.sha256 })) } } : {}) }), file: { name: 'artifact.bin', mimeType: 'application/octet-stream', buffer } } })
  const item = await response.json(); assert.equal(response.status(), 201, JSON.stringify(item)); contents.set(item.id, buffer)
  if (approved) for (const state of ['verified', 'approved']) await call('POST', base + '/' + item.id + '/transition', { state }, 200, reviewer, reviewToken)
  return item
 }
 const leaf = await upload(globalBase, 'long-dependency-' + suffix + 'x'.repeat(96)), middle = await upload(projectBase, 'middle-' + suffix, 'v1', [leaf]), root = await upload(projectBase, 'root-' + suffix, 'v1', [middle]), pending = await upload(projectBase, 'pending-' + suffix, 'v1', undefined, false)
 const endpoint = projectBase + '/' + root.id + '/manifest', manifestPath = work + '/manifest.json', artifactDirectory = work + '/offline-artifacts'
 const response = await viewer.request.get('/api/v1' + endpoint); assert.equal(response.status(), 200); assert.equal(response.headers()['cache-control'], 'private, no-store'); assert.equal(response.headers()['x-content-type-options'], 'nosniff')
 const manifest = await response.json(); assert.equal(manifest.payload.artifacts.length, 3); assert.equal(manifest.payload.root_id, root.id)
 await writeFile(manifestPath, JSON.stringify(manifest, null, 2), { mode: 0o600 }); assert.equal(offline(manifestPath).artifact_count, 3)
 await mkdir(artifactDirectory, { mode: 0o700 })
 for (const node of manifest.payload.artifacts) {
  const download = await viewer.request.get('/api/v1' + projectBase + '/' + node.media_id + '/content'); assert.equal(download.status(), 200)
  assert.deepEqual(await download.body(), contents.get(node.media_id)); await writeFile(artifactDirectory + '/' + node.media_id + '.bin', await download.body(), { mode: 0o600 })
 }
 offline(manifestPath, artifactDirectory)
 await writeFile(artifactDirectory + '/' + leaf.id + '.bin', 'tampered'); offline(manifestPath, artifactDirectory, publicKey, 1)
 await writeFile(artifactDirectory + '/' + leaf.id + '.bin', contents.get(leaf.id)); offline(manifestPath, artifactDirectory)
 // Package the real platform receipt and actual downloaded fixed bytes for offline transport.
 const bundlePath = work + '/approved-media.tar'
 function bundle(command, args, expected = 0) {
  const result = spawnSync(binary, [command, '--public-key-file', publicKey, ...args], { encoding: 'utf8', env: { PATH: process.env.PATH } })
  assert.equal(result.status, expected, result.stderr)
  if (expected === 0) { const output = JSON.parse(result.stdout); assert.equal(output.artifact_bytes_verified, true); assert.equal(output.root_id, root.id); return output }
 }
 bundle('media-bundle-create', ['--manifest-file', manifestPath, '--artifact-directory', artifactDirectory, '--output-file', bundlePath])
 assert.equal((await stat(bundlePath)).mode & 0o777, 0o600)
 const originalBundle = await readFile(bundlePath)
 bundle('media-bundle-verify', ['--bundle-file', bundlePath])
 bundle('media-bundle-create', ['--manifest-file', manifestPath, '--artifact-directory', artifactDirectory, '--output-file', bundlePath], 1)
 assert.deepEqual(await readFile(bundlePath), originalBundle)
 const corruptBundle = Buffer.from(originalBundle), offset = corruptBundle.indexOf(contents.get(leaf.id)); assert(offset > 0); corruptBundle[offset] ^= 1
 await writeFile(work + '/corrupt-media.tar', corruptBundle, { mode: 0o600 }); bundle('media-bundle-verify', ['--bundle-file', work + '/corrupt-media.tar'], 1)
 await writeFile(work + '/truncated-media.tar', originalBundle.subarray(0, originalBundle.length - 512), { mode: 0o600 }); bundle('media-bundle-verify', ['--bundle-file', work + '/truncated-media.tar'], 1)
 const wrongPrivate = work + '/wrong-private.pem', wrongPublic = work + '/wrong-public.pem'
 assert.equal(spawnSync(binary, ['media-signing-keygen', '--private-key-file', wrongPrivate, '--public-key-file', wrongPublic], { env: { PATH: process.env.PATH } }).status, 0)
 offline(manifestPath, undefined, wrongPublic, 1)
 const untrustedBundle = spawnSync(binary, ['media-bundle-verify', '--bundle-file', bundlePath, '--public-key-file', wrongPublic], { encoding: 'utf8', env: { PATH: process.env.PATH } }); assert.equal(untrustedBundle.status, 1)
 const altered = structuredClone(manifest); altered.payload.artifacts[0].version = 'forged'
 await writeFile(work + '/tampered-manifest.json', JSON.stringify(altered), { mode: 0o600 }); offline(work + '/tampered-manifest.json', undefined, publicKey, 1)
 assert.equal((await anonymous.request.get('/api/v1' + endpoint)).status(), 401)
 assert.equal((await viewer.request.get('/api/v1' + globalBase + '/' + leaf.id + '/manifest')).status(), 403)
 assert.equal((await viewer.request.get('/api/v1' + projectBase + '/' + leaf.id + '/manifest')).status(), 200)
 assert.equal((await admin.request.get('/api/v1' + projectBase + '/' + pending.id + '/manifest')).status(), 409)
 const other = await call('POST', '/projects', { name: 'Other manifest project', slug: 'other-manifest-' + suffix }, 201)
 assert.equal((await admin.request.get(`/api/v1/projects/${other.id}/media/${root.id}/manifest`)).status(), 404)
 // Both project and global shared UI export real downloads under the current CSP.
 for (const [route, item] of [[`/p/${project.id}/media`, root], ['/admin/media', leaf]]) {
  await page.goto(route, { waitUntil: 'networkidle' })
  await page.getByRole('row').filter({ hasText: item.name }).getByRole('button', { name: '导出签名清单', exact: true }).click()
  await page.locator('.action-dialog .impact-notice').waitFor()
  assert(await page.locator('.action-dialog').evaluate(e => e.scrollWidth <= e.clientWidth + 1), 'dialog overflows')
  const downloaded = page.waitForEvent('download'); await page.locator('.action-dialog button[type=submit]').click()
  const file = await downloaded, path = work + '/ui-' + item.id + '.json'; assert.equal(file.suggestedFilename(), `keel-media-${item.id}.manifest.json`); await file.saveAs(path); offline(path)
 }
 // Viewer has the same reviewed read/export action, without mutation controls.
 const viewerPage = await viewer.newPage(); await viewerPage.goto(`/p/${project.id}/media`, { waitUntil: 'networkidle' })
 const viewerRow = viewerPage.getByRole('row').filter({ hasText: root.name }); await viewerRow.getByRole('button', { name: '导出签名清单', exact: true }).click()
 assert.equal(await viewerPage.getByRole('button', { name: '上传新介质', exact: true }).count(), 0)
 const viewerDownload = viewerPage.waitForEvent('download'); await viewerPage.locator('.action-dialog button[type=submit]').click()
 await (await viewerDownload).saveAs(work + '/viewer-manifest.json'); offline(work + '/viewer-manifest.json')
 await page.goto(`/p/${project.id}/media`, { waitUntil: 'networkidle' }); await page.getByRole('button', { name: 'Switch to English' }).click()
 await page.getByRole('row').filter({ hasText: root.name }).getByRole('button', { name: 'Export signed manifest', exact: true }).click()
 await page.locator('.action-dialog .impact-notice').getByText(/independently/i).waitFor()
 await page.getByRole('button', { name: 'Cancel', exact: true }).click(); await page.getByRole('button', { name: '切换为中文' }).click()
 await upload(globalBase, leaf.name, 'v2')
 const stillPinned = await call('GET', endpoint); assert(stillPinned.payload.artifacts.some(x => x.media_id === leaf.id && x.version === 'v1'))
 await call('POST', globalBase + '/' + leaf.id + '/transition', { state: 'retired' }, 200, reviewer, reviewToken)
 const blocked = await call('GET', endpoint, undefined, 422); assert.equal(blocked.code, 'MEDIA_DEPENDENCY_UNAVAILABLE')
 offline(manifestPath, artifactDirectory)
 bundle('media-bundle-verify', ['--bundle-file', bundlePath])
 await page.goto(`/p/${project.id}/media`, { waitUntil: 'networkidle' }); await page.getByRole('row').filter({ hasText: root.name }).getByRole('button', { name: '导出签名清单', exact: true }).click(); await page.locator('.action-dialog button[type=submit]').click()
 await page.getByText('固定依赖校验失败：请检查依赖是否仍已批准、有文件、摘要一致且架构兼容。', { exact: true }).waitFor()
 await page.screenshot({ path: work + '/manifest-retired-dependency.png', fullPage: true })
 assert.deepEqual(errors, [])
 console.log('PASS signed project/global manifests, transitive pinned approvals and real MinIO byte downloads; Viewer/inherited-global scope, anonymous/admin/cross-project boundaries')
 console.log('PASS offline CLI without database environment, independent public trust, metadata and full-byte validation, wrong key/payload/bytes rejection, historical receipt after retirement')
 console.log('PASS actual signed platform/MinIO bytes packaged to owner-only atomic offline archive and verified without database/network environment; no overwrite, wrong trust, corruption and truncated markers rejected; historical archive remains a receipt after retirement')
 console.log('PASS shared browser review and CSP-compatible JSON downloads; retired dependency blocks fresh export without rewriting prior receipt')
} finally { await browser.close() }
