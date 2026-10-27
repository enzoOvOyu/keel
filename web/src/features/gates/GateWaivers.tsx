import { useEffect, useRef, useState, type FormEvent } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { ActionDialog, ActionError, Pagination } from '../../components/Management'
import { useProject } from '../../components/ProjectWorkspace'
import { ApiError } from '../../lib/api'
import { approveGateWaiver, gateWaiverScope, listGateWaivers, revokeGateWaiver, type GateWaiver } from '../../lib/gates'
import { useAction } from '../../lib/management'
import { useExpireOnUnauthorized, useSession } from '../../lib/session'
import { useMonitorVisible } from '../monitor/visibility'

export function GateWaivers({ assetId, currentWaiverId, onBusy }: { assetId: string; currentWaiverId: string; onBusy: (busy: boolean) => void }) {
 const { t } = useTranslation(), project = useProject(), session = useSession(), visible = useMonitorVisible(), client = useQueryClient(), action = useAction()
 const [page, setPage] = useState(1), [reason, setReason] = useState(''), [remediation, setRemediation] = useState(''), [deadline, setDeadline] = useState(''), [revoke, setRevoke] = useState<GateWaiver | null>(null), [revokeReason, setRevokeReason] = useState(''), [approving, setApproving] = useState('')
 useEffect(() => { onBusy(action.busy); return () => onBusy(false) }, [action.busy, onBusy])
 const attempt = useRef({ body: '', key: '' })
 const root = ['private', session.user.id, 'project', project.id] as const
 const scope = useQuery({ queryKey: [...root, 'waiver-scope', assetId], queryFn: ({ signal }) => gateWaiverScope(project.id, assetId, signal), enabled: visible, retry: false, refetchOnWindowFocus: false })
 const history = useQuery({ queryKey: [...root, 'waivers', assetId, page], queryFn: ({ signal }) => listGateWaivers(project.id, assetId, page, signal), enabled: visible, retry: false, refetchOnWindowFocus: false })
 useExpireOnUnauthorized(scope.error); useExpireOnUnauthorized(history.error)
 const denied = [scope.error, history.error, action.error].some(error => error instanceof ApiError && [401, 403, 404].includes(error.status))
 const canWrite = session.user.is_admin || ['Admin', 'DL'].includes(project.role)
 const refresh = () => { void client.invalidateQueries({ queryKey: root }); void client.invalidateQueries({ queryKey: ['private', session.user.id, 'project', project.id, 'gates'] }) }
 const validInput = Array.from(reason.trim()).length >= 3 && Array.from(remediation.trim()).length >= 3 && Number.isFinite(Date.parse(deadline)) && Date.parse(deadline) > Date.now() && !!scope.data?.valid_until && Date.parse(deadline) <= Date.parse(scope.data.valid_until)
 async function approve(event: FormEvent<HTMLFormElement>) {
  event.preventDefault(); if (!scope.data?.can_approve || !canWrite || !validInput || !approving || scope.data.source_hash !== approving || action.busy) return
  const body = { source_hash: approving, reason: reason.trim(), remediation_plan: remediation.trim(), valid_until: new Date(deadline).toISOString() }, text = JSON.stringify(body)
  if (attempt.current.body !== text) attempt.current = { body: text, key: crypto.randomUUID() }
  const result = await action.run(() => approveGateWaiver(project.id, assetId, body, session.csrf_token, attempt.current.key))
  if (result.ok) { setReason(''); setRemediation(''); setDeadline(''); setApproving(''); attempt.current = { body: '', key: '' }; refresh() }
  else { setApproving(''); void scope.refetch() }
 }
 async function submitRevoke(event: FormEvent<HTMLFormElement>) {
  event.preventDefault(); if (!revoke || !canWrite || Array.from(revokeReason.trim()).length < 3 || action.busy) return
  const result = await action.run(() => revokeGateWaiver(project.id, assetId, revoke.id, revokeReason.trim(), session.csrf_token))
  if (result.ok) { setRevoke(null); setRevokeReason(''); refresh() }
 }
 return <section className="ui:grid ui:gap-3"><h2>{t('gateWaiverTitle')}</h2><p>{t('gateWaiverHelp')}</p><ActionError error={scope.error} /><ActionError error={history.error} /><ActionError error={action.error} />
  {!denied && <><Button variant="outline" disabled={scope.isFetching || history.isFetching || action.busy} onClick={() => { void scope.refetch(); void history.refetch() }}>{t('topologyRefresh')}</Button>
   {scope.data && <><code className="ui:break-all">{scope.data.evidence_id || '—'}</code><p>{t('wiringValidUntil')}: {scope.data.valid_until ? new Date(scope.data.valid_until).toLocaleString() : '—'}</p>{scope.data.blockers.map(blocker => <code key={blocker} className="ui:break-all">{blocker}</code>)}<ul>{scope.data.findings.map((finding, i) => <li key={i}>{finding.verdict} · {finding.code} · {finding.field} · {finding.target}</li>)}</ul>
    {canWrite && scope.data.can_approve && <div className="ui:grid ui:gap-3"><Label htmlFor="waiver-reason">{t('wiringReason')}</Label><Textarea id="waiver-reason" value={reason} maxLength={1000} disabled={action.busy} onChange={e => setReason(e.target.value)} /><Label htmlFor="waiver-remediation">{t('gateWaiverRemediation')}</Label><Textarea id="waiver-remediation" value={remediation} maxLength={4000} disabled={action.busy} onChange={e => setRemediation(e.target.value)} /><Label htmlFor="waiver-deadline">{t('gateWaiverDeadline')}</Label><Input id="waiver-deadline" type="datetime-local" value={deadline} disabled={action.busy} onChange={e => setDeadline(e.target.value)} /><Button disabled={action.busy || !validInput} onClick={() => setApproving(scope.data!.source_hash)}>{t('gateWaiverApprove')}</Button></div>}
   </>}
   {history.data && <><h3>{t('gateWaiverHistory')}</h3>{history.data.items.map(item => <article key={item.id} className="ui:rounded-lg ui:border ui:p-3"><code className="ui:break-all">{item.id}</code><p>{item.revoked_at ? t('gateWaiverRevoked') : item.id === currentWaiverId && Date.parse(item.valid_until) > Date.now() ? 'WAIVED' : t('gateWaiverHistorical')}</p><p>{item.reason}</p><p>{t('gateWaiverRemediation')}: {item.remediation_plan}</p><p>{t('gateWaiverApprover')}: {item.approved_by} · {new Date(item.approved_at).toLocaleString()}</p><p>{t('wiringValidUntil')}: {new Date(item.valid_until).toLocaleString()}</p>{canWrite && !item.revoked_at && <Button variant="outline" disabled={action.busy} onClick={() => { setRevoke(item); setRevokeReason('') }}>{t('gateWaiverRevoke')}</Button>}</article>)}<Pagination offset={(page - 1) * 10} limit={10} total={history.data.total} busy={history.isFetching || action.busy} onChange={offset => setPage(Math.floor(offset / 10) + 1)} /></>}
   {approving && <ActionDialog title={t('gateWaiverApprove')} description={t('gateWaiverConfirmHelp')} busy={action.busy} confirm={t('gateWaiverApprove')} submitDisabled={!validInput || !scope.data?.can_approve || scope.data.source_hash !== approving} onSubmit={event => void approve(event)} onClose={() => setApproving('')}><p>{reason}</p><p>{remediation}</p><p>{deadline}</p><code className="ui:break-all">{approving}</code></ActionDialog>}
   {revoke && <ActionDialog title={t('gateWaiverRevoke')} description={t('gateWaiverRevokeHelp')} busy={action.busy} confirm={t('gateWaiverRevoke')} danger submitDisabled={Array.from(revokeReason.trim()).length < 3} onSubmit={event => void submitRevoke(event)} onClose={() => setRevoke(null)}><ActionError error={action.error} /><Label htmlFor="waiver-revoke-reason">{t('wiringReason')}</Label><Textarea id="waiver-revoke-reason" value={revokeReason} maxLength={1000} disabled={action.busy} onChange={e => setRevokeReason(e.target.value)} /></ActionDialog>}
  </>}
 </section>
}
