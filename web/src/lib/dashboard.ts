import { request } from './api'

export const waiveGate = (projectId: string, gate: string, machineIds: string[], reason: string, scope: string, hours: number, csrf: string, key: string) =>
  request<unknown>(`/projects/${encodeURIComponent(projectId)}/gates/waive`, { method: 'POST', headers: { 'X-CSRF-Token': csrf, 'Idempotency-Key': key }, body: JSON.stringify({ gate, machine_ids: machineIds, reason, scope, waiver_expires_hours: hours }) })

export type GateLayerRate = { layer: string; planned: number; measured: number; passed: number; failed: number; rate: number }
export type GateCard = { gate: string; title: string; state: 'pass' | 'fail' | 'waived' | 'not_implemented' | 'no_evidence'; summary: string; pass_rate: number; latest_at?: string; latest_conclusion?: string; layers?: GateLayerRate[] }
export type DashboardKPIs = { assets_total: number; trust_established: number; trust_unverified: number; trust_revoked: number; open_issues: number; in_repair_issues: number; lldp_open_diffs: number }
export type GateDashboard = { cards: Record<'G1' | 'G2' | 'G3' | 'G4' | 'G5' | 'G6', GateCard>; kpis: DashboardKPIs }
export const getGateDashboard = (projectId: string, signal: AbortSignal) =>
  request<GateDashboard>(`/projects/${encodeURIComponent(projectId)}/dashboard`, { signal })
