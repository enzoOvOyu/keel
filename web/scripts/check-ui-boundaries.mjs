/** New UI controls use the owned shadcn primitives. Legacy debt is a frozen budget. */
import ts from 'typescript'
import {readdirSync,readFileSync,writeFileSync} from 'node:fs'
import {fileURLToPath} from 'node:url'
import path from 'node:path'
const root=fileURLToPath(new URL('../',import.meta.url)),src=path.join(root,'src'),baselineFile=path.join(root,'scripts/ui-legacy-elements.json')
const tags=new Set(['button','input','textarea','select','option','label','table','thead','tbody','tfoot','tr','th','td','dialog'])
function files(dir){return readdirSync(dir,{withFileTypes:true}).flatMap(entry=>entry.isDirectory()?files(path.join(dir,entry.name)):/\.tsx$/.test(entry.name)?[path.join(dir,entry.name)]:[])}
const actual={}
for(const file of files(src)){
 const name=path.relative(root,file).replaceAll(path.sep,'/');if(name.startsWith('src/components/ui/'))continue
 const source=ts.createSourceFile(file,readFileSync(file,'utf8'),ts.ScriptTarget.Latest,true,ts.ScriptKind.TSX),counts={}
 function visit(node){if(ts.isJsxOpeningElement(node)||ts.isJsxSelfClosingElement(node)){
  const tag=node.tagName.getText(source)
  if(tags.has(tag)){
   const hidden=tag==='input'&&node.attributes.properties.some(prop=>ts.isJsxAttribute(prop)&&prop.name.getText(source)==='type'&&prop.initializer&&ts.isStringLiteral(prop.initializer)&&prop.initializer.text==='hidden')
   if(!hidden)counts[tag]=(counts[tag]??0)+1
  }
 };ts.forEachChild(node,visit)}
 visit(source);if(Object.keys(counts).length)actual[name]=counts
}
if(process.argv.includes('--record-legacy')){writeFileSync(baselineFile,JSON.stringify({description:'Existing controls before shadcn adoption. Do not increase this budget to bypass component reuse.',files:actual},null,2)+'\n');console.log('Recorded explicit pre-existing native element budgets.');process.exit(0)}
const baseline=JSON.parse(readFileSync(baselineFile,'utf8')).files,failures=[]
for(const [file,counts] of Object.entries(actual)){for(const [tag,count] of Object.entries(counts)){const limit=baseline[file]?.[tag]??0;if(count>limit)failures.push(`${file}: native <${tag}> increased from ${limit} to ${count}; compose @/components/ui primitives instead.`)}}
if(failures.length){console.error(failures.join('\n'));process.exit(1)}
console.log('PASS UI boundaries: new controls use components/ui; existing native element counts did not increase.')
