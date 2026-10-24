import { Fragment, useEffect, useState, type FormEvent } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { TableCell, TableRow } from '../../components/ui/table'
import { ProjectWorkspace, canManageProject, useProject } from '../../components/ProjectWorkspace'
import { ActionDialog, ActionError, Pagination, PermissionDenied, SuccessNotice } from '../../components/Management'
import { useAction } from '../../lib/management'
import { commitBomPackage, commitLegacyBom, listProjectAssets, previewBomPackage, previewLegacyBom, runTrustAction, type BomPackageIssue, type BomPackagePreview, type BomPackageSelection, type LegacyBomPreview, type ProjectAsset } from '../../lib/bom'
import { useExpireOnUnauthorized, useSession } from '../../lib/session'

function BomImportDialog({ projectId, onClose, onDone }: { projectId: string; onClose: () => void; onDone: (message: string) => Promise<void> }) {
  const { t } = useTranslation(), session = useSession(), action = useAction()
  const [file, setFile] = useState<File | null>(null), [batch, setBatch] = useState(''), [preview, setPreview] = useState<LegacyBomPreview | null>(null), [validation, setValidation] = useState('')
  async function runPreview(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!file || !batch.trim()) { setValidation(t('bomLegacyFileRequired')); return }
    setValidation('')
    const result = await action.run(() => previewLegacyBom(projectId, file, batch.trim(), session.csrf_token))
    if (result.ok) setPreview(result.value)
  }
  async function runCommit() {
    if (!file || !preview?.can_commit) return
    const result = await action.run(() => commitLegacyBom(projectId, file, batch.trim(), preview.preview_token ?? '', session.csrf_token))
    if (result.ok) await onDone(t('bomPackageCommitted', { count: result.value.item_count, batch: result.value.bom_import_batch_id }))
  }
  return <ActionDialog title={t('bomImport')} description={t('bomLegacyDetail')} busy={action.busy} confirm={t('bomPreviewRun')} onSubmit={(event) => void runPreview(event)} onClose={onClose}>
    <ActionError error={action.error} />{validation && <p className="field-error" role="alert">{validation}</p>}
    <div className="field"><label htmlFor="bom-legacy-batch">{t('bomLegacyBatch')}</label><input id="bom-legacy-batch" value={batch} onChange={(event) => { setBatch(event.target.value); setPreview(null) }} placeholder="BOM-BATCH-P1-20261020-1" maxLength={160} required disabled={action.busy} /></div>
    <div className="field"><label htmlFor="bom-legacy-file">{t('bomLegacyFile')}</label><input id="bom-legacy-file" type="file" accept=".csv,text/csv" onChange={(event) => { setFile(event.target.files?.[0] ?? null); setPreview(null) }} required disabled={action.busy} /></div>
    {preview && <>
      <p className="impact-notice">{t('bomLegacySummary', { batch: preview.bom_import_batch_id, machines: preview.machines, ports: preview.ports })}</p>
      {preview.idempotent && <p className="impact-notice">{t('bomPackageAlreadyCommitted')}</p>}
      <div className="table-scroll" tabIndex={0} role="region" aria-label={t('bomPackageIssues')}><table className="data-table"><thead><tr><th scope="col">{t('bomIssueSeverity')}</th><th scope="col">{t('bomIssueFile')}</th><th scope="col">{t('bomIssueLine')}</th><th scope="col">{t('bomIssueField')}</th><th scope="col">{t('bomIssueCode')}</th></tr></thead>
        <tbody>{preview.issues.map((issue, index) => <Fragment key={index}><tr><td>{issue.severity}</td><td className="mono-value">{issue.file}</td><td>{issue.bom_line_no || '—'}</td><td className="mono-value">{issue.field || '—'}</td><td className="mono-value">{issue.code}</td></tr>{issue.model_version_ref && <TableRow><TableCell colSpan={5} className="ui:whitespace-normal ui:break-words">{t('bomModelDifference', { model: issue.model_version_ref, expected: issue.expected ?? '—', actual: issue.actual ?? '—' })}</TableCell></TableRow>}</Fragment>)}</tbody></table>
        {!preview.issues.length && <p className="table-loading">{t('bomPackageNoIssues')}</p>}</div>
      <div className="table-actions"><button type="button" className="secondary-button" disabled={action.busy || !preview.issues.length} onClick={() => downloadBomIssues(preview.bom_import_batch_id, preview.issues)}>{t('bomExportIssues')}</button>
        <button type="button" className="primary-button" disabled={action.busy || !preview.can_commit} onClick={() => void runCommit()}>{t('bomCommit')}</button></div>
    </>}
  </ActionDialog>
}

function downloadBomIssues(batch: string, issues: BomPackageIssue[]) {
  const cell = (value: string) => `"${value.replaceAll('"', '""')}"`
  const rows = [['severity', 'file', 'bom_line_no', 'field', 'code', 'model_version_ref', 'expected', 'actual'].join(','), ...issues.map((issue) =>
    [issue.severity, issue.file, String(issue.bom_line_no ?? ''), issue.field ?? '', issue.code, issue.model_version_ref ?? '', issue.expected ?? '', issue.actual ?? ''].map(cell).join(','))]
  const blob = new Blob(['\uFEFF', rows.join('\r\n'), '\r\n'], { type: 'text/csv;charset=utf-8' })
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url; link.download = `${batch || 'bom'}-issues.csv`; link.click()
  URL.revokeObjectURL(url)
}
function BomPackageDialog({ projectId, onClose, onDone }: { projectId: string; onClose: () => void; onDone: (message: string) => Promise<void> }) {
  const { t } = useTranslation(), session = useSession(), action = useAction()
  const [selection, setSelection] = useState<BomPackageSelection | null>(null), [preview, setPreview] = useState<BomPackagePreview | null>(null), [validation, setValidation] = useState('')
  async function runPreview(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!selection) { setValidation(t('bomPackageFileRequired')); return }
    setValidation('')
    const result = await action.run(() => previewBomPackage(projectId, selection, session.csrf_token))
    if (result.ok) setPreview(result.value)
  }
  async function runCommit() {
    if (!selection || !preview?.can_commit) return
    const result = await action.run(() => commitBomPackage(projectId, selection, preview.preview_token ?? '', session.csrf_token))
    if (result.ok) await onDone(t('bomPackageCommitted', { count: result.value.item_count, batch: result.value.bom_import_batch_id }))
  }
  return <ActionDialog title={t('bomPackageTitle')} description={t('bomPackageDetail')} busy={action.busy} confirm={t('bomPreviewRun')} onSubmit={(event) => void runPreview(event)} onClose={onClose}>
    <ActionError error={action.error} />{validation && <p className="field-error" role="alert">{validation}</p>}
    <div className="field"><label htmlFor="bom-package-file">{t('bomPackageFile')}</label><input id="bom-package-file" type="file" accept=".zip,.gz,.tar.gz" disabled={action.busy} onChange={(event) => { const file = event.target.files?.[0]; setSelection(file ? { kind: 'archive', file } : null); setPreview(null) }} /><span className="field-hint">{t('bomPackageFileHint')}</span></div>
    <div className="field"><label htmlFor="bom-package-directory">{t('bomPackageDirectory')}</label><input id="bom-package-directory" type="file" multiple {...{ webkitdirectory: '' }} disabled={action.busy} onChange={(event) => { const files = Array.from(event.target.files ?? []); setSelection(files.length ? { kind: 'directory', files } : null); setPreview(null) }} /></div>
    {preview && <>
      <p className="impact-notice">{t('bomPackageSummary', { batch: preview.manifest.bom_import_batch_id, rows: preview.manifest.row_count, profile: preview.manifest.profile_version_ref })}</p>
      {preview.idempotent && <p className="impact-notice">{t('bomPackageAlreadyCommitted')}</p>}
      {preview.derivations?.['port.sub_type'] && <p className="impact-notice">{t('bomPortSubtypeSource')}</p>}
      <div className="table-scroll" tabIndex={0} role="region" aria-label={t('bomPackageIssues')}><table className="data-table"><thead><tr><th scope="col">{t('bomIssueSeverity')}</th><th scope="col">{t('bomIssueFile')}</th><th scope="col">{t('bomIssueLine')}</th><th scope="col">{t('bomIssueField')}</th><th scope="col">{t('bomIssueCode')}</th></tr></thead>
        <tbody>{preview.issues.map((issue, index) => <Fragment key={index}><tr><td>{issue.severity}</td><td className="mono-value">{issue.file}</td><td>{issue.bom_line_no || '—'}</td><td className="mono-value">{issue.field || '—'}</td><td className="mono-value">{issue.code}</td></tr>{issue.model_version_ref && <TableRow><TableCell colSpan={5} className="ui:whitespace-normal ui:break-words">{t('bomModelDifference', { model: issue.model_version_ref, expected: issue.expected ?? '—', actual: issue.actual ?? '—' })}</TableCell></TableRow>}</Fragment>)}</tbody></table>
        {!preview.issues.length && <p className="table-loading">{t('bomPackageNoIssues')}</p>}</div>
      <div className="table-actions"><button type="button" className="secondary-button" disabled={action.busy || !preview.issues.length} onClick={() => downloadBomIssues(preview.manifest.bom_import_batch_id, preview.issues)}>{t('bomExportIssues')}</button>
      <button type="button" className="primary-button" disabled={action.busy || !preview.can_commit} onClick={() => void runCommit()}>{t('bomCommit')}</button></div>
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
  const [packageImporting, setPackageImporting] = useState(false)
  const manage = canManageProject(project, session.user.is_admin) && session.permissions.includes('model:manage')
  const assets = useQuery({ queryKey: ['private', session.user.id, 'project', project.id, 'assets', page], queryFn: ({ signal }) => listProjectAssets(project.id, page, signal), enabled: allowed })
  useExpireOnUnauthorized(assets.error)
  useEffect(() => { document.title = `${t('bomTitle')} · ${project.name}` }, [t, project.name])
  async function done(message: string) {
    setImporting(false); setPackageImporting(false); setTrustTarget(null); setSuccess(message)
    await client.invalidateQueries({ queryKey: ['private', session.user.id, 'project', project.id, 'assets'] })
  }
  if (!allowed) return <PermissionDenied />
  return <>
    <div className="page-heading"><div><p className="section-eyebrow">{project.name}</p><h1>{t('bomTitle')}</h1><p>{t('bomIntro')}</p></div>
      {manage && <div className="table-actions"><button className="primary-button" onClick={() => { setSuccess(''); setPackageImporting(true) }}>{t('bomPackageTitle')}</button><button className="secondary-button" onClick={() => { setSuccess(''); setImporting(true) }}>{t('bomImport')}</button><button className="secondary-button" disabled={!selected.size} onClick={() => { setSuccess(''); setTrustTarget((assets.data?.items ?? []).filter((a) => selected.has(a.machine_id))) }}>{t('trustTitle')}</button></div>}</div>
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
    {packageImporting && <BomPackageDialog projectId={project.id} onClose={() => setPackageImporting(false)} onDone={(message) => done(message)} />}
    {importing && <BomImportDialog projectId={project.id} onClose={() => setImporting(false)} onDone={(message) => done(message)} />}
    {trustTarget && <TrustDialog projectId={project.id} machines={trustTarget} onClose={() => setTrustTarget(null)} onDone={(message) => { setSelected(new Set()); return done(message) }} />}
  </>
}
export function AssetsPage({ projectId }: { projectId: string }) {
  return <ProjectWorkspace projectId={projectId}><AssetsContent /></ProjectWorkspace>
}
