import { request } from './api'

export type BomPortRow = { mac_address: string; port_name: string; rail: number }
export type BomMachineRow = {
  serial_number: string; rack: string; unit_position: string; bmc_address: string
  hostname: string; ip_reference: string; model_name: string; ports: BomPortRow[]
}
export type BomImportInput = { file_version: string; machines: BomMachineRow[] }
export type BomPortIssue = { mac_address: string; reason: string }
export type BomMachineIssue = { serial_number: string; reason: string; ports?: BomPortIssue[] }
export type BomPreview = {
  machines: number; ports: number; new_machines: number; existing_machines: number
  new_assets: number; updated_assets: number; issues: BomMachineIssue[]; model_names: string[]
}
export type BomCommitResult = {
  import_id: string; machines_created: number; ports_created: number
  assets_created: number; assets_updated: number; committed_at: string
}
export type ProjectAsset = {
  id: string; machine_id: string; serial_number: string; hostname: string; ip_reference: string
  state: string; version: number; model_name: string; rack: string; unit_position: string; ssh_trust: string
}
export type TrustResult = { machine_id: string; serial_number: string; before_state: string; after_state: string; outcome: string; reason?: string; checked_at: string }
export type TrustLogEntry = { id: string; machine_id: string; serial_number: string; action: string; outcome: string; fingerprint: string; created_by: string; created_at: string }
export const runTrustAction = (projectId: string, action: string, machineIds: string[], fingerprint: string, csrf: string, key: string) =>
  request<{ results: TrustResult[] }>(`/projects/${encodeURIComponent(projectId)}/trust/actions`, { method: 'POST', headers: { 'X-CSRF-Token': csrf, 'Idempotency-Key': key }, body: JSON.stringify({ action, machine_ids: machineIds, fingerprint }) })
export const listTrustLogs = (projectId: string, page: number, signal: AbortSignal) =>
  request<{ items: TrustLogEntry[]; total: number; page: number; page_size: number }>(`/projects/${encodeURIComponent(projectId)}/trust/logs?page=${page}&page_size=20`, { signal })

const pageQuery = (page: number) => new URLSearchParams({ page: String(page), page_size: '20' }).toString()

export const listProjectAssets = (projectId: string, page: number, signal: AbortSignal) =>
  request<{ items: ProjectAsset[]; total: number; page: number; page_size: number }>(`/projects/${encodeURIComponent(projectId)}/assets?${pageQuery(page)}`, { signal })
export const previewBomImport = (projectId: string, body: BomImportInput, csrf: string) =>
  request<BomPreview>(`/projects/${encodeURIComponent(projectId)}/bom/preview`, { method: 'POST', headers: { 'X-CSRF-Token': csrf }, body: JSON.stringify(body) })
export const commitBomImport = (projectId: string, body: BomImportInput, csrf: string, key: string) =>
  request<BomCommitResult>(`/projects/${encodeURIComponent(projectId)}/bom/commit`, { method: 'POST', headers: { 'X-CSRF-Token': csrf, 'Idempotency-Key': key }, body: JSON.stringify(body) })

export type BomPackageIssue = { severity: 'BLOCK' | 'WARN' | 'INFO'; file: string; bom_line_no?: number; field?: string; code: string }
export type BomPackagePreview = {
  manifest: { bom_import_batch_id: string; profile_version_ref: string; row_count: number; content_hash: string }
  issues: BomPackageIssue[]; file_hashes: Record<string, string>; can_commit: boolean; idempotent: boolean; preview_token?: string
}
export type BomPackageCommitResult = {
  package_id: string; bom_import_batch_id: string; content_hash: string
  item_count: number; idempotent: boolean; committed_at: string
}
export type BomPackageSelection = { kind: 'archive'; file: File } | { kind: 'directory'; files: File[] }
const packageForm = (selection: BomPackageSelection) => {
  const form = new FormData()
  if (selection.kind === 'archive') form.append('file', selection.file)
  else for (const file of selection.files) {
    form.append('files', file)
    form.append('paths', file.webkitRelativePath)
  }
  return form
}
export const previewBomPackage = (projectId: string, selection: BomPackageSelection, csrf: string) =>
  request<BomPackagePreview>(`/projects/${encodeURIComponent(projectId)}/bom/packages/preview`, { method: 'POST', headers: { 'X-CSRF-Token': csrf }, body: packageForm(selection) })
export const commitBomPackage = (projectId: string, selection: BomPackageSelection, token: string, csrf: string) => {
  const form = packageForm(selection); form.append('preview_token', token)
  return request<BomPackageCommitResult>(`/projects/${encodeURIComponent(projectId)}/bom/packages/commit`, { method: 'POST', headers: { 'X-CSRF-Token': csrf }, body: form })
}
