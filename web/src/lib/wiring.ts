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

export type WiringBatch = components['schemas']['WiringBatch']
export const createWiringBatch = (project: string, handshakeId: string, factsHash: string, reason: string, csrf: string, key: string) => request<WiringBatch>(`${base(project)}/batches`, { method: 'POST', headers: { 'X-CSRF-Token': csrf, 'Idempotency-Key': key }, body: JSON.stringify({ handshake_id: handshakeId, facts_hash: factsHash, reason }) })
export const getWiringBatch = (project: string, id: string, signal: AbortSignal) => request<WiringBatch>(`${base(project)}/batches/${encodeURIComponent(id)}`, { signal })

export const createWiringExecution = (project: string, body: components['schemas']['WiringRunInput'], csrf: string, key: string) => request<import('../generated/execution').components['schemas']['Execution']>(`${base(project)}/executions`, { method: 'POST', headers: { 'X-CSRF-Token': csrf, 'Idempotency-Key': key }, body: JSON.stringify(body) })

export const confirmWiring = (project: string, batch: string, body: components['schemas']['WiringConfirmationInput'], csrf: string, key: string) => request<components['schemas']['WiringConfirmation']>(`${base(project)}/batches/${encodeURIComponent(batch)}/confirmations`, { method: 'POST', headers: { 'X-CSRF-Token': csrf, 'Idempotency-Key': key }, body: JSON.stringify(body) })
export const getWiringConfirmation = (project: string, id: string, signal: AbortSignal) => request<components['schemas']['WiringConfirmation']>(`${base(project)}/confirmations/${encodeURIComponent(id)}`, { signal })

export const previewWiringRetest = (issue: string, signal: AbortSignal) => request<components['schemas']['WiringRetestScope']>(`/issues/${encodeURIComponent(issue)}/wiring-retests/scope`, { signal })
export const listWiringRetests = (issue: string, page: number, signal: AbortSignal) => request<components['schemas']['WiringRetestPage']>(`/issues/${encodeURIComponent(issue)}/wiring-retests?page=${page}&page_size=10`, { signal })
export const createWiringRetest = (issue: string, body: components['schemas']['WiringRetestInput'], csrf: string, key: string) => request<components['schemas']['WiringRetest']>(`/issues/${encodeURIComponent(issue)}/wiring-retests`, { method: 'POST', headers: { 'X-CSRF-Token': csrf, 'Idempotency-Key': key }, body: JSON.stringify(body) })
