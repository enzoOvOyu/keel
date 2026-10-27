import type { components } from '../generated/api'
import { ApiError, request } from './api'
export type AssetView = components['schemas']['AssetView']
export type AssetViewInput = components['schemas']['AssetViewInput']
export type AssetViewColumn = components['schemas']['AssetViewColumn']
export type AssetViewCell = components['schemas']['AssetViewCell']
export type AssetViewDefinition = components['schemas']['AssetViewDefinition']
const base = (project: string) => `/projects/${encodeURIComponent(project)}/asset-views`
export const listAssetViews = (project: string, page: number, signal: AbortSignal) => request<components['schemas']['AssetViewPage']>(`${base(project)}?page=${page}&page_size=20`, { signal })
export const getAssetView = (project: string, id: string, signal: AbortSignal) => request<AssetView>(`${base(project)}/${encodeURIComponent(id)}`, { signal })
export const saveAssetView = (project: string, id: string, body: AssetViewInput, csrf: string) => request<AssetView>(`${base(project)}/${encodeURIComponent(id)}`, { method: 'PUT', headers: { 'X-CSRF-Token': csrf }, body: JSON.stringify(body) })
export const assetViewData = (project: string, id: string, version: number, page: number, signal: AbortSignal) => request<components['schemas']['AssetViewData']>(`${base(project)}/${encodeURIComponent(id)}/data?version=${version}&page=${page}&page_size=20`, { signal })
export function exportAssetViewDefinition(view: AssetView) {
 const blob = new Blob([JSON.stringify({ schema: 'keel.asset-view/v1', name: view.name, definition: view.definition }, null, 2)], { type: 'application/json' }), url = URL.createObjectURL(blob), link = document.createElement('a')
 link.href = url; link.download = 'asset-view-template.json'; link.click(); URL.revokeObjectURL(url)
}
export async function exportAssetViewData(project: string, id: string, version: number) {
 let response: Response
 try { response = await fetch(`/api/v1${base(project)}/${encodeURIComponent(id)}/export.csv?version=${version}`, { credentials: 'same-origin', cache: 'no-store', headers: { Accept: 'text/csv' } }) } catch { throw new ApiError(0, 'NETWORK_ERROR') }
 if (!response.ok) { const error = await response.json().catch(() => null) as { code?: string; trace_id?: string } | null; throw new ApiError(response.status, error?.code ?? 'DOWNLOAD_FAILED', error?.trace_id ?? '') }
 const blob = await response.blob(), url = URL.createObjectURL(blob), link = document.createElement('a'); link.href = url; link.download = `asset-view-v${version}.csv`; link.click(); URL.revokeObjectURL(url)
}
