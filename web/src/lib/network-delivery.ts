import type {components} from '../generated/api'
import {ApiError,request} from './api'
export type DeliveryPreview=components['schemas']['NetworkDeliveryPreview']
export type DeliveryExport=components['schemas']['NetworkDeliveryExport']
export type DeliveryReferencePage=components['schemas']['NetworkDeliveryReferencePage']
export type DeliveryKind='draft'|'delivery'
const path=(id:string)=>`/projects/${encodeURIComponent(id)}/fabric/delivery`
export const deliveryPreview=(id:string,signal:AbortSignal)=>request<DeliveryPreview>(`${path(id)}/preflight`,{signal})
export const deliveryReferences=(id:string,page:number,signal:AbortSignal)=>request<DeliveryReferencePage>(`${path(id)}/references?page=${page}&page_size=100`,{signal})
export const deliveryExports=(id:string,signal:AbortSignal)=>request<{items:DeliveryExport[]}>(`${path(id)}/exports`,{signal})
export const createDeliveryExport=(id:string,kind:DeliveryKind,hash:string,key:string,csrf:string)=>request<DeliveryExport>(`${path(id)}/exports`,{method:'POST',headers:{'X-CSRF-Token':csrf,'Idempotency-Key':key},body:JSON.stringify({kind,expected_preflight_hash:hash})})
export async function downloadDeliveryExport(id:string,archive:DeliveryExport){
 let response:Response
 try{response=await fetch('/api/v1'+path(id)+`/exports/${encodeURIComponent(archive.id)}/download`,{credentials:'same-origin',cache:'no-store',headers:{Accept:'application/json'}})}catch{throw new ApiError(0,'NETWORK_ERROR')}
 if(!response.ok){let data:{code?:string;trace_id?:string}={};try{data=await response.json()}catch{/* Generic proxy failure. */};throw new ApiError(response.status,data.code??'HTTP_ERROR',data.trace_id??'')}
 const body=await response.blob(),url=URL.createObjectURL(body),link=document.createElement('a');link.href=url;link.download=`keel-network-${archive.kind}-${archive.id}.json`;link.click();window.setTimeout(()=>URL.revokeObjectURL(url),1000)
}
