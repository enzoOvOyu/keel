import { useEffect, useState, type FormEvent } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'

import { ProjectWorkspace, canManageProject, useProject } from '../../components/ProjectWorkspace'
import { ActionDialog, ActionError, PermissionDenied } from '../../components/Management'
import { useAction } from '../../lib/management'
import { useSession, useExpireOnUnauthorized } from '../../lib/session'
import { listProjectAssets } from '../../lib/bom'
import { listApprovedMedia } from '../../lib/driver'
import {
  abortAgentPlan, createAgentPlan, getAgentPlan, listAgentPlans,
  revokeAgentCredential, rotateAgentCredential,
  type AgentPlan, type AgentTarget,
} from '../../lib/agent'

const planStateClass: Record<string, string> = {
  running: '', completed: 'enabled', failed: 'disabled', aborted: 'disabled',
}
const targetStateClass: Record<string, string> = {
  pending: '', installing: '', installed: 'enabled', failed: 'disabled', blocked: 'disabled',
}
const cap = (value: string) => value[0].toUpperCase() + value.slice(1)

function PlanForm({ projectId, manage, onCreated }: { projectId: string; manage: boolean; onCreated: (plan: AgentPlan) => void }) {
  const { t } = useTranslation(), session = useSession(), action = useAction()
  const assets = useQuery({ queryKey: ['private', session.user.id, 'project', projectId, 'assets', 1], queryFn: ({ signal }) => listProjectAssets(projectId, 1, signal) })
  const media = useQuery({ queryKey: ['private', session.user.id, 'project', projectId, 'agent-media'], queryFn: ({ signal }) => listApprovedMedia(projectId, 'agent', signal) })
  useExpireOnUnauthorized(assets.error); useExpireOnUnauthorized(media.error)
  const [name, setName] = useState('')
  const [mediaId, setMediaId] = useState('')
  const [machines, setMachines] = useState<string[]>([])
  const [validation, setValidation] = useState('')
  useEffect(() => {
    if (machines.length === 0 && assets.data?.items.length) setMachines(assets.data.items.map((asset) => asset.machine_id))
  }, [assets.data, machines.length])
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setValidation('')
    if (!name.trim()) { setValidation(t('agentPlanName')); return }
    if (!mediaId) { setValidation(t('agentMediaRequired')); return }
    if (machines.length === 0) { setValidation(t('agentMachines')); return }
    const result = await action.run(() => createAgentPlan(projectId, { name: name.trim(), media_id: mediaId, machines }, session.csrf_token, crypto.randomUUID()))
    if (result.ok) onCreated(result.value)
  }
  const disabled = !manage || action.busy
  const items = media.data ?? []
  return <section className="management-panel">
    <h2>{t('agentCreate')}</h2>
    <ActionError error={action.error} />
    {validation && <p className="field-error" role="alert">{validation}</p>}
    {assets.isPending && <p className="table-loading" role="status">{t('loadingRecords')}</p>}
    {assets.data && <form onSubmit={(event) => void submit(event)}>
      <div className="field"><label htmlFor="agent-plan-name">{t('agentPlanName')}</label>
        <input id="agent-plan-name" value={name} disabled={disabled} onChange={(event) => setName(event.target.value)} />
        <p className="field-hint">{t('agentPlanNameHint')}</p></div>
      <div className="field"><label htmlFor="agent-media">{t('agentMedia')}</label>
        {media.isPending && <p className="table-loading" role="status">{t('loadingRecords')}</p>}
        {media.data && items.length === 0 && <p className="field-hint">{t('agentMediaNone')}</p>}
        {items.length > 0 && <select id="agent-media" value={mediaId} disabled={disabled} onChange={(event) => setMediaId(event.target.value)}>
          <option value="">—</option>
          {items.map((entry) => <option key={entry.id} value={entry.id}>{entry.name}@{entry.version} · {entry.arch}</option>)}
        </select>}
        <p className="field-hint">{t('agentMediaHint')}</p></div>
      <fieldset className="agent-machines" disabled={disabled}>
        <legend>{t('agentMachines')}</legend>
        <p className="field-hint">{t('agentMachinesHint')}</p>
        {assets.data.items.map((asset) => <label key={asset.machine_id} className="agent-machine">
          <input type="checkbox" checked={machines.includes(asset.machine_id)}
            onChange={(event) => setMachines(event.target.checked ? [...machines, asset.machine_id] : machines.filter((id) => id !== asset.machine_id))} />
          <span>{asset.hostname}</span>
        </label>)}
      </fieldset>
      {manage && <button type="submit" className="primary-button" disabled={action.busy}>{t('agentCreate')}</button>}
    </form>}
  </section>
}

function PlansList({ projectId, selected, onSelect }: { projectId: string; selected: string; onSelect: (id: string) => void }) {
  const { t } = useTranslation(), session = useSession()
  const plans = useQuery({ queryKey: ['private', session.user.id, 'project', projectId, 'agent-plans'], queryFn: ({ signal }) => listAgentPlans(projectId, signal) })
  useExpireOnUnauthorized(plans.error)
  return <section className="management-panel" aria-busy={plans.isFetching}>
    <h2>{t('agentPlans')}</h2>
    <ActionError error={plans.error} />
    {plans.isPending && <p className="table-loading" role="status">{t('loadingRecords')}</p>}
    {plans.data && plans.data.items.length === 0 && <p className="empty-hint">{t('agentNoPlans')}</p>}
    {plans.data && plans.data.items.length > 0 && <table className="data-table">
      <thead><tr>
        <th scope="col">{t('agentPlan')}</th><th scope="col">{t('agentState')}</th>
        <th scope="col">{t('agentMediaLabel')}</th><th scope="col">{t('agentCreatedAt')}</th>
      </tr></thead>
      <tbody>{plans.data.items.map((entry) => <tr key={entry.id} aria-selected={entry.id === selected}>
        <td><button type="button" className="link-button" onClick={() => onSelect(entry.id === selected ? '' : entry.id)}>{entry.name}</button></td>
        <td><span className={`status-label ${planStateClass[entry.state] ?? ''}`}>{t('agentState' + cap(entry.state))}</span></td>
        <td className="mono-value">{entry.media?.uri ?? '—'}</td>
        <td className="cell-subtitle">{new Date(entry.created_at).toLocaleString()}</td>
      </tr>)}</tbody>
    </table>}
  </section>
}

function CredentialDialog({ projectId, machineId, mode, onClose, onDone }: { projectId: string; machineId: string; mode: 'rotate' | 'revoke'; onClose: () => void; onDone: () => Promise<void> }) {
  const { t } = useTranslation(), session = useSession(), action = useAction()
  const [fingerprint, setFingerprint] = useState('')
  const [validation, setValidation] = useState('')
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setValidation('')
    if (mode === 'rotate') {
      if (fingerprint.trim().length < 2) { setValidation(t('agentFingerprintRequired')); return }
      const result = await action.run(() => rotateAgentCredential(projectId, machineId, fingerprint.trim(), session.csrf_token, crypto.randomUUID()))
      if (result.ok) await onDone()
    } else {
      const result = await action.run(() => revokeAgentCredential(projectId, machineId, session.csrf_token, crypto.randomUUID()))
      if (result.ok) await onDone()
    }
  }
  return <ActionDialog title={t(mode === 'rotate' ? 'agentRotate' : 'agentRevoke')} description={t('agentCredentialHint')} busy={action.busy} confirm={t(mode === 'rotate' ? 'agentRotate' : 'agentRevoke')} onSubmit={(event) => void submit(event)} onClose={onClose}>
    <ActionError error={action.error} />{validation && <p className="field-error" role="alert">{validation}</p>}
    {mode === 'rotate' && <div className="field"><label htmlFor="agent-fingerprint">{t('agentFingerprint')}</label>
      <input id="agent-fingerprint" value={fingerprint} disabled={action.busy} onChange={(event) => setFingerprint(event.target.value)} /></div>}
  </ActionDialog>
}

function PlanDetail({ projectId, plan, manage, onChanged }: { projectId: string; plan: AgentPlan; manage: boolean; onChanged: () => void }) {
  const { t } = useTranslation(), session = useSession(), client = useQueryClient(), action = useAction()
  const detail = useQuery({ queryKey: ['private', session.user.id, 'project', projectId, 'agent-plan', plan.id], queryFn: ({ signal }) => getAgentPlan(projectId, plan.id, signal) })
  const [rotateFor, setRotateFor] = useState(''), [revokeFor, setRevokeFor] = useState('')
  useExpireOnUnauthorized(detail.error)
  const targets: AgentTarget[] = detail.data?.targets ?? []
  const summary = (detail.data?.summary ?? {}) as Record<string, number>
  async function run<T>(call: () => Promise<T>) {
    const result = await action.run(call)
    if (result.ok) {
      await Promise.all([
        detail.refetch(),
        client.invalidateQueries({ queryKey: ['private', session.user.id, 'project', projectId, 'agent-plans'] }),
      ])
      onChanged()
    }
    return result
  }
  return <section className="management-panel" aria-busy={detail.isFetching}>
    <div className="nettest-run-head">
      <h2>{plan.name}</h2>
      <span className={`status-label ${planStateClass[detail.data?.plan.state ?? plan.state] ?? ''}`}>{t('agentState' + cap(detail.data?.plan.state ?? plan.state))}</span>
    </div>
    <ActionError error={action.error} />
    {detail.isPending && <p className="table-loading" role="status">{t('loadingRecords')}</p>}
    {detail.data && <>
      <p className="field-hint"><span className="mono-value">{detail.data.plan.media?.uri ?? ''}</span></p>
      <h3>{t('agentTargets')}</h3>
      <table className="data-table">
        <thead><tr>
          <th scope="col">{t('agentMachine')}</th><th scope="col">{t('agentState')}</th>
          <th scope="col">{t('agentReason')}</th><th scope="col">{t('agentCacheHit')}</th>
          <th scope="col">{t('agentCredential')}</th><th scope="col">{t('agentActions')}</th>
        </tr></thead>
        <tbody>{targets.map((target) => <tr key={target.id}>
          <td className="mono-value">{target.hostname || target.machine_id}</td>
          <td><span className={`status-label ${targetStateClass[target.state] ?? ''}`}>{t('agentTarget' + cap(target.state))}</span></td>
          <td className="cell-subtitle">{target.reason || '—'}</td>
          <td>{target.cache_hit ? t('agentYes') : t('agentNo')}</td>
          <td className="mono-value">{target.credential_id ? target.credential_fingerprint.slice(0, 24) : '—'}</td>
          <td>{manage && target.credential_id && <>
            <button type="button" className="link-button" disabled={action.busy} onClick={() => setRotateFor(target.machine_id)}>{t('agentRotate')}</button>
            {' '}<button type="button" className="link-button" disabled={action.busy} onClick={() => setRevokeFor(target.machine_id)}>{t('agentRevoke')}</button>
          </>}</td>
        </tr>)}</tbody>
      </table>
      <h3>{t('agentSummary')}</h3>
      <p className="gate-card-summary">
        <span className="status-label enabled">{t('agentSummaryInstalled')}: {summary.installed ?? 0}</span>
        <span className="status-label disabled">{t('agentSummaryFailed')}: {summary.failed ?? 0}</span>
        <span className="status-label">{t('agentSummaryBlocked')}: {summary.blocked ?? 0}</span>
        <span className="status-label">{t('agentSummaryPending')}: {summary.pending ?? 0}</span>
        <span className="status-label">{t('agentSummaryCacheHits')}: {summary.cache_hits ?? 0}</span>
        <span className="status-label">{t('agentSummaryBytes')}: {summary.bytes ?? 0}</span>
      </p>
      <h3>{t('agentCredentialEvents')}</h3>
      {detail.data.events.length === 0 && <p className="empty-hint">{t('agentNoCredentialEvents')}</p>}
      {detail.data.events.length > 0 && <table className="data-table">
        <thead><tr>
          <th scope="col">{t('agentMachine')}</th><th scope="col">{t('agentEventAction')}</th>
          <th scope="col">{t('agentEventCredential')}</th><th scope="col">{t('agentCreatedAt')}</th>
        </tr></thead>
        <tbody>{detail.data.events.map((event) => <tr key={event.id}>
          <td className="mono-value">{event.machine_id.slice(0, 12)}</td>
          <td><span className={`status-label ${event.action === 'revoke' ? 'disabled' : event.action === 'issue' ? 'enabled' : ''}`}>{t('agentEvent' + cap(event.action))}</span></td>
          <td className="mono-value">{event.credential_id.slice(0, 12)}</td>
          <td className="cell-subtitle">{new Date(event.created_at).toLocaleString()}</td>
        </tr>)}</tbody>
      </table>}
      <div className="form-actions">
        {manage && detail.data.plan.state === 'running' && <button type="button" className="secondary-button" disabled={action.busy} onClick={() => void run(() => abortAgentPlan(projectId, plan.id, session.csrf_token, crypto.randomUUID()))}>{t('agentAbort')}</button>}
      </div>
    </>}
    {rotateFor && <CredentialDialog projectId={projectId} machineId={rotateFor} mode="rotate" onClose={() => setRotateFor('')} onDone={async () => { setRotateFor(''); await detail.refetch() }} />}
    {revokeFor && <CredentialDialog projectId={projectId} machineId={revokeFor} mode="revoke" onClose={() => setRevokeFor('')} onDone={async () => { setRevokeFor(''); await detail.refetch() }} />}
  </section>
}

function AgentContent() {
  const project = useProject()
  const { t } = useTranslation(), session = useSession()
  const [selected, setSelected] = useState('')
  const manage = canManageProject(project, session.user.is_admin) && session.permissions.includes('model:manage')
  const plans = useQuery({ queryKey: ['private', session.user.id, 'project', project.id, 'agent-plans'], queryFn: ({ signal }) => listAgentPlans(project.id, signal) })
  useExpireOnUnauthorized(plans.error)
  useEffect(() => { document.title = `${t('agentTitle')} · ${project.name}` }, [t, project.name])
  const plan = (plans.data?.items ?? []).find((entry) => entry.id === selected) ?? null
  if (!session.permissions.includes('model:read')) return <PermissionDenied />
  return <>
    <div className="page-heading"><div><p className="section-eyebrow">{project.name}</p><h1>{t('agentTitle')}</h1><p>{t('agentIntro')}</p></div></div>
    {manage && <PlanForm projectId={project.id} manage={manage} onCreated={(created) => { setSelected(created.id); void plans.refetch() }} />}
    <PlansList projectId={project.id} selected={selected} onSelect={setSelected} />
    {plan && <PlanDetail projectId={project.id} plan={plan} manage={manage} onChanged={() => void plans.refetch()} />}
  </>
}

export function AgentPage({ projectId }: { projectId: string }) {
  return <ProjectWorkspace projectId={projectId}><AgentContent /></ProjectWorkspace>
}
