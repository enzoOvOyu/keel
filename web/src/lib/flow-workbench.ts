import type {components} from '../generated/execution'
import {request} from './api'
export type FlowValidation=components['schemas']['FlowValidation']
export type FlowDiff=components['schemas']['FlowDiff']
export type FlowRevisionDetail=components['schemas']['FlowRevisionDetail']
export type FlowSearch={flow?:string;tab?:'overview'|'source'|'revisions';page?:number;versionPage?:number;left?:string;right?:string}
const base=(project:string)=>`/projects/${encodeURIComponent(project)}/flows`
const write=<T,>(path:string,body:unknown,csrf:string,key?:string)=>request<T>(path,{method:'POST',headers:{'X-CSRF-Token':csrf,...(key?{'Idempotency-Key':key}:{})},body:JSON.stringify(body)})
export const validateFlow=(project:string,yaml_source:string,parameters:Record<string,unknown>,csrf:string)=>write<FlowValidation>(base(project)+'/validate',{yaml_source,parameters},csrf)
export const listFlowVersions=(project:string,flow:string,page:number,signal:AbortSignal)=>request<components['schemas']['FlowVersionPage']>(base(project)+`/${flow}/versions?page=${page}&page_size=20`,{signal})
export const publishFlowVersion=(project:string,flow:string,body:components['schemas']['PublishFlowInput'],csrf:string,key:string)=>write<components['schemas']['FlowVersion']>(base(project)+`/${flow}/versions`,body,csrf,key)
export const rollbackFlow=(project:string,flow:string,body:components['schemas']['RollbackFlowInput'],csrf:string,key:string)=>write<components['schemas']['FlowVersion']>(base(project)+`/${flow}/rollback`,body,csrf,key)
export const getFlowDiff=(project:string,flow:string,before:string,after:string,signal:AbortSignal)=>request<FlowDiff>(base(project)+`/${flow}/diff?${new URLSearchParams({before,after})}`,{signal})
export function flowSearch(value:Record<string,unknown>):FlowSearch{
 const id=(v:unknown)=>typeof v==='string'&&/^[a-f0-9]{32}$/.test(v)?v:undefined
 const page=(v:unknown)=>Number.isSafeInteger(Number(v))&&Number(v)>0&&Number(v)<=1000000?Number(v):undefined
 return{flow:id(value.flow),tab:['overview','source','revisions'].includes(String(value.tab))?value.tab as FlowSearch['tab']:'overview',page:page(value.page),versionPage:page(value.versionPage),left:id(value.left),right:id(value.right)}
}
