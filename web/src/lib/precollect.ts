import type { components } from '../generated/api'
import type { components as execution } from '../generated/execution'
import { ApiError, request } from './api'
export type PrecollectScope = components['schemas']['PrecollectScope']
export type PrecollectRecord = components['schemas']['PrecollectRecord']
export type PrecollectInput = components['schemas']['PrecollectCreateInput']
const assetBase = (project: string, asset: string) => `/projects/${encodeURIComponent(project)}/assets/${encodeURIComponent(asset)}`
export const previewPrecollect = (project: string, asset: string, signal: AbortSignal) => request<PrecollectScope>(`${assetBase(project, asset)}/precollect/scope`, { signal })
export const listPrecollect = (project: string, asset: string, page: number, signal: AbortSignal) => request<components['schemas']['PrecollectPage']>(`${assetBase(project, asset)}/precollect?page=${page}&page_size=20`, { signal })
export const getPrecollect = (project: string, asset: string, id: string, signal: AbortSignal) => request<PrecollectRecord>(`${assetBase(project, asset)}/precollect/${encodeURIComponent(id)}`, { signal })
export const currentG1 = (project: string, asset: string, signal: AbortSignal) => request<components['schemas']['CurrentG1']>(`${assetBase(project, asset)}/gates/G1`, { signal })
export const createPrecollect = (project: string, body: PrecollectInput, csrf: string, key: string) => request<execution['schemas']['Execution']>(`/projects/${encodeURIComponent(project)}/validation/precollect`, { method: 'POST', headers: { 'X-CSRF-Token': csrf, 'Idempotency-Key': key }, body: JSON.stringify(body) })
export async function downloadPrecollect(project: string, asset: string, id: string) {
 let response: Response
 try { response = await fetch(`/api/v1${assetBase(project, asset)}/precollect/${encodeURIComponent(id)}/raw`, { credentials: 'same-origin', cache: 'no-store', headers: { Accept: 'application/json' } }) } catch { throw new ApiError(0, 'NETWORK_ERROR') }
 if (!response.ok) { const body = await response.json().catch(() => null) as { code?: string; trace_id?: string } | null; throw new ApiError(response.status, body?.code ?? 'DOWNLOAD_FAILED', body?.trace_id ?? '') }
 const blob = await response.blob(), url = URL.createObjectURL(blob), link = document.createElement('a')
 link.href = url; link.download = `precollect-${id}.json`; link.click(); URL.revokeObjectURL(url)
}
