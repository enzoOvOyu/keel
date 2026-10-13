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
  state: string; version: number; model_name: string; rack: string; unit_position: string
}

const pageQuery = (page: number) => new URLSearchParams({ page: String(page), page_size: '20' }).toString()

export const listProjectAssets = (projectId: string, page: number, signal: AbortSignal) =>
  request<{ items: ProjectAsset[]; total: number; page: number; page_size: number }>(`/projects/${encodeURIComponent(projectId)}/assets?${pageQuery(page)}`, { signal })
export const previewBomImport = (projectId: string, body: BomImportInput, csrf: string) =>
  request<BomPreview>(`/projects/${encodeURIComponent(projectId)}/bom/preview`, { method: 'POST', headers: { 'X-CSRF-Token': csrf }, body: JSON.stringify(body) })
export const commitBomImport = (projectId: string, body: BomImportInput, csrf: string, key: string) =>
  request<BomCommitResult>(`/projects/${encodeURIComponent(projectId)}/bom/commit`, { method: 'POST', headers: { 'X-CSRF-Token': csrf, 'Idempotency-Key': key }, body: JSON.stringify(body) })
