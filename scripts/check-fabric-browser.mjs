/** Network-side collection: real API/PostgreSQL/browser, external mTLS fixture. */
import assert from 'node:assert/strict'
import {readFile,writeFile,stat} from 'node:fs/promises'
import {pathToFileURL} from 'node:url'
const file=process.env.KEEL_AUTH_BROWSER_CREDENTIALS_FILE;assert(file?.startsWith('/tmp/keel-'));assert.equal((await stat(file)).mode&0o077,0)
const c=JSON.parse(await readFile(file,'utf8'));assert.equal(c.base_url,'http://127.0.0.1:3102')
const {chromium}=await import(pathToFileURL('/tmp/keel-browser-check/node_modules/playwright/index.mjs').href)
const browser=await chromium.launch({headless:true}),context=await browser.newContext({baseURL:c.base_url,viewport:{width:1440,height:1080}}),page=await context.newPage(),errors=[]
page.on('pageerror',e=>errors.push(e.message))
const base='/api/v1/projects/'+c.management_project_id
try{
 await page.goto('/login');await page.locator('#username').fill(c.username);await page.locator('#password').fill(c.password);await page.locator('#login-form button[type="submit"]').click();await page.waitForURL('**/projects')
 const session=await(await context.request.get('/api/v1/auth/session')).json(),headers={Origin:c.base_url,'X-CSRF-Token':session.csrf_token}
 const latest=await(await context.request.get(base+'/fabric/collections/latest')).json();assert.equal(latest.configured,true);assert.equal(latest.report,null);assert.equal(latest.inventory.length,2)
 await page.goto(`/p/${c.management_project_id}/fabric`);await page.getByRole('button',{name:'读取采集快照',exact:true}).waitFor()
 const collection=page.waitForResponse(r=>r.url().endsWith('/fabric/collections')&&r.request().method()==='POST');await page.getByRole('button',{name:'读取采集快照',exact:true}).click();const response=await collection,report=await response.json();assert.equal(response.status(),201,JSON.stringify(report));assert.equal(report.evidence_source,'protocol_fixture');assert.equal(report.np3,'BLOCKED');assert.equal(report.devices.length,2);assert.equal(report.findings.length,2);assert.equal(report.findings.filter(f=>f.verdict==='precheck_pass').length,1);assert.equal(report.findings.filter(f=>f.verdict==='pending'&&f.reason==='server_side_pending').length,1)
 await page.getByText('交换机互联预校验通过',{exact:true}).waitFor();await page.getByRole('button',{name:'fixture-2',exact:true}).click();const drawer=page.getByRole('dialog',{name:'网络设备详情'});await drawer.waitFor();assert.equal(await drawer.getByRole('tab').count(),8)
 await drawer.getByRole('tab',{name:'运行',exact:true}).click();await drawer.getByText('FIXTURE-SW-3 · swp1',{exact:true}).waitFor()
 await drawer.getByRole('tab',{name:'光模块',exact:true}).click();await drawer.getByText('fixture-optic',{exact:false}).waitFor();assert.match(await drawer.textContent(),/未知/)
 assert.match(await drawer.evaluate(e=>getComputedStyle(e).fontFamily),/JetBrains Mono/);assert.notEqual(await drawer.evaluate(e=>getComputedStyle(e).backgroundColor),'rgba(0, 0, 0, 0)')
 await page.screenshot({path:process.env.KEEL_WORKSPACE_EVIDENCE_DIR+'/fabric-drawer.png',fullPage:true});await drawer.getByRole('button',{name:'关闭详情',exact:true}).click();assert(!new URL(page.url()).searchParams.get('device'))
 const portFault=await writeFile(c.fault_file,'fabric-wrong-port');void portFault
 const wrong=await context.request.post(base+'/fabric/collections',{headers});assert.equal(wrong.status(),201);const wrongReport=await wrong.json();assert(wrongReport.findings.some(f=>f.issue_type==='T2'&&f.verdict==='anomaly'))
 for(const fault of ['fabric-foreign-device','fabric-stale']){await writeFile(c.fault_file,fault);const rejected=await context.request.post(base+'/fabric/collections',{headers});assert.equal(rejected.status(),502,fault);const retained=await(await context.request.get(base+'/fabric/collections/latest')).json();assert.equal(retained.report.id,wrongReport.id)}
 await writeFile(c.fault_file,'fabric-no-data');const empty=await context.request.post(base+'/fabric/collections',{headers});assert.equal(empty.status(),201);const missing=await empty.json();assert(missing.devices.every(d=>d.state==='unknown'));assert(missing.findings.every(f=>f.verdict==='pending'));assert.equal(missing.np3,'BLOCKED')
 await writeFile(c.fault_file,'');await context.request.post(base+'/fabric/collections',{headers})
 const name='fabric-viewer-'+crypto.randomUUID().slice(0,8),password=crypto.randomUUID()+'-Fixture!'
 assert.equal((await context.request.post('/api/v1/admin/users',{headers,data:{username:name,display_name:'Fabric viewer',password,is_admin:false}})).status(),201)
 assert.equal((await context.request.post(base+'/members',{headers,data:{username:name,role:'Viewer'}})).status(),201)
 const viewer=await browser.newContext({baseURL:c.base_url}),csrf=await(await viewer.request.get('/api/v1/auth/csrf')).json();assert.equal((await viewer.request.post('/api/v1/auth/login',{headers:{Origin:c.base_url,'X-CSRF-Token':csrf.csrf_token},data:{username:name,password,method:'local'}})).status(),200)
 const vs=await(await viewer.request.get('/api/v1/auth/session')).json();assert.equal((await viewer.request.get(base+'/fabric/collections/latest')).status(),200);assert.equal((await viewer.request.post(base+'/fabric/collections',{headers:{Origin:c.base_url,'X-CSRF-Token':vs.csrf_token}})).status(),403)
 assert.equal((await viewer.request.get('/api/v1/projects/'+'f'.repeat(32)+'/fabric/collections/latest')).status(),404);await viewer.close()
 const neName='fabric-ne-'+crypto.randomUUID().slice(0,8);assert.equal((await context.request.post('/api/v1/admin/users',{headers,data:{username:neName,display_name:'Fabric NE',password,is_admin:false}})).status(),201)
 assert.equal((await context.request.post(base+'/members',{headers,data:{username:neName,role:'NE'}})).status(),201)
 const ne=await browser.newContext({baseURL:c.base_url}),ncsrf=await(await ne.request.get('/api/v1/auth/csrf')).json();assert.equal((await ne.request.post('/api/v1/auth/login',{headers:{Origin:c.base_url,'X-CSRF-Token':ncsrf.csrf_token},data:{username:neName,password,method:'local'}})).status(),200);const ns=await(await ne.request.get('/api/v1/auth/session')).json();assert(ns.permissions.includes('fabric:collect'));assert.equal((await ne.request.post(base+'/fabric/collections',{headers:{Origin:c.base_url,'X-CSRF-Token':ns.csrf_token}})).status(),201);await ne.close()
 await page.reload();await page.getByRole('button',{name:'fixture-2',exact:true}).waitFor();await page.screenshot({path:process.env.KEEL_WORKSPACE_EVIDENCE_DIR+'/fabric-page.png',fullPage:true});assert.deepEqual(errors,[])
 console.log('PASS mTLS collector -> typed immutable switch snapshots -> real PostgreSQL/API -> Fabric page/8-tab drawer')
 console.log('PASS reciprocal switch LLDP, server pending/NP-3 BLOCKED, wrong-port T2, absent evidence unknown, stale/foreign sample rejection without replacing evidence')
 console.log('PASS server-side DDM unknown without catalog thresholds, source labels, JetBrains Mono font/theme surface, Viewer read-only, NE retest permission and cross-project denial')
}finally{await browser.close()}
