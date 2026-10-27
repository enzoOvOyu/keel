import { useEffect, useRef, useState, type FormEvent } from 'react'
import { Link } from '@tanstack/react-router'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { NativeCheckbox } from '@/components/ui/checkbox'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { ActionError, Pagination } from '../../components/Management'
import { useProject } from '../../components/ProjectWorkspace'
import { ApiError } from '../../lib/api'
import { useAction } from '../../lib/management'
import { useExpireOnUnauthorized, useSession } from '../../lib/session'
import { createWiringBatch, createWiringExecution, confirmWiring, getWiringBatch, createWiringHandshake, getWiringHandshake, listWiringHandshakes, previewWiring, type WiringFacts, type WiringTarget } from '../../lib/wiring'

import WiringTimeline from './WiringTimeline'

type PortChoice = { id: string; name: string; mac: string }
export default function WiringDialog({ asset, ports, onClose }: { asset: { id: string; hostname: string }; ports: PortChoice[]; onClose: () => void }) {
 const { t } = useTranslation(), { session } = useSession(), { project } = useProject(), action = useAction(), queries = useQueryClient()
 const [chosen, setChosen] = useState(() => new Set(ports.map(port => port.id))), [scope, setScope] = useState<WiringFacts | null>(null), [reason, setReason] = useState(''), [page, setPage] = useState(1), [selected, setSelected] = useState(''), [validation, setValidation] = useState(''), [now, setNow] = useState(Date.now())
 const confirmAttempt = useRef({ body: '', key: '' })
 const [executionId, setExecutionId] = useState(''), runAttempt = useRef({ body: '', key: '' })
 const [batchId, setBatchId] = useState(''), batchAttempt = useRef({ body: '', key: '' })
 const mounted = useRef(true), attempt = useRef({ body: '', key: '' })
 useEffect(() => { mounted.current = true; return () => { mounted.current = false } }, [])
 const key = ['private', session.user.id, 'project', project.id, 'wiring']
 const list = useQuery({ queryKey: [...key, 'history', page], queryFn: ({ signal }) => listWiringHandshakes(project.id, page, signal), retry: false, refetchOnWindowFocus: false })
 const detail = useQuery({ queryKey: [...key, 'detail', selected], queryFn: ({ signal }) => getWiringHandshake(project.id, selected, signal), enabled: !!selected, retry: false, refetchOnWindowFocus: false })
 const batch = useQuery({ queryKey: [...key, 'batch', batchId], queryFn: ({ signal }) => getWiringBatch(project.id, batchId, signal), enabled: !!batchId, retry: false, refetchOnWindowFocus: false })
 useExpireOnUnauthorized(batch.error)
 useExpireOnUnauthorized(list.error); useExpireOnUnauthorized(detail.error)
 const errors = [list.error, detail.error, batch.error, action.error], denied = errors.some(error => error instanceof ApiError && [401, 403, 404].includes(error.status))
 const canWrite = !denied && (session.user.is_admin || ['Admin', 'DE', 'DL', 'NE', 'SRE'].includes(project.role)) && !['archived', 'delivered'].includes(project.state)
 const targets: WiringTarget[] = [{ asset_id: asset.id, physical_port_ids: [...chosen].sort() }]
 const freshScope = !!scope?.ready && Date.parse(scope.evidence_valid_until) > now
 const lease = detail.data
 const liveLease = !!lease?.ready && !detail.isError && Date.parse(lease.valid_until) > now
 useEffect(() => {
  const dates = [scope?.evidence_valid_until, lease?.valid_until, ...(batch.data?.targets.flatMap(target => target.reports.map(report => report.valid_until)) ?? [])].map(value => value ? Date.parse(value) : NaN).filter(value => Number.isFinite(value) && value > Date.now())
  if (!dates.length) return
  const timer = window.setTimeout(() => setNow(Date.now()), Math.min(2_147_483_647, Math.max(1, Math.min(...dates) - Date.now() + 1)))
  return () => window.clearTimeout(timer)
 }, [scope?.evidence_valid_until, lease?.valid_until, batch.data, now])
 async function preview() {
  setScope(null)
  const result = await action.run(() => previewWiring(project.id, targets, session.csrf_token))
  if (result.ok && mounted.current) { setScope(result.value); setNow(Date.now()) }
 }
 async function create(event: FormEvent<HTMLFormElement>) {
  event.preventDefault()
  if (!canWrite || !freshScope) return
  const count = Array.from(reason.trim()).length
  if (count < 3 || count > 1000) { setValidation(t('wiringInvalidReason')); return }
  setValidation('')
  const body = JSON.stringify({ targets, reason: reason.trim() })
  if (attempt.current.body !== body) attempt.current = { body, key: crypto.randomUUID() }
  const result = await action.run(() => createWiringHandshake(project.id, targets, reason.trim(), session.csrf_token, attempt.current.key))
  if (!mounted.current) return
  if (result.ok) { queries.setQueryData([...key, 'detail', result.value.id], result.value); setSelected(result.value.id); setNow(Date.now()); attempt.current = { body: '', key: '' }; void list.refetch() }
  else setScope(null)
 }
 async function publishStep1() {
  if (!lease || !liveLease || !canWrite) return
  const count = Array.from(reason.trim()).length
  if (count < 3 || count > 1000) { setValidation(t('wiringInvalidReason')); return }
  const body = JSON.stringify({ handshake: lease.id, facts: lease.facts.facts_hash, reason: reason.trim() })
  if (batchAttempt.current.body !== body) batchAttempt.current = { body, key: crypto.randomUUID() }
  const result = await action.run(() => createWiringBatch(project.id, lease.id, lease.facts.facts_hash, reason.trim(), session.csrf_token, batchAttempt.current.key))
  if (result.ok && mounted.current) {
   queries.setQueryData([...key, 'batch', result.value.id], result.value); setBatchId(result.value.id); setNow(Date.now())
   queries.setQueryData([...key, 'detail', lease.id], { ...lease, ready: false, batch_id: result.value.id, validity: 'invalidated', invalidation_reason: 'WIRING_HANDSHAKE_CONSUMED' })
   void list.refetch()
  }
 }
 async function confirmTarget(assetId: string, step3Id: string) {
  if (!batch.data || !canWrite || !(session.user.is_admin || ['Admin', 'DL'].includes(project.role))) return
  const count = Array.from(reason.trim()).length
  if (count < 3 || count > 1000) { setValidation(t('wiringInvalidReason')); return }
  const input = { asset_id: assetId, step3_id: step3Id, reason: reason.trim() }, body = JSON.stringify(input)
  if (confirmAttempt.current.body !== body) confirmAttempt.current = { body, key: crypto.randomUUID() }
  const result = await action.run(() => confirmWiring(project.id, batch.data!.id, input, session.csrf_token, confirmAttempt.current.key))
  if (result.ok && mounted.current) { void batch.refetch(); void queries.invalidateQueries({ queryKey: ['private', session.user.id, 'project', project.id, 'assets'] }) }
 }
 async function startCapture() {
  const frozen = batch.data
  if (!frozen || !canWrite || !frozen.can_start_step2) return
  const count = Array.from(reason.trim()).length
  if (count < 3 || count > 1000) { setValidation(t('wiringInvalidReason')); return }
  const frozenTargets = frozen.targets.map(target => ({ asset_id: target.asset_id, physical_port_ids: target.expectation.ports.map(port => port.physical_port_id) }))
  const body = JSON.stringify({ batch: frozen.id, reason: reason.trim() })
  if (runAttempt.current.body !== body) runAttempt.current = { body, key: crypto.randomUUID() }
  const submissionKey = runAttempt.current.key
  const result = await action.run(async () => {
   try {
    const readiness = await createWiringHandshake(project.id, frozenTargets, reason.trim(), session.csrf_token, submissionKey)
    return await createWiringExecution(project.id, { batch_id: frozen.id, handshake_id: readiness.id, facts_hash: readiness.facts.facts_hash, reason: reason.trim() }, session.csrf_token, submissionKey)
   } catch (error) { if (error instanceof ApiError && error.status === 409) runAttempt.current = { body: '', key: '' }; throw error }
  })
  if (!mounted.current) return
  if (result.ok) { setExecutionId(result.value.id); void batch.refetch(); void list.refetch() }
 }
 function changePort(id: string, checked: boolean) {
  setChosen(previous => { const next = new Set(previous); if (checked) next.add(id); else next.delete(id); return next })
  setScope(null)
 }
 return <Dialog open onOpenChange={open => { if (!open && !action.busy) onClose() }}><DialogContent className="ui:max-h-[90vh] ui:overflow-y-auto ui:sm:max-w-5xl"><DialogHeader><DialogTitle>{t('wiringTitle')} · {asset.hostname}</DialogTitle><DialogDescription>{t('wiringHelp')}</DialogDescription></DialogHeader>
  {errors.map((error, index) => error ? <ActionError key={index} error={error as ApiError} /> : null)}
  {!denied && <>
   {canWrite && <div className="ui:grid ui:gap-2"><Label htmlFor="wiring-reason">{t('wiringReason')}</Label><Input id="wiring-reason" value={reason} maxLength={1000} disabled={action.busy} onChange={event => setReason(event.target.value)} />{validation && <p role="alert">{validation}</p>}</div>}
   <fieldset disabled={action.busy}><legend>{t('wiringPort')}</legend>{ports.map(port => <Label key={port.id} className="ui:flex ui:items-center ui:gap-2 ui:py-1"><NativeCheckbox checked={chosen.has(port.id)} onChange={event => changePort(port.id, event.target.checked)} />{port.name} · <code>{port.mac}</code></Label>)}</fieldset>
   <p className="field-hint">{t('wiringScopeSelection')}</p><Button variant="outline" disabled={action.busy || !chosen.size} onClick={() => void preview()}>{t('wiringPreview')}</Button>
   {scope && <section><h3>{t(freshScope ? 'wiringReady' : 'wiringBlocked')}</h3><p className="field-hint">{t('wiringGateHint')}</p><p>{t('wiringCurrentSelection')}: {scope.targets.reduce((n, host) => n + host.ports.length, 0)}</p>{scope.blockers.length > 0 && <ul aria-label={t('wiringBlockers')}>{scope.blockers.map(code => <li key={code}><code>{code}</code></li>)}</ul>}
    <Table><TableHeader><TableRow><TableHead>{t('wiringSwitches')}</TableHead><TableHead>NP-1</TableHead><TableHead>NP-2</TableHead><TableHead>NP-4</TableHead></TableRow></TableHeader><TableBody>{scope.switches.map(device => <TableRow key={device.id}><TableCell>{device.serial}<span className="cell-subtitle">{device.ports.join(', ')}</span></TableCell>{[device.np1, device.np2, device.np4].map(gate => <TableCell key={gate.id}>{t(`precollect_${gate.state}`)}{gate.reasons.map(code => <code className="cell-subtitle" key={code}>{code}</code>)}</TableCell>)}</TableRow>)}</TableBody></Table>
    {canWrite && <form onSubmit={event => void create(event)} className="ui:grid ui:gap-2"><Button type="submit" disabled={action.busy || !freshScope}>{t('wiringCreate')}</Button></form>}
   </section>}
   <section><h3>{t('wiringHistory')}</h3><Button variant="outline" onClick={() => { setNow(Date.now()); void list.refetch(); if (selected) void detail.refetch(); if (batchId) void batch.refetch() }} disabled={action.busy || list.isFetching || detail.isFetching}>{t('topologyRefresh')}</Button>
    {list.isPending && <p role="status">{t('loadingRecords')}</p>}{list.data && !list.isError && <><Table><TableHeader><TableRow><TableHead>{t('wiringReadinessAt')}</TableHead><TableHead>{t('wiringValidUntil')}</TableHead><TableHead>{t('wiringDetail')}</TableHead></TableRow></TableHeader><TableBody>{list.data.items.map(row => <TableRow key={row.id}><TableCell>{new Date(row.readiness_at).toLocaleString()}</TableCell><TableCell>{new Date(row.valid_until).toLocaleString()}</TableCell><TableCell><Button variant="outline" size="sm" onClick={() => { setNow(Date.now()); setSelected(row.id); if (selected === row.id) void detail.refetch() }}>{t('wiringDetail')}</Button></TableCell></TableRow>)}</TableBody></Table>{!list.data.items.length && <p>{t('wiringEmpty')}</p>}<Pagination offset={(page - 1) * list.data.page_size} limit={list.data.page_size} total={list.data.total} busy={list.isFetching} onChange={offset => setPage(Math.floor(offset / (list.data?.page_size ?? 10)) + 1)} /></>}
   </section>
   {lease && !detail.isError && <section aria-live="polite"><h3>{t('wiringLease')}: {t(liveLease ? 'wiringLeaseReady' : 'wiringLeaseExpired')}</h3><code className="cell-subtitle">{lease.id}</code><p>{t('wiringCollector')}: {lease.collector.state} · {lease.collector.duration_ms}ms</p><p>{t('wiringValidUntil')}: {new Date(lease.valid_until).toLocaleString()}</p>{!liveLease && <code>{lease.invalidation_reason === 'WIRING_HANDSHAKE_CONSUMED' ? t('wiringConsumed') : lease.invalidation_reason || 'WIRING_HANDSHAKE_EXPIRED'}</code>}<details><summary>{t('wiringCurrentSelection')}</summary>{lease.facts.targets.map(host => <p key={host.asset_id}>{host.hostname}: {host.ports.map(port => `${port.interface} → ${port.switch_serial}/${port.switch_port}`).join(', ')}</p>)}</details>{liveLease && canWrite && <Button disabled={action.busy} onClick={() => void publishStep1()}>{t('wiringStep1Publish')}</Button>}{lease.batch_id && <Button variant="outline" onClick={() => setBatchId(lease.batch_id)}>{t('wiringViewBatch')}</Button>}</section>}
   {batch.data && !batch.isError && <><WiringTimeline batch={batch.data} now={now} canConfirm={canWrite && (session.user.is_admin || ['Admin', 'DL'].includes(project.role))} busy={action.busy} onConfirm={(assetId, step3Id) => void confirmTarget(assetId, step3Id)} /><div className="action-row"><Button disabled={action.busy || !canWrite || !batch.data.can_start_step2} onClick={() => void startCapture()}>{t('wiringStartCapture')}</Button><Button variant="outline" disabled={batch.isFetching} onClick={() => void batch.refetch()}>{t('wiringRefreshReports')}</Button>{executionId && <Link to="/p/$projectId/executions" params={{ projectId: project.id }} search={{ execution: executionId }}>{t('wiringViewExecution')}</Link>}</div></>}
  </>}
  <p className="field-hint">{t('wiringNoExecution')}</p><Button variant="outline" disabled={action.busy} onClick={onClose}>{t('bmcClose')}</Button>
 </DialogContent></Dialog>
}
