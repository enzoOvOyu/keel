import type { components } from '../generated/api'
import { request } from './api'
export type FabricReport=NonNullable<components['schemas']['FabricCollectionReport']>
export type FabricDevice=FabricReport['devices'][number]
export type FabricEvidence=components['schemas']['FabricDeviceEvidence']
const path=(id:string)=>`/projects/${encodeURIComponent(id)}/fabric`
export const latestFabric=(id:string,signal:AbortSignal)=>request<{configured:boolean;report:FabricReport|null;inventory:FabricDevice[]}>(path(id)+'/collections/latest',{signal})
export const collectFabric=(id:string,csrf:string)=>request<FabricReport>(path(id)+'/collections',{method:'POST',headers:{'X-CSRF-Token':csrf}})
export const deviceEvidence=(id:string,device:string,signal:AbortSignal)=>request<FabricEvidence>(`${path(id)}/devices/${encodeURIComponent(device)}/evidence`,{signal})

export const networkReadiness=(id:string,signal:AbortSignal)=>request<components['schemas']['NetworkReadiness']>(path(id)+'/readiness',{signal})
export const networkCables=(id:string,page:number,signal:AbortSignal)=>request<components['schemas']['NetworkCablePage']>(path(id)+`/cables?page=${page}&page_size=20`,{signal})

export const deviceConfiguration=(project:string,device:string,signal:AbortSignal)=>request<components['schemas']['NetworkDeviceConfiguration']>(`${path(project)}/devices/${encodeURIComponent(device)}/configuration`,{signal})
export const deviceConfigurationEvents=(project:string,device:string,page:number,signal:AbortSignal)=>request<components['schemas']['NetworkDeviceConfigEventPage']>(`${path(project)}/devices/${encodeURIComponent(device)}/configuration-events?page=${page}&page_size=20`,{signal})
