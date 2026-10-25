import { executionViewSearch } from './execution-views'
import type { components } from '../generated/execution'
import { request } from './api'

export type SimTarget = components['schemas']['SimTarget']
export type FlowVersion = components['schemas']['FlowVersion']
export type Execution = components['schemas']['ExecutionSummary']
export type ExecutionDetail = components['schemas']['ExecutionDetail']
export type ExecutionAttempt = components['schemas']['ExecutionAttempt']
export type ExecutionEvent = components['schemas']['ExecutionEvent']
export type SSHCreateInput = components['schemas']['SSHCreateInput']
export const createSSHExecution = (projectId: string, body: SSHCreateInput, csrf: string, key: string) => request<components['schemas']['Execution']>(`/projects/${encodeURIComponent(projectId)}/ssh-executions`, { method: 'POST', headers: { 'X-CSRF-Token': csrf, 'Idempotency-Key': key }, body: JSON.stringify(body) })

export const executionStates = ['created', 'scheduled', 'dispatching', 'running', 'pausing', 'paused', 'cancelling', 'reconciling', 'success', 'failed', 'killed', 'warning'] as const
export const attemptStates = ['pending', 'running', 'success', 'failed', 'cancelled', 'unknown', 'skipped'] as const

const pageQuery = (page: number) => new URLSearchParams({ page: String(page), page_size: '20' }).toString()
const read = <T>(path: string, signal: AbortSignal) => request<T>(path, { signal })
const write = <T>(path: string, method: string, body: unknown, csrf: string, key?: string) =>
  request<T>(path, { method, headers: { 'X-CSRF-Token': csrf, ...(key ? { 'Idempotency-Key': key } : {}) }, body: JSON.stringify(body) })

export const listSimTargets = (projectId: string, page: number, signal: AbortSignal, pageSize = 20) =>
  read<components['schemas']['SimTargetPage']>(`/projects/${encodeURIComponent(projectId)}/sim-targets?page=${page}&page_size=${pageSize}`, signal)
export const registerSimTargets = (projectId: string, body: components['schemas']['RegisterSimTargetsInput'], csrf: string, key: string) =>
  write<components['schemas']['RegisterSimTargetsResponse']>(`/projects/${encodeURIComponent(projectId)}/sim-targets`, 'POST', body, csrf, key)
export const listFlows = (projectId: string, page: number, signal: AbortSignal) =>
  read<components['schemas']['FlowVersionPage']>(`/projects/${encodeURIComponent(projectId)}/flows?${pageQuery(page)}`, signal)
export const publishFlow = (projectId: string, body: components['schemas']['PublishFlowInput'], csrf: string, key: string) =>
  write<components['schemas']['FlowVersion']>(`/projects/${encodeURIComponent(projectId)}/flows`, 'POST', body, csrf, key)
export const listExecutions = (projectId: string, page: number, signal: AbortSignal) =>
  read<components['schemas']['ExecutionPage']>(`/projects/${encodeURIComponent(projectId)}/executions?${pageQuery(page)}`, signal)
export const createExecution = (projectId: string, body: components['schemas']['CreateExecutionInput'], csrf: string, key: string) =>
  write<components['schemas']['Execution']>(`/projects/${encodeURIComponent(projectId)}/executions`, 'POST', body, csrf, key)
export const getExecution = (projectId: string, executionId: string, signal: AbortSignal) =>
  read<components['schemas']['ExecutionDetail']>(`/projects/${encodeURIComponent(projectId)}/executions/${encodeURIComponent(executionId)}`, signal)
export const listAttempts = (projectId: string, executionId: string, page: number, signal: AbortSignal) =>
  read<components['schemas']['ExecutionAttemptPage']>(`/projects/${encodeURIComponent(projectId)}/executions/${encodeURIComponent(executionId)}/attempts?${pageQuery(page)}`, signal)
export const controlExecution = (projectId: string, executionId: string, body: components['schemas']['ControlExecutionInput'], csrf: string) =>
  write<components['schemas']['Execution']>(`/projects/${encodeURIComponent(projectId)}/executions/${encodeURIComponent(executionId)}/control`, 'POST', body, csrf)
export const listExecutionEvents = (projectId: string, executionId: string, after: number, signal: AbortSignal) =>
  read<components['schemas']['ExecutionEventPage']>(`/projects/${encodeURIComponent(projectId)}/executions/${encodeURIComponent(executionId)}/events?after=${after}`, signal)

// The execution identifier travels as a validated search parameter, so a forged
// or malformed value falls back to the list instead of reaching the API.
export function executionSearch(search: Record<string, unknown>): { page?: number; execution?: string } & ReturnType<typeof executionViewSearch> {
  const page = Number(search.page)
  const execution = typeof search.execution === 'string' && /^[a-f0-9]{32}$/.test(search.execution) ? search.execution : undefined
  return { ...executionViewSearch(search), ...(Number.isSafeInteger(page) && page > 0 && page <= 1_000_000 ? { page } : {}), ...(execution ? { execution } : {}) }
}
