import assert from 'node:assert/strict'
import { readFile, stat } from 'node:fs/promises'
import { pathToFileURL } from 'node:url'
const file = process.env.KEEL_AUTH_BROWSER_CREDENTIALS_FILE
assert(file?.startsWith('/tmp/keel-media-')); assert.equal((await stat(file)).mode & 0o077, 0)
const c = JSON.parse(await readFile(file, 'utf8')); assert.equal(c.base_url, 'http://127.0.0.1:3102')
const { chromium } = await import(pathToFileURL('/tmp/keel-browser-check/node_modules/playwright/index.mjs').href)
const browser = await chromium.launch({ headless: true }), admin = await browser.newContext({ baseURL: c.base_url, viewport: { width: 1440, height: 1080 } }), page = await admin.newPage(), errors = []
page.setDefaultTimeout(15000)
page.on('pageerror', e => errors.push(e.message))
async function login(ctx, username, password) {
 const before = await (await ctx.request.get('/api/v1/auth/csrf')).json()
 const r = await ctx.request.post('/api/v1/auth/login', { headers: { Origin: c.base_url, 'X-CSRF-Token': before.csrf_token }, data: { method: 'local', username, password } }); assert.equal(r.status(), 200)
 return (await r.json()).csrf_token
}
try {
 const csrf = await login(admin, c.username, c.password)
 async function call(method, path, data, expected = 200, ctx = admin, token = csrf) {
  const r = await ctx.request.fetch('/api/v1' + path, { method, headers: { Origin: c.base_url, 'X-CSRF-Token': token, 'Idempotency-Key': crypto.randomUUID() }, ...(data === undefined ? {} : { data }) })
  const out = await r.json(); assert.equal(r.status(), expected, `${method} ${path}: ${JSON.stringify(out)}`); return out
 }
 const suffix = crypto.randomUUID().slice(0, 8), project = await call('POST', '/projects', { name: 'Artifact dependencies', slug: 'media-deps-' + suffix }, 201)
 const username = 'deps-reviewer-' + suffix, password = crypto.randomUUID() + '-Password!'
 await call('POST', '/admin/users', { username, display_name: 'Independent reviewer', password, is_admin: true }, 201)
 const reviewer = await browser.newContext({ baseURL: c.base_url }), reviewToken = await login(reviewer, username, password)
 for (const scope of ['project','global']) {
  const base = scope === 'global' ? '/admin/media' : `/projects/${project.id}/media`, route = scope === 'global' ? '/admin/media' : `/p/${project.id}/media`
  async function upload(name, arch, approved) {
   const response = await admin.request.post('/api/v1' + base + '/upload', { headers: { Origin: c.base_url, 'X-CSRF-Token': csrf }, multipart: { metadata: JSON.stringify({ type: 'tool', name, version: 'v1', arch }), file: { name: 'tool.bin', mimeType: 'application/octet-stream', buffer: Buffer.from(name+' payload') } } })
   const out = await response.json(); assert.equal(response.status(),201,JSON.stringify(out))
   if(approved) for(const state of ['verified','approved']) await call('POST',base+'/'+out.id+'/transition',{state},200,reviewer,reviewToken)
   return out
  }
  const leaf = await upload('leaf-'+scope+'-'+suffix,'noarch',true)
  const pending = await upload('pending-'+scope+'-'+suffix,'noarch',false)
  const arm = await upload('arm-'+scope+'-'+suffix,'arm64',true)
  await page.goto(route, {waitUntil:'networkidle'})
  await page.getByRole('button',{name:'上传新介质',exact:true}).click()
  await page.locator('#media-name').fill('parent-'+scope+'-'+suffix);await page.locator('#media-version').fill('v1');await page.locator('#media-arch').selectOption('amd64')
  const picker=page.locator('#media-dependency');await picker.locator(`option[value="${leaf.id}"]`).waitFor({state:'attached'})
  assert.equal(await picker.locator(`option[value="${pending.id}"]`).count(),0);assert.equal(await picker.locator(`option[value="${arm.id}"]`).count(),0)
  await picker.selectOption(leaf.id)
  const selected=page.getByRole('list',{name:'已固定的制品依赖'})
  await selected.getByText(leaf.sha256,{exact:true}).waitFor();assert.equal(await picker.locator(`option[value="${leaf.id}"]`).isDisabled(),true)
  await page.locator('#media-file').setInputFiles({name:'parent.bin',mimeType:'application/octet-stream',buffer:Buffer.from('parent payload')})
  const request=page.waitForResponse(r=>r.url().endsWith(base+'/upload')&&r.request().method()==='POST')
  await page.locator('.action-dialog button[type=submit]').click();const r=await request;assert.equal(r.status(),201);const parent=await r.json()
  assert.equal(parent.deps.artifacts[0].media_id,leaf.id);assert.equal(parent.deps.artifacts[0].sha256,leaf.sha256);assert.equal(parent.deps.artifacts[0].version,'v1');assert.equal(parent.deps.artifacts[0].name,leaf.name)
  const row=page.getByRole('row').filter({hasText:parent.name});await row.getByText('1 个固定制品依赖',{exact:true}).waitFor()
  await row.getByRole('button',{name:'校验',exact:true}).click()
  await page.locator('.action-dialog').getByText(leaf.name+' · v1 · noarch',{exact:true}).waitFor()
  assert.equal(await page.locator('.action-dialog').getByText('校验前需要上传真实文件。',{exact:true}).count(),0)
  await page.locator('.action-dialog button[type=submit]').click();await row.getByText('已校验',{exact:true}).waitFor()
  await call('POST',base+'/'+parent.id+'/transition',{state:'approved'},200,reviewer,reviewToken)
  const download=await admin.request.get('/api/v1'+base+'/'+parent.id+'/content');assert.equal(download.status(),200);assert.equal((await download.body()).toString(),'parent payload')
  // A reviewed dependency retirement blocks the parent's next download and a new parent verification.
  const pendingParent=await call('POST',base,{type:'tool',name:'pending-parent-'+scope+'-'+suffix,version:'v1',arch:'amd64',sha256:parent.sha256,size:parent.size,deps:{artifacts:[{media_id:leaf.id,sha256:leaf.sha256}]}},201)
  const attachment=await admin.request.put('/api/v1'+base+'/'+pendingParent.id+'/content',{headers:{Origin:c.base_url,'X-CSRF-Token':csrf,'Content-Type':'application/octet-stream'},data:Buffer.from('parent payload')});assert.equal(attachment.status(),200)
  await call('POST',base+'/'+leaf.id+'/transition',{state:'retired'},200,reviewer,reviewToken)
  const blocked=await admin.request.get('/api/v1'+base+'/'+parent.id+'/content');assert.equal(blocked.status(),422);assert.equal((await blocked.json()).code,'MEDIA_DEPENDENCY_UNAVAILABLE')
  await page.reload({waitUntil:'networkidle'});const blockedRow=page.getByRole('row').filter({hasText:pendingParent.name});await blockedRow.getByRole('button',{name:'校验',exact:true}).click()
  await page.locator('.action-dialog button[type=submit]').click();await page.getByText('固定依赖校验失败：请检查依赖是否仍已批准、有文件、摘要一致且架构兼容。',{exact:true}).waitFor()
  await page.screenshot({path:process.env.KEEL_WORKSPACE_EVIDENCE_DIR+'/media-dependencies-'+scope+'.png',fullPage:true})
  await page.getByRole('button',{name:'取消',exact:true}).click()
  await page.getByRole('button',{name:'Switch to English'}).click()
  await page.getByRole('row').filter({hasText:pendingParent.name}).getByText('1 pinned artifact dependencies',{exact:true}).waitFor()
  await page.getByRole('row').filter({hasText:pendingParent.name}).getByRole('button',{name:'Verify',exact:true}).click()
  await page.locator('.action-dialog').getByText(leaf.name+' · v1 · noarch',{exact:true}).waitFor()
  await page.locator('.action-dialog button[type=submit]').click()
  await page.getByText('Pinned dependency validation failed. Check approval, stored bytes, digests and CPU architecture.',{exact:true}).waitFor()
  await page.getByRole('button',{name:'Cancel',exact:true}).click()
  await page.getByRole('button',{name:'切换为中文'}).click()
  // Close our fixtures through the real one-way lifecycle so global approved versions cannot pollute later parameter checks.
  for (const item of [parent,pendingParent,arm,pending]) await call('POST',base+'/'+item.id+'/transition',{state:'retired'},200,reviewer,reviewToken)
 }
 assert.deepEqual(errors,[])
 console.log('PASS project/global approved stored compatible dependency selection, exact immutable ID/digest registration, canonical metadata, reviewed verification and actual content download')
 console.log('PASS pending/incompatible dependencies excluded, duplicate selection disabled, retired dependencies block distribution and localized verification without closing the review dialog')
} finally {await browser.close()}
