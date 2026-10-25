import assert from 'node:assert/strict'
import {readFile,stat} from 'node:fs/promises'
import {createHash} from 'node:crypto'
import {pathToFileURL} from 'node:url'
const file=process.env.KEEL_AUTH_BROWSER_CREDENTIALS_FILE;assert(file?.startsWith('/tmp/keel-media-'));assert.equal((await stat(file)).mode&0o077,0)
const c=JSON.parse(await readFile(file,'utf8'));assert.equal(c.base_url,'http://127.0.0.1:3102')
const {chromium}=await import(pathToFileURL('/tmp/keel-browser-check/node_modules/playwright/index.mjs').href)
const browser=await chromium.launch({headless:true}),admin=await browser.newContext({baseURL:c.base_url,viewport:{width:1440,height:1080}}),page=await admin.newPage(),errors=[]
page.on('pageerror',e=>errors.push(e.message))
async function login(ctx,username,password){const csrf=await(await ctx.request.get('/api/v1/auth/csrf')).json();const r=await ctx.request.post('/api/v1/auth/login',{headers:{Origin:c.base_url,'X-CSRF-Token':csrf.csrf_token},data:{method:'local',username,password}});assert.equal(r.status(),200);return (await(await ctx.request.get('/api/v1/auth/session')).json()).csrf_token}
try{
 const csrf=await login(admin,c.username,c.password),headers={Origin:c.base_url,'X-CSRF-Token':csrf}
 async function post(path,data,expected=200,ctx=admin,token=csrf){const r=await ctx.request.post('/api/v1'+path,{headers:{Origin:c.base_url,'X-CSRF-Token':token,'Idempotency-Key':crypto.randomUUID()},data});const out=await r.json();assert.equal(r.status(),expected,`${path}: ${JSON.stringify(out)}`);return out}
 const suffix=crypto.randomUUID().slice(0,8),project=await post('/projects',{name:'Media byte verification',slug:'media-bytes-'+suffix},201),base='/projects/'+project.id+'/media'
 const users={}
 for(const role of ['SRE','Viewer']){
  const username='media-'+role.toLowerCase()+'-'+suffix,password=crypto.randomUUID()+'-Password!'
  await post('/admin/users',{username,display_name:role,password,is_admin:false},201);await post('/projects/'+project.id+'/members',{username,role},201)
  const context=await browser.newContext({baseURL:c.base_url});users[role]={context,csrf:await login(context,username,password),username,password}
 }
 const payload=Buffer.from('keel private immutable artifact\n'),sha=createHash('sha256').update(payload).digest('hex'),metadata={type:'tool',name:'verified-bytes',version:'v1',arch:'noarch',os_scope:'linux'}
 async function upload(meta,expected=201){const r=await admin.request.post('/api/v1'+base+'/upload',{headers,multipart:{metadata:JSON.stringify(meta),file:{name:'tool.bin',mimeType:'application/octet-stream',buffer:payload}}});const out=await r.json();assert.equal(r.status(),expected,JSON.stringify(out));return out}
 const media=await upload(metadata);assert.equal(media.sha256,sha);assert.equal(media.size,payload.length);assert.equal(media.artifact_stored,true);await upload(metadata,409)
 let r=await admin.request.get('/api/v1'+base+'/'+media.id+'/content');assert.equal(r.status(),409)
 await post(base+'/'+media.id+'/transition',{state:'verified'});const self=await post(base+'/'+media.id+'/transition',{state:'approved'},409);assert.equal(self.code,'MEDIA_SELF_APPROVAL')
 const sre=users.SRE,viewer=users.Viewer
 await post(base+'/'+media.id+'/transition',{state:'approved'},200,sre.context,sre.csrf)
 r=await viewer.context.request.get('/api/v1'+base+'/'+media.id+'/content');assert.equal(r.status(),200);assert.deepEqual(await r.body(),payload);assert.equal(r.headers()['x-content-sha256'],sha)
 r=await viewer.context.request.get('/api/v1'+base+'/'+media.id+'/content',{headers:{Range:'bytes=5-11'}});assert.equal(r.status(),206);assert.deepEqual(await r.body(),payload.subarray(5,12));assert.match(r.headers()['content-range'],/^bytes 5-11\//)
 r=await viewer.context.request.get('/api/v1'+base+'/'+media.id+'/content',{headers:{Range:'bytes=9999-'}});assert.equal(r.status(),416)
 await post(base+'/'+media.id+'/transition',{state:'retired'},403,viewer.context,viewer.csrf)
 r=await viewer.context.request.post('/api/v1'+base+'/upload',{headers:{Origin:c.base_url,'X-CSRF-Token':viewer.csrf},multipart:{metadata:JSON.stringify({...metadata,version:'viewer'}),file:{name:'x',mimeType:'application/octet-stream',buffer:payload}}});assert.equal(r.status(),403)
 r=await admin.request.put('/api/v1'+base+'/'+media.id+'/content',{headers:{...headers,'Content-Type':'application/octet-stream'},data:payload});assert.equal(r.status(),409)
 const other=await post('/projects',{name:'Other bytes project',slug:'media-other-'+suffix},201)
 r=await admin.request.get(`/api/v1/projects/${other.id}/media/${media.id}/content`);assert.equal(r.status(),404)
 const anonymous=await browser.newContext({baseURL:c.base_url});assert.equal((await anonymous.request.get('/api/v1'+base+'/'+media.id+'/content')).status(),401);await anonymous.close()
 r=await admin.request.put('/api/v1'+base+'/'+media.id+'/content',{headers:{Origin:c.base_url,'Content-Type':'application/octet-stream'},data:payload});assert.equal(r.status(),403)
 const bare=await post(base,{...metadata,name:'attach-bytes',sha256:sha,size:payload.length,manifest_ref:'minio://untrusted/path'},201)
 const missing=await post(base+'/'+bare.id+'/transition',{state:'verified'},409);assert.equal(missing.code,'MEDIA_ARTIFACT_MISSING')
 r=await admin.request.put('/api/v1'+base+'/'+bare.id+'/content',{headers:{...headers,'Content-Type':'application/octet-stream'},data:Buffer.from('wrong')});assert.equal(r.status(),422)
 r=await sre.context.request.put('/api/v1'+base+'/'+bare.id+'/content',{headers:{Origin:c.base_url,'X-CSRF-Token':sre.csrf,'Content-Type':'application/octet-stream'},data:payload});assert.equal(r.status(),403)
 r=await admin.request.put('/api/v1'+base+'/'+bare.id+'/content',{headers:{...headers,'Content-Type':'application/octet-stream'},data:payload});assert.equal(r.status(),200)
 r=await admin.request.put('/api/v1'+base+'/'+bare.id+'/content',{headers:{...headers,'Content-Type':'application/octet-stream'},data:payload});assert.equal(r.status(),409)
 await post(base+'/'+media.id+'/transition',{state:'retired'},200,sre.context,sre.csrf);assert.equal((await viewer.context.request.get('/api/v1'+base+'/'+media.id+'/content')).status(),409)
 // Upload and approve a new version using real shared dialogs and file controls.
 await page.goto(`/p/${project.id}/media`);await page.getByRole('button',{name:'上传新介质',exact:true}).click();await page.locator('#media-name').fill('browser-upload');await page.locator('#media-version').fill('v1');await page.locator('#media-file').setInputFiles({name:'fixture.bin',mimeType:'application/octet-stream',buffer:payload});await page.locator('.action-dialog button[type=submit]').click();await page.getByText('介质已登记。',{exact:true}).waitFor()
 let row=page.getByRole('row').filter({hasText:'browser-upload'});await row.getByText('文件已存储',{exact:true}).waitFor();await row.getByRole('button',{name:'校验',exact:true}).click();await page.locator('.action-dialog button[type=submit]').click();await row.getByText('已校验',{exact:true}).waitFor()
 const reviewerPage=await sre.context.newPage();await reviewerPage.goto(`/p/${project.id}/media`);row=reviewerPage.getByRole('row').filter({hasText:'browser-upload'});await row.getByRole('button',{name:'审批',exact:true}).click();await reviewerPage.locator('.action-dialog button[type=submit]').click();await row.getByText('已审批',{exact:true}).waitFor();await row.getByRole('link',{name:'下载已批准文件',exact:true}).waitFor()
 await reviewerPage.screenshot({path:process.env.KEEL_WORKSPACE_EVIDENCE_DIR+'/media-approved.png',fullPage:true});assert.match(await reviewerPage.locator('main').evaluate(e=>getComputedStyle(e).fontFamily),/JetBrains Mono/)
 assert.deepEqual(errors,[])
 console.log('PASS real MinIO upload/hash/size/immutable versions, independent SRE approval, Viewer download and HTTP Range, retirement/scope/anonymous/CSRF/role denial')
 console.log('PASS manifest-only verification blocked, digest mismatch and replacement blocked, uploader binding enforced; actual upload/verify/approve/download UI and shared font')
}finally{await browser.close()}
