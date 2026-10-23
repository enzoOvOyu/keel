import type {components} from '../generated/api'
import {request} from './api'
export type ManagementArtifacts=components['schemas']['ZTPManagementArtifactPreview']
export type ManageabilityCheck=components['schemas']['ZTPManageabilityCheck']
export type DeviceSession=components['schemas']['ZTPSessionSummary']
const path=(id:string)=>`/projects/${encodeURIComponent(id)}/ztp`
export const managementArtifacts=(id:string,batch:string,signal:AbortSignal)=>request<ManagementArtifacts>(`${path(id)}/management-artifacts/preview?batch_ref=${encodeURIComponent(batch)}`,{signal})
export const manageabilityChecks=(id:string,signal:AbortSignal)=>request<{items:ManageabilityCheck[]}>(`${path(id)}/manageability-checks`,{signal})
export const deviceSessions=(id:string,signal:AbortSignal)=>request<{items:DeviceSession[]}>(`${path(id)}/device-sessions`,{signal})
export const deviceSession=(id:string,session:string,signal:AbortSignal)=>request<components['schemas']['ZTPSessionDetail']>(`${path(id)}/device-sessions/${encodeURIComponent(session)}`,{signal})
