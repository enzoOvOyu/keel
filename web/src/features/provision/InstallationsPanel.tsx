import { useEffect, useRef, useState } from 'react'
import { Link } from '@tanstack/react-router'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { NativeCheckbox } from '@/components/ui/checkbox'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { ActionDialog, ActionError, Pagination } from '../../components/Management'
import { useProject } from '../../components/ProjectWorkspace'
import { useSession, useExpireOnUnauthorized } from '../../lib/session'
import { useAction } from '../../lib/management'
import {
 approveProvisionStorage, getProvisionInstallation, listProvisionBootScopes, listProvisionInstallations,
 selectProvisionStorage, startProvisionInstallation, type ProvisionDisk, type ProvisionInstallation, type ProvisionRequest,
} from '../../lib/provision'

export function StartInstallation({ item, onClose, onDone }: { item: ProvisionRequest; onClose: () => void; onDone: () => Promise<void> }) {
 const { t } = useTranslation(), session = useSession(), action = useAction(), key = useRef(crypto.randomUUID())
 const [page, setPage] = useState(1), [scope, setScope] = useState(''), [reason, setReason] = useState(''), [confirmed, setConfirmed] = useState(false)
 const domains = useQuery({ queryKey: ['private', session.user.id, 'project', item.project_id, 'provision-boot-scopes', page], queryFn: ({ signal }) => listProvisionBootScopes(item.project_id, page, signal), retry: false })
 useExpireOnUnauthorized(domains.error)
 const approved = domains.data?.items.filter(value => value.state === 'approved') ?? []
 return <ActionDialog title={t('installStart')} description={`${item.snapshot.target.hostname} · ${item.snapshot.target.serial_number}`} busy={action.busy} confirm={t('installStart')} submitDisabled={!confirmed || !reason.trim() || !approved.some(value => value.id === scope) || !!domains.error || domains.isFetching || Date.parse(item.expires_at) <= Date.now()} onClose={onClose} onSubmit={event => {
  event.preventDefault()
  if (!confirmed || !approved.some(value => value.id === scope)) return
  void action.run(() => startProvisionInstallation(item, scope, reason.trim(), session.csrf_token, key.current)).then(async result => { if (result.ok) await onDone() })
 }}>
  <p className="impact-notice">{t('installStartImpact')}</p><ActionError error={domains.error || action.error} />
  <div className="field"><Label htmlFor="install-boot-scope">{t('bootScopeDomain')}</Label><Select value={scope} disabled={action.busy || domains.isFetching || !!domains.error} onValueChange={value => { setScope(value); setConfirmed(false); key.current = crypto.randomUUID() }}><SelectTrigger id="install-boot-scope" className="ui:w-full"><SelectValue placeholder={t('installChooseScope')} /></SelectTrigger><SelectContent>{approved.map(value => <SelectItem key={value.id} value={value.id}>{value.scope.clients} · {value.scope.interface} · {value.source_ref}</SelectItem>)}</SelectContent></Select></div>
  {domains.isPending && <p role="status">{t('loadingRecords')}</p>}
  {domains.data && !domains.error && <>{!approved.length && <p>{t('installNoScopes')}</p>}<Pagination offset={(page - 1) * domains.data.page_size} limit={domains.data.page_size} total={domains.data.total} busy={domains.isFetching || action.busy} onChange={offset => { setPage(offset / domains.data!.page_size + 1); setScope(''); setConfirmed(false); key.current = crypto.randomUUID() }} /></>}
  <div className="field"><Label htmlFor="install-start-reason">{t('installReason')}</Label><Input id="install-start-reason" value={reason} maxLength={500} required disabled={action.busy} onChange={event => { setReason(event.target.value); key.current = crypto.randomUUID() }} /></div>
  <div className="checkbox-field"><NativeCheckbox id="install-start-confirm" checked={confirmed} disabled={action.busy} onChange={event => setConfirmed(event.target.checked)} /><Label htmlFor="install-start-confirm">{t('installStartConfirm')}</Label></div>
 </ActionDialog>
}

// Browser filtering is only a usability aid. The server re-evaluates the exact
// signed report and disk identity under the current installation lease.
function diskEligible(disk: ProvisionDisk, all: ProvisionDisk[]) {
 return disk.healthy === true && disk.read_only === false && disk.removable === false && disk.in_use === false &&
  !!disk.wwid && !!disk.serial_number && !!disk.device_number && Number.isSafeInteger(disk.disk_sequence) && disk.disk_sequence! > 0 &&
  Number.isSafeInteger(disk.size_bytes) && disk.size_bytes! >= (16 * 1073741824) + 514 * 1048576 &&
  (disk.logical_block_bytes === 512 || disk.logical_block_bytes === 4096) &&
  all.filter(value => value.wwid === disk.wwid || value.serial_number === disk.serial_number || value.device === disk.device || value.device_number === disk.device_number).length === 1
}

function InstallationAction({ item, kind, onClose, onDone }: { item: ProvisionInstallation; kind: 'select' | 'approve'; onClose: () => void; onDone: () => Promise<void> }) {
 const { t } = useTranslation(), session = useSession(), action = useAction()
 const [selected, setSelected] = useState(''), [reason, setReason] = useState(''), [erase, setErase] = useState(false), [confirmation, setConfirmation] = useState(''), [now, setNow] = useState(Date.now())
 // Pin the fetched version for this review. Background board polling must not
 // silently replace the plan/report the operator is confirming.
 const evidence = useQuery({ queryKey: ['private', session.user.id, 'project', item.project_id, 'provision-installation-review', item.id, item.state_version], queryFn: ({ signal }) => getProvisionInstallation(item.project_id, item.id, signal), retry: false, refetchOnWindowFocus: false, refetchOnReconnect: false, staleTime: Infinity, gcTime: 0 })
 useExpireOnUnauthorized(evidence.error)
 useEffect(() => { const timer = window.setInterval(() => setNow(Date.now()), 1000); return () => window.clearInterval(timer) }, [])
 const current = evidence.data
 const disks = current?.preflight?.observation.disks ?? []
 const disk = disks.find(value => value.device === selected)
 const finished = Date.parse(current?.preflight?.observation.finished_at ?? '')
 const fresh = !!current && current.preflight?.status === 'PASS' && current.preflight.observation.firmware === 'uefi' && now - finished <= 30000 && finished <= now + 5000
 const plan = current?.storage_plan
 const expired = !current || Date.parse(current.expires_at) <= now
 const versionChanged = !!current && current.state_version !== item.state_version
 const canApprove = !!current && current.state === 'awaiting_storage_review' && !!plan && plan.plan_sha256 === current.storage_plan_sha256 && current.storage_requested_by !== session.user.id && current.created_by !== session.user.id
 const valid = !expired && !versionChanged && !!reason.trim() && !!current && !evidence.error && (kind === 'select' ? current.state === 'preflight_ready' && fresh && !!disk && diskEligible(disk, disks) && erase && confirmation === disk.serial_number : canApprove && erase)
 return <ActionDialog title={t(kind === 'select' ? 'installSelectStorage' : 'installApproveStorage')} description={current?.spec?.expected.serial_number ?? item.machine_id} busy={action.busy} confirm={t(kind === 'select' ? 'installSubmitStorage' : 'installApproveStorage')} submitDisabled={!valid} danger onClose={onClose} onSubmit={event => {
  event.preventDefault()
  if (!valid || !current) return
  void action.run(async () => {
   if (kind === 'approve') await approveProvisionStorage(current, reason.trim(), session.csrf_token)
   else if (disk && disk.size_bytes && disk.logical_block_bytes && disk.wwid && disk.serial_number) await selectProvisionStorage(current, { schema: 'keel.install-storage-intent/v1', layout: 'uefi-gpt-ext4/v1', wwid: disk.wwid, serial_number: disk.serial_number, size_bytes: disk.size_bytes, logical_block_bytes: disk.logical_block_bytes, erase_selected_disk: true }, reason.trim(), session.csrf_token)
  }).then(async result => { if (result.ok) await onDone() })
 }}>
  <p className="impact-notice">{t('installStorageImpact')}</p><ActionError error={evidence.error || action.error} />
  {evidence.isPending && <p role="status">{t('loadingRecords')}</p>}
  {versionChanged && <p role="alert">{t('installVersionChanged')}</p>}
  {expired && current && <p role="alert">{t('provisionExpired')}</p>}
  {kind === 'select' && current && <>
   {!fresh && <p role="alert">{t('installFactsStale')}</p>}
   <p>{t('installObservedAt')}: {Number.isFinite(finished) ? new Date(finished).toLocaleString() : '—'}</p>
   <div className="field"><Label htmlFor="install-disk">{t('installDisk')}</Label><Select value={selected} disabled={action.busy || !fresh || versionChanged} onValueChange={value => { setSelected(value); setErase(false); setConfirmation('') }}><SelectTrigger id="install-disk" className="ui:w-full"><SelectValue placeholder={t('installChooseDisk')} /></SelectTrigger><SelectContent>{disks.map(value => <SelectItem key={value.device} value={value.device} disabled={!diskEligible(value, disks)}>{value.device} · {value.serial_number || '—'} · {value.size_bytes ? `${(value.size_bytes / 1073741824).toFixed(1)} GiB` : '—'}</SelectItem>)}</SelectContent></Select></div>
   {!disks.some(value => diskEligible(value, disks)) && <p>{t('installNoDisks')}</p>}
   {disk && <dl className="ui:break-all"><dt>WWID</dt><dd className="mono-value">{disk.wwid || '—'}</dd><dt>{t('installDiskGeometry')}</dt><dd>{disk.size_bytes} B · {disk.logical_block_bytes} B</dd><dt>{t('installDeviceIdentity')}</dt><dd>{disk.device_number} · {disk.disk_sequence}</dd></dl>}
   <div className="field"><Label htmlFor="install-disk-serial">{t('installSerialConfirm')}</Label><Input id="install-disk-serial" value={confirmation} maxLength={256} disabled={action.busy || !disk} autoComplete="off" onChange={event => setConfirmation(event.target.value)} /></div>
  </>}
  {kind === 'approve' && plan && <><StoragePlan plan={plan} /><p>{t('installIndependentApproval')}</p></>}
  <div className="field"><Label htmlFor="install-storage-reason">{t('installReason')}</Label><Input id="install-storage-reason" maxLength={500} value={reason} required disabled={action.busy} onChange={event => setReason(event.target.value)} /></div>
  <div className="checkbox-field"><NativeCheckbox id="install-erase-confirm" checked={erase} disabled={action.busy} onChange={event => setErase(event.target.checked)} /><Label htmlFor="install-erase-confirm">{t('installEraseConfirm')}</Label></div>
 </ActionDialog>
}

function StoragePlan({ plan }: { plan: NonNullable<ProvisionInstallation['storage_plan']> }) {
 const { t } = useTranslation()
 return <><dl className="ui:break-all"><dt>{t('installDisk')}</dt><dd>{plan.device.node} · {plan.device.serial_number}</dd><dt>WWID</dt><dd className="mono-value">{plan.device.wwid}</dd><dt>{t('installDiskGeometry')}</dt><dd>{plan.device.size_bytes} B · {plan.device.logical_block_bytes} B</dd><dt>{t('installPlanDigest')}</dt><dd className="mono-value">{plan.plan_sha256}</dd><dt>{t('installEvidenceDigest')}</dt><dd className="mono-value">{plan.preflight_report_sha256}</dd></dl><div className="table-scroll"><Table><TableHeader><TableRow><TableHead>{t('installPartition')}</TableHead><TableHead>{t('installFilesystem')}</TableHead><TableHead>{t('installSize')}</TableHead></TableRow></TableHeader><TableBody>{plan.partitions.map(partition => <TableRow key={partition.number}><TableCell>{partition.number} · {partition.mount_point}</TableCell><TableCell>{partition.filesystem}</TableCell><TableCell>{(partition.size_bytes / 1073741824).toFixed(2)} GiB</TableCell></TableRow>)}</TableBody></Table></div></>
}

export function InstallationsPanel() {
 const { t } = useTranslation(), project = useProject(), session = useSession(), client = useQueryClient()
 const [page, setPage] = useState(1), [choice, setChoice] = useState<{ item: ProvisionInstallation; kind: 'select' | 'approve' }>()
 const readable = session.user.is_admin || ['Admin', 'DE', 'DL', 'SRE'].includes(project.role)
 const writable = readable && !['archived', 'delivered'].includes(project.state)
 const reviewer = writable && (session.user.is_admin || ['Admin', 'DL'].includes(project.role))
 const prefix = ['private', session.user.id, 'project', project.id, 'provision-installations']
 const records = useQuery({ queryKey: [...prefix, page], enabled: readable, queryFn: ({ signal }) => listProvisionInstallations(project.id, page, signal), retry: false, refetchInterval: query => query.state.error || !query.state.data?.items.some(item => !['registered', 'failed', 'canceled', 'unknown'].includes(item.state)) ? false : 5000, refetchIntervalInBackground: false })
 useExpireOnUnauthorized(records.error)
 useEffect(() => { setPage(1); setChoice(undefined) }, [project.id])
 useEffect(() => { if (!writable || records.error || (choice?.kind === 'approve' && !reviewer)) setChoice(undefined) }, [writable, reviewer, records.error, choice?.kind])
 const refresh = async () => { await client.invalidateQueries({ queryKey: prefix }) }
 if (!readable) return null
 return <section className="management-page" aria-labelledby="installations-title"><div className="page-heading"><div><h2 id="installations-title">{t('installTitle')}</h2><p>{t('installDetail')}</p></div><Button variant="outline" disabled={records.isFetching} onClick={() => void refresh()}>{t('refresh')}</Button></div>
  <ActionError error={records.error} />{records.isPending && <p role="status">{t('loadingRecords')}</p>}
  {records.data && !records.error && <>{!records.data.items.length && <p>{t('installEmpty')}</p>}<div className="table-scroll" role="region" aria-label={t('installTitle')} tabIndex={0}><Table><TableHeader><TableRow><TableHead>{t('provisionIdentity')}</TableHead><TableHead>{t('status')}</TableHead><TableHead>{t('provisionExpiry')}</TableHead><TableHead>{t('actions')}</TableHead></TableRow></TableHeader><TableBody>{records.data.items.map(item => {
   const expired = Date.parse(item.expires_at) <= Date.now()
   return <TableRow key={item.id} data-provision-installation={item.id}><TableCell><span>{item.spec?.expected.serial_number ?? item.machine_id}</span><span className="cell-subtitle mono-value">{item.id} · v{item.state_version}</span><details><summary>{t('provisionProof')}</summary><dl className="ui:max-w-96 ui:whitespace-normal ui:break-all"><dt>{t('provisionReason')}</dt><dd>{item.reason}</dd><dt>{t('provisionSnapshot')}</dt><dd>{item.snapshot_sha256}</dd><dt>{t('bootScopeDomain')}</dt><dd>{item.scope_id}</dd><dt>{t('installEvidenceDigest')}</dt><dd>{item.latest_report_sha256 || '—'}</dd><dt>{t('installPlanDigest')}</dt><dd>{item.storage_plan_sha256 || '—'}</dd></dl>{item.failure_reason && <p role="status">{item.failure_reason}</p>}</details></TableCell><TableCell>{t(`installState_${item.state}`)}{expired && !['registered', 'failed', 'canceled', 'unknown'].includes(item.state) && <span className="cell-subtitle">{t('provisionExpired')}</span>}{item.state === 'unknown' && <span className="cell-subtitle">{t('installUnknown')}</span>}</TableCell><TableCell>{new Date(item.expires_at).toLocaleString()}</TableCell><TableCell><div className="table-actions">{writable && !expired && item.state === 'preflight_ready' && <Button size="sm" onClick={() => setChoice({ item, kind: 'select' })}>{t('installSelectStorage')}</Button>}{reviewer && !expired && item.state === 'awaiting_storage_review' && item.created_by !== session.user.id && item.storage_requested_by !== session.user.id && <Button size="sm" variant="outline" onClick={() => setChoice({ item, kind: 'approve' })}>{t('installApproveStorage')}</Button>}<Link to="/p/$projectId/executions" params={{ projectId: project.id }} search={{ execution: item.execution_id, tab: 'overview' }}>{t('sshTrustViewExecution')}</Link></div></TableCell></TableRow>
  })}</TableBody></Table></div><Pagination offset={(page - 1) * records.data.page_size} limit={records.data.page_size} total={records.data.total} busy={records.isFetching} onChange={offset => setPage(offset / records.data!.page_size + 1)} /></>}
  {choice && choice.item.project_id === project.id && writable && !records.error && (choice.kind !== 'approve' || reviewer) && <InstallationAction key={`${choice.item.id}:${choice.item.state_version}:${choice.kind}`} {...choice} onClose={() => { setChoice(undefined); void refresh() }} onDone={async () => { setChoice(undefined); await refresh() }} />}
 </section>
}
