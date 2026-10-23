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
export type ZTPMockKeaOptions = components['schemas']['ZTPMockKeaOptions']
export type ZTPMockKeaPlan = components['schemas']['ZTPMockKeaPlan']
export const stageZTPMockKea = (projectId: string, input: ZTPMockKeaOptions, csrf: string) => request<ZTPMockKeaPlan>(`/projects/${encodeURIComponent(projectId)}/ztp/mock-kea/plans/stage`, { method: 'POST', headers: { 'X-CSRF-Token': csrf }, body: JSON.stringify(input) })
export const commitZTPMockKea = (projectId: string, input: components['schemas']['ZTPMockKeaCommitInput'], csrf: string) => request<ZTPMockKeaPlan>(`/projects/${encodeURIComponent(projectId)}/ztp/mock-kea/plans/commit`, { method: 'POST', headers: { 'X-CSRF-Token': csrf }, body: JSON.stringify(input) })
export type ZTPIsolationScriptManifest = components['schemas']['ZTPIsolationScriptManifest']
export const getZTPIsolationScriptManifest = (projectId: string, signal?: AbortSignal) => request<ZTPIsolationScriptManifest>(`/projects/${encodeURIComponent(projectId)}/ztp/isolation-script`, { signal })

export type ZTPDeviceObservation = components['schemas']['ZTPDeviceObservation']
export const listZTPDeviceObservations = (projectId: string, signal?: AbortSignal) => request<ZTPDeviceObservation[]>(`/projects/${encodeURIComponent(projectId)}/ztp/device-observations`, { signal })

export const deactivateZTPMockKea = (projectId: string, input: components['schemas']['ZTPMockKeaDeactivateInput'], csrf: string) => request<ZTPMockKeaPlan>(`/projects/${encodeURIComponent(projectId)}/ztp/mock-kea/plans/deactivate`, { method: 'POST', headers: { 'X-CSRF-Token': csrf }, body: JSON.stringify(input) })

export type ZTPIdentityIssue = components['schemas']['ZTPIdentityIssue']
export const listZTPIdentityIssues = (projectId: string, signal?: AbortSignal) => request<ZTPIdentityIssue[]>(`/projects/${encodeURIComponent(projectId)}/ztp/identity-issues`, { signal })

export const resolveZTPIdentityIssue = (projectId: string, issueId: string, reason: string, csrf: string) => request<ZTPIdentityIssue>(`/projects/${encodeURIComponent(projectId)}/ztp/identity-issues/${encodeURIComponent(issueId)}/resolve`, { method: 'POST', headers: { 'X-CSRF-Token': csrf }, body: JSON.stringify({ reason }) })

export type ZTPCredentialIssue = components['schemas']['ZTPCredentialIssue']
export const listZTPCredentialIssues = (projectId: string, signal?: AbortSignal) => request<ZTPCredentialIssue[]>(`/projects/${encodeURIComponent(projectId)}/ztp/credential-issues`, { signal })

export const runZTPSimSession = (projectId: string, id: string, input: components['schemas']['ZTPSimRunInput'], csrf: string) => request<ZTPDetail>(`${base(projectId)}/${encodeURIComponent(id)}/run`, { method: 'POST', headers: { 'X-CSRF-Token': csrf }, body: JSON.stringify(input) })
