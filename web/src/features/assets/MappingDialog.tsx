import { useServerDetailDraft } from './server-detail-drafts'
import { useEffect, useRef, useState, type FormEvent } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import AssetDetailFrame from './AssetDetailFrame'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { ActionError, Pagination } from '../../components/Management'
import { useProject } from '../../components/ProjectWorkspace'
import { ApiError } from '../../lib/api'
import { useAction } from '../../lib/management'
import { mappingAssessment, mappingBase, mappingHistory, previewMapping, publishMapping } from '../../lib/mapping'
import MappingCorrections from './MappingCorrections'
import { useExpireOnUnauthorized, useSession } from '../../lib/session'

export default function MappingDialog({ asset, onClose, initialAssessmentId = '', embedded = false, onBusy }: { asset: { id: string; hostname: string }; onClose: () => void; initialAssessmentId?: string; embedded?: boolean; onBusy?: (busy: boolean) => void }) {
 const { t } = useTranslation(), session = useSession(), project = useProject(), queries = useQueryClient(), action = useAction()
 const [reason, setReason] = useState(''), [validation, setValidation] = useState(''), [page, setPage] = useState(1), [selected, setSelected] = useState(initialAssessmentId), [rail, setRail] = useState(''), [now, setNow] = useState(Date.now())
 useServerDetailDraft('mapping', !!reason.trim())
 const [correctionBusy, setCorrectionBusy] = useState(false)
 const busy = action.busy || correctionBusy
 const attempt = useRef({ body: '', key: '' }), mounted = useRef(true)
 useEffect(() => { mounted.current = true; return () => { mounted.current = false } }, [])
 const key = ['private', session.user.id, 'project', project.id, 'mapping', asset.id]
 const preview = useQuery({ queryKey: [...key, 'preview'], queryFn: ({ signal }) => previewMapping(project.id, asset.id, signal), retry: false, refetchOnWindowFocus: false })
 const history = useQuery({ queryKey: [...key, 'history', page], queryFn: ({ signal }) => mappingHistory(project.id, asset.id, page, signal), retry: false, refetchOnWindowFocus: false })
 const detail = useQuery({ queryKey: [...key, 'assessment', selected], queryFn: ({ signal }) => mappingAssessment(project.id, asset.id, selected, signal), enabled: !!selected, retry: false, refetchOnWindowFocus: false })
 useExpireOnUnauthorized(preview.error); useExpireOnUnauthorized(history.error); useExpireOnUnauthorized(detail.error)
 const errors = [preview.error, history.error, detail.error, action.error], denied = errors.some(error => error instanceof ApiError && [401, 403, 404].includes(error.status))
 const canWrite = !denied && (session.user.is_admin || ['Admin', 'DL'].includes(project.role)) && !['archived', 'delivered'].includes(project.state)
 const candidate = selected ? detail.data?.candidate : preview.data, revision = selected ? detail.data?.revision : undefined
 const expired = !!candidate?.valid_until && Date.parse(candidate.valid_until) <= now
 useEffect(() => {
  const dates = [preview.data?.valid_until, candidate?.valid_until].map(value => value ? Date.parse(value) : NaN).filter(value => Number.isFinite(value) && value > Date.now())
  if (!dates.length) return
  const timer = window.setTimeout(() => setNow(Date.now()), Math.min(2_147_483_647, Math.max(1, Math.min(...dates) - Date.now() + 1)))
  return () => window.clearTimeout(timer)
 }, [preview.data?.valid_until, candidate?.valid_until, now])
 async function submit(event: FormEvent<HTMLFormElement>) {
  event.preventDefault()
  if (!canWrite || !preview.data) return
  const count = Array.from(reason.trim()).length
  if (count < 3 || count > 1000) { setValidation(t('wiringInvalidReason')); return }
  setValidation('')
  const body = { scope_hash: preview.data.scope_hash, reason: reason.trim() }, encoded = JSON.stringify(body)
  if (attempt.current.body !== encoded) attempt.current = { body: encoded, key: crypto.randomUUID() }
  const result = await action.run(async () => {
   try { return await publishMapping(project.id, asset.id, body, session.csrf_token, attempt.current.key) }
   catch (error) { if (error instanceof ApiError && error.status === 409) { attempt.current = { body: '', key: '' }; void preview.refetch() }; throw error }
  })
  if (!mounted.current) return
  if (result.ok) {
   setReason('')
   queries.setQueryData([...key, 'assessment', result.value.assessment_id], result.value); setSelected(result.value.assessment_id); setPage(1); setNow(Date.now())
   void queries.invalidateQueries({ queryKey: [...key, 'history'] }); void preview.refetch(); void queries.invalidateQueries({ queryKey: ['private', session.user.id, 'project', project.id, 'assets'] })
  }
 }
 return <AssetDetailFrame title={`${t('mappingTitle')} · ${asset.hostname}`} description={t('mappingHelp')} busy={busy} embedded={embedded} onBusy={onBusy} onClose={onClose}>
  {errors.map((error, i) => error ? <ActionError key={i} error={error as ApiError} /> : null)}
  {!denied && <>
   <div className="action-row"><Button variant="outline" disabled={preview.isFetching || history.isFetching || detail.isFetching} onClick={() => { setNow(Date.now()); void preview.refetch(); void history.refetch(); if (selected) void detail.refetch() }}>{t('topologyRefresh')}</Button><Button variant="outline" onClick={() => setSelected('')}>{t('mappingCurrentPreview')}</Button></div>
   {(preview.isPending || (selected && detail.isPending)) && <p role="status">{t('loadingRecords')}</p>}
   {candidate && <section aria-live="polite"><h3>{selected ? revision ? `${t('mappingPublished')} · v${revision.version}` : t('mappingConflictAssessment') : t('mappingCurrentPreview')}</h3><p>{t(`precollect_${expired ? 'BLOCKED' : revision?.effective_verdict ?? candidate.verdict}`)}</p>{revision?.invalidation_reason && <code>{revision.invalidation_reason}</code>}<p>{t('mappingCoverage')}: GPU {candidate.coverage.mapped_gpus}/{candidate.coverage.required_gpus} · NIC {candidate.coverage.mapped_nics}/{candidate.coverage.required_nics} · {t('wiringPort')} {candidate.coverage.mapped_ports}/{candidate.coverage.required_ports}</p><code className="cell-subtitle">{candidate.scope_hash}</code>{candidate.correction_id && <p>{t('mappingCorrectionApplied')}: <code>{candidate.correction_id}</code></p>}{candidate.valid_until && <p>{t('wiringValidUntil')}: {new Date(candidate.valid_until).toLocaleString()}</p>}
    {candidate.conflicts.length > 0 && <ul aria-label={t('mappingConflicts')}>{candidate.conflicts.map((conflict, i) => <li key={`${conflict.physical_port_id}:${conflict.code}:${i}`}><code>{conflict.code}</code> · <code>{conflict.physical_port_id || conflict.component_id}</code></li>)}</ul>}
    <Label htmlFor="mapping-rail">Rail</Label><NativeSelect id="mapping-rail" value={rail} onChange={event => setRail(event.target.value)}><NativeSelectOption value="">{t('mappingAllPorts')}</NativeSelectOption>{[...new Set(candidate.bindings.map(binding => binding.plan.rail))].filter(Boolean).sort().map(value => <NativeSelectOption key={value} value={value}>{value}</NativeSelectOption>)}</NativeSelect>
    <Table><TableHeader><TableRow><TableHead>{t('mappingLogicalName')}</TableHead><TableHead>{t('wiringPort')}</TableHead><TableHead>OS / MAC / BDF</TableHead><TableHead>{t('mappingSwitchPort')}</TableHead><TableHead>GPU</TableHead></TableRow></TableHeader><TableBody>{candidate.bindings.filter(binding => !rail || binding.plan.rail === rail).map(binding => <TableRow key={binding.plan.physical_port_id}><TableCell>{binding.logical_name}<span className="cell-subtitle">{binding.plan.role} · Rail {binding.plan.rail || '—'}</span></TableCell><TableCell>{binding.plan.port_index}<code className="cell-subtitle">{binding.plan.physical_port_id}</code><span className="cell-subtitle">SN: {binding.plan.component_serial || t('fabricUnknown')}</span></TableCell><TableCell>{binding.observed.interface}<code className="cell-subtitle">{binding.observed.permanent_mac}</code><code className="cell-subtitle">{binding.observed.bdf}</code></TableCell><TableCell>{binding.observed.switch_serial}/{binding.observed.switch_port}<code className="cell-subtitle">{binding.switch_port_id}</code><code className="cell-subtitle">G2: {binding.confirmation_id}</code></TableCell><TableCell>{binding.gpus.length ? binding.gpus.map(gpu => <p key={gpu.component_id}>{gpu.model_key} · {gpu.affinity}<code className="cell-subtitle">{gpu.uuid}</code></p>) : t('mappingGPUNonCompute')}</TableCell></TableRow>)}</TableBody></Table>
    {!!candidate.excluded_ports?.length && <section><h4>{t('mappingNotApplicable')}</h4>{candidate.excluded_ports.map(item => <p key={item.plan.physical_port_id}><code>{item.plan.physical_port_id}</code> · {item.plan.name || item.plan.port_index} · {item.reason}</p>)}</section>}
    {revision && <div className="action-row"><a href={`/api/v1${mappingBase(project.id, asset.id)}/revisions/${encodeURIComponent(revision.id)}/export?format=json`}>{t('mappingExportJSON')}</a><a href={`/api/v1${mappingBase(project.id, asset.id)}/revisions/${encodeURIComponent(revision.id)}/export?format=csv`}>{t('mappingExportCSV')}</a></div>}
   </section>}
   {canWrite && preview.data && <form onSubmit={event => void submit(event)} className="ui:grid ui:gap-2"><Label htmlFor="mapping-reason">{t('wiringReason')}</Label><Input id="mapping-reason" value={reason} maxLength={1000} disabled={busy} onChange={event => setReason(event.target.value)} />{validation && <p role="alert">{validation}</p>}<Button type="submit" disabled={busy || preview.isFetching || preview.isError}>{t(preview.data.ready ? 'mappingPublish' : 'mappingSaveConflicts')}</Button></form>}
   <MappingCorrections assetId={asset.id} preview={preview.data} canWrite={canWrite && !action.busy} onBusy={setCorrectionBusy} onChanged={() => { void preview.refetch(); void history.refetch(); if (selected) void detail.refetch() }} />
   <section><h3>{t('mappingHistory')}</h3>{history.data && <>{history.data.items.map(item => <p key={item.id}><Button variant="outline" onClick={() => { setSelected(item.id); setRail('') }}>{item.revision_id ? `v${item.version}` : t('mappingConflictAssessment')} · {new Date(item.created_at).toLocaleString()} · {item.verdict}</Button></p>)}{!history.data.items.length && <p>{t('mappingNoHistory')}</p>}<Pagination offset={(page - 1) * history.data.page_size} limit={history.data.page_size} total={history.data.total} busy={history.isFetching} onChange={offset => setPage(Math.floor(offset / history.data!.page_size) + 1)} /></>}</section>
  </>}
  {!embedded && <Button variant="outline" disabled={busy} onClick={onClose}>{t('bmcClose')}</Button>}
 </AssetDetailFrame>
}
