import assert from 'node:assert/strict'
import {readFile,stat} from 'node:fs/promises'
import {pathToFileURL} from 'node:url'
const file=process.env.KEEL_AUTH_BROWSER_CREDENTIALS_FILE;assert(file?.startsWith('/tmp/keel-'));assert.equal((await stat(file)).mode&0o077,0)
const c=JSON.parse(await readFile(file,'utf8'));assert.equal(c.base_url,'http://127.0.0.1:3102')
const {chromium}=await import(pathToFileURL('/tmp/keel-browser-check/node_modules/playwright/index.mjs').href)
const browser=await chromium.launch({headless:true}),context=await browser.newContext({baseURL:c.base_url,viewport:{width:1440,height:1080}}),page=await context.newPage(),errors=[]
page.on('pageerror',e=>errors.push(e.message));const base='/api/v1/projects/'+c.management_project_id+'/ztp'
try{
 await page.goto('/login');await page.locator('#username').fill(c.username);await page.locator('#password').fill(c.password);await page.locator('#login-form button[type="submit"]').click();await page.waitForURL('**/projects')
 for(const endpoint of ['manageability-checks','device-sessions']){const response=await context.request.get(base+'/'+endpoint);assert.equal(response.status(),200);assert.deepEqual((await response.json()).items,[])}
 assert.equal((await context.request.get(base+'/device-sessions/'+'f'.repeat(32))).status(),404)
 assert.equal((await context.request.get('/api/v1/projects/'+'f'.repeat(32)+'/ztp/device-sessions')).status(),404)
 await page.goto(`/p/${c.management_project_id}/ztp`)
 await page.getByRole('heading',{name:'最小管理开通与独立复验',exact:true}).waitFor()
 await page.getByRole('heading',{name:'真实设备开通会话',exact:true}).waitFor()
 await page.getByText('尚无真实设备复验会话；协议夹具不会生成真实可管理状态。',{exact:true}).waitFor()
 assert.equal(await page.getByRole('button',{name:'下载待审批管理清单',exact:true}).isDisabled(),true)
 assert.match(await page.locator('#workspace-content').evaluate(e=>getComputedStyle(e).fontFamily),/Intel One Mono/)
 assert.deepEqual(errors,[])
 await page.screenshot({path:process.env.KEEL_WORKSPACE_EVIDENCE_DIR+'/ztp-completion.png',fullPage:true})
 console.log('PASS real API/DB/browser ZTP management workspace, project-scoped receipts/device sessions, absent evidence cannot become managed, unapproved manifest export blocked, Intel font and foreign-project denial')
}finally{await browser.close()}
