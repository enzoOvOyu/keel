import { useEffect, useRef, useState, type FormEvent } from 'react'
import { Link } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { Input } from '@/components/ui/input'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { ActionError, Pagination } from '../../components/Management'
import { useProject } from '../../components/ProjectWorkspace'
import { useAction } from '../../lib/management'
import { ApiError } from '../../lib/api'
import { useExpireOnUnauthorized, useSession } from '../../lib/session'
import { listBMCPolicies, listBMCPolicyRuns, saveBMCPolicy, type BMCPolicy, type BMCPolicyInput } from '../../lib/bmc'

const initial = (): BMCPolicyInput => ({ level: 'L1', trigger_kind: 'cron', cron_expression: '0 */6 * * *', event_type: '', enabled: true, max_retries: 0, retry_delay_seconds: 60, expected_version: 0, reason: '' })
// Mounted explicitly from the collection dialog. History and policy data never
// poll; immutable run details remain separate from actual collection success.
export function BMCPolicyPanel({ asset, canWrite, onBusyChange }: { asset: string; canWrite: boolean; onBusyChange: (busy: boolean) => void }) {
 const { t } = useTranslation(), project = useProject(), session = useSession(), action = useAction()
 const [editing, setEditing] = useState<BMCPolicy | null>(null), [draft, setDraft] = useState(initial), [formOpen, setFormOpen] = useState(false)
 const [policy, setPolicy] = useState(''), [page, setPage] = useState(1)
 useEffect(() => { onBusyChange(action.busy); return () => onBusyChange(false) }, [action.busy, onBusyChange])
 const attempt = useRef({ body: '', key: '' })
 const key = ['private', session.user.id, 'project', project.id, 'bmc', asset, 'policies']
 const list = useQuery({ queryKey: key, queryFn: ({ signal }) => listBMCPolicies(project.id, asset, signal), retry: false, refetchOnWindowFocus: false })
 const history = useQuery({ queryKey: [...key, policy, 'runs', page], queryFn: ({ signal }) => listBMCPolicyRuns(project.id, asset, policy, page, signal), enabled: Boolean(policy), retry: false, refetchOnWindowFocus: false })
 useExpireOnUnauthorized(list.error); useExpireOnUnauthorized(history.error)
 const denied = [list.error, history.error, action.error].some(error => error instanceof ApiError && [401, 403, 404].includes(error.status))
 const allowedToEdit = (p: BMCPolicy) => canWrite && (p.created_by === session.user.id || session.user.is_admin || ['Admin', 'DL', 'SRE'].includes(project.role))
 function edit(p: BMCPolicy) {
  setEditing(p); setDraft({ level: p.level, trigger_kind: p.trigger_kind, cron_expression: p.cron_expression, event_type: p.event_type, enabled: p.enabled, max_retries: p.max_retries, retry_delay_seconds: p.retry_delay_seconds, expected_version: p.state_version, reason: '' }); setFormOpen(true)
 }
 async function save(event: FormEvent) {
  event.preventDefault(); if (!canWrite || denied || action.busy || !session.csrf_token || draft.reason.trim().length < 3) return
  const body = JSON.stringify({ id: editing?.id ?? '', draft })
  if (body !== attempt.current.body) attempt.current = { body, key: crypto.randomUUID() }
  const result = await action.run(() => saveBMCPolicy(project.id, asset, editing?.id ?? '', draft, session.csrf_token, attempt.current.key))
  if (result.ok) { setFormOpen(false); setEditing(null); await list.refetch(); if (policy === result.value.id) await history.refetch() }
 }
 return <section className="ui:grid ui:gap-3" aria-label={t('bmcPolicies')}>
  <div className="panel-heading"><h3>{t('bmcPolicies')}</h3><Button type="button" variant="outline" disabled={list.isFetching || action.busy || denied} onClick={() => void list.refetch()}>{t('bmcRefresh')}</Button></div>
  <p className="field-hint">{t('bmcPolicyIntro')}</p><ActionError error={list.error} /><ActionError error={action.error} />
  {denied ? null : <>
   {list.data && <><Table><TableHeader><TableRow><TableHead>{t('bmcLevel')}</TableHead><TableHead>{t('bmcPolicyTrigger')}</TableHead><TableHead>{t('bmcPolicyEnabled')}</TableHead><TableHead>{t('bmcPolicyNext')}</TableHead><TableHead>{t('bmcPolicies')}</TableHead></TableRow></TableHeader><TableBody>{list.data.map(p => <TableRow key={p.id}><TableCell>{p.level} · v{p.state_version}</TableCell><TableCell>{p.trigger_kind === 'cron' ? <span className="mono-value">{p.cron_expression} UTC</span> : t(`bmcEvent_${p.event_type}`)}</TableCell><TableCell>{t(p.enabled ? 'bmcPolicyOn' : 'bmcPolicyOff')}</TableCell><TableCell>{p.next_due ? new Date(p.next_due).toLocaleString() : '—'}</TableCell><TableCell><Button type="button" size="sm" variant="outline" disabled={action.busy} onClick={() => { setPolicy(p.id); setPage(1) }}>{t('bmcPolicyRuns')}</Button>{allowedToEdit(p) && <Button type="button" size="sm" variant="ghost" disabled={action.busy} onClick={() => edit(p)}>{t('bmcPolicyEdit')}</Button>}</TableCell></TableRow>)}</TableBody></Table>{!list.data.length && <p>{t('bmcPolicyEmpty')}</p>}</>}
   {canWrite && !formOpen && <Button type="button" variant="outline" disabled={action.busy || (list.data?.length ?? 0) >= 12} onClick={() => { setEditing(null); setDraft(initial()); setFormOpen(true) }}>{t('bmcPolicyAdd')}</Button>}
   {canWrite && formOpen && <form className="ui:grid ui:gap-3" onSubmit={event => void save(event)}>
    <p className="field-hint">{t('bmcPolicyRevision')}</p>
    <Label htmlFor="bmc-policy-level">{t('bmcLevel')}</Label><NativeSelect id="bmc-policy-level" value={draft.level} disabled={action.busy} onChange={e => setDraft({ ...draft, level: e.target.value as BMCPolicyInput['level'] })}>{(['L1', 'L2', 'L3'] as const).map(level => <NativeSelectOption key={level} value={level}>{t(`bmc${level}`)}</NativeSelectOption>)}</NativeSelect>
    <Label htmlFor="bmc-policy-trigger">{t('bmcPolicyTrigger')}</Label><NativeSelect id="bmc-policy-trigger" value={draft.trigger_kind} disabled={action.busy} onChange={e => { const kind = e.target.value as BMCPolicyInput['trigger_kind']; setDraft({ ...draft, trigger_kind: kind, cron_expression: kind === 'cron' ? '0 */6 * * *' : '', event_type: kind === 'event' ? 'bom.committed' : '' }) }}><NativeSelectOption value="cron">{t('bmcPolicyCron')}</NativeSelectOption><NativeSelectOption value="event">{t('bmcPolicyEvent')}</NativeSelectOption></NativeSelect>
    {draft.trigger_kind === 'cron' ? <><Label htmlFor="bmc-policy-cron">{t('bmcPolicyCronUTC')}</Label><Input id="bmc-policy-cron" value={draft.cron_expression} required maxLength={160} disabled={action.busy} onChange={e => setDraft({ ...draft, cron_expression: e.target.value })} /></> : <><Label htmlFor="bmc-policy-event">{t('bmcPolicyEvent')}</Label><NativeSelect id="bmc-policy-event" value={draft.event_type} disabled={action.busy} onChange={e => setDraft({ ...draft, event_type: e.target.value as BMCPolicyInput['event_type'] })}>{(['bom.committed', 'credential.batch.rotated', 'asset.provisioned'] as const).map(kind => <NativeSelectOption key={kind} value={kind}>{t(`bmcEvent_${kind}`)}</NativeSelectOption>)}</NativeSelect></>}
    <Label htmlFor="bmc-policy-enabled">{t('bmcPolicyEnabled')}</Label><NativeSelect id="bmc-policy-enabled" value={String(draft.enabled)} disabled={action.busy} onChange={e => setDraft({ ...draft, enabled: e.target.value === 'true' })}><NativeSelectOption value="true">{t('bmcPolicyOn')}</NativeSelectOption><NativeSelectOption value="false">{t('bmcPolicyOff')}</NativeSelectOption></NativeSelect>
    <Label htmlFor="bmc-policy-retries">{t('bmcPolicyRetries')}</Label><Input id="bmc-policy-retries" type="number" min={0} max={3} step={1} required value={draft.max_retries} disabled={action.busy} onChange={e => setDraft({ ...draft, max_retries: e.target.valueAsNumber })} />
    <Label htmlFor="bmc-policy-delay">{t('bmcPolicyDelay')}</Label><Input id="bmc-policy-delay" type="number" min={30} max={3600} step={1} required value={draft.retry_delay_seconds} disabled={action.busy} onChange={e => setDraft({ ...draft, retry_delay_seconds: e.target.valueAsNumber })} />
    <Label htmlFor="bmc-policy-reason">{t('bmcReason')}</Label><Input id="bmc-policy-reason" minLength={3} maxLength={1000} required value={draft.reason} disabled={action.busy} onChange={e => setDraft({ ...draft, reason: e.target.value })} />
    <div className="ui:flex ui:gap-2"><Button type="submit" disabled={action.busy || draft.reason.trim().length < 3}>{t('bmcPolicySave')}</Button><Button type="button" variant="outline" disabled={action.busy} onClick={() => setFormOpen(false)}>{t('bmcPolicyCancel')}</Button></div>
   </form>}
   <ActionError error={history.error} />
   {policy && history.isPending && <p role="status">{t('loadingRecords')}</p>}
   {policy && history.data && <><h4>{t('bmcPolicyRuns')}</h4><Button type="button" variant="outline" disabled={history.isFetching} onClick={() => void history.refetch()}>{t('bmcRefresh')}</Button><Table><TableHeader><TableRow><TableHead>{t('bmcCollectedAt')}</TableHead><TableHead>{t('bmcPolicyTrigger')}</TableHead><TableHead>{t('bmcPolicyOutcome')}</TableHead><TableHead>{t('bmcPolicyRetries')}</TableHead><TableHead>{t('bmcViewExecution')}</TableHead></TableRow></TableHeader><TableBody>{history.data.items.map(run => <TableRow key={run.id}><TableCell>{new Date(run.created_at).toLocaleString()}</TableCell><TableCell>{t(`bmcTrigger_${run.trigger_kind}`)} · v{run.policy_version}</TableCell><TableCell>{t(`bmcRun_${run.state}`)} · <span className="mono-value">{run.code}</span></TableCell><TableCell>{run.attempt_no}</TableCell><TableCell>{run.execution_id ? <Link to="/p/$projectId/executions" params={{ projectId: project.id }} search={{ execution: run.execution_id, tab: 'overview' }}>{t('bmcViewExecution')}</Link> : '—'}</TableCell></TableRow>)}</TableBody></Table><Pagination offset={(page - 1) * history.data.page_size} limit={history.data.page_size} total={history.data.total} busy={history.isFetching} onChange={offset => setPage(Math.floor(offset / (history.data?.page_size ?? 20)) + 1)} /></>}
  </>}
 </section>
}
