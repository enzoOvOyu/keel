import { useEffect, useRef, useState, type FormEvent } from 'react'
import { useNavigate } from '@tanstack/react-router'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { ApprovedScripts } from './SSHScripts'
import { AgentNetworkInventoryDialog } from './AgentNetworkInventory'
import { ActionDialog, ActionError, Pagination } from '../../components/Management'
import { useProject } from '../../components/ProjectWorkspace'
import { ApiError } from '../../lib/api'
import { createAgentExecution, listAgentTargets, type AgentCreateInput, type AgentTarget } from '../../lib/execution'
import { useAction } from '../../lib/management'
import { useExpireOnUnauthorized, useSession } from '../../lib/session'

function connected(target: AgentTarget, now: number) {
  return target.connected && Date.parse(target.credential_expires_at) > now && !!target.last_seen_at && Date.parse(target.last_seen_at) + 30_000 > now
}

function Launch({ targets, now, onClose, onDenied, onRefresh }: { targets: AgentTarget[]; now: number; onClose: () => void; onDenied: (error: ApiError) => void; onRefresh: () => void }) {
  const { t } = useTranslation(), project = useProject(), session = useSession(), action = useAction(), navigate = useNavigate(), client = useQueryClient()
  const [selected, setSelected] = useState<string[]>([]), [operation, setOperation] = useState<AgentCreateInput['operation']>('host.identity')
  const [timeout, setTimeout] = useState('5000'), [wait, setWait] = useState('1000'), [validation, setValidation] = useState('')
  const attempt = useRef({ body: '', key: '' }), mounted = useRef(true)
  useEffect(() => { mounted.current = true; return () => { mounted.current = false } }, [])
  const eligible = targets.filter(target => connected(target, now))
  useEffect(() => { setSelected(previous => previous.filter(id => targets.some(target => target.machine_id === id && connected(target, now)))) }, [targets, now])
  useEffect(() => { if (action.error && [401, 403, 404].includes(action.error.status)) onDenied(action.error) }, [action.error])
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const timeoutMS = Number(timeout), waitMS = operation === 'host.wait' ? Number(wait) : 0
    if (!selected.length || selected.length > 32 || !Number.isSafeInteger(timeoutMS) || timeoutMS < 100 || timeoutMS > 30000
      || !Number.isSafeInteger(waitMS) || waitMS < 0 || waitMS >= timeoutMS || selected.some(id => !targets.some(target => target.machine_id === id && connected(target, Date.now())))) { setValidation(t('agentLaunchInvalid')); return }
    setValidation('')
    const body: AgentCreateInput = { machine_ids: [...selected].sort(), operation, timeout_ms: timeoutMS, wait_ms: waitMS }
    const serialized = JSON.stringify(body)
    if (attempt.current.body !== serialized) attempt.current = { body: serialized, key: crypto.randomUUID() }
    const result = await action.run(() => createAgentExecution(project.id, body, session.csrf_token, attempt.current.key))
    if (result.ok && mounted.current) {
      await client.invalidateQueries({ queryKey: ['private', session.user.id, 'project', project.id, 'executions'] })
      if (!mounted.current) return
      await navigate({ to: '/p/$projectId/executions', params: { projectId: project.id }, search: { execution: result.value.id, tab: 'overview' } })
    }
  }
  return <ActionDialog title={t('agentLaunchTitle')} description={t('agentLaunchBoundary')} busy={action.busy} confirm={t('sshLaunchConfirm')} submitDisabled={!selected.length} onSubmit={event => void submit(event)} onClose={onClose}>
    <ActionError error={action.error} />
    {action.error?.code === 'CACHE_INVENTORY_DISABLED' && <p role="status">{t('cacheInventoryDisabled')}</p>}
    {action.error?.code === 'AGENT_DISABLED' && <p className="impact-notice">{t('agentLaunchDisabled')}</p>}
    {action.error && ['AGENT_OFFLINE', 'AGENT_IDENTITY_REQUIRED', 'STATE_CONFLICT'].includes(action.error.code) && <p className="impact-notice">{t('agentUnavailable')} <Button type="button" variant="outline" disabled={action.busy} onClick={onRefresh}>{t('refresh')}</Button></p>}
    {validation && <p className="field-error" role="alert">{validation}</p>}
    <p className="field-hint">{t('agentLaunchCurrentPage')}</p>
    <div className="ui:flex ui:flex-wrap ui:gap-2" role="group" aria-label={t('agentLaunchTargets')}>
      {eligible.map(target => <Button key={target.machine_id} type="button" className="ui:h-auto ui:min-h-9 ui:min-w-0 ui:max-w-full ui:whitespace-normal ui:break-all ui:text-left" variant={selected.includes(target.machine_id) ? 'default' : 'outline'} aria-pressed={selected.includes(target.machine_id)} disabled={action.busy || !selected.includes(target.machine_id) && selected.length >= 32} onClick={() => setSelected(previous => previous.includes(target.machine_id) ? previous.filter(id => id !== target.machine_id) : [...previous, target.machine_id])}>{target.hostname} · {target.serial_number}</Button>)}
    </div>
    <p role="status">{t('sshLaunchSelected', { count: selected.length })}</p>
    <div className="field"><Label htmlFor="agent-launch-operation">{t('sshLaunchOperation')}</Label><NativeSelect id="agent-launch-operation" value={operation} disabled={action.busy} onChange={event => setOperation(event.target.value === 'host.wait' ? 'host.wait' : event.target.value === 'host.cache.inventory' ? 'host.cache.inventory' : 'host.identity')}><NativeSelectOption value="host.identity">{t('sshLaunchIdentity')}</NativeSelectOption><NativeSelectOption value="host.wait">{t('sshLaunchWait')}</NativeSelectOption><NativeSelectOption value="host.cache.inventory">{t('cacheInventoryOperation')}</NativeSelectOption></NativeSelect></div>
    <div className="field"><Label htmlFor="agent-launch-timeout">{t('sshLaunchTimeout')}</Label><Input id="agent-launch-timeout" type="number" inputMode="numeric" min={100} max={30000} step={1} required value={timeout} disabled={action.busy} onChange={event => setTimeout(event.target.value)} /></div>
    {operation === 'host.cache.inventory' && <p className="field-hint">{t('cacheInventoryBoundary')}</p>}
    {operation === 'host.wait' && <div className="field"><Label htmlFor="agent-launch-wait">{t('sshLaunchWaitDuration')}</Label><Input id="agent-launch-wait" type="number" inputMode="numeric" min={0} max={Math.max(0, Number(timeout) - 1)} step={1} required value={wait} disabled={action.busy} onChange={event => setWait(event.target.value)} /></div>}
    <p className="impact-notice">{t('agentLaunchAdmission')}</p>
  </ActionDialog>
}

export function AgentExecutions() {
  const { t } = useTranslation(), project = useProject(), session = useSession(), client = useQueryClient()
  const [page, setPage] = useState(1), [now, setNow] = useState(Date.now()), [open, setOpen] = useState(false), [denied, setDenied] = useState<ApiError | null>(null)
  const [networkTarget, setNetworkTarget] = useState<AgentTarget | null>(null)
  useEffect(() => { setNetworkTarget(null) }, [project.id, session.user.id])
  const canLaunch = !['archived', 'delivered'].includes(project.state) && (session.user.is_admin || ['Admin', 'DE', 'DL', 'SRE'].includes(project.role))
  const records = useQuery({ queryKey: ['private', session.user.id, 'project', project.id, 'agent-targets', page], queryFn: ({ signal }) => listAgentTargets(project.id, page, signal), enabled: !denied, retry: false })
  useExpireOnUnauthorized(records.error)
  useEffect(() => {
    if (denied || !(records.error instanceof ApiError) || ![401, 403, 404].includes(records.error.status)) return
    setDenied(records.error)
    void client.invalidateQueries({ queryKey: ['private', session.user.id, 'project', project.id] })
  }, [records.error, denied])
  useEffect(() => { if (!canLaunch || denied || records.isError || !records.data?.enabled) setOpen(false) }, [canLaunch, denied, records.isError, records.data?.enabled])
  // Expire connection/credential badges locally as well as on refresh. A cached
  // online response never remains eligible indefinitely in an idle dialog.
  useEffect(() => {
    setNow(Date.now())
  }, [records.dataUpdatedAt])
  useEffect(() => {
    const next = Math.min(...(records.data?.items ?? []).flatMap(target => [Date.parse(target.credential_expires_at), target.last_seen_at ? Date.parse(target.last_seen_at) + 30_000 : 0]).filter(expiry => expiry > now))
    if (!Number.isFinite(next)) return
    const timer = window.setTimeout(() => setNow(Date.now()), Math.max(1, Math.min(next - Date.now() + 1, 2_147_483_647)))
    return () => window.clearTimeout(timer)
  }, [records.data, now])
  const eligible = !records.isError && !denied && records.data?.enabled && records.data.items.some(target => connected(target, now))
  function onDenied(error: ApiError) { setDenied(error); void client.invalidateQueries({ queryKey: ['private', session.user.id, 'project', project.id] }) }
  function refresh() { setNow(Date.now()); void records.refetch() }
  return <><section className="management-panel ui:mt-6" aria-labelledby="agent-executions-heading" aria-busy={records.isFetching}>
    <div className="panel-heading"><div><h2 id="agent-executions-heading">{t('agentLaunchTitle')}</h2><p>{t('agentLaunchIntro')}</p></div><div className="table-actions"><Button type="button" variant="outline" disabled={records.isFetching || !!denied} onClick={refresh}>{t('refresh')}</Button>{canLaunch && <Button type="button" disabled={!eligible || records.isFetching} onClick={() => setOpen(true)}>{t('agentLaunchOpen')}</Button>}</div></div>
    <ActionError error={denied ?? records.error} />
    {records.isPending && <p className="table-loading" role="status">{t('loadingRecords')}</p>}
    {!records.isError && !denied && records.data && <>
      {!records.data.enabled ? <p className="impact-notice">{t('agentLaunchDisabled')}</p> : <>
        {records.data.items.length ? <Table aria-label={t('agentLaunchTargets')}><TableHeader><TableRow><TableHead>{t('bomHostname')}</TableHead><TableHead>{t('bomSerial')}</TableHead><TableHead>{t('status')}</TableHead><TableHead>{t('agentLastSeen')}</TableHead><TableHead>{t('agentCredential')}</TableHead><TableHead>{t('agentNetworkTitle')}</TableHead></TableRow></TableHeader><TableBody>{records.data.items.map(target => <TableRow key={target.machine_id} data-agent-target={target.machine_id}><TableCell>{target.hostname}<span className="cell-subtitle">v{target.asset_version}</span></TableCell><TableCell className="mono-value">{target.serial_number}</TableCell><TableCell><span className={`status-label ${connected(target, now) ? 'enabled' : ''}`}>{t(Date.parse(target.credential_expires_at) <= now ? 'agentConnectionExpired' : connected(target, now) ? 'agentConnectionOnline' : target.connected ? 'agentConnectionStale' : 'agentConnectionOffline')}</span></TableCell><TableCell>{target.last_seen_at ? new Date(target.last_seen_at).toLocaleString() : '—'}</TableCell><TableCell className="mono-value">{target.credential_id}<span className="cell-subtitle">{t('sshTrustExpiry')}: {new Date(target.credential_expires_at).toLocaleString()}</span></TableCell><TableCell><Button type="button" variant="outline" onClick={() => setNetworkTarget(target)}>{t('agentNetworkOpen')}</Button></TableCell></TableRow>)}</TableBody></Table> : <p className="table-loading">{t('agentLaunchEmpty')}</p>}
        {!!records.data.items.length && !eligible && <p className="impact-notice">{t('agentLaunchNoConnected')}</p>}
        <Pagination offset={(page - 1) * records.data.page_size} limit={records.data.page_size} total={records.data.total} busy={records.isFetching || open} onChange={offset => setPage(offset / records.data!.page_size + 1)} />
      </>}
    </>}
    <p className="impact-notice">{t('agentConnectionBoundary')}</p>
    {open && canLaunch && !denied && !records.isError && records.data?.enabled && <Launch targets={records.data.items} now={now} onClose={() => setOpen(false)} onDenied={onDenied} onRefresh={refresh} />}
    {networkTarget && !denied && <AgentNetworkInventoryDialog key={`${project.id}:${session.user.id}:${networkTarget.machine_id}`} target={networkTarget} onClose={() => setNetworkTarget(null)} onDenied={onDenied} />}
  </section><ApprovedScripts transport="agent" assets={!records.isError && !denied && !records.isFetching && records.data?.enabled ? records.data.items.filter(target => connected(target, now)) : []} /></>
}
