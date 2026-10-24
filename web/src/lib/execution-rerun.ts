import type {components} from '../generated/execution'
import {request} from './api'
export type RerunCandidates=components['schemas']['RerunCandidates']
export type RerunInput=components['schemas']['RerunInput']
const base=(project:string,execution:string)=>`/projects/${encodeURIComponent(project)}/executions/${encodeURIComponent(execution)}/rerun`
const post=<T,>(path:string,body:unknown,csrf:string,key?:string,signal?:AbortSignal)=>request<T>(path,{method:'POST',headers:{'X-CSRF-Token':csrf,...(key?{'Idempotency-Key':key}:{})},body:JSON.stringify(body),signal})
export const previewRerun=(project:string,execution:string,mode:'failed'|'targets',targetIDs:string[],csrf:string,signal:AbortSignal)=>post<RerunCandidates>(base(project,execution)+'/preview',{mode,...(mode==='targets'?{target_ids:targetIDs}:{})},csrf,undefined,signal)
export const rerunExecution=(project:string,execution:string,body:RerunInput,csrf:string,key:string)=>post<components['schemas']['Execution']>(base(project,execution),body,csrf,key)
