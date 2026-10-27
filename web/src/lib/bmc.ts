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
export type BMCPolicy = components['schemas']['BMCPolicy']
export type BMCPolicyInput = components['schemas']['BMCPolicyInput']
const policyBase = (project: string, asset: string) => `/projects/${encodeURIComponent(project)}/assets/${encodeURIComponent(asset)}/bmc/policies`
export const listBMCPolicies = (project: string, asset: string, signal: AbortSignal) => request<BMCPolicy[]>(policyBase(project, asset), { signal })
export const saveBMCPolicy = (project: string, asset: string, id: string, body: BMCPolicyInput, csrf: string, key: string) => request<BMCPolicy>(`${policyBase(project, asset)}${id ? `/${encodeURIComponent(id)}` : ''}`, { method: id ? 'PUT' : 'POST', headers: { 'X-CSRF-Token': csrf, 'Idempotency-Key': key }, body: JSON.stringify(body) })
export const listBMCPolicyRuns = (project: string, asset: string, policy: string, page: number, signal: AbortSignal) => request<components['schemas']['BMCPolicyRunPage']>(`${policyBase(project, asset)}/${encodeURIComponent(policy)}/runs?page=${page}&page_size=20`, { signal })
export type BMCCredentialVerificationInput = components['schemas']['BMCCredentialVerificationInput']
const batchBase = (project: string) => `/projects/${encodeURIComponent(project)}/bmc/credential-batches`
const verificationBase = (project: string, batch: string) => `${batchBase(project)}/${encodeURIComponent(batch)}/verifications`
export const listBMCCredentialBatches = (project: string, page: number, signal: AbortSignal) => request<components['schemas']['BMCCredentialBatchPage']>(`${batchBase(project)}?page=${page}&page_size=20`, { signal })
export const previewBMCCredentialVerification = (project: string, batch: string, signal: AbortSignal) => request<components['schemas']['BMCCredentialVerificationScope']>(`${batchBase(project)}/${encodeURIComponent(batch)}/verification-scope`, { signal })
export const listBMCCredentialVerifications = (project: string, batch: string, page: number, signal: AbortSignal) => request<components['schemas']['BMCCredentialVerificationPage']>(`${verificationBase(project, batch)}?page=${page}&page_size=20`, { signal })
export const getBMCCredentialVerification = (project: string, batch: string, id: string, signal: AbortSignal) => request<components['schemas']['BMCCredentialVerification']>(`${verificationBase(project, batch)}/${encodeURIComponent(id)}`, { signal })
export const createBMCCredentialVerification = (project: string, batch: string, body: BMCCredentialVerificationInput, csrf: string, key: string) => request<components['schemas']['BMCCredentialVerification']>(verificationBase(project, batch), { method: 'POST', headers: { 'X-CSRF-Token': csrf, 'Idempotency-Key': key }, body: JSON.stringify(body) })
