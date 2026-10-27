import { useEffect, useRef, useState, type FormEvent } from 'react'
import { Link } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { ActionError, Pagination } from '../../components/Management'
import { useProject } from '../../components/ProjectWorkspace'
import { ApiError } from '../../lib/api'
import { useAction } from '../../lib/management'
import { useExpireOnUnauthorized, useSession } from '../../lib/session'
import { createBMCCredentialVerification, getBMCCredentialVerification, listBMCCredentialBatches, listBMCCredentialVerifications, previewBMCCredentialVerification, type BMCCredentialVerificationInput } from '../../lib/bmc'

// Explicitly mounted, bounded reads, no polling and no browser credential input.
export function BMCCredentialVerificationPanel({ canWrite, onBusyChange }: { canWrite: boolean; onBusyChange: (busy: boolean) => void }) {
 const { t } = useTranslation(), project = useProject(), session = useSession(), action = useAction()
 const [batchPage, setBatchPage] = useState(1), [batch, setBatch] = useState(''), [page, setPage] = useState(1), [id, setID] = useState(''), [reason, setReason] = useState('')
 const attempt = useRef({ body: '', key: '' }), mounted = useRef(true)
 useEffect(() => { mounted.current = true; return () => { mounted.current = false } }, [])
 useEffect(() => { onBusyChange(action.busy); return () => onBusyChange(false) }, [action.busy, onBusyChange])
 const key = ['private', session.user.id, 'project', project.id, 'bmc', 'credential-batches']
 const options = { retry: false, refetchOnWindowFocus: false } as const
 const batches = useQuery({ queryKey: [...key, batchPage], queryFn: ({ signal }) => listBMCCredentialBatches(project.id, batchPage, signal), ...options })
 const scope = useQuery({ queryKey: [...key, batch, 'scope'], queryFn: ({ signal }) => previewBMCCredentialVerification(project.id, batch, signal), enabled: !!batch, ...options })
 const history = useQuery({ queryKey: [...key, batch, 'history', page], queryFn: ({ signal }) => listBMCCredentialVerifications(project.id, batch, page, signal), enabled: !!batch, ...options })
 const detail = useQuery({ queryKey: [...key, batch, 'verification', id], queryFn: ({ signal }) => getBMCCredentialVerification(project.id, batch, id, signal), enabled: !!batch && !!id, ...options })
 useExpireOnUnauthorized(batches.error); useExpireOnUnauthorized(scope.error); useExpireOnUnauthorized(history.error); useExpireOnUnauthorized(detail.error)
 const errors = [batches.error, scope.error, history.error, detail.error, action.error]
 const denied = errors.some(error => error instanceof ApiError && [401, 403, 404].includes(error.status))
 async function start(event: FormEvent) {
  event.preventDefault(); if (!canWrite || denied || action.busy || scope.isFetching || !scope.data?.ready || reason.trim().length < 3 || !session.csrf_token) return
  const body: BMCCredentialVerificationInput = { expected_credential_version: scope.data.credential_version, expected_scope_hash: scope.data.scope_hash, reason: reason.trim() }
  const serialized = JSON.stringify({ batch, body }); if (serialized !== attempt.current.body) attempt.current = { body: serialized, key: crypto.randomUUID() }
  const result = await action.run(() => createBMCCredentialVerification(project.id, batch, body, session.csrf_token, attempt.current.key))
  if (mounted.current && result.ok) { setID(result.value.id); setPage(1); await history.refetch() }
 }
 return <section className="ui:grid ui:gap-3" aria-label={t('bmcBatchVerification')}>
  <div className="panel-heading"><h3>{t('bmcBatchVerification')}</h3><Button type="button" variant="outline" disabled={batches.isFetching || action.busy || denied} onClick={() => void batches.refetch()}>{t('bmcRefresh')}</Button></div>
  <p className="field-hint">{t('bmcBatchIntro')}</p>{errors.map((error, i) => <ActionError key={i} error={error} />)}
  {denied ? null : <>
   {batches.isPending && <p role="status">{t('loadingRecords')}</p>}
   {batches.data && <><Table><TableHeader><TableRow><TableHead>{t('bmcCredentialVersion')}</TableHead><TableHead>{t('bmcBatchState')}</TableHead><TableHead>{t('bmcBatchExpires')}</TableHead><TableHead>{t('bmcDetails')}</TableHead></TableRow></TableHeader><TableBody>{batches.data.items.map(item => <TableRow key={item.credential_batch_ref}><TableCell><span className="mono-value">{item.credential_batch_ref} · {item.credential_version || '—'}</span></TableCell><TableCell>{t(item.revoked ? 'bmcBatchRevoked' : item.verification_state === 'verified' ? 'bmcBatchVerified' : 'bmcBatchPending')}</TableCell><TableCell>{item.expires_at ? new Date(item.expires_at).toLocaleString() : '—'}</TableCell><TableCell><Button type="button" variant="outline" size="sm" disabled={action.busy} onClick={() => { setBatch(item.credential_batch_ref); setID(''); setPage(1); setReason('') }}>{t('bmcBatchPreview')}</Button></TableCell></TableRow>)}</TableBody></Table>{!batches.data.items.length && <p>{t('bmcBatchEmpty')}</p>}<Pagination offset={(batchPage - 1) * batches.data.page_size} limit={batches.data.page_size} total={batches.data.total} busy={batches.isFetching || action.busy} onChange={offset => setBatchPage(Math.floor(offset / (batches.data?.page_size ?? 20)) + 1)} /></>}
   {batch && <><h4 className="mono-value">{batch}</h4><Button type="button" variant="outline" disabled={scope.isFetching || action.busy} onClick={() => void scope.refetch()}>{t('bmcBatchPreview')}</Button>{scope.isPending && <p role="status">{t('loadingRecords')}</p>}
    {scope.data && <><p>{t('bmcBatchScope', { count: scope.data.target_count })} · <span className="mono-value">{scope.data.credential_version || '—'}</span></p><p className="mono-value">{scope.data.scope_hash}</p>{!scope.data.ready && <p role="status">{t('bmcBatchBlocked')} · <span className="mono-value">{scope.data.code}</span></p>}<Table><TableHeader><TableRow><TableHead>{t('bmcBatchTarget')}</TableHead><TableHead>{t('bmcSource')}</TableHead><TableHead>{t('bmcBatchState')}</TableHead></TableRow></TableHeader><TableBody>{scope.data.targets.map(target => <TableRow key={target.asset_id}><TableCell>{target.hostname} · {target.serial} · v{target.asset_version}</TableCell><TableCell>{target.origin ? t(`bmcOrigin_${target.origin}`) : '—'}</TableCell><TableCell className="mono-value">{target.code}</TableCell></TableRow>)}</TableBody></Table>
     {canWrite && <form className="ui:grid ui:gap-2" onSubmit={event => void start(event)}><Label htmlFor="bmc-batch-reason">{t('bmcBatchReason')}</Label><Input id="bmc-batch-reason" value={reason} required minLength={3} maxLength={1000} disabled={action.busy} onChange={e => setReason(e.target.value)} /><Button type="submit" disabled={action.busy || scope.isFetching || !scope.data.ready || reason.trim().length < 3}>{t('bmcBatchStart')}</Button></form>}
    </>}
    <div className="panel-heading"><h4>{t('bmcBatchHistory')}</h4><Button type="button" variant="outline" disabled={history.isFetching || action.busy} onClick={() => void history.refetch()}>{t('bmcRefresh')}</Button></div>
    {history.data && <><Table><TableHeader><TableRow><TableHead>{t('bmcCollectedAt')}</TableHead><TableHead>{t('bmcCredentialVersion')}</TableHead><TableHead>{t('bmcBatchState')}</TableHead><TableHead>{t('bmcDetails')}</TableHead></TableRow></TableHeader><TableBody>{history.data.items.map(item => <TableRow key={item.id}><TableCell>{new Date(item.created_at).toLocaleString()}</TableCell><TableCell>{item.credential_version} · {item.target_count}</TableCell><TableCell>{t(`bmcBatchOutcome_${item.outcome}`)}</TableCell><TableCell><Button type="button" variant="outline" size="sm" onClick={() => setID(item.id)}>{t('bmcDetails')}</Button></TableCell></TableRow>)}</TableBody></Table><Pagination offset={(page - 1) * history.data.page_size} limit={history.data.page_size} total={history.data.total} busy={history.isFetching || action.busy} onChange={offset => setPage(Math.floor(offset / (history.data?.page_size ?? 20)) + 1)} /></>}
   </>}
   {id && <><Button type="button" variant="outline" disabled={detail.isFetching || action.busy} onClick={() => { void detail.refetch(); void batches.refetch() }}>{t('bmcRefresh')}</Button>{detail.isPending && <p role="status">{t('loadingRecords')}</p>}{detail.data && <><p>{t(`bmcBatchOutcome_${detail.data.outcome}`)} · <span className="mono-value">{detail.data.code}</span></p><Table><TableHeader><TableRow><TableHead>{t('bmcBatchTarget')}</TableHead><TableHead>{t('bmcBatchState')}</TableHead><TableHead>{t('bmcSource')}</TableHead><TableHead>{t('bmcViewExecution')}</TableHead></TableRow></TableHeader><TableBody>{detail.data.targets.map(target => <TableRow key={target.asset_id}><TableCell className="mono-value">{target.machine_id}</TableCell><TableCell>{target.execution_state} · {target.code || '—'}</TableCell><TableCell>{t(`bmcOrigin_${target.origin}`)}</TableCell><TableCell><Link to="/p/$projectId/executions" params={{ projectId: project.id }} search={{ execution: target.execution_id, tab: 'overview' }}>{t('bmcViewExecution')}</Link></TableCell></TableRow>)}</TableBody></Table></>}</>}
  </>}
 </section>
}
