import type { components } from '../generated/api'
import { request } from './api'
export type MappingCandidate = components['schemas']['MappingCandidate']
export type MappingPublication = components['schemas']['MappingPublication']
export const mappingBase = (project: string, asset: string) => `/projects/${encodeURIComponent(project)}/assets/${encodeURIComponent(asset)}/mapping`
export const previewMapping = (project: string, asset: string, signal: AbortSignal) => request<MappingCandidate>(`${mappingBase(project, asset)}/preview`, { signal })
export const mappingHistory = (project: string, asset: string, page: number, signal: AbortSignal) => request<components['schemas']['MappingHistoryPage']>(`${mappingBase(project, asset)}/assessments?page=${page}&page_size=10`, { signal })
export const mappingAssessment = (project: string, asset: string, id: string, signal: AbortSignal) => request<MappingPublication>(`${mappingBase(project, asset)}/assessments/${encodeURIComponent(id)}`, { signal })
export const publishMapping = (project: string, asset: string, body: components['schemas']['MappingPublishInput'], csrf: string, key: string) => request<MappingPublication>(`${mappingBase(project, asset)}/revisions`, { method: 'POST', headers: { 'X-CSRF-Token': csrf, 'Idempotency-Key': key }, body: JSON.stringify(body) })

export const mappingFleet = (project: string, rail: string, page: number, signal: AbortSignal) => request<components['schemas']['MappingFleetPage']>(`/projects/${encodeURIComponent(project)}/mapping?rail=${encodeURIComponent(rail)}&page=${page}&page_size=10`, { signal })
export const mappingCorrections = (project: string, asset: string, page: number, signal: AbortSignal) => request<components['schemas']['MappingCorrectionPage']>(`${mappingBase(project, asset)}/corrections?page=${page}&page_size=10`, { signal })
export const requestMappingCorrection = (project: string, asset: string, body: components['schemas']['MappingCorrectionInput'], csrf: string, key: string) => request<components['schemas']['MappingCorrection']>(`${mappingBase(project, asset)}/corrections`, { method: 'POST', headers: { 'X-CSRF-Token': csrf, 'Idempotency-Key': key }, body: JSON.stringify(body) })
export const decideMappingCorrection = (project: string, asset: string, id: string, body: components['schemas']['MappingCorrectionDecisionInput'], csrf: string, key: string) => request<components['schemas']['MappingCorrection']>(`${mappingBase(project, asset)}/corrections/${encodeURIComponent(id)}/decision`, { method: 'POST', headers: { 'X-CSRF-Token': csrf, 'Idempotency-Key': key }, body: JSON.stringify(body) })
