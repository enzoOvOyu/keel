import { request } from './api'

export type AgentPlan = {
  id: string
  project_id: string
  name: string
  state: string // running / completed / failed / aborted
  media: Record<string, any>
  machines: string[]
  created_by: string
  created_at: string
  finished_at?: string
}
export type AgentTarget = {
  id: string
  plan_id: string
  machine_id: string
  hostname: string
  state: string // pending / installing / installed / failed / blocked
  reason: string
  media_uri: string
  media_sha256: string
  cache_hit: boolean
  bytes_transferred: number
  enrollment_token: string
  credential_id: string
  credential_fingerprint: string
  updated_at: string
}
export type AgentCredentialEvent = {
  id: string
  machine_id: string
  credential_id: string
  action: string // issue / rotate / revoke / token_replay_refused
  detail: Record<string, any>
  created_at: string
}
export type AgentPlanDetail = {
  plan: AgentPlan
  targets: AgentTarget[]
  summary: Record<string, any>
  events: AgentCredentialEvent[]
}
export type AgentPlanInput = {
  name: string
  media_id: string
  machines: string[]
}
export type AgentMediaSummary = {
  id: string
  name: string
  version: string
  arch: string
  state: string
  type: string
}

export const listAgentPlans = (projectId: string, signal: AbortSignal, page = 1) =>
  request<{ items: AgentPlan[]; total: number }>(`/projects/${encodeURIComponent(projectId)}/agent/plans?page=${page}&page_size=20`, { signal })
export const getAgentPlan = (projectId: string, planId: string, signal: AbortSignal) =>
  request<AgentPlanDetail>(`/projects/${encodeURIComponent(projectId)}/agent/plans/${encodeURIComponent(planId)}`, { signal })
export const createAgentPlan = (projectId: string, body: AgentPlanInput, csrf: string, key: string) =>
  request<AgentPlan>(`/projects/${encodeURIComponent(projectId)}/agent/plans`,
    { method: 'POST', headers: { 'X-CSRF-Token': csrf, 'Idempotency-Key': key }, body: JSON.stringify(body) })
export const abortAgentPlan = (projectId: string, planId: string, csrf: string, key: string) =>
  request<AgentPlanDetail>(`/projects/${encodeURIComponent(projectId)}/agent/plans/${encodeURIComponent(planId)}/abort`,
    { method: 'POST', headers: { 'X-CSRF-Token': csrf, 'Idempotency-Key': key } })
export const rotateAgentCredential = (projectId: string, machineId: string, fingerprint: string, csrf: string, key: string) =>
  request<AgentTarget>(`/projects/${encodeURIComponent(projectId)}/agent/credentials/rotate`,
    { method: 'POST', headers: { 'X-CSRF-Token': csrf, 'Idempotency-Key': key }, body: JSON.stringify({ machine_id: machineId, fingerprint }) })
export const revokeAgentCredential = (projectId: string, machineId: string, csrf: string, key: string) =>
  request<AgentTarget>(`/projects/${encodeURIComponent(projectId)}/agent/credentials/revoke`,
    { method: 'POST', headers: { 'X-CSRF-Token': csrf, 'Idempotency-Key': key }, body: JSON.stringify({ machine_id: machineId }) })
