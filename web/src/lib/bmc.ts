import type { components } from '../generated/api'
import type { components as execution } from '../generated/execution'
import { ApiError, request } from './api'
export type BMCInput = components['schemas']['BMCCollectionInput']
export type BMCManifest = components['schemas']['BMCCollectionManifest']
export type BMCResource = components['schemas']['BMCResource']
const base = (project: string, asset: string) => `/projects/${encodeURIComponent(project)}/assets/${encodeURIComponent(asset)}/bmc/collections`
export const listBMCCollections = (project: string, asset: string, page: number, signal: AbortSignal) => request<components['schemas']['BMCCollectionPage']>(`${base(project, asset)}?page=${page}&page_size=20`, { signal })
export const getBMCManifest = (project: string, asset: string, collection: string, signal: AbortSignal) => request<BMCManifest>(`${base(project, asset)}/${encodeURIComponent(collection)}/manifest`, { signal })
export const listBMCResources = (project: string, asset: string, collection: string, page: number, signal: AbortSignal) => request<components['schemas']['BMCResourcePage']>(`${base(project, asset)}/${encodeURIComponent(collection)}/resources?page=${page}&page_size=20`, { signal })
export const getBMCResource = (project: string, asset: string, collection: string, path: string, signal: AbortSignal) => request<BMCResource>(`${base(project, asset)}/${encodeURIComponent(collection)}/resource?${new URLSearchParams({ path })}`, { signal })
export const createBMCCollection = (project: string, asset: string, body: BMCInput, csrf: string, key: string) => request<execution['schemas']['Execution']>(base(project, asset), { method: 'POST', headers: { 'X-CSRF-Token': csrf, 'Idempotency-Key': key }, body: JSON.stringify(body) })
export async function downloadBMCRaw(project: string, asset: string, collection: string, path: string, signal?: AbortSignal) {
 let response: Response
 try { response = await fetch(`/api/v1${base(project, asset)}/${encodeURIComponent(collection)}/raw?${new URLSearchParams({ path })}`, { credentials: 'same-origin', cache: 'no-store', signal, headers: { Accept: 'application/json' } }) }
 catch (error) { if (error instanceof DOMException && error.name === 'AbortError') throw error; throw new ApiError(0, 'NETWORK_ERROR') }
 if (!response.ok) { let problem: { code?: string; trace_id?: string } = {}; try { problem = await response.json() } catch { /* Preserve safe proxy failure. */ }; throw new ApiError(response.status, problem.code ?? 'HTTP_ERROR', problem.trace_id ?? '') }
 const blob = await response.blob(), url = URL.createObjectURL(blob), link = document.createElement('a'); link.href = url; link.download = `bmc-${collection.slice(0, 8)}-resource.json`; link.click(); window.setTimeout(() => URL.revokeObjectURL(url), 1000)
}
