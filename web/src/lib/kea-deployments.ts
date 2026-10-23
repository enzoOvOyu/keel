import type { components, operations } from '../generated/api'
import { request } from './api'
export type KeaDeployment = components['schemas']['KeaDeployment']
export type KeaCatalog = operations['listKeaDeployments']['responses'][200]['content']['application/json']
const path=(id:string)=>`/projects/${encodeURIComponent(id)}/ztp/kea-deployments`
export const listKeaDeployments=(id:string,signal:AbortSignal)=>request<KeaCatalog>(path(id),{signal})
export const stageKeaDeployment=(id:string,batch:string,policy:string,csrf:string)=>request<KeaDeployment>(path(id)+'/stage',{method:'POST',headers:{'X-CSRF-Token':csrf},body:JSON.stringify({batch_ref:batch,policy_media_id:policy})})
export const executeKeaDeployment=(id:string,job:string,csrf:string)=>request<{id:string}>(`${path(id)}/${encodeURIComponent(job)}/execute`,{method:'POST',headers:{'X-CSRF-Token':csrf,'Idempotency-Key':job}})
