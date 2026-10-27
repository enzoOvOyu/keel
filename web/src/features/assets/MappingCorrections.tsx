import { useEffect, useRef, useState, type FormEvent } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { ActionError, Pagination } from '../../components/Management'
import { useProject } from '../../components/ProjectWorkspace'
import { ApiError } from '../../lib/api'
import { useAction } from '../../lib/management'
import { decideMappingCorrection, mappingCorrections, requestMappingCorrection, type MappingCandidate } from '../../lib/mapping'
import { useExpireOnUnauthorized, useSession } from '../../lib/session'

export default function MappingCorrections({ assetId, preview, canWrite, onChanged, onBusy }: { assetId: string; preview?: MappingCandidate; canWrite: boolean; onChanged: () => void; onBusy: (busy: boolean) => void }) {
 const { t } = useTranslation(), session = useSession(), project = useProject(), action = useAction()
 const [page, setPage] = useState(1), [reason, setReason] = useState(''), [names, setNames] = useState<Record<string, string>>({}), [validation, setValidation] = useState('')
 const attempt = useRef({ body: '', key: '' }), mounted = useRef(true)
 useEffect(() => { mounted.current = true; return () => { mounted.current = false; onBusy(false) } }, [onBusy])
 useEffect(() => { onBusy(action.busy) }, [action.busy, onBusy])
 // Typed edits belong to this exact preview; changing evidence clears them.
 useEffect(() => { setNames({}) }, [preview?.scope_hash])
 const history = useQuery({ queryKey: ['private', session.user.id, 'project', project.id, 'mapping', assetId, 'corrections', page], queryFn: ({ signal }) => mappingCorrections(project.id, assetId, page, signal), retry: false, refetchOnWindowFocus: false })
 useExpireOnUnauthorized(history.error)
 const denied = [history.error, action.error].some(error => error instanceof ApiError && [401, 403, 404].includes(error.status))
 async function mutate(id?: string, approve?: boolean) {
  if (!canWrite || denied || !preview) return
  const text = reason.trim()
  if (Array.from(text).length < 3 || Array.from(text).length > 1000) { setValidation(t('wiringInvalidReason')); return }
  const changes = preview.bindings.filter(binding => names[binding.plan.physical_port_id] !== undefined && names[binding.plan.physical_port_id] !== binding.logical_name).map(binding => ({ physical_port_id: binding.plan.physical_port_id, logical_name: names[binding.plan.physical_port_id].trim() }))
  if (!id && !changes.length) { setValidation(t('mappingCorrectionNoChanges')); return }
  setValidation('')
  const proposal = { scope_hash: preview.scope_hash, changes, reason: text }, decision = { approve: !!approve, reason: text }
  const encoded = JSON.stringify({ id, body: id ? decision : proposal })
  if (attempt.current.body !== encoded) attempt.current = { body: encoded, key: crypto.randomUUID() }
  const result = await action.run(async () => {
   try { return await (id ? decideMappingCorrection(project.id, assetId, id, decision, session.csrf_token, attempt.current.key) : requestMappingCorrection(project.id, assetId, proposal, session.csrf_token, attempt.current.key)) }
   catch (error) { if (error instanceof ApiError && error.status === 409) { attempt.current = { body: '', key: '' }; void history.refetch(); onChanged() }; throw error }
  })
  if (!mounted.current) return
  if (result.ok) { attempt.current = { body: '', key: '' }; setNames({}); setReason(''); setPage(1); void history.refetch(); onChanged() }
 }
 return <section className="ui:grid ui:gap-3"><h3>{t('mappingCorrectionTitle')}</h3><p>{t('mappingCorrectionHelp')}</p>
  {history.error && <ActionError error={history.error as ApiError} />}{action.error && <ActionError error={action.error} />}
  {!denied && <>
   {canWrite && preview && <form onSubmit={(event: FormEvent<HTMLFormElement>) => { event.preventDefault(); void mutate() }} className="ui:grid ui:gap-2">
    {preview.bindings.map(binding => <div key={binding.plan.physical_port_id}><Label htmlFor={`mapping-name-${binding.plan.physical_port_id}`}>{binding.plan.port_index} · {binding.plan.physical_port_id}</Label><Input id={`mapping-name-${binding.plan.physical_port_id}`} value={names[binding.plan.physical_port_id] ?? binding.logical_name} maxLength={63} disabled={action.busy} onChange={event => setNames(previous => ({ ...previous, [binding.plan.physical_port_id]: event.target.value }))} /><span className="cell-subtitle">BOM: {binding.plan.name || '—'}</span></div>)}
    <Label htmlFor="mapping-correction-reason">{t('wiringReason')}</Label><Input id="mapping-correction-reason" value={reason} maxLength={1000} disabled={action.busy} onChange={event => setReason(event.target.value)} />{validation && <p role="alert">{validation}</p>}<Button type="submit" disabled={action.busy || !preview.bindings.length}>{t('mappingCorrectionRequest')}</Button>
   </form>}
   {history.isPending && <p role="status">{t('loadingRecords')}</p>}
   {history.data && <>{history.data.items.map(item => <article key={item.id} className="ui:rounded-md ui:border ui:p-3"><p>{new Date(item.created_at).toLocaleString()} · {t(`mappingCorrection_${item.effective_state}`)}</p><code className="cell-subtitle">{item.id}</code><p>{item.reason} · {item.requested_by}</p>{item.changes.map(change => <p key={change.physical_port_id}><code>{change.physical_port_id}</code> → {change.logical_name}</p>)}
    {item.decision && <p>{item.decision.reason} · {item.decision.approved_by} · {new Date(item.decision.created_at).toLocaleString()}</p>}
    {canWrite && !item.decision && <div className="action-row"><Button disabled={action.busy || item.effective_state !== 'pending'} onClick={() => void mutate(item.id, true)}>{t('mappingCorrectionApprove')}</Button><Button variant="outline" disabled={action.busy} onClick={() => void mutate(item.id, false)}>{t('mappingCorrectionReject')}</Button></div>}
   </article>)}<Pagination offset={(page - 1) * history.data.page_size} limit={history.data.page_size} total={history.data.total} busy={history.isFetching || action.busy} onChange={offset => setPage(Math.floor(offset / history.data!.page_size) + 1)} /></>}
  </>}
 </section>
}
