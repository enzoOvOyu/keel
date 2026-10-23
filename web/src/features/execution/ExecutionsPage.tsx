import { useEffect, useRef, useState, type FormEvent } from 'react'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { ProjectWorkspace, useProject } from '../../components/ProjectWorkspace'
import { ActionDialog, ActionError, Pagination, PermissionDenied, SuccessNotice } from '../../components/Management'
import { useAction } from '../../lib/management'
import { controlExecution, createExecution, getExecution, listAttempts, listExecutions, listFlows, listSimTargets, registerSimTargets } from '../../lib/execution'
import { useExpireOnUnauthorized, useSession } from '../../lib/session'

const terminalStates = ['success', 'failed', 'killed']
const targetPageSize = 100

// One key per distinct payload, so a retry of the same request is idempotent
// while a genuinely new request gets a fresh key.
function useIdempotencyKey() {
  const ref = useRef<{ payload: string; key: string } | null>(null)
  return (payload: string) => {
    if (!ref.current || ref.current.payload !== payload) ref.current = { payload, key: crypto.randomUUID() }
    return ref.current.key
  }
}
async function collectTargets(projectId: string, limit: number) {
  const controller = new AbortController()
  const ids: string[] = []
  for (let page = 1; ids.length < limit; page += 1) {
    const result = await listSimTargets(projectId, page, controller.signal, targetPageSize)
    ids.push(...result.items.map((item) => item.id))
    if (!result.items.length || ids.length >= result.total) break
  }
  return ids.slice(0, limit)
}
function RegisterTargetsDialog({ projectId, onClose, onDone }: {
  projectId: string; onClose: () => void; onDone: (count: number) => Promise<void>
}) {
  const { t } = useTranslation(), session = useSession(), action = useAction(), keyFor = useIdempotencyKey()
  const [prefix, setPrefix] = useState('sim'), [count, setCount] = useState('100'), [seed, setSeed] = useState('42'), [validation, setValidation] = useState('')
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const total = Number(count), seedValue = Number(seed), name = prefix.trim()
    if (!/^[a-z0-9][a-z0-9-]{0,31}$/.test(name)) { setValidation(t('slugInvalid')); return }
    if (!Number.isSafeInteger(total) || total < 1 || total > 2000) { setValidation(t('targetCountHint')); return }
    if (!Number.isSafeInteger(seedValue) || seedValue < 0) { setValidation(t('targetSeedHint')); return }
    setValidation('')
    const body = { prefix: name, count: total, seed: seedValue }
    const result = await action.run(() => registerSimTargets(projectId, body, session.csrf_token, keyFor(JSON.stringify(body))))
    if (result.ok) await onDone(result.value.items.length)
  }
  return <ActionDialog title={t('registerTargets')} description={t('registerTargetsDetail')} busy={action.busy} confirm={t(action.busy ? 'registering' : 'register')} onSubmit={(event) => void submit(event)} onClose={onClose}>
    <ActionError error={action.error} />{validation && <p className="field-error" role="alert">{validation}</p>}
    <div className="field"><label htmlFor="target-prefix">{t('targetPrefix')}</label><input id="target-prefix" value={prefix} onChange={(event) => setPrefix(event.target.value)} maxLength={32} required disabled={action.busy} /><span className="field-hint">{t('targetPrefixPlaceholder')}</span></div>
    <div className="field"><label htmlFor="target-count">{t('targetCount')}</label><input id="target-count" type="number" min={1} max={2000} value={count} onChange={(event) => setCount(event.target.value)} required disabled={action.busy} /><span className="field-hint">{t('targetCountHint')}</span></div>
    <div className="field"><label htmlFor="target-seed">{t('targetSeed')}</label><input id="target-seed" type="number" min={0} value={seed} onChange={(event) => setSeed(event.target.value)} required disabled={action.busy} /><span className="field-hint">{t('targetSeedHint')}</span></div>
    <p className="impact-notice">{t('simulationNoticeDetail')}</p>
  </ActionDialog>
}
function CreateExecutionDialog({ projectId, onClose, onDone }: { projectId: string; onClose: () => void; onDone: () => Promise<void> }) {
  const { t } = useTranslation(), session = useSession(), action = useAction(), keyFor = useIdempotencyKey()
  const [flowVersionId, setFlowVersionId] = useState(''), [profile, setProfile] = useState('smoke')
  const [total, setTotal] = useState('50'), [concurrency, setConcurrency] = useState('16'), [breaker, setBreaker] = useState('5'), [validation, setValidation] = useState('')
  const flows = useQuery({ queryKey: ['private', session.user.id, 'project', projectId, 'flows'], queryFn: ({ signal }) => listFlows(projectId, 1, signal) })
  useExpireOnUnauthorized(flows.error)
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const count = Number(total), lanes = Number(concurrency), percent = Number(breaker)
    if (!flowVersionId) { setValidation(t('noFlows')); return }
    if (!Number.isSafeInteger(count) || count < 1 || count > 2000) { setValidation(t('targetCountHint')); return }
    if (!Number.isSafeInteger(lanes) || lanes < 1 || lanes > 64) { setValidation(t('invalidFields')); return }
    if (!Number.isSafeInteger(percent) || percent < 0 || percent > 100) { setValidation(t('invalidFields')); return }
    setValidation('')
    const targetIds = await collectTargets(projectId, count)
    if (!targetIds.length) { setValidation(t('noFlows')); return }
    const body = { mode: 'sim' as const, flow_version_id: flowVersionId, target_ids: targetIds, parameters: { profile }, concurrency: lanes, circuit_breaker_percent: percent }
    const result = await action.run(() => createExecution(projectId, body, session.csrf_token, keyFor(JSON.stringify(body))))
    if (result.ok) await onDone()
  }
  return <ActionDialog title={t('newExecution')} description={t('newExecutionDetail')} busy={action.busy} confirm={t(action.busy ? 'starting' : 'startExecution')} onSubmit={(event) => void submit(event)} onClose={onClose}>
    <ActionError error={action.error} />{validation && <p className="field-error" role="alert">{validation}</p>}
    <div className="field"><label htmlFor="execution-flow">{t('selectFlow')}</label><select id="execution-flow" value={flowVersionId} onChange={(event) => setFlowVersionId(event.target.value)} required disabled={action.busy}>{!flowVersionId && <option value="">—</option>}{flows.data?.items.filter(flow=>flow.definition.api_version==='keel.sim/v1').map((flow) => <option key={flow.id} value={flow.id}>{flow.name} · v{flow.version}</option>)}</select>{!flows.data?.items.length && <span className="field-hint">{t('noFlows')}</span>}</div>
    <div className="field"><label htmlFor="execution-profile">{t('parameterProfile')}</label><input id="execution-profile" value={profile} onChange={(event) => setProfile(event.target.value)} maxLength={64} required disabled={action.busy} /><span className="field-hint">{t('parameterProfileHint')}</span></div>
    <div className="field"><label htmlFor="execution-total">{t('targetCount')}</label><input id="execution-total" type="number" min={1} max={2000} value={total} onChange={(event) => setTotal(event.target.value)} required disabled={action.busy} /></div>
    <div className="field"><label htmlFor="execution-concurrency">{t('concurrency')}</label><input id="execution-concurrency" type="number" min={1} max={64} value={concurrency} onChange={(event) => setConcurrency(event.target.value)} required disabled={action.busy} /></div>
    <div className="field"><label htmlFor="execution-breaker">{t('circuitBreaker')}</label><input id="execution-breaker" type="number" min={0} max={100} value={breaker} onChange={(event) => setBreaker(event.target.value)} required disabled={action.busy} /></div>
    <p className="impact-notice">{t('simulationNoticeDetail')}</p>
  </ActionDialog>
}
function ExecutionPanel({ projectId, executionId, onClose }: { projectId: string; executionId: string; onClose: () => void }) {
  const { t, i18n } = useTranslation(), session = useSession(), client = useQueryClient()
  const control = useAction()
  const [attemptPage, setAttemptPage] = useState(1)
  const currentProject=useProject()
  const detail = useQuery({ queryKey: ['private', session.user.id, 'project', projectId, 'execution', executionId],
    queryFn: ({ signal }) => getExecution(projectId, executionId, signal),
    refetchInterval: (query) => { const state = query.state.data?.execution.state; return state && !terminalStates.includes(state) ? 3000 : false } })
  const attempts = useQuery({ queryKey: ['private', session.user.id, 'project', projectId, 'execution', executionId, 'attempts', attemptPage],
    queryFn: ({ signal }) => listAttempts(projectId, executionId, attemptPage, signal) })
  useExpireOnUnauthorized(detail.error)
  const format = (value: string | null) => value ? new Intl.DateTimeFormat(i18n.language === 'en' ? 'en-US' : 'zh-CN', { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value)) : t('executionFinishedNone')
  async function runControl(action: 'pause' | 'resume' | 'cancel') {
    const current = detail.data?.execution
    if (!current) return
    const result = await control.run(() => controlExecution(projectId, executionId, { action, expected_version: current.state_version }, session.csrf_token))
    if (result.ok) await client.invalidateQueries({ queryKey: ['private', session.user.id, 'project', projectId, 'execution', executionId] })
  }
  if (detail.isPending) return <section className="management-panel" role="status">{t('loadingRecords')}</section>
  if (detail.isError || !detail.data) return <section className="management-panel"><ActionError error={detail.error ?? new Error('unavailable')} /><button className="secondary-button" onClick={onClose}>{t('backToList')}</button></section>
  const execution = detail.data.execution
  const canControl=execution.mode==='kea'?(session.user.is_admin||['Admin','NDE'].includes(currentProject.role)):session.permissions.includes('execution:control')
  const active = !terminalStates.includes(execution.state)
  return <section className="management-panel" aria-busy={detail.isFetching}>
    <div className="page-heading"><div><p className="section-eyebrow">{t(execution.mode==='kea'?'keaControlNotice':'simulationNotice')}</p><h1>{t('executionDetail')}</h1><p>{t(execution.mode==='kea'?'keaControlDetail':'noDeviceConnection')}</p></div>
      <button className="secondary-button" onClick={onClose}>{t('backToList')}</button></div>
    <dl className="action-target"><dt>{t('executionId')}</dt><dd className="mono-value">{execution.id}</dd><dt>{t('executionState')}</dt><dd>{t('state_' + execution.state)}</dd>
      <dt>{t('executionTargets')}</dt><dd>{detail.data.attempts_total}</dd><dt>{t('executionConcurrency')}</dt><dd>{execution.concurrency}</dd>
      <dt>{t('executionStarted')}</dt><dd>{format(execution.started_at)}</dd><dt>{t('executionFinished')}</dt><dd>{format(execution.finished_at)}</dd></dl>
    <h2>{t('countsTitle')}</h2><div className="table-scroll"><table className="data-table"><tbody>{Object.entries(detail.data.counts).map(([state, value]) => <tr key={state}><th scope="row">{t('attempt_state_' + state)}</th><td>{value}</td></tr>)}</tbody></table></div>
    {canControl && <div className="management-toolbar"><span className="field-hint">{t('controlHint')}</span>
      <button className="secondary-button" disabled={!active || control.busy} onClick={() => void runControl('pause')}>{t('pause')}</button>
      <button className="secondary-button" disabled={!active || control.busy} onClick={() => void runControl('resume')}>{t('resume')}</button>
      <button className="danger-text" disabled={!active || control.busy} onClick={() => void runControl('cancel')}>{t('cancelExecution')}</button></div>}
    <ActionError error={control.error} />{control.error?.status === 409 && <p className="field-hint">{t('controlConflict')}</p>}
    <h2>{t('attemptsTitle')}</h2>
    {attempts.isPending ? <p className="table-loading" role="status">{t('loadingRecords')}</p> : attempts.data && <><div className="table-scroll" tabIndex={0} role="region" aria-label={t('attemptsTitle')}><table className="data-table"><thead><tr><th scope="col">{t('attemptTask')}</th><th scope="col">{t('attemptNo')}</th><th scope="col">{t('attemptState')}</th><th scope="col">{t('attemptTarget')}</th><th scope="col">{t('attemptLease')}</th></tr></thead><tbody>{attempts.data.items.map((attempt) => <tr key={attempt.id}><td><code>{attempt.task_id}</code></td><td>{attempt.attempt_no}</td><td>{t('attempt_state_' + attempt.state)}</td><td className="mono-value">{attempt.target_id}</td><td className="mono-value">{attempt.lease_owner || '—'}</td></tr>)}</tbody></table>{!attempts.data.items.length && <p className="table-loading">{t('noAttempts')}</p>}</div><Pagination offset={(attemptPage - 1) * attempts.data.page_size} limit={attempts.data.page_size} total={attempts.data.total} busy={attempts.isFetching} onChange={(offset) => setAttemptPage(offset / attempts.data.page_size + 1)} /></>}
  </section>
}
function ExecutionsContent() {
  const project = useProject()
  const { t, i18n } = useTranslation(), session = useSession(), client = useQueryClient(), navigate = useNavigate()
  const search = useSearch({ strict: false }) as { page?: number; execution?: string }
  const page = search.page ?? 1, executionId = search.execution
  const [register, setRegister] = useState(false), [create, setCreate] = useState(false), [success, setSuccess] = useState('')
  const allowed = session.permissions.includes('execution:read')
  const canRegister = session.permissions.includes('sim:target:register')
  const canCreate = session.permissions.includes('execution:create')
  const targets = useQuery({ queryKey: ['private', session.user.id, 'project', project.id, 'sim-targets'], queryFn: ({ signal }) => listSimTargets(project.id, 1, signal), enabled: allowed })
  const executions = useQuery({ queryKey: ['private', session.user.id, 'project', project.id, 'executions', page], queryFn: ({ signal }) => listExecutions(project.id, page, signal), enabled: allowed && !executionId, refetchInterval: 5000 })
  useExpireOnUnauthorized(executions.error)
  useEffect(() => { document.title = `${t('executionsTitle')} · ${project.name}` }, [t, project.name])
  function go(nextPage: number, nextExecution?: string) {
    void navigate({ to: '/p/$projectId/executions', params: { projectId: project.id }, search: { page: nextPage, execution: nextExecution } })
  }
  async function done(message: string) {
    setRegister(false); setCreate(false); setSuccess(message)
    await client.invalidateQueries({ queryKey: ['private', session.user.id, 'project', project.id] })
  }
  if (!allowed) return <PermissionDenied />
  const format = (value: string | null) => value ? new Intl.DateTimeFormat(i18n.language === 'en' ? 'en-US' : 'zh-CN', { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value)) : t('executionFinishedNone')
  return <>
    <div className="page-heading"><div><p className="section-eyebrow">{project.name}</p><h1>{t('executionsTitle')}</h1><p>{t('executionsIntro')}</p></div>
      <div className="table-actions">{canRegister && <button className="secondary-button" onClick={() => { setSuccess(''); setRegister(true) }}>{t('registerTargets')}</button>}
        {canCreate && <button className="primary-button" onClick={() => { setSuccess(''); setCreate(true) }}>{t('newExecution')}</button>}</div></div>
    <p className="impact-notice">{t('executionModesNotice')}</p>
    {success && <SuccessNotice>{success}</SuccessNotice>}
    {canRegister && targets.data && <p className="field-hint">{t('targetsRegistered', { total: targets.data.total })}</p>}
    {executionId ? <ExecutionPanel projectId={project.id} executionId={executionId} onClose={() => go(page)} /> : <>
      <section className="management-panel" aria-busy={executions.isFetching}>
        <ActionError error={executions.error} />
        {executions.isPending ? <p className="table-loading" role="status">{t('loadingRecords')}</p> : executions.data && <>
          <div className="table-scroll" tabIndex={0} role="region" aria-label={t('executionListTitle')}><table className="data-table"><thead><tr><th scope="col">{t('executionState')}</th><th scope="col">Mode</th><th scope="col">{t('executionTargets')}</th><th scope="col">{t('executionConcurrency')}</th><th scope="col">{t('executionStarted')}</th><th scope="col">{t('executionFinished')}</th><th scope="col">{t('actions')}</th></tr></thead>
            <tbody>{executions.data.items.map((execution) => <tr key={execution.id}><td>{t('state_' + execution.state)}</td><td>{execution.mode}</td><td>{execution.target_count}</td><td>{execution.concurrency}</td><td>{format(execution.started_at)}</td><td>{format(execution.finished_at)}</td><td><button onClick={() => go(page, execution.id)}>{t('viewExecution')}</button></td></tr>)}</tbody></table>
            {!executions.data.items.length && <p className="table-loading">{t('noExecutions')}</p>}</div>
          <Pagination offset={(page - 1) * executions.data.page_size} limit={executions.data.page_size} total={executions.data.total} busy={executions.isFetching} onChange={(offset) => go(offset / executions.data.page_size + 1)} /></>}
      </section></>}
    {register && <RegisterTargetsDialog projectId={project.id} onClose={() => setRegister(false)} onDone={(count) => done(t('registerDone', { count }))} />}
    {create && <CreateExecutionDialog projectId={project.id} onClose={() => setCreate(false)} onDone={() => done(t('executionCreated'))} />}
  </>
}
export function ExecutionsPage({ projectId }: { projectId: string }) {
  return <ProjectWorkspace projectId={projectId}><ExecutionsContent /></ProjectWorkspace>
}
