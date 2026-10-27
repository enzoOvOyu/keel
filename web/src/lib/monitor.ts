import type { components } from '../generated/api'
import type { components as execution } from '../generated/execution'
import { request } from './api'
export type MonitorPolicy = components['schemas']['MonitorPolicy']
export type MonitorPolicyRevision = components['schemas']['MonitorPolicyRevision']
export type MonitorObservation = components['schemas']['MonitorObservation']
export type MonitorAlert = components['schemas']['MonitorAlert']
export type MonitorTrend = components['schemas']['MonitorTrend']
export const monitorBase = (project: string) => `/projects/${encodeURIComponent(project)}/monitor`
export const monitorPolicies = (project: string, signal: AbortSignal) => request<components['schemas']['MonitorPolicies']>(`${monitorBase(project)}/policies`, { signal })
export const saveMonitorPolicy = (project: string, body: components['schemas']['MonitorPolicyInput'], csrf: string, key: string) => request<MonitorPolicyRevision>(`${monitorBase(project)}/policies`, { method: 'POST', headers: { 'X-CSRF-Token': csrf, 'Idempotency-Key': key }, body: JSON.stringify(body) })
export const monitorAlerts = (project: string, page: number, filters: Record<string, string>, signal: AbortSignal) => request<components['schemas']['MonitorAlertPage']>(`${monitorBase(project)}/alerts?${new URLSearchParams({ page: String(page), page_size: '20', ...filters })}`, { signal })
export const suppressMonitorAlert = (project: string, id: string, body: components['schemas']['MonitorSuppressInput'], csrf: string) => request<MonitorAlert>(`${monitorBase(project)}/alerts/${encodeURIComponent(id)}/suppress`, { method: 'POST', headers: { 'X-CSRF-Token': csrf }, body: JSON.stringify(body) })
const assetBase = (project: string, asset: string) => `/projects/${encodeURIComponent(project)}/assets/${encodeURIComponent(asset)}/monitor`
export const monitorSnapshot = (project: string, asset: string, cursor: number, signal: AbortSignal) => request<components['schemas']['MonitorSnapshot']>(`${assetBase(project, asset)}?cursor=${cursor}`, { signal })
export const monitorTrend = (project: string, asset: string, sample: MonitorObservation, window: string, signal: AbortSignal) => request<MonitorTrend>(`${assetBase(project, asset)}/trends?${new URLSearchParams({ key: sample.sample.key, entity: sample.sample.entity, source: sample.source, method: sample.method, origin: sample.origin, window })}`, { signal })

export const createMonitorSSHSnapshot = (project: string, body: components['schemas']['MonitorSSHInput'], csrf: string, key: string) => request<execution['schemas']['Execution']>(`${monitorBase(project)}/ssh-snapshots`, { method: 'POST', headers: { 'X-CSRF-Token': csrf, 'Idempotency-Key': key }, body: JSON.stringify(body) })

export const getPortMonitorIssueEvidence = (issue: string, signal: AbortSignal) => request<components['schemas']['PortMonitorIssueEvidence']>(`/issues/${encodeURIComponent(issue)}/monitor-evidence`, { signal })
