import type { components } from '../generated/api'
import { request } from './api'
export type ManagementPlan = NonNullable<components['schemas']['ZTPManagementPlan']>
export type NetworkBaselineInput = components['schemas']['CreateNetworkBaselineInput']
export type NetworkBaseline = components['schemas']['NetworkBaselineVersion']
const path = (id: string) => `/projects/${encodeURIComponent(id)}`
const post = <T>(url: string, body: unknown, csrf: string) => request<T>(url, { method: 'POST', headers: { 'X-CSRF-Token': csrf }, body: JSON.stringify(body) })
export const latestManagementPlan = (id: string, signal: AbortSignal) => request<ManagementPlan | null>(`${path(id)}/ztp/management-plan`, { signal })
export const previewManagementPlan = (id: string, ref: string, csrf: string) => post<ManagementPlan>(`${path(id)}/ztp/management-plan/preview`, { baseline_ref: ref }, csrf)
export const commitManagementPlan = (id: string, plan: ManagementPlan, csrf: string) => post<ManagementPlan>(`${path(id)}/ztp/management-plan/commit`, { baseline_ref: plan.baseline_ref, expected_hash: plan.content_hash }, csrf)
export const importNetworkBaseline = (id: string, input: NetworkBaselineInput, csrf: string) => post<NetworkBaseline>(`${path(id)}/network-baselines`, input, csrf)
export const approveNetworkBaseline = (id: string, ref: string, customer: string, network: string, csrf: string) => post<NetworkBaseline>(`${path(id)}/network-baselines/${encodeURIComponent(ref)}/approve`, { customer_approval_ref: customer, network_approval_ref: network }, csrf)
export type EnvironmentReport = NonNullable<components['schemas']['ZTPEnvironmentReport']>
export const latestEnvironmentReport = (id: string, batch: string, channel: '239'|'143', signal: AbortSignal) => request<EnvironmentReport|null>(`${path(id)}/ztp/environment-checks?${new URLSearchParams({batch_ref:batch,boot_channel:channel})}`,{signal})
export const runEnvironmentCheck = (id: string, batch: string, channel: '239'|'143', csrf: string) => post<EnvironmentReport>(`${path(id)}/ztp/environment-checks`,{batch_ref:batch,boot_channel:channel},csrf)
