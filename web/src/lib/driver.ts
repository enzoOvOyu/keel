import { request } from './api'
import { listMedia } from './media'

export type DriverComponent = {
  key: string
  display_name: string
  media_type: string
  rollback: string // rollback_verified / manual_only / none
  backup_required: boolean
  companion_rule: Record<string, unknown>
  notes: string
}
export type DriverPlan = {
  id: string
  project_id: string
  name: string
  state: string
  components: Record<string, any>
  machines: string[]
  baseline: Record<string, any>
  baseline_overrides: Record<string, any>
  canary_machine_id: string
  batch_size: number
  backup_plan: string
  precheck: Record<string, any>
  conclusion: string
  created_by: string
  created_at: string
  started_at?: string
  finished_at?: string
}
export type DriverTarget = {
  id: string
  plan_id: string
  machine_id: string
  role: string
  current_versions: Record<string, string>
  target_versions: Record<string, string>
  state: string
  cache_hit: boolean
  bytes_transferred: number
  readback: Record<string, string>
  failure_class: string
  attempt: number
  reconcile: Record<string, any>
  evidence: Record<string, any>
  updated_at: string
}
export type DriverTransfer = {
  id: string
  plan_id: string
  machine_id: string
  component: string
  uri: string
  sha256: string
  size: number
  cache_hit: boolean
  bytes: number
  checksum: string // "" / match / mismatch
  retries: number
  created_at: string
}
export type DriverPlanDetail = {
  plan: DriverPlan
  targets: DriverTarget[]
  transfers: DriverTransfer[]
  summary: Record<string, any>
}
export type DriverComponentInput = { media_id: string; uri?: string }
export type DriverPlanInput = {
  name: string
  components: Record<string, DriverComponentInput>
  machines: string[]
  baseline: Record<string, string>
  baseline_overrides?: Record<string, Record<string, string>>
  canary_machine_id: string
  batch_size: number
  backup_plan: string
}
export type DriverAdvanceInput = {
  results: { machine_id: string; outcome: string; readback?: Record<string, string>; cache?: string[]; corrupt?: string[] }[]
}

export type MediaSummary = {
  id: string
  type: string
  name: string
  version: string
  state: string
  sha256: string
  size: number
  arch?: string
}

export const listDriverComponents = (signal: AbortSignal) =>
  request<{ components: DriverComponent[] }>('/driver/components', { signal })
export const createDriverPlan = (projectId: string, body: DriverPlanInput, csrf: string, key: string) =>
  request<DriverPlan>(`/projects/${encodeURIComponent(projectId)}/driver/plans`,
    { method: 'POST', headers: { 'X-CSRF-Token': csrf, 'Idempotency-Key': key }, body: JSON.stringify(body) })
export const listDriverPlans = (projectId: string, signal: AbortSignal, page = 1) =>
  request<{ items: DriverPlan[]; total: number }>(`/projects/${encodeURIComponent(projectId)}/driver/plans?page=${page}&page_size=20`, { signal })
export const getDriverPlan = (projectId: string, planId: string, signal: AbortSignal) =>
  request<DriverPlanDetail>(`/projects/${encodeURIComponent(projectId)}/driver/plans/${encodeURIComponent(planId)}`, { signal })
export const precheckDriverPlan = (projectId: string, planId: string, csrf: string, key: string) =>
  request<DriverPlanDetail>(`/projects/${encodeURIComponent(projectId)}/driver/plans/${encodeURIComponent(planId)}/precheck`,
    { method: 'POST', headers: { 'X-CSRF-Token': csrf, 'Idempotency-Key': key } })
export const startDriverPlan = (projectId: string, planId: string, csrf: string, key: string) =>
  request<DriverPlanDetail>(`/projects/${encodeURIComponent(projectId)}/driver/plans/${encodeURIComponent(planId)}/start`,
    { method: 'POST', headers: { 'X-CSRF-Token': csrf, 'Idempotency-Key': key } })
export const advanceDriverPlan = (projectId: string, planId: string, body: DriverAdvanceInput, csrf: string, key: string) =>
  request<DriverPlanDetail>(`/projects/${encodeURIComponent(projectId)}/driver/plans/${encodeURIComponent(planId)}/advance`,
    { method: 'POST', headers: { 'X-CSRF-Token': csrf, 'Idempotency-Key': key }, body: JSON.stringify(body) })
export const rollbackDriverTarget = (projectId: string, planId: string, machineId: string, csrf: string, key: string) =>
  request<DriverPlanDetail>(`/projects/${encodeURIComponent(projectId)}/driver/plans/${encodeURIComponent(planId)}/targets/${encodeURIComponent(machineId)}/rollback`,
    { method: 'POST', headers: { 'X-CSRF-Token': csrf, 'Idempotency-Key': key } })
export const manualRecoverDriverTarget = (projectId: string, planId: string, machineId: string, note: string, csrf: string, key: string) =>
  request<DriverPlanDetail>(`/projects/${encodeURIComponent(projectId)}/driver/plans/${encodeURIComponent(planId)}/targets/${encodeURIComponent(machineId)}/manual`,
    { method: 'POST', headers: { 'X-CSRF-Token': csrf, 'Idempotency-Key': key }, body: JSON.stringify({ note }) })
export const abortDriverPlan = (projectId: string, planId: string, csrf: string, key: string) =>
  request<DriverPlanDetail>(`/projects/${encodeURIComponent(projectId)}/driver/plans/${encodeURIComponent(planId)}/abort`,
    { method: 'POST', headers: { 'X-CSRF-Token': csrf, 'Idempotency-Key': key } })

// Pulls approved media of a given type for the plan form dropdowns.
export const listApprovedMedia = (projectId: string, type: string, signal: AbortSignal): Promise<MediaSummary[]> => {
  const collect = async (page: number, acc: MediaSummary[]): Promise<MediaSummary[]> => {
    const pageData = await listMedia(projectId, page, signal)
    const items = (pageData.items ?? []).filter((m) => m.type === type && m.state === 'approved')
    const merged = [...acc, ...items]
    if (pageData.items.length < 20 || merged.length >= 60) return merged
    return collect(page + 1, merged)
  }
  return collect(1, [])
}
