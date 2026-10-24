import assert from 'node:assert/strict'
import {readFile,stat,writeFile} from 'node:fs/promises'
import {pathToFileURL} from 'node:url'
const file=process.env.KEEL_AUTH_BROWSER_CREDENTIALS_FILE;assert(file?.startsWith('/tmp/keel-views-'));assert.equal((await stat(file)).mode&0o077,0)
const c=JSON.parse(await readFile(file,'utf8'));assert.equal(c.base_url,'http://127.0.0.1:3104');assert(c.worker_start_file.startsWith('/tmp/keel-views-'))
const {chromium}=await import(pathToFileURL('/tmp/keel-browser-check/node_modules/playwright/index.mjs').href)
const browser=await chromium.launch({headless:true}),context=await browser.newContext({baseURL:c.base_url,viewport:{width:1440,height:1080}}),page=await context.newPage(),errors=[]
page.on('pageerror',e=>errors.push(e.message));const base='/api/v1/projects/'+c.project_id
async function login(p,username){await p.goto('/login');await p.locator('#username').fill(username);await p.locator('#password').fill(c.password);await p.locator('#login-form button[type="submit"]').click();await p.waitForURL('**/projects')}
try{
 await login(page,c.username);const session=await(await context.request.get('/api/v1/auth/session')).json(),headers={Origin:c.base_url,'X-CSRF-Token':session.csrf_token}
 async function post(path,data,expected=200){const r=await context.request.post(path,{headers:{...headers,'Idempotency-Key':crypto.randomUUID()},data});const out=await r.json();assert.equal(r.status(),expected,`${path}: ${JSON.stringify(out)}`);return out}
 async function get(path,expected=200){const r=await context.request.get(path);const out=await r.json();assert.equal(r.status(),expected,`${path}: ${JSON.stringify(out)}`);return out}
 const yaml=`api_version: keel.sim/v1
inputs:
  profile: {type: enum, required: true, enum: [smoke, full], default: smoke}
tasks:
  - id: collect
    type: sim.read
    retry: 0
    idempotent: true
    timeout_ms: 1000
    resources: [machine]
    sim: {outcome: success, duration_ms: 25, seed: 42}
  - id: verify
    type: sim.read
    depends_on: [collect]
    retry: 0
    idempotent: true
    timeout_ms: 1000
    resources: [machine]
    sim: {outcome: success, duration_ms: 25, seed: 42}
`
 const flow=await post(base+'/flows',{name:'Views fixture',yaml_source:yaml,expected_version:0},201)
 const targets=await post(base+'/sim-targets',{prefix:'view-matrix',count:500,seed:42},201)
 const big=await post(base+'/executions',{mode:'sim',flow_version_id:flow.id,target_ids:targets.items.map(t=>t.id),parameters:{profile:'full'},concurrency:4,circuit_breaker_percent:5},201)
 await post(base+'/executions/'+big.id+'/control',{action:'pause',expected_version:big.state_version})
 let overview=await get(base+'/executions/'+big.id+'/overview');assert.equal(overview.progress.total_targets,500);assert.equal(overview.progress.complete_targets,0);assert.equal(overview.progress.success_targets,0);assert.equal(overview.timeline.length,2);assert(overview.timeline.every(t=>t.first_started_at===null))
 const matrix=await get(base+'/executions/'+big.id+'/matrix?page_size=500');assert.equal(matrix.total,500);assert.equal(matrix.items.length,500);assert.equal(matrix.tasks.length,2);assert(matrix.items.every(row=>row.cells.length===2&&row.cells.every(cell=>cell.state==='pending')))
 const filtered=await get(base+'/executions/'+big.id+'/matrix?q=view-matrix-0001&state=pending');assert.equal(filtered.total,1)
 await get(base+'/executions/'+big.id+'/matrix?page_size=501',400)
 const journal=await get(base+'/executions/'+big.id+'/journal');assert.equal(journal.kind,'lifecycle');assert(journal.items.length>=2);assert(journal.items.every(e=>e.execution_id===big.id&&e.level==='info'));assert.equal((await get(base+'/executions/'+big.id+'/journal?after='+journal.next_cursor)).items.length,0)
 const future=await get(base+'/executions/'+big.id+'/journal?after='+(journal.next_cursor+99999));assert.equal(future.resync_required,true)
 await get(base+'/executions/'+big.id+'/stream?after=-1',400)
 const relations=await get(base+'/executions/'+big.id+'/relations');assert.equal(relations.flow.id,flow.id);assert.equal(relations.parameters.profile,'full');assert.equal(relations.target_set_hash,big.target_set_hash);assert.equal((await get(base+'/executions/'+big.id+'/outputs')).artifacts_supported,false)
 const requests=[];page.on('request',r=>requests.push(r.url()))
 await page.goto(`/p/${c.project_id}/executions?execution=${big.id}&tab=overview`);await page.getByRole('heading',{name:'任务时间线',exact:true}).waitFor();assert(!requests.some(url=>/\/(matrix|journal|outputs|relations)(\?|$)/.test(url)),'inactive views loaded eagerly')
 await page.getByRole('tab',{name:'目标',exact:true}).click();await page.locator('.execution-matrix-row').first().waitFor();assert(await page.locator('.execution-matrix-row').count()<100,'matrix rendered all frozen targets');assert.match(page.url(),/tab=targets/)
 for(const size of [8,16,24,32]) { await page.locator('select').filter({has:page.locator('option[value="8"]')}).selectOption(String(size)); await page.waitForFunction(expected=>{const cell=document.querySelector('.execution-matrix-cell');return cell&&Math.round(cell.getBoundingClientRect().width)===expected&&Math.round(cell.getBoundingClientRect().height)===expected},size) }
 await page.locator('.execution-matrix-cell').first().click();await page.getByRole('dialog').waitFor();await page.getByRole('heading',{name:'任务尝试详情',exact:true}).waitFor();await page.waitForFunction(()=>{const box=document.querySelector('[role=dialog]').getBoundingClientRect();return Math.abs(box.right-innerWidth)<2&&Math.abs(box.width-640)<2&&Math.abs(box.top)<2});await page.getByRole('button',{name:'查看本次尝试的日志',exact:true}).click();await page.getByRole('heading',{name:'执行生命周期日志',exact:true}).waitFor();assert.match(page.url(),/logAttempt=/)
 await page.getByRole('button',{name:/清除尝试筛选/}).click();await page.locator('.execution-journal article').first().waitFor()
 assert.match(await page.locator('main').evaluate(e=>getComputedStyle(e).fontFamily),/Intel One Mono/)
 await page.screenshot({path:process.env.KEEL_WORKSPACE_EVIDENCE_DIR+'/execution-logs.png',fullPage:true})
 // A worker is started only after the large render fixture is safely paused.
 // Just two targets execute; this is connectivity validation, not a throughput SLO.
 await writeFile(c.worker_start_file,'start')
 const small=await post(base+'/executions',{mode:'sim',flow_version_id:flow.id,target_ids:targets.items.slice(0,2).map(t=>t.id),parameters:{profile:'smoke'},concurrency:2,circuit_breaker_percent:5},201)
 for(let i=0;i<150;i++){overview=await get(base+'/executions/'+small.id+'/overview');if(['success','failed','warning','killed'].includes(overview.execution.state))break;await new Promise(resolve=>setTimeout(resolve,100))}
 assert.equal(overview.execution.state,'success');assert.equal(overview.progress.success_targets,2);assert.equal(overview.progress.complete_targets,2);assert.equal(overview.progress.unknown_targets,0);assert(overview.timeline.every(t=>t.first_started_at&&t.last_observed_at))
 const outputs=await get(base+'/executions/'+small.id+'/outputs?page_size=1');assert.equal(outputs.total,4);assert.equal(outputs.items.length,1);assert.equal(outputs.parameters.profile,'smoke')
 const receipt=await get(base+'/executions/'+small.id+'/outputs/'+outputs.items[0].attempt_id+'/download');assert.equal(receipt.receipt.result,'success');assert.equal(receipt.digest,outputs.items[0].digest)
 const audits=await get(base+'/audits?action=execution.output.export');assert.equal(audits.total,1)
 const attempt=await get(base+'/executions/'+small.id+'/attempts/'+outputs.items[0].attempt_id);assert.equal(attempt.attempt.receipt.result,'success')
 await get(base+'/executions/'+big.id+'/attempts/'+outputs.items[0].attempt_id,404)
 assert.equal((await context.request.get('/api/v1/projects/'+'f'.repeat(32)+'/executions/'+small.id+'/overview')).status(),404)
 await get(base+'/executions/'+big.id+'/outputs/'+outputs.items[0].attempt_id+'/download',404)
 const uncertainFlow=await post(base+'/flows',{name:'Unknown receipt fixture',yaml_source:yaml.replace('outcome: success','outcome: drop_receipt'),expected_version:0},201)
 const uncertain=await post(base+'/executions',{mode:'sim',flow_version_id:uncertainFlow.id,target_ids:[targets.items[0].id],parameters:{profile:'smoke'},concurrency:1,circuit_breaker_percent:5},201)
 let unknown;for(let i=0;i<150;i++){unknown=await get(base+'/executions/'+uncertain.id+'/overview');if(unknown.progress.unknown_targets===1)break;await new Promise(resolve=>setTimeout(resolve,100))}
 assert.equal(unknown.progress.unknown_targets,1);assert.equal(unknown.progress.success_targets,0);assert.equal(unknown.progress.complete_targets,0)
 // Same-origin SSE snapshot, cursor resume and explicit future-cursor resync.
 const chunk=await page.evaluate(async path=>{const response=await fetch(path,{credentials:'same-origin'}),reader=response.body.getReader(),part=await reader.read();await reader.cancel();return{status:response.status,type:response.headers.get('content-type'),text:new TextDecoder().decode(part.value)}},base+'/executions/'+small.id+'/stream')
 assert.equal(chunk.status,200);assert.match(chunk.type,/text\/event-stream/);assert.match(chunk.text,/event: snapshot/)
 const reset=await page.evaluate(async path=>{const response=await fetch(path),reader=response.body.getReader(),part=await reader.read();await reader.cancel();return new TextDecoder().decode(part.value)},base+'/executions/'+small.id+'/stream?after=999999999');assert.match(reset,/event: resync/)
 const viewerContext=await browser.newContext({baseURL:c.base_url}),viewerPage=await viewerContext.newPage();await login(viewerPage,'flowviewer');const viewerSession=await(await viewerContext.request.get('/api/v1/auth/session')).json()
 const revocation=viewerPage.evaluate(path=>new Promise((resolve,reject)=>{const source=new EventSource(path),timer=setTimeout(()=>{source.close();reject(new Error('revoked stream did not close'))},8000);source.addEventListener('snapshot',()=>{window.__viewStreamOpen=true});source.addEventListener('access_revoked',()=>{clearTimeout(timer);source.close();resolve(true)})}),base+'/executions/'+small.id+'/stream')
 await viewerPage.waitForFunction(()=>window.__viewStreamOpen===true)
 assert.equal((await viewerContext.request.post('/api/v1/auth/logout',{headers:{Origin:c.base_url,'X-CSRF-Token':viewerSession.csrf_token}})).status(),204)
 assert.equal(await revocation,true);await viewerContext.close()
 await page.goto(`/p/${c.project_id}/executions?execution=${small.id}&tab=outputs`);await page.getByRole('heading',{name:'输出与变量',exact:true}).waitFor();await page.getByRole('button',{name:'下载 JSON 回执',exact:true}).first().waitFor();await page.getByRole('tab',{name:'关联',exact:true}).click();await page.getByRole('heading',{name:'执行关联',exact:true}).waitFor();await page.getByRole('link',{name:'Views fixture · v1',exact:true}).waitFor()
 await page.screenshot({path:process.env.KEEL_WORKSPACE_EVIDENCE_DIR+'/execution-relations.png',fullPage:true});assert.deepEqual(errors,[])
 console.log('PASS consistent overview and task Gantt projections; immutable 500-target matrix server filters/pagination and virtual rendering (not a performance SLO)')
 console.log('PASS lazy five-tab browser, URL filters/attempt drilldown, Intel font; actual shared worker two-target run, frozen inputs, unknown receipt remains incomplete, structured receipt outputs and audited download, cross-execution/project denial')
 console.log('PASS same-origin lifecycle SSE snapshot/resume contract, future cursor resync, active session revocation closes stream; no fabricated stdout or file artifacts')
}finally{await browser.close()}
