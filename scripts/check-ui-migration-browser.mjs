import assert from 'node:assert/strict'
import {readFile, stat} from 'node:fs/promises'
import {pathToFileURL} from 'node:url'
const file=process.env.KEEL_AUTH_BROWSER_CREDENTIALS_FILE
assert(file?.startsWith('/tmp/keel-ui-'));assert.equal((await stat(file)).mode & 0o077,0)
const c=JSON.parse(await readFile(file,'utf8'));assert.equal(c.base_url,'http://127.0.0.1:3109')
const {chromium}=await import(pathToFileURL('/tmp/keel-browser-check/node_modules/playwright/index.mjs').href)
const browser=await chromium.launch({headless:true}),context=await browser.newContext({baseURL:c.base_url,viewport:{width:1440,height:1000},reducedMotion:'reduce'}),page=await context.newPage(),errors=[],fontRequests=[]
page.on('pageerror',e=>errors.push(e.message));page.on('request',r=>{if(r.resourceType()==='font')fontRequests.push(r.url())})
const evidence=process.env.KEEL_WORKSPACE_EVIDENCE_DIR
async function fontCheck(){await page.evaluate(async()=>{await Promise.all([400,500,600,700].map(w=>document.fonts.load(`${w} 14px "JetBrains Mono"`)))})
 assert(await page.evaluate(()=>[400,500,600,700].every(w=>[...document.fonts].some(f=>f.family.replaceAll('"','')==='JetBrains Mono'&&f.weight===String(w)&&f.status==='loaded'))))
 for(const selector of ['body','button','input','select','textarea','code']){const els=page.locator(selector);if(await els.count())assert.match(await els.first().evaluate(e=>getComputedStyle(e).fontFamily),/JetBrains Mono/)}
 assert(await page.evaluate(()=>{const x=document.createElement('canvas').getContext('2d');x.font='14px "JetBrains Mono"';return Math.abs(x.measureText('iiii').width-x.measureText('WWWW').width)<.01}))
}
try{
 await page.goto('/login',{waitUntil:'networkidle'});await fontCheck()
 assert.equal(await page.locator('#username').evaluate(e=>e.getBoundingClientRect().height),48)
 const toggle=page.locator('[aria-controls=password]');await toggle.click();assert.equal(await page.locator('#password').getAttribute('type'),'text');await toggle.click()
 await page.locator('#username').fill(c.username);await page.locator('#password').fill(c.password);await page.locator('#login-form button[type=submit]').click();await page.waitForURL('**/projects')
 await page.locator('#new-project-button').click();await page.locator('#project-name').fill('Shared controls');await page.locator('#project-slug').fill('shared-controls');await page.locator('#project-description').fill('Actual UI migration check')
 const created=page.waitForResponse(r=>r.request().method()==='POST'&&r.url().endsWith('/api/v1/projects'))
 await page.locator('.project-form button[type=submit], form button[type=submit]').last().click();const response=await created;assert.equal(response.status(),201);const project=await response.json();await page.waitForURL('**/p/'+project.id)
 for(const route of ['assets','ztp','fabric','configuration','health','rules','delivery','executions','flows','params','models','media','members','audits']){
  await page.goto(`/p/${project.id}/${route}`,{waitUntil:'networkidle'});await page.locator('main h1').first().waitFor();assert(await page.locator('main h1').count(),route);if(route==='health') { const alerts=await page.locator('main [role=alert]').allTextContents();assert(alerts.every(text=>text.includes('请检查网络 BOM')),'Empty project health remains explicitly unavailable') } else if(route==='delivery') assert.equal(await page.locator('main [role=alert]:not(ul)').count(),0,route+' shows missing-evidence list only'); else assert.equal(await page.locator('main [role=alert]').count(),0,route);await fontCheck()
  const bare=await page.locator('main button:not([data-slot]),main input:not([data-slot]):not([type=hidden]),main select:not([data-slot]),main textarea:not([data-slot]),main table:not([data-slot])').count();assert.equal(bare,0,route+' uses shared controls');console.log('PASS workspace '+route)
 }
 await page.goto('/admin/users',{waitUntil:'networkidle'});await page.getByRole('button',{name:'创建账号',exact:true}).click();const dialog=page.getByRole('dialog');await dialog.waitFor()
 const checkbox=dialog.locator('input[type=checkbox]');await checkbox.check();assert(await checkbox.isChecked());assert.equal(await checkbox.evaluate(e=>e.getBoundingClientRect().width),16);await page.keyboard.press('Space');assert(!(await checkbox.isChecked()))
 await page.keyboard.press('Escape');await dialog.waitFor({state:'hidden'});await page.waitForFunction(()=>document.activeElement?.textContent==='创建账号')
 for(const theme of ['dark','light']){
  await page.evaluate(t=>document.documentElement.dataset.theme=t,theme);await fontCheck();await page.screenshot({path:`${evidence}/users-${theme}.png`,fullPage:true})
 }
 await page.goto('/projects',{waitUntil:'networkidle'});await page.setViewportSize({width:390,height:844});assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth))
 await page.screenshot({path:`${evidence}/projects-mobile.png`,fullPage:true})
 assert(fontRequests.length>=4);assert(fontRequests.every(u=>u.startsWith(c.base_url+'/')),'All fonts remain same-origin');assert(fontRequests.filter(u=>new URL(u).pathname.endsWith('.woff2')).every(u=>u.startsWith(c.base_url+'/fonts/jetbrains-mono/')),'Text fonts use bundled JetBrains Mono; Monaco retains its local icon font');assert.deepEqual(errors,[])
 console.log('PASS actual login/project form; 14 project workspaces; native checkbox keyboard and 16px geometry; dialog focus restoration; local fonts 400/500/600/700 and monospace metrics; light/dark/mobile')
}finally{await browser.close()}
