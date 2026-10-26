import { useRef, useState } from 'react'
import type { components } from '../generated/api'
import { ApiError, request } from './api'
import { useExpireOnUnauthorized } from './session'
export type UserAccount = components['schemas']['UserAccount']
export type Member = components['schemas']['ProjectMember']
export type Audit = components['schemas']['AuditEntry']
export type ProjectRole = components['schemas']['ProjectRole']
export const projectRoles: ProjectRole[] = ['Viewer', 'DE', 'DL', 'NE', 'NDE', 'NH', 'SRE', 'Admin']
const pageQuery = (page: number, filter?: [string, string]) => {
  const params = new URLSearchParams({ page: String(page), page_size: '20' })
  if (filter?.[1]) params.set(...filter)
  return params.toString()
}
const write = <T>(path: string, method: string, body: unknown, csrf: string) => request<T>(path, { method, headers: { 'X-CSRF-Token': csrf }, body: JSON.stringify(body) })
export const listUsers = (page: number, q: string, signal: AbortSignal) => request<components['schemas']['UserPage']>(`/admin/users?${pageQuery(page, ['q', q])}`, { signal })
export const addUser = (body: components['schemas']['CreateUserRequest'], csrf: string) => write<UserAccount>('/admin/users', 'POST', body, csrf)
export const updateUser = (id: string, body: components['schemas']['UpdateUserRequest'], csrf: string) => write<UserAccount>(`/admin/users/${encodeURIComponent(id)}`, 'PATCH', body, csrf)
export const resetUserPassword = (id: string, body: components['schemas']['ResetPasswordRequest'], csrf: string) => write<UserAccount>(`/admin/users/${encodeURIComponent(id)}/password`, 'POST', body, csrf)
export const unlockUser = (id: string, body: components['schemas']['UnlockUserRequest'], csrf: string) => write<UserAccount>(`/admin/users/${encodeURIComponent(id)}/unlock`, 'POST', body, csrf)
export const changeOwnPassword = (body: components['schemas']['ChangeOwnPasswordRequest'], csrf: string) => write<void>('/auth/password', 'POST', body, csrf)
export const listMembers = (id: string, page: number, signal: AbortSignal) => request<components['schemas']['MemberPage']>(`/projects/${encodeURIComponent(id)}/members?${pageQuery(page)}`, { signal })
export const addMember = (id: string, body: components['schemas']['CreateMemberRequest'], csrf: string) => write<Member>(`/projects/${encodeURIComponent(id)}/members`, 'POST', body, csrf)
export const updateMember = (id: string, userId: string, body: components['schemas']['UpdateMemberRequest'], csrf: string) => write<Member>(`/projects/${encodeURIComponent(id)}/members/${encodeURIComponent(userId)}`, 'PATCH', body, csrf)
export const removeMember = (id: string, userId: string, version: number, csrf: string) => write<void>(`/projects/${encodeURIComponent(id)}/members/${encodeURIComponent(userId)}`, 'DELETE', { version } satisfies components['schemas']['VersionRequest'], csrf)
export const listAudits = (projectId: string | undefined, page: number, action: string, signal: AbortSignal) => request<components['schemas']['AuditPage']>(`${projectId ? `/projects/${encodeURIComponent(projectId)}/audits` : '/admin/audits'}?${pageQuery(page, ['action', action])}`, { signal })
export function useAction() {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<ApiError | null>(null)
  const busyRef = useRef(false)
  useExpireOnUnauthorized(error)
  async function run<T>(operation: () => Promise<T>): Promise<{ ok: true; value: T } | { ok: false }> {
    if (busyRef.current) return { ok: false }
    busyRef.current = true; setBusy(true); setError(null)
    try { return { ok: true, value: await operation() } }
    catch (cause) { setError(cause instanceof ApiError ? cause : new ApiError(0, 'NETWORK_ERROR')); return { ok: false } }
    finally { busyRef.current = false; setBusy(false) }
  }
  return { busy, error, run }
}
export function validNewPassword(value: string): boolean { const bytes = new TextEncoder().encode(value).length; return bytes >= 12 && bytes <= 1024 }
export function managementSearch(search: Record<string, unknown>): { page?: number; q?: string; action?: string } {
  const page = Number(search.page)
  return { ...(Number.isSafeInteger(page) && page > 0 && page <= 1_000_000 ? { page } : {}),
    ...(typeof search.q === 'string' && search.q.length <= 128 ? { q: search.q } : {}),
    ...(typeof search.action === 'string' && search.action.length <= 128 ? { action: search.action } : {}) }
}

export const listWebhookDeliveries = (projectId: string, page: number, signal: AbortSignal) => request<components['schemas']['WebhookDeliveryPage']>(`/projects/${encodeURIComponent(projectId)}/notifications/webhook-deliveries?${pageQuery(page)}`, { signal })
