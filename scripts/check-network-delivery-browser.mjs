/** Real API/PG/browser integration; device boundary uses the shared mTLS fixture. */
import assert from 'node:assert/strict'
import {readFile,writeFile,stat} from 'node:fs/promises'
import {pathToFileURL} from 'node:url'
import {createHash,randomUUID} from 'node:crypto'
import {execFileSync} from 'node:child_process'
const file=process.env.KEEL_AUTH_BROWSER_CREDENTIALS_FILE
assert(file?.startsWith('/tmp/keel-'));assert.equal((await stat(file)).mode&0o077,0)
const c=JSON.parse(await readFile(file,'utf8'));assert.equal(c.base_url,'http://127.0.0.1:3106');assert.match(c.test_database,/^keel_test_kea_3106_\d+$/)
const hash=bytes=>createHash('sha256').update(bytes).digest('hex')
function sql(statement){return execFileSync('docker',['exec','-i','keel-auth-test-postgres','psql','-v','ON_ERROR_STOP=1','-At','-U',c.test_database_owner,'-d',c.test_database],{input:statement,encoding:'utf8'})}
const {chromium}=await import(pathToFileURL('/tmp/keel-browser-check/node_modules/playwright/index.mjs').href)
const browser=await chromium.launch({headless:true}),context=await browser.newContext({baseURL:c.base_url,viewport:{width:1440,height:1080}}),page=await context.newPage(),errors=[]
page.on('pageerror',e=>errors.push(e.message));const base='/api/v1/projects/'+c.management_project_id,delivery=base+'/fabric/delivery'
try{
 await page.goto('/login');await page.locator('#username').fill(c.username);await page.locator('#password').fill(c.password);await page.locator('#login-form button[type="submit"]').click();await page.waitForURL('**/projects')
 const session=await(await context.request.get('/api/v1/auth/session')).json(),headers={Origin:c.base_url,'X-CSRF-Token':session.csrf_token}
 async function get(path){const response=await context.request.get(path);const out=await response.json();assert.equal(response.status(),200,JSON.stringify(out));return out}
 async function post(path,data,expected=200,extra={}){const response=await context.request.post(path,{headers:{...headers,...extra},data});const out=await response.json();assert.equal(response.status(),expected,JSON.stringify(out));return out}
 for(const fault of ['delivery-review-bad-scope','delivery-review-stale']){await writeFile(c.fault_file,fault);assert.equal((await context.request.get(delivery+'/preflight')).status(),502)}
 await writeFile(c.fault_file,'delivery-review-fixture');const fixtureReview=await get(delivery+'/preflight');assert.equal(fixtureReview.preflight.delivery_allowed,false);assert.equal(fixtureReview.preflight.stages[8].state,'BLOCKED');await writeFile(c.fault_file,'')
 const before=sql('SELECT count(*) FROM audits;')
 let preview=await get(delivery+'/preflight');assert.equal(preview.preflight.delivery_allowed,false);assert.equal(preview.preflight.stages.length,9);assert.equal(sql('SELECT count(*) FROM audits;'),before,'preflight caused a business audit write')
 assert.equal(preview.preflight.stages[0].state,'BLOCKED');assert.equal(preview.preflight.stages[8].state,'BLOCKED');assert.equal(preview.object_count,9)
 const again=await get(delivery+'/preflight');assert.equal(preview.preflight.preflight_hash,again.preflight.preflight_hash,'stable evidence changed between reads')
 const schema=await get(delivery+'/schema');assert.equal(schema.$id,'urn:keel:network-delivery:v1')
 const refs=await get(delivery+'/references?page=1&page_size=2');assert.equal(refs.items.length,2);assert(refs.total>2)
 assert.equal((await context.request.get(delivery+'/references?page=0')).status(),400)
 assert.equal((await context.request.get(delivery+'/references?page_size=1001')).status(),400)
 const request={kind:'draft',expected_preflight_hash:preview.preflight.preflight_hash},key=randomUUID()
 const archive=await post(delivery+'/exports',request,201,{'Idempotency-Key':key})
 const replay=await post(delivery+'/exports',request,201,{'Idempotency-Key':key});assert.equal(replay.id,archive.id)
 await post(delivery+'/exports',{...request,kind:'delivery'},409,{'Idempotency-Key':key})
 await post(delivery+'/exports',{...request,kind:'delivery'},409,{'Idempotency-Key':randomUUID()})
 await post(delivery+'/exports',request,400)
 assert.equal((await context.request.post(delivery+'/exports',{headers:{Origin:c.base_url,'Idempotency-Key':randomUUID()},data:request})).status(),403)
 const downloaded=await context.request.get(delivery+'/exports/'+archive.id+'/download'),body=await downloaded.body();assert.equal(downloaded.status(),200);assert.equal(hash(body),archive.content_hash);assert.equal(downloaded.headers()['x-keel-content-sha256'],archive.content_hash)
 const artifact=JSON.parse(body);assert.equal(artifact.kind,'draft');assert.equal(artifact.is_delivery,false);assert.match(artifact.notice,/NOT A NETWORK DELIVERY/);assert.equal(artifact.objects.length,preview.object_count);assert(artifact.objects.some(o=>o.category==='cable'));assert(artifact.objects.some(o=>o.category==='server'));assert.equal(artifact.preflight.preflight_hash,archive.preflight_hash)
 await writeFile(process.env.KEEL_WORKSPACE_EVIDENCE_DIR+'/network-draft.json',body)
 const alias=await context.request.get(base+'/fabric/baseline/export?export_id='+archive.id);assert.equal(hash(await alias.body()),archive.content_hash)
 assert.equal((await context.request.get(delivery+'/exports/'+archive.id+'/download?format=csv')).status(),400)
 console.log('PASS repeatable-read preflight, nine stage gates, exact scope hash, schema, reference pagination, draft-only admission, CSRF, audited idempotent archive and byte-exact historical download')
 const baseline=JSON.parse(await readFile('internal/fabricboot/testdata/management-baseline-v1.json','utf8'))
 const intent={no_drop_priorities:[6],dscp_map:{17:6},trust_mode:'dscp',ecn:{enabled_tc:[6],min_threshold_bytes:20,max_threshold_bytes:100,probability:0.05},traffic_pool:{lossless_ratio_percent:60},roce_profile:'lossless',pool:'shared-lossless',queue:'6'}
 baseline.engineering_limits.configuration_variables={'FIXTURE-SW-2':{lossless:{ports:{swp1:intent,swp2:intent}}},'FIXTURE-SW-3':{lossless:{ports:{swp1:intent}}}}
 await post(base+'/network-baselines',baseline,201);await post(base+'/network-baselines/'+baseline.version_ref+'/approve',{customer_approval_ref:'fixture:customer',network_approval_ref:'fixture:network'})
 const plan=await post(base+'/ztp/management-plan/preview',{baseline_ref:baseline.version_ref});await post(base+'/ztp/management-plan/commit',{baseline_ref:baseline.version_ref,expected_hash:plan.content_hash})
 await writeFile(c.fault_file,'fabric-lossless');await post(base+'/fabric/collections',undefined,201)
 await post(delivery+'/exports',request,409,{'Idempotency-Key':randomUUID()})
 preview=await get(delivery+'/preflight');assert.equal(preview.preflight.delivery_allowed,false);assert(preview.preflight.integrity_errors.some(e=>e.includes('protocol_fixture')))
 const observed=await post(delivery+'/exports',{kind:'draft',expected_preflight_hash:preview.preflight.preflight_hash},201,{'Idempotency-Key':randomUUID()})
 const observedBytes=await(await context.request.get(delivery+'/exports/'+observed.id+'/download')).body(),observedArtifact=JSON.parse(observedBytes)
 assert(observedArtifact.objects.some(o=>o.evidence_source==='protocol_fixture'));assert(observedArtifact.rule_versions.includes('LL-01@1'));assert(observedArtifact.objects.some(o=>o.category==='switch'&&o.details.lossless));assert.equal(observedArtifact.is_delivery,false)
 const old=await context.request.get(delivery+'/exports/'+archive.id+'/download');assert.equal(hash(await old.body()),archive.content_hash)
 console.log('PASS BOM + approved management plan + mTLS collector facts fused with exact sample and applied-rule references; changed scope rejects stale review; fixture cannot become formal delivery; prior archived bytes remain unchanged')
 // Concurrent requests use one durable idempotency ledger entry.
 const latest=await get(delivery+'/preflight'),concurrentRequest={kind:'draft',expected_preflight_hash:latest.preflight.preflight_hash},concurrentKey=randomUUID()
 const concurrent=await Promise.all([post(delivery+'/exports',concurrentRequest,201,{'Idempotency-Key':concurrentKey}),post(delivery+'/exports',concurrentRequest,201,{'Idempotency-Key':concurrentKey})]);assert.equal(concurrent[0].id,concurrent[1].id)
 assert.equal((await get(delivery+'/exports')).items.length,3)
 assert(Number(sql("SELECT count(*) FROM audits WHERE action IN ('fabric.delivery.export','fabric.delivery.download');"))>=6)
 assert.equal((await context.request.get('/api/v1/projects/'+'f'.repeat(32)+'/fabric/delivery/preflight')).status(),404)
 const outsider=randomUUID().replaceAll('-','');await post('/api/v1/projects',{name:'Delivery isolation',slug:'delivery-isolation-'+outsider.slice(0,8),description:''},201,{'Idempotency-Key':randomUUID()}).then(async p=>{assert.equal((await context.request.get('/api/v1/projects/'+p.id+'/fabric/delivery/exports/'+archive.id+'/download')).status(),404)})
 await page.goto(`/p/${c.management_project_id}/delivery`);await page.getByRole('heading',{name:'网络交付证据',exact:true}).waitFor();await page.getByRole('heading',{name:'N-S0',exact:true}).waitFor();assert.match(await page.locator('main').evaluate(e=>getComputedStyle(e).fontFamily),/JetBrains Mono/)
 await page.getByRole('button',{name:'保存证据草稿',exact:true}).click();await page.getByRole('dialog').waitFor();await page.getByRole('button',{name:'确认保存',exact:true}).click();await page.waitForURL(url=>url.searchParams.get('tab')==='history');await page.getByRole('heading',{name:'导出历史',exact:true}).waitFor();assert((await page.getByRole('button',{name:'下载 JSON',exact:true}).count())>=4)
 await page.screenshot({path:process.env.KEEL_WORKSPACE_EVIDENCE_DIR+'/network-delivery-history.png',fullPage:true})
 await page.getByRole('button',{name:'交付复核',exact:true}).click();await page.locator('select').filter({has:page.locator('option[value="delivery"]')}).selectOption('delivery');assert(await page.getByRole('button',{name:'生成交付基线',exact:true}).isDisabled())
 const viewerName='delivery-viewer-'+randomUUID().slice(0,8),viewerPassword=randomUUID()+'-Fixture!'
 await post('/api/v1/admin/users',{username:viewerName,display_name:'Delivery Viewer',password:viewerPassword,is_admin:false},201,{'Idempotency-Key':randomUUID()});await post(base+'/members',{username:viewerName,role:'Viewer'},201,{'Idempotency-Key':randomUUID()})
 const viewer=await browser.newContext({baseURL:c.base_url}),csrf=await(await viewer.request.get('/api/v1/auth/csrf')).json()
 const login=await viewer.request.post('/api/v1/auth/login',{headers:{Origin:c.base_url,'X-CSRF-Token':csrf.csrf_token},data:{username:viewerName,password:viewerPassword,method:'local'}});assert.equal(login.status(),200)
 assert.equal((await viewer.request.get(delivery+'/exports/'+archive.id+'/download')).status(),200);assert.equal((await viewer.request.get('/api/v1/projects/'+ 'f'.repeat(32)+'/fabric/delivery/exports')).status(),404);await viewer.close()
 const anonymous=await browser.newContext({baseURL:c.base_url});assert.equal((await anonymous.request.get(delivery+'/preflight')).status(),401);await anonymous.close()
 assert.deepEqual(errors,[])
 console.log('PASS bounded mTLS review adapter rejects wrong scope, stale and fixture acceptance; current project Viewer may download, anonymous access denied')
 console.log('PASS concurrent idempotency, immutable history, export/download audit, cross-project archive denial, URL tabs, real confirmation/export/history browser flow, JetBrains Mono font and disabled formal action')
}finally{await browser.close()}
