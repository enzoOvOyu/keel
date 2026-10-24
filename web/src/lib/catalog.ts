import { request } from './api'
import type { components } from '../generated/api'
export type CatalogKey = components['schemas']['ParamKeySummary']
export type CatalogValidation = components['schemas']['CatalogValidation']
export type CatalogValues = Record<string, string | number | boolean>
export async function listCatalog(projectId: string, signal: AbortSignal): Promise<CatalogKey[]> {
  const items: CatalogKey[] = []
  for (let page = 1; page <= 41; page++) {
    const data = await request<components['schemas']['ParamKeyPage']>(`/projects/${encodeURIComponent(projectId)}/params?page=${page}&page_size=100`, { signal })
    items.push(...data.items)
    if (items.length >= data.total) return items
  }
  throw new Error('CATALOG_TOO_LARGE')
}
export const validateCatalog = (projectId: string, values: CatalogValues, csrf: string, signal?: AbortSignal) =>
  request<CatalogValidation>(`/projects/${encodeURIComponent(projectId)}/params/validate`, { method: 'POST', signal, headers: { 'X-CSRF-Token': csrf }, body: JSON.stringify({ values }) })

export type CatalogEdit = components['schemas']['UpsertParamKeyInput']
export const listCatalogManagement = (projectId: string, page: number, signal: AbortSignal) =>
  request<components['schemas']['ParamKeyPage']>(`/projects/${encodeURIComponent(projectId)}/params?page=${page}&page_size=20&include_retired=true`, { signal })
export const saveCatalogKey = (projectId: string, input: CatalogEdit, csrf: string) =>
  request<CatalogKey>(`/projects/${encodeURIComponent(projectId)}/params`, { method: 'PUT', headers: { 'X-CSRF-Token': csrf }, body: JSON.stringify(input) })
export const retireCatalogKey = (projectId: string, entry: CatalogKey, csrf: string) =>
  request<CatalogKey>(`/projects/${encodeURIComponent(projectId)}/params/${entry.id}/retire`, { method: 'POST', headers: { 'X-CSRF-Token': csrf }, body: JSON.stringify({ expected_updated_at: entry.updated_at } satisfies components['schemas']['RetireParamKeyInput']) })
export async function catalogApprovedMedia(projectId: string, signal: AbortSignal) {
  const items: components['schemas']['MediaItemSummary'][] = []
  for (let page = 1; page <= 41; page++) {
    const data = await request<components['schemas']['MediaPage']>(`/projects/${encodeURIComponent(projectId)}/media?page=${page}&page_size=100`, { signal })
    items.push(...data.items.filter(item => item.state === 'approved' && item.artifact_stored))
    if (page * data.page_size >= data.total) return items
  }
  throw new Error('CATALOG_TOO_LARGE')
}
