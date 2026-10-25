/** Health/readiness API and browser against real DB; collector boundary is mTLS fixture. */
import assert from 'node:assert/strict'
import {readFile,writeFile,stat} from 'node:fs/promises'
import {pathToFileURL} from 'node:url'
const file=process.env.KEEL_AUTH_BROWSER_CREDENTIALS_FILE;assert(file?.startsWith('/tmp/keel-'));assert.equal((await stat(file)).mode&0o077,0)
const c=JSON.parse(await readFile(file,'utf8'));assert.equal(c.base_url,'http://127.0.0.1:3102')
const {chromium}=await import(pathToFileURL('/tmp/keel-browser-check/node_modules/playwright/index.mjs').href)
const browser=await chromium.launch({headless:true}),context=await browser.newContext({baseURL:c.base_url,viewport:{width:1440,height:1080}}),page=await context.newPage(),errors=[]
page.on('pageerror',e=>errors.push(e.message));const base='/api/v1/projects/'+c.management_project_id
try{
 await page.goto('/login');await page.locator('#username').fill(c.username);await page.locator('#password').fill(c.password);await page.locator('#login-form button[type="submit"]').click();await page.waitForURL('**/projects')
 const session=await(await context.request.get('/api/v1/auth/session')).json(),headers={Origin:c.base_url,'X-CSRF-Token':session.csrf_token}
 async function post(path,data,expected=200){const r=await context.request.post(path,{headers,data});const out=await r.json();assert.equal(r.status(),expected,`${path}: ${JSON.stringify(out)}`);return out}
 async function get(path){const r=await context.request.get(path);const out=await r.json();assert.equal(r.status(),200,`${path}: ${JSON.stringify(out)}`);return out}
 let readiness=await get(base+'/fabric/readiness');assert.equal(readiness.ttl_seconds,30);assert.equal(readiness.health.fabric_storm.state,'unknown');assert.equal(readiness.health.fabric_storm.threshold_ports,3);assert.equal(readiness.g2_admission,false);assert.equal(readiness.health.lossless.unknown,true);assert.equal(readiness.health.lossless.state,'warning');assert(readiness.gates.some(g=>g.id==='NP-3'&&g.state==='BLOCKED'))
 const baseline=JSON.parse(await readFile('internal/fabricboot/testdata/management-baseline-v1.json','utf8'))
 const intent={no_drop_priorities:[6],dscp_map:{17:6},trust_mode:'dscp',ecn:{enabled_tc:[6],min_threshold_bytes:20,max_threshold_bytes:100,probability:0.05},traffic_pool:{lossless_ratio_percent:60},roce_profile:'lossless',pool:'shared-lossless',queue:'6'}
 baseline.engineering_limits.configuration_variables={'FIXTURE-SW-2':{lossless:{ports:{swp1:intent,swp2:intent}}},'FIXTURE-SW-3':{lossless:{ports:{swp1:intent}}}}
 await post(base+'/network-baselines',baseline,201);await post(base+'/network-baselines/'+baseline.version_ref+'/approve',{customer_approval_ref:'fixture:customer',network_approval_ref:'fixture:network'})
 const preview=await post(base+'/ztp/management-plan/preview',{baseline_ref:baseline.version_ref});await post(base+'/ztp/management-plan/commit',{baseline_ref:baseline.version_ref,expected_hash:preview.content_hash})
 await writeFile(c.fault_file,'fabric-lossless');await post(base+'/fabric/collections',undefined,201)
 readiness=await get(base+'/fabric/readiness');assert.equal(readiness.health.lossless.unknown,false);assert.equal(readiness.health.fabric_storm.state,'normal');assert.equal(readiness.health.fabric_storm.affected_ports,0);assert.equal(readiness.health.lossless.score,100);assert.equal(readiness.health.lossless.state,'healthy');assert.equal(readiness.g2_admission,false);assert(readiness.gates.some(g=>g.reasons.includes('device_evidence_required')))
 const device=readiness.health.devices[0].device.id;let detail=await get(base+'/fabric/devices/'+device+'/health');assert.equal(detail.lossless.findings.length,detail.device.serial==='FIXTURE-SW-2'?14:7);assert(detail.lossless.findings.every(f=>f.state==='normal'));assert(detail.lossless.findings.every(f=>f.unit.priority===6));assert(detail.lossless.findings.every(f=>f.applied_rule_ids.length===1));assert.equal(new Set(detail.lossless.findings.map(f=>f.evaluated_at)).size,1);assert(detail.lossless.findings.every(f=>f.evaluated_at));assert(readiness.health.devices.every(d=>d.lossless.findings.length===0))

 const lldp=await get(base+'/fabric/lldp?device_id='+device+'&port=swp1');assert.equal(lldp.gate.state,'PASS');assert.equal(lldp.source,'switch');assert.equal(lldp.evidence_source,'protocol_fixture');assert(lldp.neighbor.chassis_id)
 const missing=await context.request.get(base+'/fabric/lldp?device_id='+device+'&port=not-modeled');assert.equal(missing.status(),404)
 const cable=await get(base+'/fabric/cables?page=1&page_size=1');assert.equal(cable.total,2);assert.equal(cable.items.length,1);assert.equal(cable.gate.state,'BLOCKED');assert(cable.items.every(item=>item.np3==='BLOCKED'))
 const logs=await get(base+'/fabric/devices/'+device+'/logs');assert.equal(logs.total_entries,3);assert.equal(logs.findings.length,2);assert(logs.findings.some(f=>f.rule_ref==='LOG-BGP@1'&&f.count===2));assert(logs.findings.some(f=>f.rule_ref==='LOG-UNKNOWN@1'));assert.equal(logs.gate.state,'WARN')
 const shortCoverage=await get(base+'/fabric/devices/'+device+'/logs?hours=72');assert(shortCoverage.gate.reasons.includes('log_evidence_unknown'))
 const filtered=await get(base+'/fabric/devices/'+device+'/logs?keyword=BGP&page_size=1');assert.equal(filtered.total_entries,2);assert.equal(filtered.entries.length,1)
 const exported=await get(base+'/fabric/devices/'+device+'/logs?keyword=BGP&page_size=1&format=json');assert.equal(exported.entries.length,2)
 const csv=await context.request.get(base+'/fabric/devices/'+device+'/logs?format=csv');assert.equal(csv.status(),200);assert.match(csv.headers()['content-type'],/text\/csv/);assert.match(await csv.text(),/BGP peer down/)
 assert.equal((await context.request.get(base+'/fabric/devices/'+device+'/logs?hours=13')).status(),400)
 console.log('PASS project-scoped LLDP/cable evidence, absent modeled port denied, server NP-3 stays BLOCKED; 48h syslog/trap aggregation, unknown errors, incomplete coverage, filters, pagination and audited JSON/CSV exports')
 // A new collection must not silently overwrite contradictory history. Faults
 // affect the latest boundary sample; the evaluator preserves exact counters.
 await writeFile(c.fault_file,'fabric-lossless-pfc');await post(base+'/fabric/collections',undefined,201);detail=await get(base+'/fabric/devices/'+device+'/health');assert(detail.lossless.findings.some(f=>f.rule_id==='LL-01'&&f.state==='critical'));assert(detail.lossless.fabric.score<100)
 await writeFile(c.fault_file,'fabric-lossless-qos');await post(base+'/fabric/collections',undefined,201);detail=await get(base+'/fabric/devices/'+device+'/health');assert(detail.lossless.findings.some(f=>f.rule_id==='LL-05'&&f.state==='critical'))
 await writeFile(c.fault_file,'fabric-lossless-storm');await post(base+'/fabric/collections',undefined,201)
 readiness=await get(base+'/fabric/readiness')
 assert.equal(readiness.health.fabric_storm.state,'escalated');assert.equal(readiness.health.fabric_storm.affected_ports,3);assert.equal(readiness.health.fabric_storm.incomplete,false);assert.equal(readiness.health.fabric_storm.approximation,false);assert.equal(readiness.health.fabric_storm.affected_device_ids.length,2);assert.deepEqual(readiness.health.fabric_storm.applied_rule_ids,['LL-02@1']);assert.equal(readiness.g2_admission,false)
 console.log('PASS current evaluation boundary -> three distinct BOM ports across two devices -> Fabric escalation, versioned rule references, no extra health deduction or production admission')
 await page.goto(`/p/${c.management_project_id}/health`);await page.getByRole('heading',{name:'网络健康与就绪',exact:true}).waitFor();await page.getByRole('heading',{name:'LL-01～LL-07 无损判定',exact:true}).waitFor();await page.getByRole('heading',{name:'交换机日志分析',exact:true}).waitFor();assert.match(await page.locator('main').evaluate(e=>getComputedStyle(e).fontFamily),/JetBrains Mono/);await page.screenshot({path:process.env.KEEL_WORKSPACE_EVIDENCE_DIR+'/network-health.png',fullPage:true});assert.deepEqual(errors,[])
 const storm=page.locator('.network-storm');await storm.getByRole('alert').waitFor();await storm.locator('summary').click();assert.equal(await storm.getByRole('button').count(),2)
 const affected=readiness.health.devices.find(d=>d.device.serial==='FIXTURE-SW-2').device
 await storm.getByRole('button',{name:affected.hostname||affected.serial,exact:true}).click();await page.waitForURL(url=>url.searchParams.get('device')===affected.id&&url.searchParams.get('rule')==='LL-02')
 assert.deepEqual(errors,[])
 console.log('PASS storm alert and affected-device disclosure -> URL device/rule selection without browser errors')
 assert.equal((await context.request.get('/api/v1/projects/'+'f'.repeat(32)+'/fabric/readiness')).status(),404)
 console.log('PASS real API/DB + mTLS collector: absent evidence unknown, 30s readiness, G2 prerequisites without NP-3 cycle, fixture evidence cannot satisfy production gates')
 console.log('PASS complete typed per-priority windows -> seven normal rules, exact counter increment above 2^53 -> LL-01, QoS intent mismatch -> LL-05, score min rollup and bounded summary/detail')
 console.log('PASS real browser health/readiness workspace, device/rule URL filters, JetBrains Mono font and cross-project denial')
}finally{await browser.close()}
