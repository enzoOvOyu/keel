import assert from 'node:assert/strict'
import {readFile,stat} from 'node:fs/promises'
import {pathToFileURL} from 'node:url'
const file=process.env.KEEL_AUTH_BROWSER_CREDENTIALS_FILE;assert(file?.startsWith('/tmp/keel-flows-'));assert.equal((await stat(file)).mode&0o077,0)
const c=JSON.parse(await readFile(file,'utf8'));assert.equal(c.base_url,'http://127.0.0.1:3104')
const{chromium}=await import(pathToFileURL('/tmp/keel-browser-check/node_modules/playwright/index.mjs').href)
const browser=await chromium.launch({headless:true}),context=await browser.newContext({baseURL:c.base_url,viewport:{width:1440,height:1080}}),page=await context.newPage(),errors=[]
page.on('pageerror',error=>errors.push(error.message));const base='/api/v1/projects/'+c.project_id
try{
 await page.goto('/login');await page.locator('#username').fill(c.username);await page.locator('#password').fill(c.password);await page.locator('#login-form button[type="submit"]').click();await page.waitForURL('**/projects')
 const session=await(await context.request.get('/api/v1/auth/session')).json(),headers={Origin:c.base_url,'X-CSRF-Token':session.csrf_token}
 async function post(path,data,expected=200,key=crypto.randomUUID()){const r=await context.request.post(path,{headers:{...headers,'Idempotency-Key':key},data});const out=await r.json();assert.equal(r.status(),expected,`${path}: ${JSON.stringify(out)}`);return out}
 async function get(path){const r=await context.request.get(path);const out=await r.json();assert.equal(r.status(),200,`${path}: ${JSON.stringify(out)}`);return out}
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
    sim: {outcome: success, duration_ms: 100, seed: 42}
  - id: verify
    type: sim.read
    depends_on: [collect]
    retry: 0
    idempotent: true
    timeout_ms: 1000
    resources: [machine]
    sim: {outcome: success, duration_ms: 100, seed: 42}
`
 const validation=await post(base+'/flows/validate',{yaml_source:yaml,parameters:{profile:'full'}});assert.deepEqual(validation.task_order,['collect','verify']);assert.equal(validation.parameters.profile,'full')
 await post(base+'/flows/validate',{yaml_source:yaml.replace('type: sim.read','type: fabric.apply')},400)
 await post(base+'/flows/validate',{yaml_source:yaml+'\n---\nhello: world'},400)
 const first=await post(base+'/flows',{name:'Revision fixture',yaml_source:yaml,expected_version:0},201)
 const second=await post(base+'/flows/'+first.flow_id+'/versions',{name:first.name,yaml_source:yaml.replaceAll('duration_ms: 100','duration_ms: 150'),expected_version:1},201)
 assert.equal(second.version,2);assert.notEqual(second.content_hash,first.content_hash)
 let revisions=await get(base+'/flows/'+first.flow_id+'/versions');assert.equal(revisions.total,2)
 const detail=await get(base+'/flows/'+first.flow_id+'/versions/'+first.id);assert.equal(detail.revision.id,first.id);assert.equal(detail.created_by,session.user.id)
 const diff=await get(base+'/flows/'+first.flow_id+'/diff?before='+first.id+'&after='+second.id);assert.equal(diff.changes.length,2)
 const rollbackBody={source_version_id:first.id,expected_version:2,change_reason:'Restore verified workflow'},key=crypto.randomUUID()
 const third=await post(base+'/flows/'+first.flow_id+'/rollback',rollbackBody,201,key);assert.equal(third.version,3);assert.equal(third.content_hash,first.content_hash)
 const replay=await post(base+'/flows/'+first.flow_id+'/rollback',rollbackBody,201,key);assert.equal(replay.id,third.id)
 await post(base+'/flows/'+first.flow_id+'/rollback',{...rollbackBody,change_reason:'Different request'},409,key)
 await post(base+'/flows/'+first.flow_id+'/rollback',{...rollbackBody,expected_version:1},409)
 revisions=await get(base+'/flows/'+first.flow_id+'/versions');assert.equal(revisions.total,3)
 const audits=await get(base+'/audits?action=flow.version.rollback');assert.equal(audits.total,1)

 await post(base+'/flows/'+c.owned_flow_id+'/versions',{name:'Owned Kea fixture',yaml_source:yaml,expected_version:1},400)
 await post(base+'/flows/'+c.owned_flow_id+'/rollback',{source_version_id:c.owned_version_id,expected_version:1,change_reason:'Attempt owned rollback'},400)
 const viewerContext=await browser.newContext({baseURL:c.base_url}),viewerPage=await viewerContext.newPage()
 await viewerPage.goto('/login');await viewerPage.locator('#username').fill('flowviewer');await viewerPage.locator('#password').fill(c.password);await viewerPage.locator('#login-form button[type="submit"]').click();await viewerPage.waitForURL('**/projects')
 const viewerSession=await(await viewerContext.request.get('/api/v1/auth/session')).json()
 assert.equal((await viewerContext.request.get(base+'/flows/'+first.flow_id+'/versions')).status(),200)
 assert.equal((await viewerContext.request.post(base+'/flows/'+first.flow_id+'/rollback',{headers:{Origin:c.base_url,'X-CSRF-Token':viewerSession.csrf_token,'Idempotency-Key':crypto.randomUUID()},data:{...rollbackBody,expected_version:3}})).status(),403)
 await viewerContext.close()
 const targets=await post(base+'/sim-targets',{prefix:'flow-contract',count:2,seed:42},201)
 const execution=await post(base+'/executions',{mode:'sim',flow_version_id:third.id,target_ids:targets.items.map(t=>t.id),parameters:{profile:'smoke'},concurrency:2,circuit_breaker_percent:5},201)
 let finished;for(let i=0;i<100;i++){finished=await get(base+'/executions/'+execution.id);if(['success','failed','warning','killed'].includes(finished.execution.state))break;await new Promise(resolve=>setTimeout(resolve,150))}
 assert.equal(finished.execution.state,'success');assert.equal(finished.execution.snapshot.flow.id,third.id);assert.equal(finished.attempts_total,4)
 assert.equal((await context.request.get('/api/v1/projects/'+'f'.repeat(32)+'/flows/'+first.flow_id+'/versions/'+first.id)).status(),404)
 await page.goto(`/p/${c.project_id}/flows?flow=${first.flow_id}&tab=source`);await page.getByRole('heading',{name:'作业流',exact:true}).waitFor();await page.locator('.monaco-editor .view-lines').first().waitFor();assert.match(await page.locator('main').evaluate(e=>getComputedStyle(e).fontFamily),/Intel One Mono/)
 await page.locator('.monaco-editor').first().click();await page.keyboard.press('Control+End');await page.keyboard.insertText('\n# browser edit');await page.getByRole('button',{name:'校验 YAML',exact:true}).click();await page.getByText('校验通过',{exact:true}).waitFor()
 await page.getByRole('button',{name:'发布新版本',exact:true}).click();await page.getByRole('dialog').getByRole('button',{name:'发布新版本',exact:true}).click();await page.getByText('已发布 v4',{exact:true}).waitFor();const fourth=(await get(base+'/flows/'+first.flow_id+'/versions')).items[0];assert.equal(fourth.version,4);assert.match(fourth.yaml_source,/# browser edit/);await page.getByRole('tab',{name:'版本历史',exact:true}).click();await page.locator('.monaco-diff-editor').waitFor();assert.match(page.url(),/tab=revisions/);await page.screenshot({path:process.env.KEEL_WORKSPACE_EVIDENCE_DIR+'/flow-revisions.png',fullPage:true})
 await page.getByRole('button',{name:'运行此版本',exact:true}).click();await page.getByRole('dialog').waitFor();await page.getByRole('checkbox').first().check();await page.getByRole('dialog').locator('button[type=submit]').click();await page.getByRole('dialog').waitFor({state:'hidden'});await page.locator('a[href*="execution="]').waitFor()
 assert.deepEqual(errors,[])
 console.log('PASS real API/PostgreSQL: strict YAML/plugin/input validation, immutable publish/history/diff, atomic audited rollback creates v3, stale version and idempotency conflicts denied, prior versions intact, owned Kea flows immutable through public endpoints, Viewer writes denied')
 console.log('PASS shared worker execution freezes rolled-back flow and explicit target/input set; cross-project revision access denied')
 console.log('PASS real browser: Flow workspace, lazy local Monaco YAML editor/DiffEditor, URL revision tab, Intel font, existing shared execution launch; no browser errors')
}finally{await browser.close()}
