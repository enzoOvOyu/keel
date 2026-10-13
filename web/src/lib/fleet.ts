import { request } from './api'

export type MachineStatusPill = { machine_id: string; mapping: 'ok' | 'error' | 'unset'; network: 'ok' | 'error' | 'unknown' }
export type PortPanelEntry = { port_id: string; port_name: string; mac_address: string; rail: number; os_interface?: string; link_state: string; link_source?: string; link_at?: string; dom_summary?: string; dom_source?: string; lldp_peer?: string; lldp_at?: string; alert_rule?: string; nettest_failure?: string; nettest_at?: string }
export type PortPanel = { machine_id: string; hostname: string; monitored: boolean; ports: PortPanelEntry[] }
export type MappingRevision = { id: string; sequence: number; machine_id: string; hostname: string; note: string; published_at: string; previous_revision?: string; entries: Array<{ port_id: string; port_name: string; os_interface: string; mac_address: string; bdf: string; switch_port: string; gpu_affinity: string }> }
export type PcieDiffView = { id: string; dimension: string; detail: string; created_at: string; resolved_at?: string; repair_count: number; escalated: boolean }
export type PortEvent = { port: string; metric: string; value: string; source: string; ingested_at: string }

export const getFleetStatus = (projectId: string, signal: AbortSignal) =>
  request<{ items: MachineStatusPill[] }>(`/projects/${encodeURIComponent(projectId)}/fleet-status`, { signal })
export const getPortPanel = (projectId: string, machineId: string, signal: AbortSignal) =>
  request<PortPanel>(`/projects/${encodeURIComponent(projectId)}/machines/${encodeURIComponent(machineId)}/port-panel`, { signal })
export const getMappingRevisions = (projectId: string, machineId: string, signal: AbortSignal) =>
  request<{ items: MappingRevision[]; total: number }>(`/projects/${encodeURIComponent(projectId)}/mappings?machine_id=${encodeURIComponent(machineId)}&page=1&page_size=10`, { signal })
export const getPcieDiffs = (projectId: string, machineId: string, signal: AbortSignal) =>
  request<{ items: PcieDiffView[]; total: number }>(`/projects/${encodeURIComponent(projectId)}/pcie/diffs?machine_id=${encodeURIComponent(machineId)}&page=1&page_size=50`, { signal })
export const getMachineEvents = (projectId: string, machineId: string, signal: AbortSignal) =>
  request<{ items: PortEvent[] }>(`/projects/${encodeURIComponent(projectId)}/machines/${encodeURIComponent(machineId)}/events?limit=50`, { signal })
