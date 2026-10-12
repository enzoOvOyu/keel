import type { components } from '../generated/api'

export type DeviceModelSummary = components['schemas']['DeviceModelSummary']
export type DeviceModelVersionDetail = components['schemas']['DeviceModelVersionDetail']
export type ModelSlot = components['schemas']['ModelSlot']

const pageQuery = (page: number) => new URLSearchParams({ page: String(page), page_size: '20' }).toString()
const read = <T>(path: string, signal: AbortSignal) => fetch(`/api/v1${path}`, { signal, credentials: 'same-origin', cache: 'no-store', headers: { Accept: 'application/json' } }).then(async (response) => {
  if (!response.ok) throw new Error(`HTTP ${response.status}`)
  return await response.json() as T
})
const write = <T>(path: string, method: string, body: unknown, csrf: string, key?: string) => fetch(`/api/v1${path}`, {
  method, credentials: 'same-origin', cache: 'no-store',
  headers: { Accept: 'application/json', 'Content-Type': 'application/json', 'X-CSRF-Token': csrf, ...(key ? { 'Idempotency-Key': key } : {}) },
  body: JSON.stringify(body),
}).then(async (response) => {
  const value = await response.json().catch(() => null)
  if (!response.ok) {
    const problem = value as { code?: string } | null
    throw Object.assign(new Error(problem?.code ?? `HTTP ${response.status}`), { status: response.status, code: problem?.code })
  }
  return value as T
})

export const listDeviceModels = (page: number, signal: AbortSignal) =>
  read<components['schemas']['DeviceModelPage']>(`/models?${pageQuery(page)}`, signal)
export const listProjectModels = (projectId: string, page: number, signal: AbortSignal) =>
  read<components['schemas']['DeviceModelPage']>(`/projects/${encodeURIComponent(projectId)}/models?${pageQuery(page)}`, signal)
export const listModelVersions = (projectId: string, modelId: string, page: number, signal: AbortSignal) =>
  read<components['schemas']['DeviceModelVersionPage']>(`/projects/${encodeURIComponent(projectId)}/models/${encodeURIComponent(modelId)}/versions?${pageQuery(page)}`, signal)
export const createDeviceModel = (projectId: string, body: components['schemas']['CreateDeviceModelInput'], csrf: string, key: string) =>
  write<DeviceModelSummary>(`/projects/${encodeURIComponent(projectId)}/models`, 'POST', body, csrf, key)
export const publishModelVersion = (projectId: string, modelId: string, body: components['schemas']['PublishModelVersionInput'], csrf: string, key: string) =>
  write<DeviceModelVersionDetail>(`/projects/${encodeURIComponent(projectId)}/models/${encodeURIComponent(modelId)}/versions`, 'POST', body, csrf, key)
export const retireDeviceModel = (projectId: string, modelId: string, csrf: string) =>
  write<DeviceModelSummary>(`/projects/${encodeURIComponent(projectId)}/models/${encodeURIComponent(modelId)}/retire`, 'POST', {}, csrf)
