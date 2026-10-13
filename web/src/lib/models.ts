import { request } from './api'
import type { components } from '../generated/api'

export type DeviceModelSummary = components['schemas']['DeviceModelSummary']
export type DeviceModelVersionDetail = components['schemas']['DeviceModelVersionDetail']
export type ModelSlot = components['schemas']['ModelSlot']

const pageQuery = (page: number) => new URLSearchParams({ page: String(page), page_size: '20' }).toString()
const write = <T>(path: string, body: unknown, csrf: string, key?: string) =>
  request<T>(path, { method: 'POST', headers: { 'X-CSRF-Token': csrf, ...(key ? { 'Idempotency-Key': key } : {}) }, body: JSON.stringify(body) })

export const listDeviceModels = (page: number, signal: AbortSignal) =>
  request<components['schemas']['DeviceModelPage']>(`/models?${pageQuery(page)}`, { signal })
export const listProjectModels = (projectId: string, page: number, signal: AbortSignal) =>
  request<components['schemas']['DeviceModelPage']>(`/projects/${encodeURIComponent(projectId)}/models?${pageQuery(page)}`, { signal })
export const listModelVersions = (projectId: string, modelId: string, page: number, signal: AbortSignal) =>
  request<components['schemas']['DeviceModelVersionPage']>(`/projects/${encodeURIComponent(projectId)}/models/${encodeURIComponent(modelId)}/versions?${pageQuery(page)}`, { signal })
export const createDeviceModel = (projectId: string, body: components['schemas']['CreateDeviceModelInput'], csrf: string, key: string) =>
  write<DeviceModelSummary>(`/projects/${encodeURIComponent(projectId)}/models`, body, csrf, key)
export const publishModelVersion = (projectId: string, modelId: string, body: components['schemas']['PublishModelVersionInput'], csrf: string, key: string) =>
  write<DeviceModelVersionDetail>(`/projects/${encodeURIComponent(projectId)}/models/${encodeURIComponent(modelId)}/versions`, body, csrf, key)
export const retireDeviceModel = (projectId: string, modelId: string, csrf: string) =>
  write<DeviceModelSummary>(`/projects/${encodeURIComponent(projectId)}/models/${encodeURIComponent(modelId)}/retire`, {}, csrf)

export type ModelDraft = { id: string; model_id: string; model_name: string; source_host: string; hw_spec: Record<string, unknown>; expected_topology?: Record<string, unknown>; panel_layout?: Record<string, unknown>; bom_expect?: Record<string, unknown>; default_role_plan?: Record<string, unknown>; slots?: ModelSlot[]; content_hash: string; created_by: string; created_at: string }
export type ImportScanInput = { source_host: string; model_name?: string; hw_spec: Record<string, unknown>; expected_topology?: Record<string, unknown>; panel_layout?: Record<string, unknown>; bom_expect?: Record<string, unknown>; default_role_plan?: Record<string, unknown>; slots?: ModelSlot[] }
export const importModelScan = (projectId: string, body: ImportScanInput, csrf: string, key: string) =>
  write<ModelDraft>(`/projects/${encodeURIComponent(projectId)}/models/import`, body, csrf, key)
export const listModelDrafts = (projectId: string, modelId: string, signal: AbortSignal) =>
  request<{ items: ModelDraft[] }>(`/projects/${encodeURIComponent(projectId)}/models/${encodeURIComponent(modelId)}/drafts`, { signal })
export const discardModelDraft = (projectId: string, modelId: string, draftId: string, csrf: string) =>
  write<{ discarded: boolean }>(`/projects/${encodeURIComponent(projectId)}/models/${encodeURIComponent(modelId)}/drafts/${encodeURIComponent(draftId)}/discard`, {}, csrf)
