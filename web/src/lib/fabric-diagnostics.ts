import type {components} from '../generated/api'
import {request} from './api'
export type PortDiagnostics=components['schemas']['FabricDiagnosticsReport']
export type DDMTrends=components['schemas']['FabricDDMTrends']
const path=(project:string,device:string)=>`/projects/${encodeURIComponent(project)}/fabric/devices/${encodeURIComponent(device)}`
export const fabricDiagnostics=(project:string,device:string,signal:AbortSignal)=>request<PortDiagnostics>(path(project,device)+'/diagnostics',{signal})
export const ddmTrends=(project:string,device:string,port:string,hours:24|48,signal:AbortSignal)=>request<DDMTrends>(`${path(project,device)}/ddm-trends?${new URLSearchParams({port,hours:String(hours)})}`,{signal})
export const ddmExportProof=(project:string,device:string,csrf:string,report:DDMTrends)=>request<DDMTrends>(`${path(project,device)}/ddm-trends/export`,{method:'POST',headers:{'X-CSRF-Token':csrf},body:JSON.stringify({port:report.port,hours:report.hours,content_hash:report.content_hash})})
