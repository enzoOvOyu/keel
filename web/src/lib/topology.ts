import type { components } from '../generated/api'
import { request } from './api'

export type TopologyScope = components['schemas']['PciTopologyScope']
export type TopologySnapshot = components['schemas']['PciTopologySnapshot']
export type TopologyCapture = components['schemas']['PciTopologyCapture']
export type TopologyNode = components['schemas']['PciTopologyNode']
export type TopologyPage = components['schemas']['PciTopologyPage']
const base = (project: string, asset: string) => `/projects/${encodeURIComponent(project)}/assets/${encodeURIComponent(asset)}/topology`
export const previewTopology = (project: string, asset: string, signal: AbortSignal) => request<TopologyScope>(`${base(project, asset)}/scope`, { signal })
export const listTopology = (project: string, asset: string, page: number, signal: AbortSignal) => request<TopologyPage>(`${base(project, asset)}?page=${page}&page_size=20`, { signal })
export const getTopology = (project: string, asset: string, id: string, signal: AbortSignal) => request<TopologySnapshot>(`${base(project, asset)}/${encodeURIComponent(id)}`, { signal })
export const validateTopology = (project: string, asset: string, scopeHash: string, reason: string, csrf: string, key: string) => request<TopologySnapshot>(`${base(project, asset)}/validate`, { method: 'POST', headers: { 'X-CSRF-Token': csrf, 'Idempotency-Key': key }, body: JSON.stringify({ scope_hash: scopeHash, reason }) })

export function topologyGeneration(speed: number | null | undefined): string {
 const generations: Record<number, number> = { 2.5: 1, 5: 2, 8: 3, 16: 4, 32: 5, 64: 6, 128: 7 }
 return speed != null && generations[speed] ? `Gen${generations[speed]}` : 'unknown'
}
