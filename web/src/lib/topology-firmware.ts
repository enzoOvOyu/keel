import type { components } from '../generated/api'
import type { components as execution } from '../generated/execution'
import { request } from './api'
export type FirmwarePlan = components['schemas']['TopologyFirmwarePlan']
const base = (issue: string) => `/issues/${encodeURIComponent(issue)}/firmware-mock`
export const getFirmwareWorkspace = (issue: string, signal: AbortSignal) => request<components['schemas']['TopologyFirmwareWorkspace']>(base(issue), { signal })
export const createFirmwarePlan = (issue: string, body: components['schemas']['TopologyFirmwareInput'], csrf: string, key: string) => request<FirmwarePlan>(base(issue), { method: 'POST', headers: { 'X-CSRF-Token': csrf, 'Idempotency-Key': key }, body: JSON.stringify(body) })
export const approveFirmwarePlan = (issue: string, plan: string, body: components['schemas']['TopologyFirmwareApproval'], csrf: string) => request<FirmwarePlan>(`${base(issue)}/${encodeURIComponent(plan)}/approve`, { method: 'POST', headers: { 'X-CSRF-Token': csrf }, body: JSON.stringify(body) })
export const executeFirmwarePlan = (issue: string, plan: string, body: components['schemas']['TopologyFirmwareApproval'], csrf: string, key: string) => request<execution['schemas']['Execution']>(`${base(issue)}/${encodeURIComponent(plan)}/executions`, { method: 'POST', headers: { 'X-CSRF-Token': csrf, 'Idempotency-Key': key }, body: JSON.stringify(body) })
