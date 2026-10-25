/** No API/database fixture: exercise real shared components, portals and theme tokens. */
import assert from 'node:assert/strict'
import {pathToFileURL} from 'node:url'
const {chromium}=await import(pathToFileURL('/tmp/keel-browser-check/node_modules/playwright/index.mjs').href)
const browser=await chromium.launch({headless:true}),context=await browser.newContext({viewport:{width:1440,height:1080},reducedMotion:'reduce'}),page=await context.newPage(),errors=[]
page.on('pageerror',e=>errors.push(e.message))
try{
 await page.goto('http://127.0.0.1:5178/test-fixtures/shadcn.html');await page.getByText('Shared UI contract',{exact:true}).waitFor()
 await page.evaluate(()=>document.fonts.ready)
 const themeButton=page.getByTestId('preferences').getByRole('button').nth(1)
 const light=await page.locator('body').evaluate(e=>getComputedStyle(e).backgroundColor)
 await themeButton.click();assert.equal(await page.locator('html').getAttribute('data-theme'),'dark');const dark=await page.locator('body').evaluate(e=>getComputedStyle(e).backgroundColor);assert.notEqual(light,dark)
 const open=page.getByRole('button',{name:'Open confirmation'});await open.click();const dialog=page.getByRole('dialog',{name:'Confirm change'});await dialog.waitFor()
 assert.match(await dialog.evaluate(e=>getComputedStyle(e).fontFamily),/JetBrains Mono/)
 assert.match(await page.getByLabel('Review note').evaluate(e=>getComputedStyle(e).fontFamily),/JetBrains Mono/)
 const surface=await dialog.evaluate(e=>getComputedStyle(e).backgroundColor);assert.notEqual(surface,'rgba(0, 0, 0, 0)')
 await page.getByLabel('Review note').focus();await page.keyboard.press('Shift+Tab');assert(await dialog.evaluate(e=>e.contains(document.activeElement)),'Dialog must trap backward keyboard navigation')
 await page.keyboard.press('Escape');await dialog.waitFor({state:'hidden'});await page.waitForFunction(()=>document.activeElement?.textContent==='Open confirmation')
 await open.click();await dialog.getByRole('button',{name:'Confirm',exact:true}).click();assert(await dialog.getByRole('button',{name:'Confirm',exact:true}).isDisabled());await page.keyboard.press('Escape');assert(await dialog.isVisible(),'Busy confirmation must ignore Escape');await dialog.waitFor({state:'hidden'});await page.waitForFunction(()=>document.activeElement?.textContent==='Open confirmation')
 await themeButton.click();assert.equal(await page.locator('html').getAttribute('data-theme'),'light')
 await page.getByRole('tab',{name:'Evidence',exact:true}).focus();await page.keyboard.press('Enter');assert.equal(await page.getByRole('tab',{name:'Evidence',exact:true}).getAttribute('aria-selected'),'true');assert(await page.getByRole('tabpanel').getByText('Read-only evidence').isVisible())
 await page.getByRole('tab',{name:'Review',exact:true}).click();await open.focus();const focus=await open.evaluate(e=>({shadow:getComputedStyle(e).boxShadow,outline:getComputedStyle(e).outlineStyle}));assert(focus.shadow!=='none'||focus.outline!=='none','Keyboard focus must remain visible')
 await page.screenshot({path:process.env.KEEL_SHADCN_EVIDENCE_DIR+'/shadcn-light.png',fullPage:true});await themeButton.click();await open.click();await page.waitForFunction(()=>getComputedStyle(document.querySelector('[role=dialog]')).opacity==='1');await page.screenshot({path:process.env.KEEL_SHADCN_EVIDENCE_DIR+'/shadcn-dark-dialog.png',fullPage:true});assert.deepEqual(errors,[])
 console.log('PASS real shared Preferences/ActionDialog: Keel light/dark tokens, JetBrains Mono font, keyboard focus trap/restoration, busy dismissal guard, Radix tabs and no browser errors')
}finally{await browser.close()}
