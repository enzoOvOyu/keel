import { useServerDetailDraft } from './server-detail-drafts'
import { useEffect, useRef, useState, type FormEvent } from 'react'
import { Link, useNavigate } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import AssetDetailFrame from './AssetDetailFrame'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Table, TableHeader, TableHead, TableBody, TableRow, TableCell } from '@/components/ui/table'
import { ActionError, Pagination } from '../../components/Management'
import { useProject } from '../../components/ProjectWorkspace'
import { ApiError } from '../../lib/api'
import type { ProjectAsset } from '../../lib/bom'
import { useAction } from '../../lib/management'
import { createPrecollect, currentG1, downloadPrecollect, getPrecollect, listPrecollect, previewPrecollect, type PrecollectInput } from '../../lib/precollect'
import { useExpireOnUnauthorized, useSession } from '../../lib/session'

export default function PrecollectDialog({ asset, onClose, embedded = false, onBusy }: { asset: ProjectAsset; onClose: () => void; embedded?: boolean; onBusy?: (busy: boolean) => void }) {
 const { t } = useTranslation(), session = useSession(), project = useProject(), navigate = useNavigate(), action = useAction()
 const [now, setNow] = useState(Date.now())
 const [page, setPage] = useState(1), [selected, setSelected] = useState(''), [reason, setReason] = useState(''), [validation, setValidation] = useState('')
 useServerDetailDraft('precollect', !!reason.trim())
 const mounted = useRef(true), attempt = useRef({ body: '', key: '' })
 useEffect(() => { mounted.current = true; return () => { mounted.current = false } }, [])
 const key = ['private', session.user.id, 'project', project.id, 'precollect', asset.id]
 const scope = useQuery({ queryKey: [...key, 'scope'], queryFn: ({ signal }) => previewPrecollect(project.id, asset.id, signal), retry: false, refetchOnWindowFocus: false })
 const gate = useQuery({ queryKey: [...key, 'G1'], queryFn: ({ signal }) => currentG1(project.id, asset.id, signal), retry: false, refetchOnWindowFocus: false })
 const list = useQuery({ queryKey: [...key, 'history', page], queryFn: ({ signal }) => listPrecollect(project.id, asset.id, page, signal), retry: false, refetchOnWindowFocus: false })
 const detail = useQuery({ queryKey: [...key, 'detail', selected], queryFn: ({ signal }) => getPrecollect(project.id, asset.id, selected, signal), enabled: !!selected, retry: false, refetchOnWindowFocus: false })
 useExpireOnUnauthorized(scope.error); useExpireOnUnauthorized(gate.error); useExpireOnUnauthorized(list.error); useExpireOnUnauthorized(detail.error)
 const errors = [scope.error, gate.error, list.error, detail.error, action.error]
 const denied = errors.some(e => e instanceof ApiError && [401, 403, 404].includes(e.status))
 const canWrite = !denied && (session.user.is_admin || ['Admin', 'DE', 'DL', 'SRE'].includes(project.role)) && !['archived', 'delivered'].includes(project.state)
 async function start(event: FormEvent) {
  event.preventDefault(); if (!canWrite || !scope.data?.ready || scope.isFetching || scope.isError || action.busy) return
  if (reason.trim().length < 3) { setValidation(t('precollectInvalidReason')); return }; setValidation('')
  const body: PrecollectInput = { targets: [{ asset_id: asset.id, expected_scope_hash: scope.data.scope_hash }], reason: reason.trim() }, serialized = JSON.stringify(body)
  if (serialized !== attempt.current.body) attempt.current = { body: serialized, key: crypto.randomUUID() }
  const result = await action.run(() => createPrecollect(project.id, body, session.csrf_token, attempt.current.key))
  if (result.ok && mounted.current) await navigate({ to: '/p/$projectId/executions', params: { projectId: project.id }, search: { execution: result.value.id, tab: 'overview' } })
 }
 function refresh() { void scope.refetch(); void gate.refetch(); void list.refetch(); if (selected) void detail.refetch() }
 const record = detail.data
 const currentEvidence = gate.data?.evidence, selectedEvidence = record?.gate
 useEffect(() => {
  const future = [currentEvidence?.valid_until, currentEvidence?.effective_valid_until, selectedEvidence?.valid_until].map(value => value ? Date.parse(value) : NaN).filter(value => Number.isFinite(value) && value > Date.now())
  if (!future.length) return
  const timer = window.setTimeout(() => setNow(Date.now()), Math.min(Math.max(1, Math.min(...future) - Date.now() + 1), 2_147_483_647))
  return () => window.clearTimeout(timer)
 }, [currentEvidence?.valid_until, currentEvidence?.effective_valid_until, selectedEvidence?.valid_until, now])
 const validity = (evidence: NonNullable<typeof currentEvidence>) => Date.parse(evidence.valid_until) <= Math.max(now, Date.now()) ? 'stale' : evidence.validity
 const waiverExpired = !!currentEvidence?.effective_valid_until && Date.parse(currentEvidence.effective_valid_until) <= now
 const effectiveVerdict = currentEvidence && (validity(currentEvidence) !== 'fresh' || waiverExpired) ? 'BLOCKED' : gate.data?.effective_verdict ?? 'BLOCKED'
 return <AssetDetailFrame title={`${t('precollectTitle')} · ${asset.hostname}`} description={t('precollectIntro')} busy={action.busy} embedded={embedded} onBusy={onBusy} onClose={onClose}>
  {errors.map((error, i) => <ActionError key={i} error={error} />)}
  {!denied && <>
   <section className="ui:grid ui:gap-2" aria-label={t('precollectCurrent')}><h3>{t('precollectCurrent')}</h3>{gate.data ? <><p>{t(`precollect_${effectiveVerdict}`)}{gate.data.evidence?.waiver_id && <code className="cell-subtitle ui:break-all">{gate.data.evidence.waiver_id} · {gate.data.evidence.effective_valid_until && new Date(gate.data.evidence.effective_valid_until).toLocaleString()}</code>}{gate.data.reason && <> · <span className="mono-value">{gate.data.reason}</span></>}</p>{gate.data.evidence && <p>{t('precollectHistorical')}: {t(`precollect_${gate.data.evidence.verdict}`)} · {t('precollectValidity')}: {t(`precollect_${validity(gate.data.evidence)}`)} · {t('precollectUntil')}: {new Date(gate.data.evidence.valid_until).toLocaleString()}</p>}</> : gate.isFetching && <p role="status">{t('loadingRecords')}</p>}</section>
   <details><summary>{t('precollectScope')} · {scope.data ? t(scope.data.ready ? 'precollectReady' : 'precollectBlocked') : '…'}</summary>{scope.data && <><p className="mono-value">{scope.data.code}</p><pre className="ui:max-h-72 ui:overflow-auto ui:whitespace-pre-wrap ui:break-words">{JSON.stringify({ model_version: scope.data.model_version_id, asset_version: scope.data.asset_version, bom_package: scope.data.bom_package_id, expectation: scope.data.expectation, bmc: scope.data.bmc, gate_ttl_seconds: scope.data.gate_ttl_seconds }, null, 2)}</pre></>}</details>
   {canWrite && <form onSubmit={event => void start(event)} className="ui:grid ui:gap-2"><Label htmlFor="precollect-reason">{t('precollectReason')}</Label><Input id="precollect-reason" value={reason} maxLength={1000} disabled={action.busy} onChange={event => setReason(event.target.value)} /><p role="alert">{validation}</p><Button type="submit" disabled={action.busy || scope.isFetching || scope.isError || !scope.data?.ready}>{t('precollectStart')}</Button></form>}
   <Button type="button" variant="outline" disabled={action.busy || scope.isFetching || gate.isFetching || list.isFetching || detail.isFetching} onClick={refresh}>{t('precollectRefresh')}</Button>
   <h3>{t('precollectHistory')}</h3>{list.isFetching && <p role="status">{t('loadingRecords')}</p>}
   {list.data && <><Table><TableHeader><TableRow><TableHead>{t('bmcCollectedAt')}</TableHead><TableHead>{t('bomState')}</TableHead><TableHead>{t('bmcSource')}</TableHead><TableHead>{t('precollectDetails')}</TableHead></TableRow></TableHeader><TableBody>{list.data.items.map(row => <TableRow key={row.id} aria-selected={row.id === selected}><TableCell>{new Date(row.collected_at ?? row.created_at).toLocaleString()}</TableCell><TableCell>{t(`executionState_${row.execution_state}`, { defaultValue: row.execution_state })}</TableCell><TableCell>{row.origin ? t(`bmcOrigin_${row.origin}`) : '—'}</TableCell><TableCell><Button variant="outline" size="sm" onClick={() => setSelected(row.id)}>{t('precollectDetails')}</Button></TableCell></TableRow>)}</TableBody></Table>{!list.data.items.length && <p>{t('precollectEmpty')}</p>}<Pagination offset={(page - 1) * list.data.page_size} limit={list.data.page_size} total={list.data.total} busy={list.isFetching} onChange={offset => setPage(Math.floor(offset / (list.data?.page_size ?? 20)) + 1)} /></>}
   {record && !detail.isError && <section className="ui:grid ui:gap-3"><Link to="/p/$projectId/executions" params={{ projectId: project.id }} search={{ execution: record.execution_id, tab: 'overview' }}>{t('bmcViewExecution')}</Link>
    {record.gate ? <><p>{t('precollectHistorical')}: {t(`precollect_${record.gate.verdict}`)} · {t('precollectValidity')}: {t(`precollect_${validity(record.gate)}`)} · {t('precollectCompleteness')}: {record.gate.evaluation.completeness_percent.toFixed(1)}% ({record.gate.evaluation.present}/{record.gate.evaluation.required})</p>{record.gate.invalidation_reason && <p className="mono-value">{record.gate.invalidation_reason}</p>}<details open={record.gate.evaluation.findings.length > 0}><summary>{t('precollectFindings')} · {record.gate.evaluation.findings.length}</summary><ul>{record.gate.evaluation.findings.map((finding, index) => <li className="mono-value ui:break-all" key={`${finding.code}:${index}`}>{finding.verdict} · {finding.field} · {finding.target} · {finding.code}</li>)}</ul></details></> : <p>{t('precollectPending')}</p>}
    {record.report && <><p>{t('precollectNoConfigChange')}: {String(record.report.config_unchanged)}</p><details><summary>{t('precollectReport')}</summary><pre className="ui:max-h-96 ui:overflow-auto ui:whitespace-pre-wrap ui:break-words">{JSON.stringify(record.report, null, 2)}</pre></details><Button type="button" variant="outline" disabled={action.busy} onClick={() => void action.run(() => downloadPrecollect(project.id, asset.id, record.id))}>{t('precollectRaw')}</Button></>}
   </section>}
  </>}
  <p className="field-hint">{t('precollectReadOnly')}</p>{!embedded && <Button type="button" variant="outline" disabled={action.busy} onClick={onClose}>{t('bmcClose')}</Button>}
 </AssetDetailFrame>
}
