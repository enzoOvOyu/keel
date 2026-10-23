import type {components} from '../generated/api'
import {request} from './api'
export type NetworkTemplate=components['schemas']['NetworkTemplateVersion']
export type NetworkTemplateInput=components['schemas']['NetworkTemplateInput']
const path=(id:string)=>`/projects/${encodeURIComponent(id)}/fabric/templates`
export const listNetworkTemplates=(id:string,signal:AbortSignal)=>request<{templates:NetworkTemplate[];adapter_configured:boolean}>(path(id),{signal})
export const importNetworkTemplate=(id:string,input:NetworkTemplateInput,csrf:string)=>request<NetworkTemplate>(path(id),{method:'POST',headers:{'X-CSRF-Token':csrf},body:JSON.stringify(input)})
export const testNetworkTemplate=(id:string,template:string,baseline:string,batch:string,csrf:string)=>request<NetworkTemplate>(`${path(id)}/${encodeURIComponent(template)}/test`,{method:'POST',headers:{'X-CSRF-Token':csrf},body:JSON.stringify({baseline_ref:baseline,batch_ref:batch})})
export const releaseNetworkTemplate=(id:string,template:string,csrf:string)=>request<NetworkTemplate>(`${path(id)}/${encodeURIComponent(template)}/release`,{method:'POST',headers:{'X-CSRF-Token':csrf}})
