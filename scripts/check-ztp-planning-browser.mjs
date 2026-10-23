/** Planning through real API/PostgreSQL/UI; external probes remain unconfigured. */
import assert from 'node:assert/strict'
import {readFile,stat,mkdir} from 'node:fs/promises'
import {pathToFileURL} from 'node:url'
const file=process.env.KEEL_AUTH_BROWSER_CREDENTIALS_FILE
assert(file?.startsWith('/tmp/keel-'));assert.equal((await stat(file)).mode&0o077,0)
const credentials=JSON.parse(await readFile(file,'utf8'))
assert.equal(credentials.base_url,'http://127.0.0.1:3102');assert(credentials.management_project_id)
const evidence=process.env.KEEL_WORKSPACE_EVIDENCE_DIR;assert(evidence?.startsWith('/tmp/keel-'));await mkdir(evidence,{recursive:true,mode:0o700})
const {chromium}=await import(pathToFileURL('/tmp/keel-browser-check/node_modules/playwright/index.mjs').href)
const browser=await chromium.launch({headless:true}),context=await browser.newContext({baseURL:credentials.base_url,viewport:{width:1440,height:1080}}),page=await context.newPage(),errors=[]
page.on('pageerror',error=>errors.push(error.message))
try{
 await page.goto('/login');await page.locator('#username').fill(credentials.username);await page.locator('#password').fill(credentials.password);await page.locator('#login-form button[type="submit"]').click();await page.waitForURL('**/projects')
 await page.goto(`/p/${credentials.management_project_id}/ztp`)
 const planning=page.getByRole('region',{name:'N-S2 管理地址与批次规划'}),environment=page.getByRole('region',{name:'N-S3 环境就绪自检'})
 await planning.getByText('导入与登记批准基线',{exact:true}).click()
 await page.locator('#management-baseline-file').setInputFiles('internal/fabricboot/testdata/management-baseline-v1.json')
 const imported=page.waitForResponse(response=>response.url().endsWith('/network-baselines')&&response.request().method()==='POST')
 await planning.getByRole('button',{name:'导入为草稿',exact:true}).click();assert.equal((await imported).status(),201)
 await page.locator('#management-customer-approval').fill('isolated-fixture:customer-ref');await page.locator('#management-network-approval').fill('isolated-fixture:engineer-ref')
 await planning.getByRole('button',{name:'登记批准证据',exact:true}).click();await planning.getByText('基线批准记录已保存',{exact:true}).waitFor()
 await planning.getByRole('button',{name:'生成预览',exact:true}).click();await planning.getByText('dc1-p1-leaf-r1-2',{exact:true}).waitFor()
 assert.equal(await planning.locator('tbody tr').count(),2)
 await planning.getByRole('button',{name:'校验并保存规划',exact:true}).click();await planning.getByText(/^已保存 ·/).waitFor()
 await environment.getByRole('button',{name:'执行环境自检',exact:true}).click();await environment.getByRole('status').waitFor()
 assert.equal(await environment.locator('tbody tr').count(),4)
 assert.equal(await environment.getByText('未知／未配置',{exact:true}).count(),4)
 assert.equal(await environment.getByText('not_configured',{exact:false}).count(),4)
 assert.equal(await environment.getByText('环境探针通过',{exact:false}).count(),0)
 await page.evaluate(()=>window.scrollTo(0,0))
 await page.screenshot({path:evidence+'/management-environment.png',fullPage:true})
 await page.reload();await planning.getByText(/^已保存 ·/).waitFor();await environment.locator('tbody tr').first().waitFor()
 assert.equal(await planning.locator('tbody tr').count(),2);assert.equal(await environment.getByText('未知／未配置',{exact:true}).count(),4)
 assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth))
 assert.deepEqual(errors,[])
 console.log('PASS real baseline import/approval, BOM-derived preview, immutable save and reload')
 console.log('PASS four unconfigured protocol probes persist as UNKNOWN and do not claim readiness')
 console.log('PASS 1440px planning/environment layout and no browser runtime errors')
}finally{await browser.close()}
