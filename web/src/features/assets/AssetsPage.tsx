import { useEffect, useState, type FormEvent } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { ProjectWorkspace, canManageProject, useProject } from '../../components/ProjectWorkspace'
import { ActionDialog, ActionError, Pagination, PermissionDenied, SuccessNotice } from '../../components/Management'
import { useAction } from '../../lib/management'
import { commitBomImport, listProjectAssets, previewBomImport, runTrustAction, type BomImportInput, type BomPreview, type ProjectAsset } from '../../lib/bom'
import { parseBomCsv } from '../../lib/bom-csv'
import { useExpireOnUnauthorized, useSession } from '../../lib/session'

function BomImportDialog({ projectId, onClose, onDone }: { projectId: string; onClose: () => void; onDone: (message: string) => Promise<void> }) {
  const { t } = useTranslation(), session = useSession(), action = useAction()
  const [format, setFormat] = useState<'json' | 'csv'>('json'), [raw, setRaw] = useState(''), [fileVersion, setFileVersion] = useState(''), [preview, setPreview] = useState<BomPreview | null>(null), [validation, setValidation] = useState('')
  function parse(): BomImportInput | null {
    if (format === 'csv') {
      const result = parseBomCsv(raw, fileVersion)
      if (result.ok) return result.input as BomImportInput
      setValidation(t('bomCsvInvalid') + ' ' + result.errors.slice(0, 3).join('; '))
      return null
    }
    try {
      const parsed = JSON.parse(raw) as BomImportInput
      if (!parsed || typeof parsed !== 'object' || !parsed.file_version || !Array.isArray(parsed.machines) || !parsed.machines.length) throw new Error('invalid')
      return parsed
    } catch {
      setValidation(t('bomImportInvalid'))
      return null
    }
  }
  async function runPreview(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setValidation('')
    const body = parse()
    if (!body) return
    const result = await action.run(() => previewBomImport(projectId, body, session.csrf_token))
    if (result.ok) setPreview(result.value)
  }
  async function runCommit() {
    const body = parse()
    if (!body) return
    if (preview && (preview.issues ?? []).length) { setValidation(t('bomCommitBlocked')); return }
    const result = await action.run(() => commitBomImport(projectId, body, session.csrf_token, crypto.randomUUID()))
    if (result.ok) {
      const r = result.value
      await onDone(t('bomCommitted', { machines: r.machines_created, assets: r.assets_created }))
    }
  }
  const friendly = action.error && 'code' in action.error && (action.error as { code?: string }).code === 'BOM_VERSION_EXISTS' ? t('bomVersionExists') : null
  return <ActionDialog title={t('bomImport')} description={t('bomImportDetail')} busy={action.busy} confirm={t('bomPreviewRun')} onSubmit={(event) => void runPreview(event)} onClose={onClose}>
    {friendly ? <p className="impact-notice" role="alert">{friendly}</p> : <ActionError error={action.error} />}
    {validation && <p className="field-error" role="alert">{validation}</p>}
    <div className="field"><label htmlFor="bom-format">{t('bomFormat')}</label><select id="bom-format" value={format} onChange={(event) => { setFormat(event.target.value as 'json' | 'csv'); setPreview(null) }} disabled={action.busy}>
      <option value="json">{t('bomFormatJson')}</option>
      <option value="csv">{t('bomFormatCsv')}</option>
    </select><span className="field-hint">{format === 'csv' ? t('bomCsvHint') : t('bomImportDetail')}</span></div>
    {format === 'csv' && <div className="field"><label htmlFor="bom-version">{t('bomFileVersion')}</label><input id="bom-version" value={fileVersion} onChange={(event) => { setFileVersion(event.target.value); setPreview(null) }} maxLength={120} required disabled={action.busy} /></div>}
    <div className="field"><label htmlFor="bom-raw">{format === 'csv' ? t('bomCsvLabel') : t('bomImport')}</label><textarea id="bom-raw" value={raw} onChange={(event) => { setRaw(event.target.value); setPreview(null) }} rows={10} placeholder={format === 'csv' ? t('bomCsvPlaceholder') : t('bomImportPlaceholder')} disabled={action.busy} className="mono-value" /></div>
    {preview && <>
      <p className="impact-notice">{t('bomPreviewSummary', { machines: preview.machines, ports: preview.ports, newMachines: preview.new_machines, existingMachines: preview.existing_machines, newAssets: preview.new_assets, updatedAssets: preview.updated_assets })}</p>
      {(preview.issues ?? []).length > 0 && <div className="table-scroll" tabIndex={0} role="region" aria-label={t('bomPreviewIssues', { count: (preview.issues ?? []).length })}><table className="data-table"><thead><tr><th scope="col">{t('bomSerial')}</th><th scope="col">{t('bomIssueReason')}</th></tr></thead>
        <tbody>{(preview.issues ?? []).map((issue, index) => <tr key={index}><td className="mono-value">{issue.serial_number}</td><td>{t('reason_' + issue.reason, { defaultValue: issue.reason })}{(issue.ports ?? []).map((p, i) => <span key={i} className="cell-subtitle">{p.mac_address}: {t('reason_' + p.reason, { defaultValue: p.reason })}</span>)}</td></tr>)}</tbody></table></div>}
      <button type="button" className="primary-button" disabled={action.busy || (preview.issues ?? []).length > 0} onClick={() => void runCommit()}>{t('bomCommit')}</button>
    </>}
  </ActionDialog>
}
const trustActions = ['trust.bootstrap', 'trust.verify', 'trust.repair', 'trust.revoke'] as const
function TrustDialog({ projectId, machines, onClose, onDone }: { projectId: string; machines: ProjectAsset[]; onClose: () => void; onDone: (message: string) => Promise<void> }) {
  const { t } = useTranslation(), session = useSession(), action = useAction()
  const [trustAction, setTrustAction] = useState<(typeof trustActions)[number]>('trust.verify'), [fingerprint, setFingerprint] = useState(''), [validation, setValidation] = useState('')
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setValidation('')
    if ((trustAction === 'trust.bootstrap' || trustAction === 'trust.repair') && !fingerprint.trim()) { setValidation(t('trustFingerprintRequired')); return }
    const result = await action.run(() => runTrustAction(projectId, trustAction, machines.map((m) => m.machine_id), fingerprint.trim(), session.csrf_token, crypto.randomUUID()))
    if (result.ok) {
      const successes = result.value.results.filter((r) => r.outcome === 'success').length
      await onDone(t('trustDone', { action: trustAction, successes, total: machines.length }))
    }
  }
  return <ActionDialog title={t('trustTitle')} description={t('trustDetail')} busy={action.busy} confirm={t(action.busy ? 'saving' : 'trustRun')} onSubmit={(event) => void submit(event)} onClose={onClose}>
    <ActionError error={action.error} />{validation && <p className="field-error" role="alert">{validation}</p>}
    <dl className="action-target"><dt>{t('bomHostname')}</dt><dd>{machines.map((m) => m.hostname).join(', ')}</dd></dl>
    <div className="field"><label htmlFor="trust-action">{t('trustAction')}</label><select id="trust-action" value={trustAction} onChange={(event) => setTrustAction(event.target.value as (typeof trustActions)[number])} disabled={action.busy}>{trustActions.map((value) => <option key={value} value={value}>{t('trust_action_' + value)}</option>)}</select></div>
    <div className="field"><label htmlFor="trust-fp">{t('trustFingerprint')}</label><input id="trust-fp" value={fingerprint} onChange={(event) => setFingerprint(event.target.value)} placeholder="SHA256:..." maxLength={120} disabled={action.busy} className="mono-value" /><span className="field-hint">{t('trustFingerprintHint')}</span></div>
  </ActionDialog>
}
function AssetsContent() {
  const project = useProject()
  const { t } = useTranslation(), session = useSession(), client = useQueryClient()
  const [page, setPage] = useState(1), [importing, setImporting] = useState(false), [trustTarget, setTrustTarget] = useState<ProjectAsset[] | null>(null), [success, setSuccess] = useState('')
  const allowed = session.permissions.includes('model:read')
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const manage = canManageProject(project, session.user.is_admin) && session.permissions.includes('model:manage')
  const assets = useQuery({ queryKey: ['private', session.user.id, 'project', project.id, 'assets', page], queryFn: ({ signal }) => listProjectAssets(project.id, page, signal), enabled: allowed })
  useExpireOnUnauthorized(assets.error)
  useEffect(() => { document.title = `${t('bomTitle')} · ${project.name}` }, [t, project.name])
  async function done(message: string) {
    setImporting(false); setTrustTarget(null); setSuccess(message)
    await client.invalidateQueries({ queryKey: ['private', session.user.id, 'project', project.id, 'assets'] })
  }
  if (!allowed) return <PermissionDenied />
  return <>
    <div className="page-heading"><div><p className="section-eyebrow">{project.name}</p><h1>{t('bomTitle')}</h1><p>{t('bomIntro')}</p></div>
      {manage && <div className="table-actions"><button className="primary-button" onClick={() => { setSuccess(''); setImporting(true) }}>{t('bomImport')}</button><button className="secondary-button" disabled={!selected.size} onClick={() => { setSuccess(''); setTrustTarget((assets.data?.items ?? []).filter((a) => selected.has(a.machine_id))) }}>{t('trustTitle')}</button></div>}</div>
    {success && <SuccessNotice>{success}</SuccessNotice>}
    <section className="management-panel" aria-busy={assets.isFetching}>
      <ActionError error={assets.error} />
      {assets.isPending ? <p className="table-loading" role="status">{t('loadingRecords')}</p> : assets.data && <>
        <div className="table-scroll" tabIndex={0} role="region" aria-label={t('bomTitle')}><table className="data-table"><thead><tr>{manage && <th scope="col"><input type="checkbox" aria-label={t('trustSelectAll')} checked={!!(assets.data.items ?? []).length && selected.size === (assets.data.items ?? []).length} onChange={(event) => setSelected(event.target.checked ? new Set((assets.data.items ?? []).map((a) => a.machine_id)) : new Set())} /></th>}<th scope="col">{t('bomHostname')}</th><th scope="col">{t('bomSerial')}</th><th scope="col">{t('bomModel')}</th><th scope="col">{t('bomRack')}</th><th scope="col">{t('bomIpRef')}</th><th scope="col">{t('bomState')}</th><th scope="col">{t('trustTitle')}</th></tr></thead>
          <tbody>{(assets.data.items ?? []).map((asset) => <tr key={asset.id}>{manage && <td><input type="checkbox" aria-label={asset.hostname} checked={selected.has(asset.machine_id)} onChange={(event) => { const next = new Set(selected); if (event.target.checked) next.add(asset.machine_id); else next.delete(asset.machine_id); setSelected(next) }} /></td>}<td><strong>{asset.hostname}</strong><span className="cell-subtitle">v{asset.version}</span></td>
            <td className="mono-value">{asset.serial_number}</td><td>{asset.model_name || '—'}</td><td>{asset.rack}{asset.unit_position ? ` / ${asset.unit_position}` : ''}</td><td className="mono-value">{asset.ip_reference || '—'}</td>
            <td><span className={`status-label ${asset.state === 'provisioned' ? 'enabled' : asset.state === 'failed' ? 'disabled' : ''}`}>{t('bom_state_' + asset.state)}</span></td>
            <td><span className={`status-label ${asset.ssh_trust === 'established' ? 'enabled' : asset.ssh_trust === 'revoked' ? 'disabled' : ''}`}>{t('trust_state_' + asset.ssh_trust)}</span></td></tr>)}</tbody></table>
          {!(assets.data.items ?? []).length && <p className="table-loading">{t('bomNoAssets')}</p>}</div>
        <Pagination offset={(page - 1) * assets.data.page_size} limit={assets.data.page_size} total={assets.data.total} busy={assets.isFetching} onChange={(offset) => setPage(offset / assets.data.page_size + 1)} /></>}
    </section>
    {importing && <BomImportDialog projectId={project.id} onClose={() => setImporting(false)} onDone={(message) => done(message)} />}
    {trustTarget && <TrustDialog projectId={project.id} machines={trustTarget} onClose={() => setTrustTarget(null)} onDone={(message) => { setSelected(new Set()); return done(message) }} />}
  </>
}
export function AssetsPage({ projectId }: { projectId: string }) {
  return <ProjectWorkspace projectId={projectId}><AssetsContent /></ProjectWorkspace>
}
