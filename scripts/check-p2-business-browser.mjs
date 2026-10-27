import assert from 'node:assert/strict'
import {readFile} from 'node:fs/promises'
const c=JSON.parse(await readFile(process.env.KEEL_AUTH_BROWSER_CREDENTIALS_FILE,'utf8'))
const {chromium}=await import('/tmp/keel-browser-check/node_modules/playwright/index.mjs')
const browser=await chromium.launch({headless:true}),context=await browser.newContext({baseURL:c.base_url,viewport:{width:1440,height:1080},reducedMotion:'reduce'}),page=await context.newPage(),errors=[]
page.on('pageerror',e=>errors.push(e.message));page.on('response',async r=>{if(r.status()>=500){console.log('SERVER ERROR',new URL(r.url()).pathname,await r.text());errors.push('HTTP '+r.status()+' '+new URL(r.url()).pathname)}})
try{
 await page.goto('/login');await page.locator('#username').fill(c.username);await page.locator('#password').fill(c.password);await page.locator('#login-form button[type=submit]').click();await page.waitForURL('**/projects')
 await page.goto(`/p/${c.project_id}/assets`,{waitUntil:'networkidle'})
 await page.getByRole('button',{name:c.hostname,exact:true}).click()
 const dialog=page.getByRole('dialog');await dialog.waitFor();assert.equal(await dialog.getByRole('tab').count(),5)
 await dialog.getByRole('button',{name:'展开至宽视图',exact:true}).click();await page.waitForFunction(()=>Math.abs(document.querySelector('[role=dialog]').getBoundingClientRect().width-960)<1)
 await dialog.getByRole('button',{name:'收起宽视图',exact:true}).click();await page.waitForFunction(()=>Math.abs(document.querySelector('[role=dialog]').getBoundingClientRect().width-720)<1)
 for(let index=0;index<5;index++){await dialog.getByRole('tab').nth(index).click();await page.waitForLoadState('networkidle');assert.equal(await dialog.getByRole('tab').nth(index).getAttribute('aria-selected'),'true')}
 await dialog.getByRole('tab').nth(3).click();await dialog.getByRole('button',{name:'查看快照',exact:true}).first().click();await page.locator('.react-flow__node').first().waitFor();assert.equal(await page.locator('.react-flow__node').count(),9)
 await dialog.locator('#topology-reason').fill('Pending review draft')
 let discard=0;page.once('dialog',async prompt=>{discard++;await prompt.dismiss()});await dialog.getByRole('tab').nth(0).click();assert.equal(await dialog.getByRole('tab').nth(3).getAttribute('aria-selected'),'true');assert.equal(await dialog.locator('#topology-reason').inputValue(),'Pending review draft')
 page.once('dialog',async prompt=>{discard++;await prompt.accept()});await dialog.getByRole('tab').nth(0).click();assert.equal(discard,2)
 await page.keyboard.press('Escape');await dialog.waitFor({state:'hidden'})
 await page.goto(`/p/${c.project_id}/fabric`,{waitUntil:'networkidle'});assert.equal(await page.locator('main [role=alert]').count(),0)
 await page.goto(`/issues?project_id=${c.project_id}`,{waitUntil:'networkidle'})
 await page.screenshot({path:c.evidence+'/business-issues.png',fullPage:true})
 assert.deepEqual(errors,[])
 console.log('PASS actual business DB/API/browser: five drawer tabs, historical topology graph, 720/960 width, unsaved draft cancel/discard, scoped fabric and issues, no page errors')
}finally{await browser.close()}
