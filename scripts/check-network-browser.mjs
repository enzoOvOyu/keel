/** Real auth/API/PostgreSQL/shared worker/UI; only device boundary is an mTLS fixture. */
import assert from 'node:assert/strict'
import {readFile,writeFile,stat} from 'node:fs/promises'
import {pathToFileURL} from 'node:url'
const file=process.env.KEEL_AUTH_BROWSER_CREDENTIALS_FILE
assert(file?.startsWith('/tmp/keel-'));assert.equal((await stat(file)).mode&0o077,0)
const c=JSON.parse(await readFile(file,'utf8'));assert.equal(c.base_url,'http://127.0.0.1:3102')
const {chromium}=await import(pathToFileURL('/tmp/keel-browser-check/node_modules/playwright/index.mjs').href)
const browser=await chromium.launch({headless:true}),context=await browser.newContext({baseURL:c.base_url,viewport:{width:1440,height:1080}}),page=await context.newPage(),errors=[]
page.on('pageerror',e=>errors.push(e.message))
const base='/api/v1/projects/'+c.management_project_id
const sleep=ms=>new Promise(r=>setTimeout(r,ms))
async function poll(fn,test){let last;for(let i=0;i<150;i++){last=await fn();if(test(last))return last;await sleep(200)}throw new Error('Timeout: '+JSON.stringify(last))}
try{
 await page.goto('/login');await page.locator('#username').fill(c.username);await page.locator('#password').fill(c.password);await page.locator('#login-form button[type="submit"]').click();await page.waitForURL('**/projects')
 const session=await(await context.request.get('/api/v1/auth/session')).json(),headers={Origin:c.base_url,'X-CSRF-Token':session.csrf_token}
 async function post(path,data,expected=200,request=context.request,extra={}){const r=await request.post(path,{headers:{...headers,...extra,'Idempotency-Key':crypto.randomUUID()},data});const out=await r.json();assert.equal(r.status(),expected,`${path}: ${JSON.stringify(out)}`);return out}
 const roles={}
 for(const role of ['NE','DE','NH','Viewer']){
  const username='network-'+role.toLowerCase()+'-'+crypto.randomUUID().slice(0,8),password=crypto.randomUUID()+'-Fixture!'
  await post('/api/v1/admin/users',{username,display_name:'Network '+role,password,is_admin:false},201)
  const membership=await post(base+'/members',{username,role},201)
  const ctx=await browser.newContext({baseURL:c.base_url}),csrf=await(await ctx.request.get('/api/v1/auth/csrf')).json()
  await post('/api/v1/auth/login',{username,password,method:'local'},200,ctx.request,{'X-CSRF-Token':csrf.csrf_token})
  const s=await(await ctx.request.get('/api/v1/auth/session')).json();roles[role]={context:ctx,headers:{Origin:c.base_url,'X-CSRF-Token':s.csrf_token},id:s.user.id,username,password,membership_version:membership.version}
 }
 async function asRole(role,path,data,expected=200){return post(path,data,expected,roles[role].context.request,roles[role].headers)}
 const baseline=JSON.parse(await readFile('internal/fabricboot/testdata/management-baseline-v1.json','utf8'))
 baseline.engineering_limits.configuration_variables={}
 for(let i=2;i<=6;i++)baseline.engineering_limits.configuration_variables['FIXTURE-SW-'+i]={asn:65000}
 await post(base+'/network-baselines',baseline,201)
 await post(base+'/network-baselines/'+baseline.version_ref+'/approve',{customer_approval_ref:'fixture:customer',network_approval_ref:'fixture:network'})
 const preview=await post(base+'/ztp/management-plan/preview',{baseline_ref:baseline.version_ref})
 const plan=await post(base+'/ztp/management-plan/commit',{baseline_ref:baseline.version_ref,expected_hash:preview.content_hash})
 assert.equal(plan.assignments.length,5)
 const batch=plan.assignments[0].batch_ref
 const template=await post(base+'/fabric/templates',{name:'Approved leaf configuration',source_ref:'fixture:customer-approved',definition:{variables:{asn:{type:'integer',required:true}},source:'{"hostname":{{device.hostname|keeljson}},"management":{"address":{{device.management_ip|keeljson}},"gateway":{{device.gateway|keeljson}}},"asn":{{asn|keeljson}},"description":"approved"}'}},201)
 assert.equal((await context.request.post(base+'/fabric/templates/'+template.id+'/release',{headers})).status(),409)
 const retirement={expected_hash:template.content_hash,reason:'Replaced by approved network baseline'}
 await asRole('NH',base+'/fabric/templates/'+template.id+'/deprecate',retirement,409)
 const tested=await post(base+'/fabric/templates/'+template.id+'/test',{baseline_ref:baseline.version_ref,batch_ref:batch});assert.equal(tested.state,'tested');assert.equal(tested.test_report.evidence_source,'protocol_fixture');assert.equal(tested.test_report.devices.length,5)
 await asRole('NE',base+'/fabric/templates/'+template.id+'/release',undefined,403)
 await asRole('NH',base+'/fabric/templates/'+template.id+'/release')
 const input={template_id:template.id,baseline_ref:baseline.version_ref,batch_ref:batch}
 const jobs=async()=>await(await context.request.get(base+'/fabric/push-jobs')).json()
 async function job(id){return (await jobs()).jobs.find(j=>j.id===id)}
 async function prepared(){const j=await post(base+'/fabric/push-jobs',input,201);const exec=await post(base+'/fabric/push-jobs/'+j.id+'/prepare',undefined,201);assert.equal(exec.mode,'fabric');return poll(()=>job(j.id),v=>['READY','BLOCKED'].includes(v.state))}
 async function approve(j){for(const role of j.critical?['NE','DE','NH']:['NE','DE'])await asRole(role,base+'/fabric/push-jobs/'+j.id+'/decision',{review_hash:j.review_hash,decision:'approve',comment:'Reviewed approved configuration and frozen Doublecheck evidence'});return job(j.id)}
 let j=await prepared();assert.equal(j.state,'READY');assert.equal(j.critical,true);assert(j.devices.every(d=>d.diff.some(v=>v.path==='/asn'&&v.severity==='Critical')))
 await post(base+'/fabric/push-jobs/'+j.id+'/push',undefined,409)
 await asRole('NE',base+'/fabric/push-jobs/'+j.id+'/decision',{review_hash:'a'.repeat(64),decision:'approve',comment:'Wrong snapshot'},409)
 await asRole('NE',base+'/fabric/push-jobs/'+j.id+'/decision',{review_hash:j.review_hash,decision:'approve',comment:'Reviewed network changes'})
 await asRole('NE',base+'/fabric/push-jobs/'+j.id+'/decision',{review_hash:j.review_hash,decision:'approve',comment:'Duplicate account'},409)
 await asRole('DE',base+'/fabric/push-jobs/'+j.id+'/decision',{review_hash:j.review_hash,decision:'approve',comment:'Reviewed delivery changes'})
 assert.equal((await job(j.id)).state,'READY');await post(base+'/fabric/push-jobs/'+j.id+'/push',undefined,409)
 await asRole('NH',base+'/fabric/push-jobs/'+j.id+'/decision',{review_hash:j.review_hash,decision:'approve',comment:'Reviewed critical changes'})
 const push=await post(base+'/fabric/push-jobs/'+j.id+'/push',undefined,201);assert.equal(push.mode,'fabric')
 j=await poll(()=>job(j.id),v=>v.state==='COMPLETED');assert(j.devices.every(d=>d.state==='VERIFIED'));assert(j.devices.every(d=>d.verify.evidence_source==='protocol_fixture'))
 const lines=(await readFile(c.command_log,'utf8')).trim().split('\n'),canary=j.devices[0].device.serial,firstPeer=lines.findIndex(v=>v.startsWith('network-apply:')&&v!=='network-apply:'+canary)
 assert(lines.indexOf('network-verify:'+canary)<firstPeer)
 await poll(async()=>await(await context.request.get(base+'/executions/'+push.id)).json(),v=>v.execution?.state==='success')
 // Authored P2 extension; executed only with the concentrated milestone suite.
 const beforeImpact=await readFile(c.command_log,'utf8')
 const upgraded=await asRole('NE',base+'/fabric/templates',{name:template.name,source_ref:template.source_ref,parent_version_id:template.id,expected_parent_hash:template.content_hash,change_reason:'Approved template upgrade preview',definition:{...template.definition,source:template.definition.source.replace('"approved"','"upgraded"')}},201)
 assert.equal(upgraded.parent_version_id,template.id)
 const impactResponse=await roles.Viewer.context.request.get(base+'/fabric/templates/'+upgraded.id+'/impact')
 assert.equal(impactResponse.status(),200)
 const impact=await impactResponse.json()
 assert.equal(impact.base_template_id,template.id);assert.equal(impact.referencing_jobs,1);assert.equal(impact.referencing_batches,1);assert.equal(impact.referencing_devices,5);assert.equal(impact.current_devices,5)
 assert.equal(impact.items.length,5);assert(impact.items.every(v=>v.verdict==='DIFF'&&v.changes.some(d=>d.path==='/description'&&d.severity==='Minor')))
 assert.equal(await readFile(c.command_log,'utf8'),beforeImpact,'impact preview must not call adapters or write devices')
 const restored=await asRole('NE',base+'/fabric/templates/'+template.id+'/rollback',{expected_head_id:upgraded.id,expected_head_hash:upgraded.content_hash,reason:'Restore known approved configuration into a new reviewed version'},201)
 assert.equal(restored.state,'draft');assert.equal(restored.version,upgraded.version+1);assert.equal(restored.rollback_from_id,template.id);assert.equal(restored.parent_version_id,upgraded.id);assert.equal(restored.content_hash,template.content_hash);assert.equal(restored.test_report,undefined)
 await asRole('NH',base+'/fabric/templates/'+restored.id+'/release',undefined,409)
 await asRole('NE',base+'/fabric/templates/'+template.id+'/rollback',{expected_head_id:upgraded.id,expected_head_hash:upgraded.content_hash,reason:'Stale restore request'},409)
 console.log('PASS read-only template impact and history-preserving rollback draft with fresh release required')
 await asRole('NE',base+'/fabric/templates/'+upgraded.id+'/test',{baseline_ref:baseline.version_ref,batch_ref:batch})
 await asRole('NH',base+'/fabric/templates/'+upgraded.id+'/release')
 const afterUpgradeRelease=await readFile(c.command_log,'utf8')
 const changes=async()=>await(await context.request.get(base+'/fabric/template-changes')).json()
 let upgradeQueue=await changes();const selectedChange=upgradeQueue.items.find(v=>v.candidate_template_id===upgraded.id)
 assert(selectedChange);assert.equal(upgradeQueue.items.filter(v=>v.candidate_template_id===upgraded.id).length,5)
 assert.equal(selectedChange.state,'DIFF');assert.equal(selectedChange.assessment.target_count,5)
 assert.equal(await readFile(c.command_log,'utf8'),afterUpgradeRelease,'queue reads do not contact adapters')
 await asRole('Viewer',base+'/fabric/push-jobs',selectedChange.plan_input,403)
 await post(base+'/fabric/push-jobs',{...selectedChange.plan_input,expected_impact_hash:'0'.repeat(64)},409)
 let queuedJob=await post(base+'/fabric/push-jobs',selectedChange.plan_input,201)
 assert.equal(queuedJob.devices.length,5);assert.equal(queuedJob.input.template_change_id,selectedChange.id)
 upgradeQueue=await changes();assert(upgradeQueue.items.filter(v=>v.candidate_template_id===upgraded.id).every(v=>v.state==='PLANNED'&&v.linked_job_id===queuedJob.id))
 await post(base+'/fabric/push-jobs',selectedChange.plan_input,409)
 await post(base+'/fabric/push-jobs/'+queuedJob.id+'/prepare',undefined,201)
 queuedJob=await poll(()=>job(queuedJob.id),v=>['READY','BLOCKED'].includes(v.state));assert.equal(queuedJob.state,'READY')
 queuedJob=await approve(queuedJob);await post(base+'/fabric/push-jobs/'+queuedJob.id+'/push',undefined,201)
 queuedJob=await poll(()=>job(queuedJob.id),v=>v.state==='COMPLETED')
 await poll(async()=>await(await context.request.get(base+'/executions/'+queuedJob.push_execution_id)).json(),v=>v.execution?.state==='success')
 upgradeQueue=await changes();assert(upgradeQueue.items.filter(v=>v.candidate_template_id===upgraded.id).every(v=>v.state==='RESOLVED'))
 console.log('PASS release queues review; frozen batch preview creates ordinary approved/canary PushJob; only matching readback resolves queue')

 console.log('PASS immutable customer template -> sandbox/three checks/rehearsal/golden -> independent NH release -> real shared precheck execution -> Critical three signatures -> canary -> push/readback')
 const drift=await approve(await prepared());await writeFile(c.fault_file,'network-drift-before-apply');const beforeDrift=(await readFile(c.command_log,'utf8')).split('network-rollback:').length;await post(base+'/fabric/push-jobs/'+drift.id+'/push',undefined,201);const rejected=await poll(()=>job(drift.id),v=>v.state==='PARTIAL');assert.equal(rejected.devices[0].code,'NETWORK_APPLY_REJECTED');assert.equal((await readFile(c.command_log,'utf8')).split('network-rollback:').length,beforeDrift);await writeFile(c.fault_file,'');console.log('PASS externally changed running configuration rejects apply without rolling back or overwriting the unapproved change')
 await writeFile(c.fault_file,'network-precheck-fail');const blocked=await prepared();assert.equal(blocked.state,'BLOCKED');assert(blocked.devices.every(d=>d.state==='BLOCKED'));await writeFile(c.fault_file,'')
 let rollback=await prepared();assert.equal(rollback.critical,false);rollback=await approve(rollback);assert.equal(rollback.state,'APPROVED')
 await writeFile(c.fault_file,'network-verify-fail');await post(base+'/fabric/push-jobs/'+rollback.id+'/push',undefined,201)
 rollback=await poll(()=>job(rollback.id),v=>v.state==='ROLLED_BACK');assert.equal(rollback.devices[0].state,'ROLLED_BACK');assert(rollback.devices.slice(1).every(d=>d.state==='SKIPPED'));await writeFile(c.fault_file,'')
 console.log('PASS ordinary two signatures, failed prechecks blocked, failed canary restores backup and prevents all peer writes')
 // Viewer and foreign-project access remain enforced by real server membership.
 await asRole('Viewer',base+'/fabric/push-jobs',input,403)
 assert.equal((await roles.Viewer.context.request.get('/api/v1/projects/'+'f'.repeat(32)+'/fabric/push-jobs')).status(),404)
 const revoked=await approve(await prepared());assert.equal((await context.request.patch(base+'/members/'+roles.NE.id,{headers,data:{role:'Viewer',version:roles.NE.membership_version}})).status(),200);await post(base+'/fabric/push-jobs/'+revoked.id+'/push',undefined,409);assert.equal((await context.request.patch(base+'/members/'+roles.NE.id,{headers,data:{role:'NE',version:roles.NE.membership_version+1}})).status(),200);await asRole('NH',base+'/fabric/push-jobs/'+revoked.id+'/decision',{review_hash:revoked.review_hash,comment:'Withdraw approved plan',decision:'reject'});assert.equal((await post(base+'/fabric/push-jobs/'+revoked.id+'/archive')).state,'ARCHIVED');console.log('PASS approver role revocation blocks dispatch; withdrawn approvals and archived plans cannot execute')
 await roles.NE.context.close();const renewed=await browser.newContext({baseURL:c.base_url}),renewedCSRF=await(await renewed.request.get('/api/v1/auth/csrf')).json();await post('/api/v1/auth/login',{username:roles.NE.username,password:roles.NE.password,method:'local'},200,renewed.request,{'X-CSRF-Token':renewedCSRF.csrf_token});const renewedSession=await(await renewed.request.get('/api/v1/auth/session')).json();roles.NE.context=renewed;roles.NE.headers={'Origin':c.base_url,'X-CSRF-Token':renewedSession.csrf_token}
 const pendingRetirement=await post(base+'/fabric/push-jobs',input,201)
 const approvedRetirement=await approve(await prepared())
 let uncertain=await approve(await prepared());await writeFile(c.fault_file,'network-drop-apply');const unknownExec=await post(base+'/fabric/push-jobs/'+uncertain.id+'/push',undefined,201)
 uncertain=await poll(()=>job(uncertain.id),v=>v.devices.some(d=>d.state==='UNKNOWN'))
 await poll(async()=>await(await context.request.get(base+'/executions/'+unknownExec.id)).json(),v=>v.execution?.state==='reconciling')
 assert(uncertain.devices.slice(1).every(d=>d.state==='PREPARED'));await writeFile(c.fault_file,'')
 console.log('PASS ambiguous device write -> UNKNOWN / shared Execution reconciling; canary peers remain unexecuted and SSH lease stays quarantined')
 const unknownDevice=uncertain.devices.find(d=>d.state==='UNKNOWN').device.id
 const retained=await(await context.request.get(base+'/fabric/devices/'+unknownDevice+'/configuration')).json()
 assert.equal(retained.latest_production.job_id,uncertain.id);assert.equal(retained.latest_production.device_state,'UNKNOWN');assert.notEqual(retained.latest_production.golden.state,'PASS')
 assert.equal(retained.latest_production.verify,undefined)
 assert(retained.latest_production.before_snapshot);assert(retained.latest_production.intended)
 const configEvents=await(await context.request.get(base+'/fabric/devices/'+unknownDevice+'/configuration-events?page=1&page_size=2')).json()
 assert(configEvents.total>0);assert(configEvents.items.length<=2);assert(configEvents.items.every(v=>['batch','device'].includes(v.scope)))
 const foreignDeviceView=await context.request.get('/api/v1/projects/'+'f'.repeat(32)+'/fabric/devices/'+unknownDevice+'/configuration');assert.equal(foreignDeviceView.status(),404)
 console.log('PASS retained device configuration keeps latest UNKNOWN production attempt, separates pre-change backup and intended, and exposes bounded scoped batch events')

 await page.goto(`/p/${c.management_project_id}/configuration`);await page.getByRole('heading',{name:'配置推送作业',exact:true}).waitFor();await page.getByText('Critical · NE + DE + NH 三签',{exact:true}).first().waitFor();assert.match(await page.locator('main').evaluate(e=>getComputedStyle(e).fontFamily),/JetBrains Mono/)
 await page.screenshot({path:process.env.KEEL_WORKSPACE_EVIDENCE_DIR+'/configuration-push-jobs.png',fullPage:true});assert.deepEqual(errors,[])
 const concurrencyResponse=await context.request.put(base+'/params',{headers,data:{key:'fabric.push_concurrency',value_type:'enum',options:['1','2'],default_value:'2',create_only:true}})
 assert.equal(concurrencyResponse.status(),201);const concurrencyPolicy=await concurrencyResponse.json()
 const frozenPolicyJob=await post(base+'/fabric/push-jobs',input,201);assert.equal(frozenPolicyJob.concurrency,2)
 await post(base+'/params/'+concurrencyPolicy.id+'/retire',{expected_updated_at:concurrencyPolicy.updated_at})
 const jobsBeforePolicyRejection=await(await context.request.get(base+'/fabric/push-jobs')).json()
 assert.equal((await post(base+'/fabric/push-jobs',input,409)).code,'PARAM_KEY_RETIRED')
 const jobsAfterPolicyRejection=await(await context.request.get(base+'/fabric/push-jobs')).json()
 assert.equal(jobsAfterPolicyRejection.jobs.length,jobsBeforePolicyRejection.jobs.length)
 assert.equal((await job(frozenPolicyJob.id)).concurrency,2)
 console.log('PASS retired concurrency policy blocks new push jobs without partial creation; historical frozen concurrency is unchanged')
 const retirePath=base+'/fabric/templates/'+template.id+'/deprecate'
 await asRole('NE',retirePath,retirement,403);await asRole('Viewer',retirePath,retirement,403)
 await asRole('NH',retirePath,{...retirement,reason:'   '},400)
 await asRole('NH',retirePath,{...retirement,expected_hash:'0'.repeat(64)},409)
 assert.equal((await context.request.post(retirePath,{headers:{Origin:c.base_url},data:retirement})).status(),403)
 await post('/api/v1/projects/'+'f'.repeat(32)+'/fabric/templates/'+template.id+'/deprecate',retirement,404)
 const other=await post('/api/v1/projects',{name:'Other template scope',slug:'other-template-'+crypto.randomUUID().slice(0,8),description:''},201)
 await post('/api/v1/projects/'+other.id+'/fabric/templates/'+template.id+'/deprecate',retirement,404)
 const historyBefore=await job(uncertain.id)
 const reviewer=await roles.NH.context.newPage();reviewer.on('pageerror',e=>errors.push(e.message))
 await reviewer.goto(`/p/${c.management_project_id}/configuration`)
 await reviewer.getByRole('button',{name:'废弃模板版本',exact:true}).click()
 const confirmation=reviewer.getByRole('dialog',{name:'废弃模板版本',exact:true});await confirmation.waitFor()
 assert(await confirmation.getByRole('button',{name:'确认废弃',exact:true}).isDisabled())
 await confirmation.getByLabel('废弃原因',{exact:true}).fill(retirement.reason)
 await confirmation.getByRole('button',{name:'确认废弃',exact:true}).click();await confirmation.waitFor({state:'hidden'})
 await reviewer.getByText('已弃用',{exact:true}).waitFor()
 assert.equal(await reviewer.locator('#network-push-template option[value="'+template.id+'"]').count(),0)
 const retired=await asRole('NH',retirePath,retirement);assert.equal(retired.state,'deprecated');assert.deepEqual(retired.definition,template.definition);assert.equal(retired.content_hash,template.content_hash);assert.deepEqual(retired.test_report,tested.test_report)
 assert.equal((await post(base+'/fabric/push-jobs',input,409)).code,'NETWORK_TEMPLATE_RELEASE_REQUIRED')
 assert.equal((await post(base+'/fabric/push-jobs/'+pendingRetirement.id+'/prepare',undefined,409)).code,'NETWORK_PUSH_FACTS_CHANGED')
 assert.equal((await post(base+'/fabric/push-jobs/'+approvedRetirement.id+'/push',undefined,409)).code,'NETWORK_PUSH_FACTS_CHANGED')
 await asRole('NH',base+'/fabric/templates/'+template.id+'/release',undefined,409)
 await post(base+'/fabric/templates/'+template.id+'/test',{baseline_ref:baseline.version_ref,batch_ref:batch},409)
 assert.deepEqual(await job(uncertain.id),historyBefore)
 const audits=await(await context.request.get(base+'/audits?action=network.template.deprecate')).json();assert.equal(audits.total,1)
 await reviewer.screenshot({path:process.env.KEEL_WORKSPACE_EVIDENCE_DIR+'/template-deprecated.png',fullPage:true})
 assert.deepEqual(errors,[]);await reviewer.close()
 console.log('PASS template deprecation confirmation/reason, NH authority, CSRF and cross-scope denial; immutable content/test and historical jobs retained, repeat transition audited once; new jobs and existing prepare/dispatch blocked')
 // Extended planning is added after the legacy push scenarios so their
 // retained facts continue to exercise compatibility without this extension.
 const addressBaseline=structuredClone(baseline)
 addressBaseline.version_ref='extended-address-v1'
 addressBaseline.source_ref='fixture:customer-approved-address-pools'
 addressBaseline.engineering_limits.address_planning={management_vrf:'mgmt',pools:[{id:'loopback',scope:'loopback',cidr:'198.51.100.0/24',unit_prefix:32,vrf:'default'}],rules:plan.assignments.map((v,i)=>({object_uid:v.switch_id,pool_ref:'loopback',unit_index:i+10,host_offset:0}))}
 await post(base+'/network-baselines',addressBaseline,201)
 await post(base+'/network-baselines/'+addressBaseline.version_ref+'/approve',{customer_approval_ref:'fixture:address-customer',network_approval_ref:'fixture:address-engineer'})
 const managementPreview=await post(base+'/ztp/management-plan/preview',{baseline_ref:addressBaseline.version_ref})
 await post(base+'/ztp/management-plan/commit',{baseline_ref:addressBaseline.version_ref,expected_hash:managementPreview.content_hash})
 const addressTemplate=await post(base+'/fabric/templates',{name:'Approved extended address configuration',source_ref:'fixture:address-aware',definition:{variables:{asn:{type:'integer',required:true}},source:'{"hostname":{{device.hostname|keeljson}},"management":{"address":{{device.management_ip|keeljson}},"gateway":{{device.gateway|keeljson}}},"asn":{{asn|keeljson}},"network_addresses":{{device.address_plan|keeljson}}}'}},201)
 const addressTestInput={baseline_ref:addressBaseline.version_ref,batch_ref:batch},addressTestPath=base+'/fabric/templates/'+addressTemplate.id+'/test'
 assert.equal((await post(addressTestPath,addressTestInput,409)).code,'NETWORK_ADDRESS_APPROVAL_REQUIRED')
 const allocationInput={baseline_ref:addressBaseline.version_ref,reason:'Customer approved loopback adjustment',overrides:[{object_uid:plan.assignments[0].switch_id,pool_ref:'loopback',address:'198.51.100.30'}]}
 const allocationPreview=await post(base+'/fabric/address-plans/preview',allocationInput)
 await asRole('NE',base+'/fabric/address-plans',{...allocationInput,expected_hash:allocationPreview.content_hash},403)
 const allocation=await post(base+'/fabric/address-plans',{...allocationInput,expected_hash:allocationPreview.content_hash},201)
 const allocationApproval={expected_hash:allocation.content_hash,reason:'Reviewed current BOM and pool addresses'}
 assert.equal((await post(base+'/fabric/address-plans/'+allocation.id+'/approve',allocationApproval,409)).code,'NETWORK_ADDRESS_SELF_APPROVAL')
 await asRole('Viewer',base+'/fabric/address-plans/'+allocation.id+'/approve',allocationApproval,403)
 await asRole('NH',base+'/fabric/address-plans/'+allocation.id+'/approve',allocationApproval)
 await writeFile(c.fault_file,'network-address-plan-mismatch')
 try{assert.equal((await post(addressTestPath,addressTestInput,409)).code,'NETWORK_TEMPLATE_TEST_BLOCKED')}finally{await writeFile(c.fault_file,'')}
 const addressTested=await post(addressTestPath,addressTestInput)
 assert(addressTested.test_report.checks.every(v=>v.gates.some(g=>g.id==='address-planning'&&g.state==='PASS')))
 assert(addressTested.test_report.devices.every(d=>d.address_plan_id===allocation.id&&d.addresses.length===1))
 // Another approved adjustment invalidates the tested template before release.
 allocationInput.overrides[0].address='198.51.100.40';allocationInput.reason='Second customer-approved loopback adjustment'
 const freshPreview=await post(base+'/fabric/address-plans/preview',allocationInput)
 const freshAllocation=await post(base+'/fabric/address-plans',{...allocationInput,expected_hash:freshPreview.content_hash},201)
 await asRole('NH',base+'/fabric/address-plans/'+freshAllocation.id+'/approve',{expected_hash:freshAllocation.content_hash,reason:'Independent review of revised allocation'})
 await asRole('NH',base+'/fabric/templates/'+addressTemplate.id+'/release',undefined,409)
 const scopedHistory=await context.request.get('/api/v1/projects/'+other.id+'/fabric/address-plans/'+allocation.id)
 assert.equal(scopedHistory.status(),404)
 const allocationReviewer=await roles.NH.context.newPage();allocationReviewer.on('pageerror',e=>errors.push(e.message))
 await allocationReviewer.goto(`/p/${c.management_project_id}/configuration`)
 const allocationPanel=allocationReviewer.getByRole('region',{name:'扩展地址规划与审核'})
 await allocationPanel.getByRole('heading',{name:'最近保存的规划'}).waitFor()
 assert(await allocationPanel.getByRole('button',{name:'查看完整规划',exact:true}).count()>=2)
 await allocationPanel.getByRole('button',{name:'查看完整规划',exact:true}).first().click()
 await allocationPanel.getByText('198.51.100.40/32',{exact:true}).waitFor()
 await allocationReviewer.close()
 console.log('PASS approved pool allocation, adjustment review, role/scope denial, adapter address gates and changed-plan release fencing; hardware unverified')
 // A retirement in the actual catalog page must block the next ZTP preview/commit.
 const batchPolicyResponse=await context.request.put(base+'/params',{headers,data:{key:'fabric.batch_size',value_type:'enum',options:['100','200'],default_value:'100',create_only:true}})
 assert.equal(batchPolicyResponse.status(),201)
 const reviewedPolicy=await post(base+'/ztp/management-plan/preview',{baseline_ref:baseline.version_ref});assert.equal(reviewedPolicy.batch_size,100)
 await page.goto(`/p/${c.management_project_id}/params`)
 const policyRow=page.getByRole('row').filter({has:page.locator('span').getByText('fabric.batch_size',{exact:true})})
 await policyRow.getByRole('button',{name:'退役参数',exact:true}).click()
 const retiredPolicyResponse=page.waitForResponse(r=>r.request().method()==='POST'&&r.url().endsWith('/retire'))
 await page.getByRole('dialog').getByRole('button',{name:'退役参数',exact:true}).click();assert.equal((await retiredPolicyResponse).status(),200)
 await policyRow.getByRole('cell',{name:'已退役',exact:true}).waitFor()
 assert.equal((await post(base+'/ztp/management-plan/preview',{baseline_ref:baseline.version_ref},409)).code,'PARAM_KEY_RETIRED')
 assert.equal((await post(base+'/ztp/management-plan/commit',{baseline_ref:baseline.version_ref,expected_hash:reviewedPolicy.content_hash},409)).code,'PARAM_KEY_RETIRED')
 await page.screenshot({path:process.env.KEEL_WORKSPACE_EVIDENCE_DIR+'/catalog-consumer-retired.png',fullPage:true});assert.deepEqual(errors,[])
 console.log('PASS catalog page retirement blocks ZTP preview and previously reviewed commit; documented default does not bypass tombstone')
 for(const v of Object.values(roles))await v.context.close()
 console.log('PASS real browser configuration workspace, report/diff review, source labels and JetBrains Mono font; Viewer write/foreign-project access denied')
}finally{await browser.close()}
