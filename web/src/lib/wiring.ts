import type { components } from '../generated/api'
import { request } from './api'
export type WiringTarget = components['schemas']['WiringTargetInput']
export type WiringFacts = components['schemas']['WiringFacts']
export type WiringHandshake = components['schemas']['WiringHandshake']
const base = (project: string) => `/projects/${encodeURIComponent(project)}/validation/wiring`
export const previewWiring = (project: string, targets: WiringTarget[], csrf: string) => request<WiringFacts>(`${base(project)}/scope`, { method: 'POST', headers: { 'X-CSRF-Token': csrf }, body: JSON.stringify({ targets }) })
export const createWiringHandshake = (project: string, targets: WiringTarget[], reason: string, csrf: string, key: string) => request<WiringHandshake>(`${base(project)}/handshakes`, { method: 'POST', headers: { 'X-CSRF-Token': csrf, 'Idempotency-Key': key }, body: JSON.stringify({ targets, reason }) })
export const listWiringHandshakes = (project: string, page: number, signal: AbortSignal) => request<components['schemas']['WiringHandshakePage']>(`${base(project)}/handshakes?page=${page}&page_size=10`, { signal })
export const getWiringHandshake = (project: string, id: string, signal: AbortSignal) => request<WiringHandshake>(`${base(project)}/handshakes/${encodeURIComponent(id)}`, { signal })
