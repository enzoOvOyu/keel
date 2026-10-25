import { Label } from '@/components/ui/label'
import { Input } from '@/components/ui/input'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Button } from '@/components/ui/button'
import { Table, TableHeader, TableRow, TableHead, TableBody, TableCell } from '@/components/ui/table'
import { CatalogInputs } from './CatalogInputs'
import { validateCatalog, type CatalogValues } from '../../lib/catalog'
import { ExecutionWorkspace } from './ExecutionWorkspace'
import { useEffect, useRef, useState, type FormEvent } from 'react'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { ProjectWorkspace, useProject } from '../../components/ProjectWorkspace'
import { ActionDialog, ActionError, Pagination, PermissionDenied, SuccessNotice } from '../../components/Management'
import { useAction } from '../../lib/management'
import { createExecution, listExecutions, listFlows, listSimTargets, registerSimTargets } from '../../lib/execution'
import { useExpireOnUnauthorized, useSession } from '../../lib/session'

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
    <div className="field"><Label htmlFor="target-prefix">{t('targetPrefix')}</Label><Input className="ui:h-11" id="target-prefix" value={prefix} onChange={(event) => setPrefix(event.target.value)} maxLength={32} required disabled={action.busy} /><span className="field-hint">{t('targetPrefixPlaceholder')}</span></div>
    <div className="field"><Label htmlFor="target-count">{t('targetCount')}</Label><Input className="ui:h-11" id="target-count" type="number" min={1} max={2000} value={count} onChange={(event) => setCount(event.target.value)} required disabled={action.busy} /><span className="field-hint">{t('targetCountHint')}</span></div>
    <div className="field"><Label htmlFor="target-seed">{t('targetSeed')}</Label><Input className="ui:h-11" id="target-seed" type="number" min={0} value={seed} onChange={(event) => setSeed(event.target.value)} required disabled={action.busy} /><span className="field-hint">{t('targetSeedHint')}</span></div>
    <p className="impact-notice">{t('simulationNoticeDetail')}</p>
  </ActionDialog>
}
function CreateExecutionDialog({ projectId, onClose, onDone }: { projectId: string; onClose: () => void; onDone: () => Promise<void> }) {
  const { t } = useTranslation(), session = useSession(), action = useAction(), keyFor = useIdempotencyKey()
  const [flowVersionId, setFlowVersionId] = useState(''), [parameters, setParameters] = useState<CatalogValues>({}), [catalogReady, setCatalogReady] = useState(false)
  const [total, setTotal] = useState('50'), [concurrency, setConcurrency] = useState('16'), [breaker, setBreaker] = useState('5'), [validation, setValidation] = useState('')
  const flows = useQuery({ queryKey: ['private', session.user.id, 'project', projectId, 'flows'], queryFn: ({ signal }) => listFlows(projectId, 1, signal) })
  useExpireOnUnauthorized(flows.error)
  const selectedFlow = flows.data?.items.find(flow => flow.id === flowVersionId)
  useEffect(() => {
    setCatalogReady(false)
    const defaults: CatalogValues = {}
    for (const [key, spec] of Object.entries(selectedFlow?.definition.inputs ?? {})) {
      if (spec.default !== undefined) defaults[key] = spec.default
    }
    setParameters(defaults)
  }, [selectedFlow])
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const count = Number(total), lanes = Number(concurrency), percent = Number(breaker)
    if (!flowVersionId) { setValidation(t('noFlows')); return }
    if (!catalogReady) { setValidation(t('catalogMissing')); return }
    if (!Number.isSafeInteger(count) || count < 1 || count > 2000) { setValidation(t('targetCountHint')); return }
    if (!Number.isSafeInteger(lanes) || lanes < 1 || lanes > 64) { setValidation(t('invalidFields')); return }
    if (!Number.isSafeInteger(percent) || percent < 0 || percent > 100) { setValidation(t('invalidFields')); return }
    setValidation('')
    const admission = await action.run(() => validateCatalog(projectId, parameters, session.csrf_token))
    if (!admission.ok || !admission.value.valid) { setValidation(t('catalogMissing')); return }
    const targetIds = await collectTargets(projectId, count)
    if (!targetIds.length) { setValidation(t('noFlows')); return }
    const body = { mode: 'sim' as const, flow_version_id: flowVersionId, target_ids: targetIds, parameters, concurrency: lanes, circuit_breaker_percent: percent }
    const result = await action.run(() => createExecution(projectId, body, session.csrf_token, keyFor(JSON.stringify(body))))
    if (result.ok) await onDone()
  }
  return <ActionDialog submitDisabled={!catalogReady} title={t('newExecution')} description={t('newExecutionDetail')} busy={action.busy} confirm={t(action.busy ? 'starting' : 'startExecution')} onSubmit={(event) => void submit(event)} onClose={onClose}>
    <ActionError error={action.error} />{validation && <p className="field-error" role="alert">{validation}</p>}
    <div className="field"><Label htmlFor="execution-flow">{t('selectFlow')}</Label><NativeSelect className="ui:h-11" id="execution-flow" value={flowVersionId} onChange={(event) => setFlowVersionId(event.target.value)} required disabled={action.busy}>{!flowVersionId && <NativeSelectOption value="">—</NativeSelectOption>}{flows.data?.items.filter(flow=>flow.definition.api_version==='keel.sim/v1').map((flow) => <NativeSelectOption key={flow.id} value={flow.id}>{flow.name} · v{flow.version}</NativeSelectOption>)}</NativeSelect>{!flows.data?.items.length && <span className="field-hint">{t('noFlows')}</span>}</div>
    {selectedFlow && <CatalogInputs projectId={projectId} flow={selectedFlow} values={parameters} disabled={action.busy} onChange={setParameters} onReady={setCatalogReady} />}
    <div className="field"><Label htmlFor="execution-total">{t('targetCount')}</Label><Input className="ui:h-11" id="execution-total" type="number" min={1} max={2000} value={total} onChange={(event) => setTotal(event.target.value)} required disabled={action.busy} /></div>
    <div className="field"><Label htmlFor="execution-concurrency">{t('concurrency')}</Label><Input className="ui:h-11" id="execution-concurrency" type="number" min={1} max={64} value={concurrency} onChange={(event) => setConcurrency(event.target.value)} required disabled={action.busy} /></div>
    <div className="field"><Label htmlFor="execution-breaker">{t('circuitBreaker')}</Label><Input className="ui:h-11" id="execution-breaker" type="number" min={0} max={100} value={breaker} onChange={(event) => setBreaker(event.target.value)} required disabled={action.busy} /></div>
    <p className="impact-notice">{t('simulationNoticeDetail')}</p>
  </ActionDialog>
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
      <div className="table-actions">{canRegister && <Button variant="outline" className="secondary-button ui:min-h-11" onClick={() => { setSuccess(''); setRegister(true) }}>{t('registerTargets')}</Button>}
        {canCreate && <Button variant="default" className="primary-button ui:min-h-11" onClick={() => { setSuccess(''); setCreate(true) }}>{t('newExecution')}</Button>}</div></div>
    <p className="impact-notice">{t('executionModesNotice')}</p>
    {success && <SuccessNotice>{success}</SuccessNotice>}
    {canRegister && targets.data && <p className="field-hint">{t('targetsRegistered', { total: targets.data.total })}</p>}
    {executionId ? <ExecutionWorkspace key={executionId} projectId={project.id} executionId={executionId} onClose={() => go(page)} /> : <>
      <section className="management-panel" aria-busy={executions.isFetching}>
        <ActionError error={executions.error} />
        {executions.isPending ? <p className="table-loading" role="status">{t('loadingRecords')}</p> : executions.data && <>
          <div className="table-scroll" tabIndex={0} role="region" aria-label={t('executionListTitle')}><Table scrollContainer={false} className="data-table"><TableHeader><TableRow><TableHead className="ui:h-auto ui:p-4 ui:align-top ui:text-muted-foreground" scope="col">{t('executionState')}</TableHead><TableHead className="ui:h-auto ui:p-4 ui:align-top ui:text-muted-foreground" scope="col">Mode</TableHead><TableHead className="ui:h-auto ui:p-4 ui:align-top ui:text-muted-foreground" scope="col">{t('executionTargets')}</TableHead><TableHead className="ui:h-auto ui:p-4 ui:align-top ui:text-muted-foreground" scope="col">{t('executionConcurrency')}</TableHead><TableHead className="ui:h-auto ui:p-4 ui:align-top ui:text-muted-foreground" scope="col">{t('executionStarted')}</TableHead><TableHead className="ui:h-auto ui:p-4 ui:align-top ui:text-muted-foreground" scope="col">{t('executionFinished')}</TableHead><TableHead className="ui:h-auto ui:p-4 ui:align-top ui:text-muted-foreground" scope="col">{t('actions')}</TableHead></TableRow></TableHeader>
            <TableBody>{executions.data.items.map((execution) => <TableRow key={execution.id}><TableCell className="ui:p-4 ui:align-top ui:whitespace-normal">{t('state_' + execution.state)}</TableCell><TableCell className="ui:p-4 ui:align-top ui:whitespace-normal">{execution.mode}</TableCell><TableCell className="ui:p-4 ui:align-top ui:whitespace-normal">{execution.target_count}</TableCell><TableCell className="ui:p-4 ui:align-top ui:whitespace-normal">{execution.concurrency}</TableCell><TableCell className="ui:p-4 ui:align-top ui:whitespace-normal">{format(execution.started_at)}</TableCell><TableCell className="ui:p-4 ui:align-top ui:whitespace-normal">{format(execution.finished_at)}</TableCell><TableCell className="ui:p-4 ui:align-top ui:whitespace-normal"><Button className="ui:min-h-11" variant="ghost" onClick={() => go(page, execution.id)}>{t('viewExecution')}</Button></TableCell></TableRow>)}</TableBody></Table>
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
