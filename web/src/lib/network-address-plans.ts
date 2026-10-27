import type {components} from '../generated/api'
import {request} from './api'
export type NetworkAddressPlan=components['schemas']['NetworkAddressPlan']
export type NetworkAddressPlanInput=components['schemas']['NetworkAddressPlanInput']
export type NetworkAddressOverride=components['schemas']['NetworkAddressOverride']
const path=(project:string)=>`/projects/${encodeURIComponent(project)}/fabric/address-plans`
const post=<T>(url:string,body:unknown,csrf:string)=>request<T>(url,{method:'POST',headers:{'X-CSRF-Token':csrf},body:JSON.stringify(body)})
export const listAddressPlans=(project:string,signal:AbortSignal)=>request<{plans:components['schemas']['NetworkAddressPlanSummary'][]}>(path(project),{signal})
export const getAddressPlan=(project:string,id:string,signal:AbortSignal)=>request<NetworkAddressPlan>(`${path(project)}/${encodeURIComponent(id)}`,{signal})
export const previewAddressPlan=(project:string,input:NetworkAddressPlanInput,csrf:string)=>post<NetworkAddressPlan>(`${path(project)}/preview`,input,csrf)
export const saveAddressPlan=(project:string,plan:NetworkAddressPlan,csrf:string)=>post<NetworkAddressPlan>(path(project),{...plan.input,expected_hash:plan.content_hash},csrf)
export const approveAddressPlan=(project:string,id:string,hash:string,reason:string,csrf:string)=>post<NetworkAddressPlan>(`${path(project)}/${encodeURIComponent(id)}/approve`,{expected_hash:hash,reason},csrf)
