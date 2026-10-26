import {useEffect,useRef} from 'react'
import * as monaco from 'monaco-editor/editor/editor.api.js'
import 'monaco-editor/languages/definitions/yaml/register.js'
import EditorWorker from 'monaco-editor/editor/editor.worker.js?worker'
;(self as unknown as {MonacoEnvironment:unknown}).MonacoEnvironment={getWorker:()=>new EditorWorker()}
const editorTheme=()=>document.documentElement.dataset.theme==='dark'||document.documentElement.classList.contains('dark')?'vs-dark':'vs'
export function FlowEditor({value,onChange,readOnly=false,label,errorLine}:{value:string;onChange?:(value:string)=>void;readOnly?:boolean;label:string;errorLine?:number}){
 const host=useRef<HTMLDivElement>(null),editor=useRef<monaco.editor.IStandaloneCodeEditor|null>(null),callback=useRef(onChange)
 callback.current=onChange
 useEffect(()=>{if(!host.current)return;const model=monaco.editor.createModel(value,'yaml'),instance=monaco.editor.create(host.current,{model,automaticLayout:true,ariaLabel:label,readOnly,fontFamily:'JetBrains Mono',fontSize:13,minimap:{enabled:false},scrollBeyondLastLine:false,theme:editorTheme(),tabSize:2});editor.current=instance
 const sub=instance.onDidChangeModelContent(()=>callback.current?.(instance.getValue())),observer=new MutationObserver(()=>monaco.editor.setTheme(editorTheme()));observer.observe(document.documentElement,{attributes:true,attributeFilter:['data-theme','class']})
 return()=>{observer.disconnect();sub.dispose();instance.dispose();model.dispose();editor.current=null}
 },[readOnly,label])
 useEffect(()=>{if(editor.current&&editor.current.getValue()!==value)editor.current.setValue(value)},[value])
 useEffect(()=>{const instance=editor.current,model=instance?.getModel();if(!model||!instance)return;const line=errorLine&&errorLine<=model.getLineCount()?errorLine:undefined;monaco.editor.setModelMarkers(model,'keel-flow',line?[{severity:monaco.MarkerSeverity.Error,message:`YAML · ${line}`,startLineNumber:line,endLineNumber:line,startColumn:1,endColumn:model.getLineMaxColumn(line)}]:[]);if(line)instance.revealLineInCenter(line)},[value,errorLine,readOnly,label])
 return <div ref={host} className="flow-monaco"/>
}
export function FlowDiffEditor({before,after,label}:{before:string;after:string;label:string}){
 const host=useRef<HTMLDivElement>(null)
 useEffect(()=>{if(!host.current)return;const left=monaco.editor.createModel(before,'yaml'),right=monaco.editor.createModel(after,'yaml'),editor=monaco.editor.createDiffEditor(host.current,{automaticLayout:true,readOnly:true,ariaLabel:label,renderSideBySide:true,useInlineViewWhenSpaceIsLimited:false,fontFamily:'JetBrains Mono',fontSize:13,minimap:{enabled:false},scrollBeyondLastLine:false,theme:editorTheme()});editor.setModel({original:left,modified:right})
 const observer=new MutationObserver(()=>monaco.editor.setTheme(editorTheme()));observer.observe(document.documentElement,{attributes:true,attributeFilter:['data-theme','class']});return()=>{observer.disconnect();editor.dispose();left.dispose();right.dispose()}
 },[before,after,label])
 return <div ref={host} className="flow-monaco flow-diff"/>
}
