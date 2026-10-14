import { useEffect, useState, type FormEvent } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'

import { ProjectWorkspace, canManageProject, useProject } from '../../components/ProjectWorkspace'
import { ActionDialog, ActionError, PermissionDenied } from '../../components/Management'
import { useAction } from '../../lib/management'
import { useSession, useExpireOnUnauthorized } from '../../lib/session'
import { listProjectAssets } from '../../lib/bom'
import {
  abortDriverPlan, createDriverPlan, getDriverPlan, listApprovedMedia, listDriverComponents, listDriverPlans,
  manualRecoverDriverTarget, precheckDriverPlan, rollbackDriverTarget, startDriverPlan,
  type DriverComponent, type DriverPlan, type DriverTarget, type MediaSummary,
} from '../../lib/driver'

const planStateClass: Record<string, string> = {
  draft: '', prechecked: '', canary_running: '', rolling: '', completed: 'enabled',
  rolled_back: '', manual_required: '', aborted: 'disabled',
}
const targetStateClass: Record<string, string> = {
  verified: 'enabled', rolled_back: '', manual: '', failed: 'disabled', pending: '', distributing: '',
  installing: '', verifying: '', reconcile: '', precheck_blocked: 'disabled',
}
const rollbackClass: Record<string, string> = { rollback_verified: 'enabled', manual_only: '', none: 'disabled' }
const cap = (value: string) => value[0].toUpperCase() + value.slice(1)

function CapabilityMatrix({ components }: { components: DriverComponent[] }) {
  const { t } = useTranslation()
  return <section className="management-panel">
    <h2>{t('driverMatrix')}</h2>
    <table className="data-table">
      <thead><tr>
        <th scope="col">{t('driverMatrixName')}</th><th scope="col">{t('driverMatrixType')}</th>
        <th scope="col">{t('driverMatrixRollback')}</th><th scope="col">{t('driverMatrixBackup')}</th><th scope="col">{t('driverMatrixNotes')}</th>
      </tr></thead>
      <tbody>{components.map((component) => <tr key={component.key}>
        <td><strong>{component.display_name}</strong><span className="cell-subtitle"> · {component.key}</span></td>
        <td className="mono-value">{component.media_type}</td>
        <td><span className={`status-label ${rollbackClass[component.rollback]}`}>{t('driverRollback' + cap(component.rollback))}</span></td>
        <td><span className={`status-label ${component.backup_required ? '' : 'disabled'}`}>{component.backup_required ? t('driverBackupYes') : t('driverBackupNo')}</span></td>
        <td className="cell-subtitle">{component.notes}</td>
      </tr>)}</tbody>
    </table>
  </section>
}

function PlanForm({ projectId, manage, onCreated }: { projectId: string; manage: boolean; onCreated: (plan: DriverPlan) => void }) {
  const { t } = useTranslation(), session = useSession(), action = useAction()
  const components = useQuery({ queryKey: ['private', session.user.id, 'project', projectId, 'driver-components'], queryFn: ({ signal }) => listDriverComponents(signal) })
  const assets = useQuery({ queryKey: ['private', session.user.id, 'project', projectId, 'assets', 1], queryFn: ({ signal }) => listProjectAssets(projectId, 1, signal) })
  useExpireOnUnauthorized(components.error); useExpireOnUnauthorized(assets.error)
  const [name, setName] = useState('')
  const [include, setInclude] = useState<Record<string, boolean>>({})
  const [media, setMedia] = useState<Record<string, string>>({})
  const [baseline, setBaseline] = useState<Record<string, string>>({})
  const [machines, setMachines] = useState<string[]>([])
  const [canary, setCanary] = useState('')
  const [batchSize, setBatchSize] = useState(1)
  const [backupPlan, setBackupPlan] = useState('')
  const [validation, setValidation] = useState('')
  useEffect(() => {
    if (machines.length === 0 && assets.data?.items.length) {
      const ids = assets.data.items.map((asset) => asset.machine_id)
      setMachines(ids); setCanary(ids[0] ?? '')
    }
  }, [assets.data, machines.length])
  const included = components.data?.components.filter((component) => include[component.key]) ?? []
  const needsBackup = included.some((component) => component.backup_required)
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setValidation('')
    if (!name.trim()) { setValidation(t('driverPlanName') + t('driverUnknown')); return }
    if (included.length === 0) { setValidation(t('driverComponents')); return }
    if (machines.length === 0) { setValidation(t('driverMachines')); return }
    if (!canary || !machines.includes(canary)) { setValidation(t('driverCanary')); return }
    if (needsBackup && !backupPlan.trim()) { setValidation(t('driverBackupPlan')); return }
    const body = {
      name: name.trim(),
      components: Object.fromEntries(included.map((component) => [component.key, { media_id: media[component.key] }])),
      machines,
      baseline: Object.fromEntries(included.filter((component) => baseline[component.key]?.trim()).map((component) => [component.key, baseline[component.key].trim()])),
      canary_machine_id: canary,
      batch_size: batchSize,
      backup_plan: backupPlan.trim(),
    }
    const result = await action.run(() => createDriverPlan(projectId, body, session.csrf_token, crypto.randomUUID()))
    if (result.ok) onCreated(result.value)
  }
  const disabled = !manage || action.busy
  return <section className="management-panel">
    <h2>{t('driverCreate')}</h2>
    <ActionError error={action.error} />
    {validation && <p className="field-error" role="alert">{validation}</p>}
    {assets.isPending && <p className="table-loading" role="status">{t('loadingRecords')}</p>}
    {assets.data && components.data && <form onSubmit={(event) => void submit(event)}>
      <div className="field"><label htmlFor="driver-plan-name">{t('driverPlanName')}</label>
        <input id="driver-plan-name" value={name} disabled={disabled} onChange={(event) => setName(event.target.value)} />
        <p className="field-hint">{t('driverPlanNameHint')}</p></div>
      <fieldset className="driver-components" disabled={disabled}>
        <legend>{t('driverComponents')}</legend>
        {components.data.components.map((component) => <div key={component.key} className="driver-component">
          <label className="driver-component-toggle">
            <input type="checkbox" checked={Boolean(include[component.key])}
              onChange={(event) => setInclude((value) => ({ ...value, [component.key]: event.target.checked })) } />
            <span><strong>{component.display_name}</strong> <span className="cell-subtitle">· {component.key}</span></span>
          </label>
          {include[component.key] && <ApprovedMediaSelect projectId={projectId} componentKey={component.key} type={component.media_type} value={media[component.key] ?? ''}
            onChange={(id) => setMedia((value) => ({ ...value, [component.key]: id }))} disabled={disabled} />}
          {include[component.key] && <div className="field driver-baseline-field">
            <label htmlFor={`driver-baseline-${component.key}`}>{t('driverBaseline')} · {component.key}</label>
            <input id={`driver-baseline-${component.key}`} value={baseline[component.key] ?? ''} placeholder="R550" disabled={disabled}
              onChange={(event) => setBaseline((value) => ({ ...value, [component.key]: event.target.value })) } /></div>}
        </div>)}
      </fieldset>
      <fieldset className="driver-machines" disabled={disabled}>
        <legend>{t('driverMachines')}</legend>
        <p className="field-hint">{t('driverMachinesHint')}</p>
        {assets.data.items.map((asset) => <label key={asset.machine_id} className="driver-machine">
          <input type="checkbox" checked={machines.includes(asset.machine_id)}
            onChange={(event) => setMachines(event.target.checked ? [...machines, asset.machine_id] : machines.filter((id) => id !== asset.machine_id))} />
          <span>{asset.hostname}</span>
        </label>)}
      </fieldset>
      <div className="field"><label htmlFor="driver-canary">{t('driverCanary')}</label>
        <select id="driver-canary" value={canary} disabled={disabled || machines.length === 0} onChange={(event) => setCanary(event.target.value)}>
          {machines.map((id) => <option key={id} value={id}>{assets.data?.items.find((asset) => asset.machine_id === id)?.hostname ?? id}</option>)}
        </select><p className="field-hint">{t('driverCanaryHint')}</p></div>
      <div className="field"><label htmlFor="driver-batch">{t('driverBatchSize')}</label>
        <input id="driver-batch" type="number" min={1} max={64} value={batchSize} disabled={disabled} onChange={(event) => setBatchSize(Number(event.target.value))} /></div>
      <div className="field"><label htmlFor="driver-backup">{t('driverBackupPlan')}</label>
        <textarea id="driver-backup" rows={2} value={backupPlan} disabled={disabled} onChange={(event) => setBackupPlan(event.target.value)} />
        <p className="field-hint">{t('driverBackupPlanHint')}</p></div>
      {manage && <button type="submit" className="primary-button" disabled={action.busy}>{t('driverStart')}</button>}
    </form>}
  </section>
}

function ApprovedMediaSelect({ projectId, componentKey, type, value, onChange, disabled }: { projectId: string; componentKey: string; type: string; value: string; onChange: (id: string) => void; disabled: boolean }) {
  const { t } = useTranslation(), session = useSession()
  const media = useQuery({ queryKey: ['private', session.user.id, 'project', projectId, 'driver-media', type, componentKey], queryFn: ({ signal }) => listApprovedMedia(projectId, type, signal) })
  useExpireOnUnauthorized(media.error)
  const items: MediaSummary[] = media.data ?? []
  return <div className="field driver-media-field">
    <label htmlFor={`driver-media-${componentKey}`}>{t('driverComponentSelect')}</label>
    {media.isPending && <p className="table-loading" role="status">{t('loadingRecords')}</p>}
    {media.data && items.length === 0 && <p className="field-hint">{t('driverComponentNone')}</p>}
    {items.length > 0 && <select id={`driver-media-${componentKey}`} value={value} disabled={disabled} onChange={(event) => onChange(event.target.value)}>
      <option value="">—</option>
      {items.map((entry) => <option key={entry.id} value={entry.id}>{entry.name}@{entry.version}</option>)}
    </select>}
  </div>
}

function PlansList({ projectId, selected, onSelect }: { projectId: string; selected: string; onSelect: (id: string) => void }) {
  const { t } = useTranslation(), session = useSession()
  const plans = useQuery({ queryKey: ['private', session.user.id, 'project', projectId, 'driver-plans'], queryFn: ({ signal }) => listDriverPlans(projectId, signal) })
  useExpireOnUnauthorized(plans.error)
  return <section className="management-panel" aria-busy={plans.isFetching}>
    <h2>{t('driverPlans')}</h2>
    <ActionError error={plans.error} />
    {plans.isPending && <p className="table-loading" role="status">{t('loadingRecords')}</p>}
    {plans.data && plans.data.items.length === 0 && <p className="empty-hint">{t('driverNoPlans')}</p>}
    {plans.data && plans.data.items.length > 0 && <table className="data-table">
      <thead><tr>
        <th scope="col">{t('driverPlan')}</th><th scope="col">{t('driverState')}</th>
        <th scope="col">{t('driverConclusion')}</th><th scope="col">{t('driverCreatedAt')}</th>
      </tr></thead>
      <tbody>{plans.data.items.map((entry) => <tr key={entry.id} aria-selected={entry.id === selected}>
        <td><button type="button" className="link-button" onClick={() => onSelect(entry.id === selected ? '' : entry.id)}>{entry.name}</button></td>
        <td><span className={`status-label ${planStateClass[entry.state] ?? ''}`}>{t('driverState' + cap(entry.state))}</span></td>
        <td>{entry.conclusion === 'PASS' ? <span className="status-label enabled">{t('driverConclusion') + ' PASS'}</span>
          : entry.conclusion === 'FAIL' ? <span className="status-label disabled">{t('driverConclusion') + ' FAIL'}</span>
          : entry.state === 'completed' ? '—' : '—'}</td>
        <td className="cell-subtitle">{new Date(entry.created_at).toLocaleString()}</td>
      </tr>)}</tbody>
    </table>}
  </section>
}

function ManualDialog({ projectId, planId, machineId, onClose, onDone }: { projectId: string; planId: string; machineId: string; onClose: () => void; onDone: () => Promise<void> }) {
  const { t } = useTranslation(), session = useSession(), action = useAction()
  const [note, setNote] = useState(''), [validation, setValidation] = useState('')
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setValidation('')
    if (note.trim().length < 2) { setValidation(t('driverManualNote')); return }
    const result = await action.run(() => manualRecoverDriverTarget(projectId, planId, machineId, note.trim(), session.csrf_token, crypto.randomUUID()))
    if (result.ok) await onDone()
  }
  return <ActionDialog title={t('driverManual')} description={t('driverManualNoteHint')} busy={action.busy} confirm={t('driverManual')} onSubmit={(event) => void submit(event)} onClose={onClose}>
    <ActionError error={action.error} />{validation && <p className="field-error" role="alert">{validation}</p>}
    <div className="field"><label htmlFor="driver-manual-note">{t('driverManualNote')}</label>
      <textarea id="driver-manual-note" rows={3} value={note} disabled={action.busy} onChange={(event) => setNote(event.target.value)} /></div>
  </ActionDialog>
}

function PlanDetail({ projectId, plan, manage, onChanged }: { projectId: string; plan: DriverPlan; manage: boolean; onChanged: () => void }) {
  const { t } = useTranslation(), session = useSession(), client = useQueryClient(), action = useAction()
  const detail = useQuery({ queryKey: ['private', session.user.id, 'project', projectId, 'driver-plan', plan.id], queryFn: ({ signal }) => getDriverPlan(projectId, plan.id, signal) })
  const [rollbackFor, setRollbackFor] = useState(''), [manualFor, setManualFor] = useState('')
  useExpireOnUnauthorized(detail.error)
  const targets: DriverTarget[] = detail.data?.targets ?? []
  const summary = (detail.data?.summary ?? {}) as Record<string, number>
  async function run<T>(call: () => Promise<T>) {
    const result = await action.run(call)
    if (result.ok) {
      await Promise.all([
        detail.refetch(),
        client.invalidateQueries({ queryKey: ['private', session.user.id, 'project', projectId, 'driver-plans'] }),
      ])
      onChanged()
    }
    return result
  }
  return <section className="management-panel" aria-busy={detail.isFetching}>
    <div className="nettest-run-head">
      <h2>{plan.name}</h2>
      <span className={`status-label ${planStateClass[detail.data?.plan.state ?? plan.state] ?? ''}`}>{t('driverState' + cap(detail.data?.plan.state ?? plan.state))}</span>
    </div>
    <ActionError error={action.error} />
    {detail.isPending && <p className="table-loading" role="status">{t('loadingRecords')}</p>}
    {detail.data && <>
      <h3>{t('driverTargets')}</h3>
      <table className="data-table">
        <thead><tr>
          <th scope="col">{t('driverMachine')}</th><th scope="col">{t('driverRole')}</th><th scope="col">{t('driverTargetVersions')}</th>
          <th scope="col">{t('driverState')}</th><th scope="col">{t('driverCacheHit')}</th><th scope="col">{t('driverReadback')}</th>
          <th scope="col">{t('driverFailure')}</th><th scope="col">{t('driverEvidence')}</th>
        </tr></thead>
        <tbody>{targets.map((target) => <tr key={target.id}>
          <td className="mono-value">{target.machine_id}</td>
          <td>{target.role}</td>
          <td className="mono-value">{Object.entries(target.target_versions).map(([k, v]) => `${k}=${v}`).join(' ')}</td>
          <td><span className={`status-label ${targetStateClass[target.state] ?? ''}`}>{t('driverTarget' + cap(target.state))}</span></td>
          <td>{target.cache_hit ? t('driverYes') : t('driverNo')}</td>
          <td className="mono-value">{Object.entries(target.readback).map(([k, v]) => `${k}=${v}`).join(' ') || '—'}</td>
          <td className="cell-subtitle">{target.failure_class || '—'}</td>
          <td className="cell-subtitle">{target.evidence && Object.keys(target.evidence).length ? Object.keys(target.evidence).join(', ') : '—'}</td>
        </tr>)}</tbody>
      </table>
      <h3>{t('driverTransfers')}</h3>
      {detail.data.transfers.length === 0 && <p className="empty-hint">{t('driverUnknown')}</p>}
      {detail.data.transfers.length > 0 && <table className="data-table">
        <thead><tr>
          <th scope="col">{t('driverTransferComponent')}</th><th scope="col">{t('driverTransferUri')}</th><th scope="col">{t('driverTransferSha')}</th>
          <th scope="col">{t('driverTransferSize')}</th><th scope="col">{t('driverTransferBytes')}</th><th scope="col">{t('driverTransferChecksum')}</th><th scope="col">{t('driverTransferRetries')}</th>
        </tr></thead>
        <tbody>{detail.data.transfers.map((transfer) => <tr key={transfer.id}>
          <td>{transfer.component}</td>
          <td className="mono-value">{transfer.uri}</td>
          <td className="mono-value">{transfer.sha256.slice(0, 12)}</td>
          <td className="mono-value">{transfer.size}</td>
          <td className="mono-value">{transfer.bytes}</td>
          <td><span className={`status-label ${transfer.checksum === 'mismatch' ? 'disabled' : transfer.checksum === 'match' ? 'enabled' : ''}`}>{transfer.checksum === 'match' ? t('driverChecksumMatch') : transfer.checksum === 'mismatch' ? t('driverChecksumMismatch') : '—'}</span></td>
          <td className="mono-value">{transfer.retries}</td>
        </tr>)}</tbody>
      </table>}
      <h3>{t('driverSummary')}</h3>
      <p className="gate-card-summary">
        <span className="status-label enabled">{t('driverSummaryHits')}: {summary.transfer_hits ?? 0}</span>
        <span className="status-label">{t('driverSummaryMisses')}: {summary.transfer_misses ?? 0}</span>
        <span className="status-label">{t('driverSummaryBytes')}: {summary.bytes_transferred ?? 0}</span>
        <span className="status-label enabled">{t('driverSummaryVerified')}: {summary.verified ?? 0}</span>
        <span className="status-label">{t('driverSummaryRolledBack')}: {summary.rolled_back ?? 0}</span>
        <span className="status-label">{t('driverSummaryManual')}: {summary.manual ?? 0}</span>
        <span className="status-label disabled">{t('driverSummaryFailed')}: {summary.failed ?? 0}</span>
        <span className="status-label">{t('driverSummaryPending')}: {summary.pending ?? 0}</span>
      </p>
      <div className="form-actions">
        {manage && detail.data.plan.state === 'draft' && <button type="button" className="primary-button" disabled={action.busy} onClick={() => void run(() => precheckDriverPlan(projectId, plan.id, session.csrf_token, crypto.randomUUID()))}>{t('driverPrecheck')}</button>}
        {manage && detail.data.plan.state === 'prechecked' && <button type="button" className="primary-button" disabled={action.busy} onClick={() => void run(() => startDriverPlan(projectId, plan.id, session.csrf_token, crypto.randomUUID()))}>{t('driverStartRun')}</button>}
        {manage && ['draft', 'prechecked', 'canary_running', 'rolling'].includes(detail.data.plan.state) && <button type="button" className="secondary-button" disabled={action.busy} onClick={() => void run(() => abortDriverPlan(projectId, plan.id, session.csrf_token, crypto.randomUUID()))}>{t('driverAbort')}</button>}
        {manage && targets.filter((target) => ['failed', 'reconcile'].includes(target.state)).map((target) => <button key={target.id} type="button" className="secondary-button" disabled={action.busy} onClick={() => setRollbackFor(target.machine_id)}>{t('driverRollback')} · {target.machine_id.slice(0, 8)}</button>)}
        {manage && targets.filter((target) => ['failed', 'manual', 'reconcile'].includes(target.state)).map((target) => <button key={target.id} type="button" className="secondary-button" disabled={action.busy} onClick={() => setManualFor(target.machine_id)}>{t('driverManual')} · {target.machine_id.slice(0, 8)}</button>)}
      </div>
    </>}
    {rollbackFor && <ActionDialog title={t('driverRollback')} description={t('driverManualNoteHint')} busy={action.busy} confirm={t('driverRollback')}
      onSubmit={async (event) => { event.preventDefault(); await run(() => rollbackDriverTarget(projectId, plan.id, rollbackFor, session.csrf_token, crypto.randomUUID())); setRollbackFor('') }}
      onClose={() => setRollbackFor('')}>
      <ActionError error={action.error} />
    </ActionDialog>}
    {manualFor && <ManualDialog projectId={projectId} planId={plan.id} machineId={manualFor} onClose={() => setManualFor('')} onDone={async () => { setManualFor(''); await detail.refetch() }} />}
  </section>
}

function DriverContent() {
  const project = useProject()
  const { t } = useTranslation(), session = useSession()
  const [selected, setSelected] = useState('')
  const manage = canManageProject(project, session.user.is_admin) && session.permissions.includes('model:manage')
  const components = useQuery({ queryKey: ['private', session.user.id, 'project', project.id, 'driver-components'], queryFn: ({ signal }) => listDriverComponents(signal) })
  const plans = useQuery({ queryKey: ['private', session.user.id, 'project', project.id, 'driver-plans'], queryFn: ({ signal }) => listDriverPlans(project.id, signal) })
  useExpireOnUnauthorized(components.error); useExpireOnUnauthorized(plans.error)
  useEffect(() => { document.title = `${t('driverTitle')} · ${project.name}` }, [t, project.name])
  const plan = (plans.data?.items ?? []).find((entry) => entry.id === selected) ?? null
  if (!session.permissions.includes('model:read')) return <PermissionDenied />
  return <>
    <div className="page-heading"><div><p className="section-eyebrow">{project.name}</p><h1>{t('driverTitle')}</h1><p>{t('driverIntro')}</p></div></div>
    {components.data && <CapabilityMatrix components={components.data.components} />}
    {manage && <PlanForm projectId={project.id} manage={manage} onCreated={(created) => { setSelected(created.id); void plans.refetch() }} />}
    <PlansList projectId={project.id} selected={selected} onSelect={setSelected} />
    {plan && <PlanDetail projectId={project.id} plan={plan} manage={manage} onChanged={() => void plans.refetch()} />}
  </>
}

export function DriverPage({ projectId }: { projectId: string }) {
  return <ProjectWorkspace projectId={projectId}><DriverContent /></ProjectWorkspace>
}
