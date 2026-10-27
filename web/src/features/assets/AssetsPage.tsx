import StatusPillPair from './StatusPillPair'
import type { ServerDetailTab, ServerDetailTarget } from './ServerDetailDrawer'
import { portMonitorFleet } from '../../lib/port-monitor'
import { useMonitorVisible } from '../monitor/visibility'
import { BMCInventoryDialog } from './BMCInventoryDialog'
import { ArtifactDistribution } from './ArtifactDistribution'
import { ProvisionPreviewDialog } from './ProvisionPreviewDialog'
import { Label } from '@/components/ui/label'
import { Input } from '@/components/ui/input'
import { Table, TableHeader, TableHead, TableBody, TableCell, TableRow } from '@/components/ui/table'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { NativeCheckbox } from '@/components/ui/checkbox'
import { Button } from '@/components/ui/button'
import { AssetModelDialog } from './AssetModelDialog'
import { SSHTrustRequests } from './SSHTrustRequests'
import { SSHDomainKeys } from './SSHDomainKeys'
import { SSHExecutions } from './SSHExecutions'
import { SSHScripts } from './SSHScripts'
import { AgentExecutions } from './AgentExecutions'
import { Fragment, lazy, Suspense, useEffect, useState, type FormEvent } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { ProjectWorkspace, canManageProject, useProject } from '../../components/ProjectWorkspace'
import { ActionDialog, ActionError, Pagination, PermissionDenied, SuccessNotice } from '../../components/Management'
import { useAction } from '../../lib/management'
import { commitBomPackage, commitLegacyBom, listProjectAssets, previewBomPackage, previewLegacyBom, runTrustAction, type BomPackageIssue, type BomPackagePreview, type BomPackageSelection, type LegacyBomPreview, type ProjectAsset } from '../../lib/bom'
import { useExpireOnUnauthorized, useSession } from '../../lib/session'

const ServerDetailDrawer = lazy(() => import('./ServerDetailDrawer'))
const MappingFleetDialog = lazy(() => import('./MappingFleetDialog'))
const TopologyFleetDialog = lazy(() => import('./TopologyFleetDialog'))
const AssetViewsDialog = lazy(() => import('./AssetViewsDialog'))

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
    <div className="field"><Label htmlFor="bom-legacy-batch">{t('bomLegacyBatch')}</Label><Input className="ui:h-11" id="bom-legacy-batch" value={batch} onChange={(event) => { setBatch(event.target.value); setPreview(null) }} placeholder="BOM-BATCH-P1-20261020-1" maxLength={160} required disabled={action.busy} /></div>
    <div className="field"><Label htmlFor="bom-legacy-file">{t('bomLegacyFile')}</Label><Input className="ui:h-auto ui:min-h-11 ui:py-2" id="bom-legacy-file" type="file" accept=".csv,text/csv" onChange={(event) => { setFile(event.target.files?.[0] ?? null); setPreview(null) }} required disabled={action.busy} /></div>
    {preview && <>
      <p className="impact-notice">{t('bomLegacySummary', { batch: preview.bom_import_batch_id, machines: preview.machines, ports: preview.ports })}</p>
      {preview.idempotent && <p className="impact-notice">{t('bomPackageAlreadyCommitted')}</p>}
      <div className="table-scroll" tabIndex={0} role="region" aria-label={t('bomPackageIssues')}><Table scrollContainer={false} className="data-table"><TableHeader><TableRow><TableHead className="ui:h-auto ui:p-4 ui:align-top ui:text-muted-foreground" scope="col">{t('bomIssueSeverity')}</TableHead><TableHead className="ui:h-auto ui:p-4 ui:align-top ui:text-muted-foreground" scope="col">{t('bomIssueFile')}</TableHead><TableHead className="ui:h-auto ui:p-4 ui:align-top ui:text-muted-foreground" scope="col">{t('bomIssueLine')}</TableHead><TableHead className="ui:h-auto ui:p-4 ui:align-top ui:text-muted-foreground" scope="col">{t('bomIssueField')}</TableHead><TableHead className="ui:h-auto ui:p-4 ui:align-top ui:text-muted-foreground" scope="col">{t('bomIssueCode')}</TableHead></TableRow></TableHeader>
        <TableBody>{preview.issues.map((issue, index) => <Fragment key={index}><TableRow><TableCell className="ui:p-4 ui:align-top ui:whitespace-normal">{issue.severity}</TableCell><TableCell className="mono-value ui:p-4 ui:align-top ui:whitespace-normal">{issue.file}</TableCell><TableCell className="ui:p-4 ui:align-top ui:whitespace-normal">{issue.bom_line_no || '—'}</TableCell><TableCell className="mono-value ui:p-4 ui:align-top ui:whitespace-normal">{issue.field || '—'}</TableCell><TableCell className="mono-value ui:p-4 ui:align-top ui:whitespace-normal">{issue.code}</TableCell></TableRow>{issue.model_version_ref && <TableRow><TableCell colSpan={5} className="ui:whitespace-normal ui:break-words">{t('bomModelDifference', { model: issue.model_version_ref, expected: issue.expected ?? '—', actual: issue.actual ?? '—' })}</TableCell></TableRow>}</Fragment>)}</TableBody></Table>
        {!preview.issues.length && <p className="table-loading">{t('bomPackageNoIssues')}</p>}</div>
      <div className="table-actions"><Button variant="outline" type="button" className="secondary-button ui:min-h-11" disabled={action.busy || !preview.issues.length} onClick={() => downloadBomIssues(preview.bom_import_batch_id, preview.issues)}>{t('bomExportIssues')}</Button>
        <Button variant="default" type="button" className="primary-button ui:min-h-11" disabled={action.busy || !preview.can_commit} onClick={() => void runCommit()}>{t('bomCommit')}</Button></div>
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
    <div className="field"><Label htmlFor="bom-package-file">{t('bomPackageFile')}</Label><Input className="ui:h-auto ui:min-h-11 ui:py-2" id="bom-package-file" type="file" accept=".zip,.gz,.tar.gz" disabled={action.busy} onChange={(event) => { const file = event.target.files?.[0]; setSelection(file ? { kind: 'archive', file } : null); setPreview(null) }} /><span className="field-hint">{t('bomPackageFileHint')}</span></div>
    <div className="field"><Label htmlFor="bom-package-directory">{t('bomPackageDirectory')}</Label><Input className="ui:h-auto ui:min-h-11 ui:py-2" id="bom-package-directory" type="file" multiple {...{ webkitdirectory: '' }} disabled={action.busy} onChange={(event) => { const files = Array.from(event.target.files ?? []); setSelection(files.length ? { kind: 'directory', files } : null); setPreview(null) }} /></div>
    {preview && <>
      <p className="impact-notice">{t('bomPackageSummary', { batch: preview.manifest.bom_import_batch_id, rows: preview.manifest.row_count, profile: preview.manifest.profile_version_ref })}</p>
      {preview.idempotent && <p className="impact-notice">{t('bomPackageAlreadyCommitted')}</p>}
      {preview.derivations?.['port.sub_type'] && <p className="impact-notice">{t('bomPortSubtypeSource')}</p>}
      <div className="table-scroll" tabIndex={0} role="region" aria-label={t('bomPackageIssues')}><Table scrollContainer={false} className="data-table"><TableHeader><TableRow><TableHead className="ui:h-auto ui:p-4 ui:align-top ui:text-muted-foreground" scope="col">{t('bomIssueSeverity')}</TableHead><TableHead className="ui:h-auto ui:p-4 ui:align-top ui:text-muted-foreground" scope="col">{t('bomIssueFile')}</TableHead><TableHead className="ui:h-auto ui:p-4 ui:align-top ui:text-muted-foreground" scope="col">{t('bomIssueLine')}</TableHead><TableHead className="ui:h-auto ui:p-4 ui:align-top ui:text-muted-foreground" scope="col">{t('bomIssueField')}</TableHead><TableHead className="ui:h-auto ui:p-4 ui:align-top ui:text-muted-foreground" scope="col">{t('bomIssueCode')}</TableHead></TableRow></TableHeader>
        <TableBody>{preview.issues.map((issue, index) => <Fragment key={index}><TableRow><TableCell className="ui:p-4 ui:align-top ui:whitespace-normal">{issue.severity}</TableCell><TableCell className="mono-value ui:p-4 ui:align-top ui:whitespace-normal">{issue.file}</TableCell><TableCell className="ui:p-4 ui:align-top ui:whitespace-normal">{issue.bom_line_no || '—'}</TableCell><TableCell className="mono-value ui:p-4 ui:align-top ui:whitespace-normal">{issue.field || '—'}</TableCell><TableCell className="mono-value ui:p-4 ui:align-top ui:whitespace-normal">{issue.code}</TableCell></TableRow>{issue.model_version_ref && <TableRow><TableCell colSpan={5} className="ui:whitespace-normal ui:break-words">{t('bomModelDifference', { model: issue.model_version_ref, expected: issue.expected ?? '—', actual: issue.actual ?? '—' })}</TableCell></TableRow>}</Fragment>)}</TableBody></Table>
        {!preview.issues.length && <p className="table-loading">{t('bomPackageNoIssues')}</p>}</div>
      <div className="table-actions"><Button variant="outline" type="button" className="secondary-button ui:min-h-11" disabled={action.busy || !preview.issues.length} onClick={() => downloadBomIssues(preview.manifest.bom_import_batch_id, preview.issues)}>{t('bomExportIssues')}</Button>
      <Button variant="default" type="button" className="primary-button ui:min-h-11" disabled={action.busy || !preview.can_commit} onClick={() => void runCommit()}>{t('bomCommit')}</Button></div>
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
  return <ActionDialog title={t('trustMockTitle')} description={t('trustDetail')} busy={action.busy} confirm={t(action.busy ? 'saving' : 'trustRun')} onSubmit={(event) => void submit(event)} onClose={onClose}>
    <ActionError error={action.error} />{action.error?.code === 'SSH_TRUST_REAL_VERIFICATION_REQUIRED' && <p className="impact-notice">{t('sshTrustRealRequired')}</p>}{validation && <p className="field-error" role="alert">{validation}</p>}
    <dl className="action-target"><dt>{t('bomHostname')}</dt><dd>{machines.map((m) => m.hostname).join(', ')}</dd></dl>
    <div className="field"><Label htmlFor="trust-action">{t('trustAction')}</Label><NativeSelect className="ui:h-11" id="trust-action" value={trustAction} onChange={(event) => setTrustAction(event.target.value as (typeof trustActions)[number])} disabled={action.busy}>{trustActions.map((value) => <NativeSelectOption key={value} value={value}>{t('trust_action_' + value)}</NativeSelectOption>)}</NativeSelect></div>
    <div className="field"><Label htmlFor="trust-fp">{t('trustFingerprint')}</Label><Input id="trust-fp" value={fingerprint} onChange={(event) => setFingerprint(event.target.value)} placeholder="SHA256:..." maxLength={120} disabled={action.busy} className="mono-value ui:h-11" /><span className="field-hint">{t('trustFingerprintHint')}</span></div>
  </ActionDialog>
}
function AssetsContent() {
  const project = useProject()
  const { t } = useTranslation(), session = useSession(), client = useQueryClient()
  const [page, setPage] = useState(1), [importing, setImporting] = useState(false), [trustTarget, setTrustTarget] = useState<ProjectAsset[] | null>(null), [success, setSuccess] = useState('')
  const allowed = session.permissions.includes('model:read')
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [packageImporting, setPackageImporting] = useState(false)
  const [provisionTarget, setProvisionTarget] = useState<ProjectAsset | null>(null)
  const [modelTarget, setModelTarget] = useState<ProjectAsset | null>(null)
  const [bmcTarget,setBMCTarget] = useState<ProjectAsset|null>(null)
  const [showMappingFleet, setShowMappingFleet] = useState(false)
  const [detailTarget, setDetailTarget] = useState<{ target: ServerDetailTarget; tab: ServerDetailTab } | null>(null)
  const visible = useMonitorVisible()
  const [statusNow, setStatusNow] = useState(Date.now())
  const [showAssetViews, setShowAssetViews] = useState(false)
  const [showTopologyFleet, setShowTopologyFleet] = useState(false)
  const manage = canManageProject(project, session.user.is_admin) && session.permissions.includes('model:manage')
  const assets = useQuery({ queryKey: ['private', session.user.id, 'project', project.id, 'assets', page], queryFn: ({ signal }) => listProjectAssets(project.id, page, signal), enabled: allowed })
  const statusIds = (assets.data?.items ?? []).map(item => item.id).sort().join(',')
  const statuses = useQuery({ queryKey: ['private', session.user.id, 'project', project.id, 'port-monitor-fleet', statusIds], queryFn: ({ signal }) => portMonitorFleet(project.id, statusIds.split(','), signal), enabled: allowed && visible && !!statusIds && !assets.isError, retry: false, refetchOnWindowFocus: false, refetchInterval: visible ? 20000 : false })
  useExpireOnUnauthorized(statuses.error)
  useEffect(() => { if (!visible) return; setStatusNow(Date.now()); const timer = globalThis.setInterval(() => setStatusNow(Date.now()), 5000); return () => globalThis.clearInterval(timer) }, [visible])
  useExpireOnUnauthorized(assets.error)
  useEffect(() => { document.title = `${t('bomTitle')} · ${project.name}` }, [t, project.name])
  async function done(message: string) {
    setImporting(false); setPackageImporting(false); setTrustTarget(null); setSuccess(message)
    await client.invalidateQueries({ queryKey: ['private', session.user.id, 'project', project.id, 'assets'] })
  }
  if (!allowed) return <PermissionDenied />
  return <>
    <div className="page-heading"><div><p className="section-eyebrow">{project.name}</p><h1>{t('bomTitle')}</h1><p>{t('bomIntro')}</p></div>
      <Button type="button" variant="outline" onClick={() => setShowAssetViews(true)}>{t('assetViewsTitle')}</Button><Button variant="outline" onClick={() => setShowTopologyFleet(true)}>{t('topologyFleetTitle')}</Button><Button variant="outline" onClick={() => setShowMappingFleet(true)}>{t('mappingFleetTitle')}</Button>{manage && <div className="table-actions"><Button variant="default" className="primary-button ui:min-h-11" onClick={() => { setSuccess(''); setPackageImporting(true) }}>{t('bomPackageTitle')}</Button><Button variant="outline" className="secondary-button ui:min-h-11" onClick={() => { setSuccess(''); setImporting(true) }}>{t('bomImport')}</Button><Button variant="outline" className="secondary-button ui:min-h-11" disabled={!selected.size} onClick={() => { setSuccess(''); setTrustTarget((assets.data?.items ?? []).filter((a) => selected.has(a.machine_id))) }}>{t('trustMockTitle')}</Button></div>}</div>
    {success && <SuccessNotice>{success}</SuccessNotice>}
    <section className="management-panel" aria-busy={assets.isFetching}>
      <ActionError error={assets.error} />
      {assets.isPending ? <p className="table-loading" role="status">{t('loadingRecords')}</p> : assets.data && <>
        <div className="table-scroll" tabIndex={0} role="region" aria-label={t('bomTitle')}><Table scrollContainer={false} className="data-table"><TableHeader><TableRow>{manage && <TableHead className="ui:h-auto ui:p-4 ui:align-top ui:text-muted-foreground" scope="col"><NativeCheckbox aria-label={t('trustSelectAll')} checked={!!(assets.data.items ?? []).length && selected.size === (assets.data.items ?? []).length} onChange={(event) => setSelected(event.target.checked ? new Set((assets.data.items ?? []).map((a) => a.machine_id)) : new Set())} /></TableHead>}<TableHead className="ui:h-auto ui:p-4 ui:align-top ui:text-muted-foreground" scope="col">{t('bomHostname')}</TableHead><TableHead className="ui:h-auto ui:p-4 ui:align-top ui:text-muted-foreground" scope="col">{t('bomSerial')}</TableHead><TableHead className="ui:h-auto ui:p-4 ui:align-top ui:text-muted-foreground" scope="col">{t('bomModel')}</TableHead><TableHead className="ui:h-auto ui:p-4 ui:align-top ui:text-muted-foreground" scope="col">{t('bomRack')}</TableHead><TableHead className="ui:h-auto ui:p-4 ui:align-top ui:text-muted-foreground" scope="col">{t('bomIpRef')}</TableHead><TableHead className="ui:h-auto ui:p-4 ui:align-top ui:text-muted-foreground" scope="col">{t('bomState')}</TableHead><TableHead>{t('serverMappingStatus')}</TableHead><TableHead>{t('serverNetworkStatus')}</TableHead><TableHead className="ui:h-auto ui:p-4 ui:align-top ui:text-muted-foreground" scope="col">{t('trustTitle')}</TableHead></TableRow></TableHeader>
          <TableBody>{(assets.data.items ?? []).map((asset) => <TableRow key={asset.id} aria-selected={detailTarget?.target.id === asset.id}>{manage && <TableCell className="ui:p-4 ui:align-top ui:whitespace-normal"><NativeCheckbox aria-label={asset.hostname} checked={selected.has(asset.machine_id)} onChange={(event) => { const next = new Set(selected); if (event.target.checked) next.add(asset.machine_id); else next.delete(asset.machine_id); setSelected(next) }} /></TableCell>}<TableCell className="ui:p-4 ui:align-top ui:whitespace-normal"><Button variant="link" onClick={() => setDetailTarget({ target: asset, tab: 'ledger' })}>{asset.hostname}</Button><Button type="button" variant="ghost" size="sm" onClick={()=>setBMCTarget(asset)}>{t('bmcTitle')}</Button><Button type="button" variant="ghost" size="sm" onClick={() => setDetailTarget({ target: asset, tab: 'ledger' })}>{t('precollectTitle')}</Button><Button type="button" variant="ghost" size="sm" onClick={() => setDetailTarget({ target: asset, tab: 'topology' })}>{t('topologyTitle')}</Button><Button type="button" variant="ghost" size="sm" onClick={() => setDetailTarget({ target: asset, tab: 'mapping' })}>{t('mappingTitle')}</Button><span className="cell-subtitle">v{asset.version}</span></TableCell>
            <TableCell className="mono-value ui:p-4 ui:align-top ui:whitespace-normal">{asset.serial_number}</TableCell><TableCell className="ui:p-4 ui:align-top ui:whitespace-normal">{asset.model_name || '—'}{asset.model_version_id && asset.model_version > 0 ? <><span className="cell-subtitle">{t('assetModelVersion')} v{asset.model_version}</span><Button type="button" variant="ghost" size="sm" onClick={() => setModelTarget(asset)}>{t('assetModelBinding')}</Button></> : <span className="cell-subtitle">{t('assetModelUnbound')}</span>}</TableCell><TableCell className="ui:p-4 ui:align-top ui:whitespace-normal">{asset.rack}{asset.unit_position ? ` / ${asset.unit_position}` : ''}</TableCell><TableCell className="mono-value ui:p-4 ui:align-top ui:whitespace-normal">{asset.ip_reference || '—'}</TableCell>
            <TableCell className="ui:p-4 ui:align-top ui:whitespace-normal"><span className={`status-label ${asset.state === 'provisioned' ? 'enabled' : asset.state === 'failed' ? 'disabled' : ''}`}>{t('bom_state_' + asset.state)}</span>{(session.user.is_admin || ['Admin','DE','DL','SRE'].includes(project.role)) && <Button type="button" variant="ghost" size="sm" onClick={() => setProvisionTarget(asset)}>{t('provisionPreviewTitle')}</Button>}</TableCell>
            <StatusPillPair status={statuses.isError ? undefined : statuses.data?.items.find(item => item.asset_id === asset.id)} asOf={statuses.data?.as_of} now={statusNow} onMapping={() => setDetailTarget({ target: asset, tab: 'mapping' })} onPorts={() => setDetailTarget({ target: asset, tab: 'ports' })} />
            <TableCell className="ui:p-4 ui:align-top ui:whitespace-normal"><span className={`status-label ${asset.ssh_trust === 'established' ? 'enabled' : asset.ssh_trust === 'revoked' ? 'disabled' : ''}`}>{t('trust_state_' + asset.ssh_trust)}</span><span className="cell-subtitle">{t('sshTrustSource_' + asset.ssh_trust_source)}</span>{asset.ssh_trust === 'established' && asset.ssh_trust_source !== 'ssh' && <span className="cell-subtitle">{t('sshTrustNeedsRealVerification')}</span>}</TableCell></TableRow>)}</TableBody></Table>
          {!(assets.data.items ?? []).length && <p className="table-loading">{t('bomNoAssets')}</p>}</div>
        <Pagination offset={(page - 1) * assets.data.page_size} limit={assets.data.page_size} total={assets.data.total} busy={assets.isFetching} onChange={(offset) => setPage(offset / assets.data.page_size + 1)} /></>}
    </section>
    <SSHTrustRequests assets={assets.isError ? [] : assets.data?.items ?? []} />
    <SSHExecutions assets={assets.isError ? [] : assets.data?.items ?? []} />
    <SSHScripts assets={assets.isError ? [] : assets.data?.items ?? []} />
    <ArtifactDistribution transport="ssh" assets={assets.isError || assets.isFetching ? [] : (assets.data?.items ?? []).filter(asset => asset.state === 'provisioned' && asset.ssh_trust === 'established' && asset.ssh_trust_source === 'ssh')} />
    <AgentExecutions />
    <SSHDomainKeys />
    {detailTarget && <Suspense fallback={<p role="status">{t('loadingRecords')}</p>}><ServerDetailDrawer key={`${project.id}:${detailTarget.target.id}`} target={detailTarget.target} assets={assets.data?.items ?? []} initialTab={detailTarget.tab} onSelect={(target, tab) => setDetailTarget({ target, tab })} onClose={() => setDetailTarget(null)} /></Suspense>}
    {provisionTarget && <ProvisionPreviewDialog key={`${project.id}:${provisionTarget.id}`} projectId={project.id} asset={provisionTarget} onClose={() => setProvisionTarget(null)} />}
    {showTopologyFleet && <Suspense fallback={<p role="status">{t('loadingRecords')}</p>}><TopologyFleetDialog key={project.id} onClose={() => setShowTopologyFleet(false)} /></Suspense>}
    {showAssetViews && <Suspense fallback={<p role="status">{t('loadingRecords')}</p>}><AssetViewsDialog key={project.id} onClose={() => setShowAssetViews(false)} /></Suspense>}
    {showMappingFleet && <Suspense fallback={<p role="status">{t('loadingRecords')}</p>}><MappingFleetDialog key={project.id} onClose={() => setShowMappingFleet(false)} /></Suspense>}
    {bmcTarget && <BMCInventoryDialog key={`${project.id}:${bmcTarget.id}`} asset={bmcTarget} onClose={()=>setBMCTarget(null)} />}
    {modelTarget && <AssetModelDialog key={`${project.id}:${modelTarget.id}`} projectId={project.id} asset={modelTarget} onClose={() => setModelTarget(null)} />}
    {packageImporting && <BomPackageDialog projectId={project.id} onClose={() => setPackageImporting(false)} onDone={(message) => done(message)} />}
    {importing && <BomImportDialog projectId={project.id} onClose={() => setImporting(false)} onDone={(message) => done(message)} />}
    {trustTarget && <TrustDialog projectId={project.id} machines={trustTarget} onClose={() => setTrustTarget(null)} onDone={(message) => { setSelected(new Set()); return done(message) }} />}
  </>
}
export function AssetsPage({ projectId }: { projectId: string }) {
  return <ProjectWorkspace projectId={projectId}><AssetsContent key={projectId} /></ProjectWorkspace>
}
