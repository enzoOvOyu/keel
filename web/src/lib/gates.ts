import type { components } from '../generated/api'
import { request } from './api'
export const gateDashboard = (project: string, signal: AbortSignal) => request<components['schemas']['GateDashboard']>(`/projects/${encodeURIComponent(project)}/gates`, { signal })
export type GateWaiver = components['schemas']['GateWaiver']
const waiverPath = (project: string, asset: string) => `/projects/${encodeURIComponent(project)}/assets/${encodeURIComponent(asset)}/gates/G1/waivers`
export const gateWaiverScope = (project: string, asset: string, signal: AbortSignal) => request<components['schemas']['GateWaiverScope']>(`${waiverPath(project, asset)}/scope`, { signal })
export const listGateWaivers = (project: string, asset: string, page: number, signal: AbortSignal) => request<components['schemas']['GateWaiverPage']>(`${waiverPath(project, asset)}?page=${page}&page_size=10`, { signal })
export const approveGateWaiver = (project: string, asset: string, body: components['schemas']['GateWaiverInput'], csrf: string, key: string) => request<GateWaiver>(waiverPath(project, asset), { method: 'POST', headers: { 'X-CSRF-Token': csrf, 'Idempotency-Key': key }, body: JSON.stringify(body) })
export const revokeGateWaiver = (project: string, asset: string, id: string, reason: string, csrf: string) => request<void>(`${waiverPath(project, asset)}/${encodeURIComponent(id)}/revoke`, { method: 'POST', headers: { 'X-CSRF-Token': csrf }, body: JSON.stringify({ reason }) })
