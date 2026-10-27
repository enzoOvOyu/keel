import type { components } from '../generated/api'
import { request } from './api'
export type PortMonitorSnapshot = components['schemas']['PortMonitorSnapshot']
export type PortMonitorItem = components['schemas']['PortMonitorItem']
export const portMonitorBase = (project: string, asset: string) => `/projects/${encodeURIComponent(project)}/assets/${encodeURIComponent(asset)}/ports`
export const portMonitorSnapshot = (project: string, asset: string, signal: AbortSignal) => request<PortMonitorSnapshot>(portMonitorBase(project, asset), { signal })
export const portMonitorEvents = (project: string, asset: string, port: string, page: number, signal: AbortSignal) => request<components['schemas']['PortMonitorEvents']>(`${portMonitorBase(project, asset)}/${encodeURIComponent(port)}/events?page=${page}&page_size=20`, { signal })
export type PortMonitorFleetItem = components['schemas']['PortMonitorFleetItem']
export const portMonitorFleet = (project: string, ids: string[], signal: AbortSignal) => request<components['schemas']['PortMonitorFleet']>(`/projects/${encodeURIComponent(project)}/ports?${new URLSearchParams({ asset_ids: [...ids].sort().join(',') })}`, { signal })
