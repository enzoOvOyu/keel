/** Real API/worker/PostgreSQL/browser with a TLS Kea protocol fixture only. */
import assert from 'node:assert/strict'
import {execFileSync} from 'node:child_process'
import {readFile,stat,mkdir,writeFile} from 'node:fs/promises'
import {pathToFileURL} from 'node:url'
const file=process.env.KEEL_AUTH_BROWSER_CREDENTIALS_FILE
assert(file?.startsWith('/tmp/keel-'));assert.equal((await stat(file)).mode&0o077,0)
const c=JSON.parse(await readFile(file,'utf8'));assert.equal(c.base_url,'http://127.0.0.1:3102');assert(c.fault_file?.startsWith('/tmp/keel-'))
const evidence=process.env.KEEL_WORKSPACE_EVIDENCE_DIR;assert(evidence?.startsWith('/tmp/keel-'));await mkdir(evidence,{recursive:true,mode:0o700})
const {chromium}=await import(pathToFileURL('/tmp/keel-browser-check/node_modules/playwright/index.mjs').href)
const browser=await chromium.launch({headless:true}),context=await browser.newContext({baseURL:c.base_url,viewport:{width:1440,height:1080}}),page=await context.newPage(),errors=[]
page.on('pageerror',e=>errors.push(e.message))
const base='/api/v1/projects/'+c.management_project_id
const wait=ms=>new Promise(resolve=>setTimeout(resolve,ms))
async function poll(fn,test){for(let i=0;i<100;i++){const value=await fn();if(test(value))return value;await wait(200)}throw new Error('Timed out waiting for durable execution evidence')}
try{
 await page.goto('/login');await page.locator('#username').fill(c.username);await page.locator('#password').fill(c.password);await page.locator('#login-form button[type="submit"]').click();await page.waitForURL('**/projects')
 const session=await(await context.request.get('/api/v1/auth/session')).json()
 const headers={Origin:c.base_url,'X-CSRF-Token':session.csrf_token}
 async function post(path,data,expected=200,key=crypto.randomUUID()){const r=await context.request.post(path,{headers:{...headers,'Idempotency-Key':key},data});assert.equal(r.status(),expected,'API operation '+path);return r.json()}
 const approverName='kea-reviewer-'+crypto.randomUUID().slice(0,8),pw=crypto.randomUUID()+'-Fixture!'
 await post('/api/v1/admin/users',{username:approverName,display_name:'Kea fixture reviewer',password:pw,is_admin:false},201)
 await post(base+'/members',{username:approverName,role:'Admin'},201)
 const reviewer=await browser.newContext({baseURL:c.base_url});const pre=await(await reviewer.request.get('/api/v1/auth/csrf')).json()
 const login=await reviewer.request.post('/api/v1/auth/login',{headers:{Origin:c.base_url,'X-CSRF-Token':pre.csrf_token},data:{username:approverName,password:pw,method:'local'}});assert.equal(login.status(),200)
 const reviewerSession=await(await reviewer.request.get('/api/v1/auth/session')).json()
 const manifest=await(await context.request.get(base+'/ztp/isolation-script')).json()
 const media=await post(base+'/media',{type:'ztp_script',name:'Kea fixture isolation',version:'isolation-fixture-v1',sha256:manifest.sha256,size:manifest.size,arch:'noarch',os_scope:manifest.os_scope,manifest_ref:'generated:isolation-v1'},201)
 await post(base+'/media/'+media.id+'/transition',{state:'verified'})
 const approved=await reviewer.request.post(base+'/media/'+media.id+'/transition',{headers:{Origin:c.base_url,'X-CSRF-Token':reviewerSession.csrf_token},data:{state:'approved'}});assert.equal(approved.status(),200);await reviewer.close()
 await page.goto(`/p/${c.management_project_id}/ztp`)
 const planning=page.getByRole('region',{name:'N-S2 管理地址与批次规划'}),environment=page.getByRole('region',{name:'N-S3 环境就绪自检'}),kea=page.getByRole('region',{name:'Kea 配置作业'})
 await planning.getByText('导入与登记批准基线',{exact:true}).click();await page.locator('#management-baseline-file').setInputFiles('internal/fabricboot/testdata/management-baseline-v1.json')
 const imported=page.waitForResponse(r=>r.url().endsWith('/network-baselines')&&r.request().method()==='POST');await planning.getByRole('button',{name:'导入为草稿',exact:true}).click();assert.equal((await imported).status(),201)
 await page.locator('#management-customer-approval').fill('fixture:customer');await page.locator('#management-network-approval').fill('fixture:network');await planning.getByRole('button',{name:'登记批准证据',exact:true}).click();await planning.getByText('基线批准记录已保存',{exact:true}).waitFor()
 await planning.getByRole('button',{name:'生成预览',exact:true}).click();await planning.getByText('dc1-p1-leaf-r1-2',{exact:true}).waitFor();await planning.getByRole('button',{name:'校验并保存规划',exact:true}).click();await planning.getByText(/^已保存 ·/).waitFor()
 // Staging is rejected until actual protocol probes have produced fresh evidence.
 const batch=await page.locator('#environment-batch').inputValue()
 const denied=await context.request.post(base+'/ztp/kea-deployments/stage',{headers,data:{batch_ref:batch,policy_media_id:media.id}});assert.equal(denied.status(),409)
 await environment.getByRole('button',{name:'执行环境自检',exact:true}).click();await environment.getByText('环境探针通过',{exact:false}).waitFor()
 await post(base+'/ztp/environment-checks',{batch_ref:batch,boot_channel:'143'})
 await page.locator('#kea-live-policy').selectOption(media.id)
 async function stage(){const pending=page.waitForResponse(r=>r.url().endsWith('/kea-deployments/stage'));await kea.getByRole('button',{name:'读取并预检 Kea 变更',exact:true}).click();const response=await pending;const body=await response.json();assert.equal(response.status(),201,body.code??'Kea stage');return body}
 async function execute(job){const result=await post(base+'/ztp/kea-deployments/'+job.id+'/execute',undefined,201,job.id);return result}
 function verifyRoute(expected){try{execFileSync('go',['test','./internal/platform','-run','^TestLive239WorkerRoute$','-count=1'],{env:{...process.env,KEEL_ROUTE_EXPECT:expected},stdio:'pipe'})}catch(e){throw new Error(e.stdout?.toString()??'Route verification failed')}}
 const job=await stage();assert(!JSON.stringify(job).includes('fixture-only-secret'))
 const running=await execute(job);assert.equal(running.mode,'kea');assert.equal(running.snapshot.targets[0].mode,'kea')
 const detail=await poll(async()=>await(await context.request.get(base+'/executions/'+running.id)).json(),d=>d.execution?.state==='success')
 assert.equal(detail.attempts[0].result.output.config_persisted,true);assert.equal(detail.attempts[0].result.output.dora_verified,true);assert.equal(detail.attempts[0].result.output.device_managed,false)
 assert(!JSON.stringify(detail).includes('fixture-only-secret'))
 const events=await(await context.request.get(base+'/executions/'+running.id+'/events')).json();assert.equal(events.mode,'kea')
 const replay=await post(base+'/ztp/kea-deployments/'+job.id+'/execute',undefined,201,job.id);assert.equal(replay.id,running.id)
 const generic=await context.request.post(base+'/executions',{headers:{...headers,'Idempotency-Key':crypto.randomUUID()},data:{mode:'sim',flow_version_id:running.flow_version_id,target_ids:[running.snapshot.targets[0].id],parameters:{}}});assert.notEqual(generic.status(),201)
 await kea.getByText('DORA 已验证／引导路由就绪',{exact:true}).waitFor()
 const verified=await(await context.request.get(base+'/ztp/kea-deployments')).json();const proof=verified.deployments.find(v=>v.id===job.id);assert.equal(proof.state,'boot_ready');assert.equal(proof.dora.address,'192.0.2.250');assert.equal(proof.dora.option_143_uris.length,1);assert.equal(proof.dora.transcript_sha256.length,64);verifyRoute('ready')
 const failedDora=await stage();await writeFile(c.fault_file,'bad-dora');const failedRun=await execute(failedDora)
 await poll(async()=>await(await context.request.get(base+'/executions/'+failedRun.id)).json(),d=>d.execution?.state==='success')
 const rejectedProof=await(await context.request.get(base+'/ztp/kea-deployments')).json();const rejectedJob=rejectedProof.deployments.find(v=>v.id===failedDora.id);assert.equal(rejectedJob.state,'config_persisted');assert.equal(rejectedJob.verification_code,'dora_failed');assert.equal(rejectedJob.dora,undefined);verifyRoute('not_ready');await writeFile(c.fault_file,'')
 console.log('PASS approved planning/N-S3/media admission, immutable staging and shared Execution/worker receipt')
 console.log('PASS wire DORA transcript for 239+143, dedicated canary and foreign-option rejection; persisted configuration alone cannot open a route')
 console.log('PASS config-test/set/write/full readback, queue idempotency, mode separation and no raw configuration secrets in API evidence')
 const unknown=await stage();await writeFile(c.fault_file,'drop-set');const second=await execute(unknown)
 await poll(async()=>await(await context.request.get(base+'/ztp/kea-deployments')).json(),v=>v.deployments?.some(x=>x.id===unknown.id&&x.state==='unknown'))
 await poll(async()=>await(await context.request.get(base+'/executions/'+second.id)).json(),v=>v.execution?.state==='reconciling');verifyRoute('quarantined')
 const third=await stage();const blocked=await execute(third);await wait(2500)
 const after=await(await context.request.get(base+'/executions/'+blocked.id)).json();assert.equal(after.attempts[0].state,'pending')
 const commands=(await readFile(c.command_log,'utf8')).trim().split('\n');assert.equal(commands.filter(v=>v==='config-set').length,3)
 await page.evaluate(()=>window.scrollTo(0,0));await page.screenshot({path:evidence+'/kea-deployments.png',fullPage:true})
 assert.deepEqual(errors,[])
 console.log('PASS ambiguous config-set produces UNKNOWN/reconciling, retains controller resource lock and prevents another write')
}finally{await browser.close()}
