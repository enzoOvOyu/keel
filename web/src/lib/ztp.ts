import type { components } from '../generated/api'
import { request } from './api'
export type ZTPSummary = components['schemas']['ZTPSessionSummary']
export type ZTPDetail = components['schemas']['ZTPSessionDetail']
export type ZTPSimInput = components['schemas']['ZTPSimSessionInput']
export type ZTPAction = components['schemas']['ZTPSessionAction']
const base = (projectId: string) => `/projects/${encodeURIComponent(projectId)}/ztp/sim-sessions`
export const listZTPSimSessions = (projectId: string, signal?: AbortSignal) => request<ZTPSummary[]>(base(projectId), { signal })
export const getZTPSimSession = (projectId: string, id: string, signal?: AbortSignal) => request<ZTPDetail>(`${base(projectId)}/${encodeURIComponent(id)}`, { signal })
export const createZTPSimSession = (projectId: string, input: ZTPSimInput, csrf: string) => request<ZTPDetail>(base(projectId), { method: 'POST', headers: { 'X-CSRF-Token': csrf }, body: JSON.stringify(input) })
export const actZTPSimSession = (projectId: string, id: string, input: ZTPAction, csrf: string) => request<ZTPDetail>(`${base(projectId)}/${encodeURIComponent(id)}/actions`, { method: 'POST', headers: { 'X-CSRF-Token': csrf }, body: JSON.stringify(input) })
