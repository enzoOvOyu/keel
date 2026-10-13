import { request } from './api'

export type NettestPlatform = {
  id: string; project_id: string; status: string; detail: Record<string, unknown>
  refreshed_by: string; refreshed_at: string
}
export type NettestPrecheck = { check: string; state: string; detail: string }
export type NettestLayerPlan = { layer: string; kind: string; pairs: number }
export type NettestRun = {
  id: string; project_id: string; profile: string; state: string
  layers: NettestLayerPlan[]; targets: string[]; target_hash: string
  batch_size: number; params: Record<string, unknown>; precheck: NettestPrecheck[]
  coverage: Record<string, unknown>; conclusion: string; issue_id?: string
  created_by: string; created_at: string; started_at?: string; finished_at?: string
}
export type NettestPair = {
  id: string; run_id: string; layer: string; machine_id: string; hostname?: string
  port_id: string; port_name?: string; peer_machine_id: string; peer_hostname?: string
  peer_port_id?: string; peer_port_name?: string; state: string
  bandwidth_gbps: number; loss_ppm: number; pfc_pause: number; hotspot: boolean
  classification?: string; detail: Record<string, unknown>; measured_at?: string
}
export type NettestMatrixCell = {
  machine_id: string; hostname: string; peer_machine_id: string; peer_hostname: string
  layer: string; pairs: number; passed: number; failed: number; bandwidth_gbps: number; state: string
}
export type NettestAdmission = {
  machine_id: string; hostname: string; net_verified: boolean; waived: boolean; reason: string
}

export const nettestLayers = ['L1', 'L2', 'L3', 'L4', 'L5'] as const
export const nettestProfiles = ['quick', 'full'] as const
export const nettestLayerKinds: Record<string, string> = {
  L1: 'port_baseline', L2: 'mesh', L3: 'mesh', L4: 'mesh', L5: 'rail',
}

export const getNettestPlatform = (projectId: string, signal: AbortSignal) =>
  request<NettestPlatform>(`/projects/${encodeURIComponent(projectId)}/nettest/platform`, { signal })
export const refreshNettestPlatform = (projectId: string, status: string, detail: Record<string, unknown>, csrf: string) =>
  request<NettestPlatform>(`/projects/${encodeURIComponent(projectId)}/nettest/platform`,
    { method: 'PUT', headers: { 'X-CSRF-Token': csrf }, body: JSON.stringify({ status, detail }) })
export const listNettestRuns = (projectId: string, signal: AbortSignal, page = 1) =>
  request<{ items: NettestRun[]; total: number }>(`/projects/${encodeURIComponent(projectId)}/nettest/runs?page=${page}&page_size=20`, { signal })
export const planNettestRun = (projectId: string, body: { machine_ids: string[]; profile: string; issue_id?: string }, csrf: string, key: string) =>
  request<NettestRun>(`/projects/${encodeURIComponent(projectId)}/nettest/runs`,
    { method: 'POST', headers: { 'X-CSRF-Token': csrf, 'Idempotency-Key': key }, body: JSON.stringify(body) })
export const getNettestRun = (projectId: string, runId: string, signal: AbortSignal) =>
  request<{ run: NettestRun; pairs: NettestPair[] }>(`/projects/${encodeURIComponent(projectId)}/nettest/runs/${encodeURIComponent(runId)}`, { signal })
export const finishNettestRun = (projectId: string, runId: string, csrf: string, key: string) =>
  request<NettestRun>(`/projects/${encodeURIComponent(projectId)}/nettest/runs/${encodeURIComponent(runId)}/finish`,
    { method: 'POST', headers: { 'X-CSRF-Token': csrf, 'Idempotency-Key': key } })
export const getNettestMatrix = (projectId: string, runId: string, layer: string, signal: AbortSignal) =>
  request<{ items: NettestMatrixCell[] }>(`/projects/${encodeURIComponent(projectId)}/nettest/runs/${encodeURIComponent(runId)}/matrix?layer=${encodeURIComponent(layer)}`, { signal })
export const getNettestAdmission = (projectId: string, machineIds: string[], signal: AbortSignal) =>
  request<{ items: NettestAdmission[] }>(`/projects/${encodeURIComponent(projectId)}/nettest/admission?machine_ids=${encodeURIComponent(machineIds.join(','))}`, { signal })

export type NettestTicket = {
  id: string; difference_id: string; category: string; title: string
  hostname: string; local_port: string; status: string
}
export const listOpenNettestIssues = (projectId: string, signal: AbortSignal) =>
  request<{ items: NettestTicket[] }>(`/projects/${encodeURIComponent(projectId)}/issues?status=open&source=nettest&page=1&page_size=50`, { signal })
