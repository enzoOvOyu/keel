import { request } from './api'

export type RoleSpec = { enabled: boolean; required: boolean }
export type RolePlan = {
  project_id: string; roles: Record<string, RoleSpec>; slot_overrides: Record<string, string>
  coverage: Record<string, number>; missing: string[]; warnings: string[]
}
export type RoleBinding = {
  port_id: string; port_name: string; rail: number; slot_no: number; pcie_slot: string
  expect_nic_model: string; role: string; default_role: string; overridden: boolean
}
export type RenderResult = {
  kind: string; rendered: string; diff: string; warnings: string[]
  applied: boolean; verified: boolean; receipt: string
}
export const logicalRoles = ['management', 'storage', 'compute', 'oob'] as const
export const slotRoles = [...logicalRoles, 'unused'] as const
export const renderKinds = ['gpu_network', 'nic_partition', 'gpu_nic_binding', 'roce_import'] as const

export const getRolePlan = (projectId: string, signal: AbortSignal) =>
  request<RolePlan>(`/projects/${encodeURIComponent(projectId)}/roles`, { signal })
export const saveRolePlan = (projectId: string, body: { roles: Record<string, RoleSpec>; slot_overrides: Record<string, string> }, csrf: string) =>
  request<RolePlan>(`/projects/${encodeURIComponent(projectId)}/roles`, { method: 'PUT', headers: { 'X-CSRF-Token': csrf }, body: JSON.stringify(body) })
export const getRoleBindings = (projectId: string, machineId: string, signal: AbortSignal) =>
  request<{ items: RoleBinding[] }>(`/projects/${encodeURIComponent(projectId)}/roles/bindings?machine_id=${encodeURIComponent(machineId)}`, { signal })
export const renderConfig = (projectId: string, body: {
  kind: string; machine_id: string; config_set_id?: string
  params: Record<string, unknown>; readback?: Record<string, unknown>
}, apply: boolean, csrf: string) =>
  request<RenderResult>(`/projects/${encodeURIComponent(projectId)}/config/render${apply ? '?apply=1' : ''}`,
    { method: 'POST', headers: { 'X-CSRF-Token': csrf }, body: JSON.stringify(body) })
