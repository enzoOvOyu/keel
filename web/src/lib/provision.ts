import { request } from './api'
import type { components } from '../generated/api'
import type { components as execution } from '../generated/execution'
export type ProvisionRequest = components['schemas']['ProvisionRequest']
export const provisionBase = (project: string) => `/projects/${encodeURIComponent(project)}/provision`
export const listProvisionRequests = (project: string,page: number,signal: AbortSignal) => request<components['schemas']['ProvisionRequestPage']>(`${provisionBase(project)}/requests?page=${page}&page_size=20`,{signal})
export const provisionPublicKeys = (project: string,page: number,signal: AbortSignal) => request<components['schemas']['SSHDomainKeyPage']>(`${provisionBase(project)}/public-keys?page=${page}&page_size=20`,{signal})
export const createProvisionRequest = (project: string,asset: string,input: components['schemas']['CreateProvisionRequest'],csrf: string,key: string) => request<ProvisionRequest>(`/projects/${encodeURIComponent(project)}/assets/${encodeURIComponent(asset)}/provision/requests`,{method:'POST',headers:{'X-CSRF-Token':csrf,'Idempotency-Key':key},body:JSON.stringify(input)})
export const actProvisionRequest = (item: ProvisionRequest,kind:'approve'|'cancel',reason: string,csrf: string,key: string) => request<ProvisionRequest>(`${provisionBase(item.project_id)}/requests/${encodeURIComponent(item.id)}/${kind}`,{method:'POST',headers:{'X-CSRF-Token':csrf,'Idempotency-Key':key},body:JSON.stringify({expected_version:item.state_version,reason} satisfies components['schemas']['ProvisionRequestAction'])})
export const prepareProvisionRequest = (item: ProvisionRequest,csrf: string,key: string) => request<execution['schemas']['Execution']>(`${provisionBase(item.project_id)}/requests/${encodeURIComponent(item.id)}/prepare`,{method:'POST',headers:{'X-CSRF-Token':csrf,'Idempotency-Key':key},body:JSON.stringify({expected_version:item.state_version} satisfies components['schemas']['ProvisionPrepareInput'])})
