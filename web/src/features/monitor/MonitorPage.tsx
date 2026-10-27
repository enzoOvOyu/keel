import { useRef, useState, type FormEvent } from 'react'
import { Link } from '@tanstack/react-router'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { ActionError, Pagination } from '../../components/Management'
import { ProjectWorkspace, useProject } from '../../components/ProjectWorkspace'
import { ApiError } from '../../lib/api'
import { useAction } from '../../lib/management'
import { monitorAlerts, monitorPolicies, saveMonitorPolicy, suppressMonitorAlert, type MonitorAlert, type MonitorPolicyRevision } from '../../lib/monitor'
import { useExpireOnUnauthorized, useSession } from '../../lib/session'
import { useMonitorVisible } from './visibility'

export function MonitorPage({ projectId }: { projectId: string }) { return <ProjectWorkspace projectId={projectId}><MonitorContent key={projectId} /></ProjectWorkspace> }
function MonitorContent() {
 const { t } = useTranslation()
 const [tab, setTab] = useState('alerts')
 return <section><h1>{t('monitorTitle')}</h1><p>{t('monitorSourceHelp')}</p><div className="action-row"><Button variant={tab === 'alerts' ? 'default' : 'outline'} onClick={() => setTab('alerts')}>{t('monitorAlerts')}</Button><Button variant={tab === 'policies' ? 'default' : 'outline'} onClick={() => setTab('policies')}>{t('monitorPolicies')}</Button></div>{tab === 'alerts' ? <AlertCenter /> : <PolicyEditor />}</section>
}
function AlertCenter() {
 const { t } = useTranslation(), project = useProject(), session = useSession(), visible = useMonitorVisible(), action = useAction()
 const [page, setPage] = useState(1), [filters, setFilters] = useState<Record<string, string>>({ state: '', severity: '', dtype: '', source: '' }), [selected, setSelected] = useState<MonitorAlert | null>(null), [reason, setReason] = useState(''), [validation, setValidation] = useState('')
 const query = useQuery({ queryKey: ['private', session.user.id, 'project', project.id, 'monitor-alerts', filters, page], queryFn: ({ signal }) => monitorAlerts(project.id, page, filters, signal), enabled: visible, retry: false, refetchOnWindowFocus: false, refetchInterval: visible ? 30000 : false })
 useExpireOnUnauthorized(query.error)
 const denied = [query.error, action.error].some(error => error instanceof ApiError && [401, 403, 404].includes(error.status))
 const canSuppress = session.user.is_admin || ['Admin', 'DL', 'NE'].includes(project.role)
 async function suppress(event: FormEvent<HTMLFormElement>) {
  event.preventDefault(); if (!selected || !canSuppress) return
  if (Array.from(reason.trim()).length < 3) { setValidation(t('wiringInvalidReason')); return }
  setValidation(''); const result = await action.run(() => suppressMonitorAlert(project.id, selected.id, { expected_version: selected.version, reason: reason.trim() }, session.csrf_token))
  if (result.ok) { setSelected(result.value); setReason(''); void query.refetch() }
 }
 return <section className="ui:grid ui:gap-3"><h2>{t('monitorAlerts')}</h2><ActionError error={query.error} /><ActionError error={action.error} />
  {!denied && <><div className="action-row">{Object.entries({ state: ['', 'firing', 'suppressed', 'unknown', 'resolved'], severity: ['', 'critical', 'warning', 'info'], dtype: ['', 'D1', 'D2', 'D3', 'D4', 'D5', 'D6', 'B7'], source: ['', 'host', 'switch', 'bmc'] }).map(([name, values]) => <Label key={name}>{t(`monitorFilter_${name}`)}<NativeSelect value={filters[name]} onChange={event => { setPage(1); setFilters(previous => ({ ...previous, [name]: event.target.value })) }}>{values.map(value => <NativeSelectOption key={value} value={value}>{value || t('mappingAllPorts')}</NativeSelectOption>)}</NativeSelect></Label>)}<Button variant="outline" disabled={query.isFetching} onClick={() => void query.refetch()}>{t('topologyRefresh')}</Button></div>
   {query.isPending && <p role="status">{t('loadingRecords')}</p>}
   {query.data && <><p>{t('monitorFiringCritical')}: {query.data.firing_critical}</p><Table><TableHeader><TableRow><TableHead>{t('monitorSeverity')}</TableHead><TableHead>{t('monitorMetric')}</TableHead><TableHead>{t('monitorSource')}</TableHead><TableHead>{t('monitorTime')}</TableHead><TableHead>{t('monitorCount')}</TableHead><TableHead>{t('monitorState')}</TableHead><TableHead>{t('monitorIssue')}</TableHead></TableRow></TableHeader><TableBody>{query.data.items.map(alert => <TableRow key={alert.id}><TableCell>{alert.severity}</TableCell><TableCell><Button variant="link" onClick={() => { setSelected(alert); setReason('') }}>{alert.rule_id}</Button><span className="cell-subtitle">{alert.entity} · {alert.dtype}</span><code className="cell-subtitle">{alert.physical_port_id || alert.machine_id}</code></TableCell><TableCell>{alert.source}/{alert.method}/{alert.origin}</TableCell><TableCell>{new Date(alert.first_at).toLocaleString()}<span className="cell-subtitle">{new Date(alert.last_at).toLocaleString()}</span></TableCell><TableCell>{alert.count}</TableCell><TableCell>{alert.state}</TableCell><TableCell>{alert.issue_id && <Link to="/issues" search={{ project_id: project.id, issue_id: alert.issue_id }}>{t('monitorIssue')}</Link>}</TableCell></TableRow>)}</TableBody></Table>{!query.data.items.length && <p>{t('monitorNoAlerts')}</p>}<Pagination offset={(page - 1) * query.data.page_size} limit={query.data.page_size} total={query.data.total} busy={query.isFetching} onChange={offset => setPage(Math.floor(offset / query.data!.page_size) + 1)} /></>}
   {selected && <section><h3>{selected.rule_id} · {selected.entity}</h3><code>{selected.rule_version}</code><p>{t('monitorSuppressionHelp')}</p><pre className="ui:max-h-72 ui:overflow-auto">{JSON.stringify(selected.context, null, 2)}</pre>{canSuppress && selected.state === 'firing' && selected.issue_id && <form onSubmit={event => void suppress(event)}><Label htmlFor="monitor-suppress-reason">{t('wiringReason')}</Label><Input id="monitor-suppress-reason" value={reason} maxLength={1000} onChange={event => setReason(event.target.value)} disabled={action.busy} />{validation && <p role="alert">{validation}</p>}<Button type="submit" disabled={action.busy}>{t('monitorSuppress')}</Button></form>}</section>}
  </>}
 </section>
}
function PolicyEditor() {
 const { t } = useTranslation(), project = useProject(), session = useSession()
 const [stage, setStage] = useState('M2')
 const query = useQuery({ queryKey: ['private', session.user.id, 'project', project.id, 'monitor-policies'], queryFn: ({ signal }) => monitorPolicies(project.id, signal), retry: false, refetchOnWindowFocus: false })
 useExpireOnUnauthorized(query.error)
 const denied = query.error instanceof ApiError && [401, 403, 404].includes(query.error.status), revision = query.data?.items.find(item => item.policy.stage === stage)
 return <section className="ui:grid ui:gap-3"><h2>{t('monitorPolicies')}</h2><ActionError error={query.error} />{!denied && <>
  <p>{t('monitorPolicyHelp')}</p><p className="field-hint">{t('monitorAgentlessPolicyHelp')}</p><Label htmlFor="monitor-stage">{t('monitorStage')}</Label><NativeSelect id="monitor-stage" value={stage} onChange={event => setStage(event.target.value)}>{['M1', 'M2', 'M3', 'M4', 'maintained'].map(value => <NativeSelectOption key={value} value={value}>{value}</NativeSelectOption>)}</NativeSelect>
  {query.data && <p>{!query.data.enabled && <strong>{t('monitorDisabled')} · </strong>}{t('monitorActiveDevices')}: {query.data.active_devices} · {t('monitorPayloadShare')}: {query.data.payload_share_bytes_per_second} B/s · {t('monitorGlobalBudget')}: {query.data.global_bytes_per_second * 8 / 1e9} Gbit/s</p>}
  {revision && <PolicyForm key={`${stage}:${revision.id}`} revision={revision} devices={query.data!.active_devices} share={query.data!.payload_share_bytes_per_second} onSaved={() => void query.refetch()} />}
 </>}</section>
}
function PolicyForm({ revision, devices, share, onSaved }: { revision: MonitorPolicyRevision; devices: number; share: number; onSaved: () => void }) {
 const { t } = useTranslation(), session = useSession(), project = useProject(), action = useAction(), queries = useQueryClient()
 const [policy, setPolicy] = useState(() => ({ ...revision.policy, intervals: { ...revision.policy.intervals } })), [reason, setReason] = useState(''), [validation, setValidation] = useState('')
 const attempt = useRef({ body: '', key: '' })
 const canWrite = (session.user.is_admin || project.role === 'Admin') && !['archived', 'delivered'].includes(project.state)
 const reserved = devices * Math.min(share, policy.steady_bytes_per_second)
 const valid = reserved <= 100000000 && policy.steady_bytes_per_second >= 1024 && policy.steady_bytes_per_second <= 50000 && policy.window_bytes_per_second >= policy.steady_bytes_per_second && policy.window_bytes_per_second <= 200000 && policy.raw_retention_days >= 7 && policy.hourly_retention_days >= 90 && policy.agentless_seconds >= 300 && policy.agentless_seconds <= 900 && Object.values(policy.intervals).every(value => Number.isInteger(value) && (value === 0 || value >= 10 && value <= 3600))
 async function submit(event: FormEvent<HTMLFormElement>) {
  event.preventDefault(); if (!canWrite || !valid) return
  if (Array.from(reason.trim()).length < 3) { setValidation(t('wiringInvalidReason')); return }
  const body = { expected_version: revision.version, policy, reason: reason.trim() }, encoded = JSON.stringify(body)
  if (attempt.current.body !== encoded) attempt.current = { body: encoded, key: crypto.randomUUID() }
  const result = await action.run(async () => { try { return await saveMonitorPolicy(project.id, body, session.csrf_token, attempt.current.key) } catch (error) { if (error instanceof ApiError && error.status === 409) { attempt.current = { body: '', key: '' }; onSaved() }; throw error } })
  if (result.ok) { void queries.invalidateQueries({ queryKey: ['private', session.user.id, 'project', project.id, 'monitor-policies'] }); onSaved() }
 }
 return <form onSubmit={event => void submit(event)} className="ui:grid ui:gap-3"><p>v{revision.version} · {revision.id}</p><ActionError error={action.error} />
  <Table><TableHeader><TableRow><TableHead>{t('monitorMetric')}</TableHead><TableHead>{t('monitorIntervalSeconds')}</TableHead><TableHead>{t('monitorFreshness')}</TableHead></TableRow></TableHeader><TableBody>{Object.entries(policy.intervals).map(([dtype, interval]) => <TableRow key={dtype}><TableCell>{dtype}</TableCell><TableCell><Input type="number" aria-label={`${dtype} ${t('monitorIntervalSeconds')}`} min={0} max={3600} value={interval} disabled={!canWrite || action.busy || policy.stage === 'maintained'} onChange={event => setPolicy(previous => ({ ...previous, intervals: { ...previous.intervals, [dtype]: Number(event.target.value) } }))} /></TableCell><TableCell>{interval ? `${interval * policy.freshness_multiplier}s` : t('monitor_stopped')}</TableCell></TableRow>)}</TableBody></Table>
  <div className="ui:grid ui:gap-2 ui:sm:grid-cols-2">{(['steady_bytes_per_second', 'window_bytes_per_second', 'agentless_seconds', 'raw_retention_days', 'hourly_retention_days'] as const).map(name => <Label key={name}>{t(`monitorPolicy_${name}`)}<Input type="number" value={policy[name]} disabled={!canWrite || action.busy} onChange={event => setPolicy(previous => ({ ...previous, [name]: Number(event.target.value) }))} /></Label>)}</div>
  <p>{t('monitorBudgetHelp')}: {reserved.toLocaleString()} B/s + {(25000000).toLocaleString()} B/s {t('monitorProtocolReserve')}</p>{!valid && <p role="alert">{t('monitorPolicyInvalid')}</p>}
  {canWrite && <><Label htmlFor="monitor-policy-reason">{t('wiringReason')}</Label><Input id="monitor-policy-reason" value={reason} maxLength={1000} onChange={event => setReason(event.target.value)} disabled={action.busy} />{validation && <p role="alert">{validation}</p>}<Button type="submit" disabled={action.busy || !valid}>{t('monitorSavePolicy')}</Button></>}
 </form>
}
