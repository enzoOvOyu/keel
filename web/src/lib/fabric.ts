import type { components } from '../generated/api'
import { request } from './api'
export type FabricReport=NonNullable<components['schemas']['FabricCollectionReport']>
export type FabricDevice=FabricReport['devices'][number]
export type FabricEvidence=components['schemas']['FabricDeviceEvidence']
const path=(id:string)=>`/projects/${encodeURIComponent(id)}/fabric`
export const latestFabric=(id:string,signal:AbortSignal)=>request<{configured:boolean;report:FabricReport|null;inventory:FabricDevice[]}>(path(id)+'/collections/latest',{signal})
export const collectFabric=(id:string,csrf:string)=>request<FabricReport>(path(id)+'/collections',{method:'POST',headers:{'X-CSRF-Token':csrf}})
export const deviceEvidence=(id:string,device:string,signal:AbortSignal)=>request<FabricEvidence>(`${path(id)}/devices/${encodeURIComponent(device)}/evidence`,{signal})
