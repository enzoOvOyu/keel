import type { components } from '../generated/api'

export type MediaItemSummary = components['schemas']['MediaItemSummary']

const pageQuery = (page: number) => new URLSearchParams({ page: String(page), page_size: '20' }).toString()
const read = <T>(path: string, signal: AbortSignal) => fetch(`/api/v1${path}`, { signal, credentials: 'same-origin', cache: 'no-store', headers: { Accept: 'application/json' } }).then(async (response) => {
  if (!response.ok) throw Object.assign(new Error(`HTTP ${response.status}`), { status: response.status })
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

export const listMedia = (projectId: string, page: number, signal: AbortSignal) =>
  read<components['schemas']['MediaPage']>(`/projects/${encodeURIComponent(projectId)}/media?${pageQuery(page)}`, signal)
export const registerMedia = (projectId: string, body: components['schemas']['RegisterMediaInput'], csrf: string, key: string) =>
  write<MediaItemSummary>(`/projects/${encodeURIComponent(projectId)}/media`, 'POST', body, csrf, key)
export const transitionMedia = (projectId: string, mediaId: string, state: string, csrf: string) =>
  write<MediaItemSummary>(`/projects/${encodeURIComponent(projectId)}/media/${encodeURIComponent(mediaId)}/transition`, 'POST', { state }, csrf)
