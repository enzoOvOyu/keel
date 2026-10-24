import { request } from './api'
import type { components } from '../generated/api'

export type DeviceModelSummary = components['schemas']['DeviceModelSummary']
export type DeviceModelVersionDetail = components['schemas']['DeviceModelVersionDetail']
export type ModelSlot = components['schemas']['ModelSlot']

const modelsBase = (projectId: string | null) => projectId === null ? '/admin/models' : `/projects/${encodeURIComponent(projectId)}/models`
const pageQuery = (page: number) => new URLSearchParams({ page: String(page), page_size: '20' }).toString()
const write = <T>(path: string, body: unknown, csrf: string, key?: string) =>
  request<T>(path, { method: 'POST', headers: { 'X-CSRF-Token': csrf, ...(key ? { 'Idempotency-Key': key } : {}) }, body: JSON.stringify(body) })

export const listDeviceModels = (page: number, signal: AbortSignal) =>
  request<components['schemas']['DeviceModelPage']>(`/models?${pageQuery(page)}`, { signal })
export const listProjectModels = (projectId: string | null, page: number, signal: AbortSignal) =>
  request<components['schemas']['DeviceModelPage']>(`${modelsBase(projectId)}?${pageQuery(page)}`, { signal })
export const listModelVersions = (projectId: string | null, modelId: string, page: number, signal: AbortSignal) =>
  request<components['schemas']['DeviceModelVersionPage']>(`${modelsBase(projectId)}/${encodeURIComponent(modelId)}/versions?${pageQuery(page)}`, { signal })
export const createDeviceModel = (projectId: string | null, body: components['schemas']['CreateDeviceModelInput'], csrf: string, key: string) =>
  write<DeviceModelSummary>(`${modelsBase(projectId)}`, body, csrf, key)
export const publishModelVersion = (projectId: string | null, modelId: string, body: components['schemas']['PublishModelVersionInput'], csrf: string, key: string) =>
  write<DeviceModelVersionDetail>(`${modelsBase(projectId)}/${encodeURIComponent(modelId)}/versions`, body, csrf, key)
export const retireDeviceModel = (projectId: string | null, modelId: string, csrf: string) =>
  write<DeviceModelSummary>(`${modelsBase(projectId)}/${encodeURIComponent(modelId)}/retire`, {}, csrf)

export type ModelDraft = components['schemas']['ModelDraft']
export type ImportScanInput = components['schemas']['ImportModelScanInput']
export const importModelScan = (projectId: string | null, body: ImportScanInput, csrf: string, key: string) =>
  write<ModelDraft>(`${modelsBase(projectId)}/import`, body, csrf, key)
export const listModelDrafts = (projectId: string | null, modelId: string, signal: AbortSignal) =>
  request<{ items: ModelDraft[] }>(`${modelsBase(projectId)}/${encodeURIComponent(modelId)}/drafts`, { signal })
export const discardModelDraft = (projectId: string | null, modelId: string, draftId: string, csrf: string) =>
  write<{ discarded: boolean }>(`${modelsBase(projectId)}/${encodeURIComponent(modelId)}/drafts/${encodeURIComponent(draftId)}/discard`, {}, csrf)
