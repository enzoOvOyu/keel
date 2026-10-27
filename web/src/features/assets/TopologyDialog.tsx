import { useEffect, useRef, useState, type FormEvent } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import AssetDetailFrame from './AssetDetailFrame'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { ActionError, Pagination } from '../../components/Management'
import { useProject } from '../../components/ProjectWorkspace'
import { ApiError } from '../../lib/api'
import type { ProjectAsset } from '../../lib/bom'
import { useAction } from '../../lib/management'
import { downloadPrecollect } from '../../lib/precollect'
import { useExpireOnUnauthorized, useSession } from '../../lib/session'
import { getTopology, getTopologyRelations, listTopology, previewTopology, validateTopology } from '../../lib/topology'
import PcieTopologyView from './PcieTopologyView'
import WiringDialog from './WiringDialog'

export default function TopologyDialog({ asset, onClose, embedded = false, onBusy }: { asset: Pick<ProjectAsset, 'id' | 'hostname'>; onClose: () => void; embedded?: boolean; onBusy?: (busy: boolean) => void }) {
 const { t } = useTranslation(), session = useSession(), project = useProject(), action = useAction(), queries = useQueryClient()
 const [page, setPage] = useState(1), [selected, setSelected] = useState(''), [reason, setReason] = useState(''), [validation, setValidation] = useState(''), [now, setNow] = useState(Date.now())
 const [showWiring, setShowWiring] = useState(false)
 const mounted = useRef(true), attempt = useRef({ body: '', key: '' })
 useEffect(() => { mounted.current = true; return () => { mounted.current = false } }, [])
 const key = ['private', session.user.id, 'project', project.id, 'topology', asset.id]
 const scope = useQuery({ queryKey: [...key, 'scope'], queryFn: ({ signal }) => previewTopology(project.id, asset.id, signal), retry: false, refetchOnWindowFocus: false })
 const list = useQuery({ queryKey: [...key, 'history', page], queryFn: ({ signal }) => listTopology(project.id, asset.id, page, signal), retry: false, refetchOnWindowFocus: false })
 const detail = useQuery({ queryKey: [...key, 'detail', selected], queryFn: ({ signal }) => getTopology(project.id, asset.id, selected, signal), enabled: !!selected, retry: false, refetchOnWindowFocus: false })
 const relations = useQuery({ queryKey: [...key, 'relations', selected], queryFn: ({ signal }) => getTopologyRelations(project.id, asset.id, selected, signal), enabled: !!selected, retry: false, refetchOnWindowFocus: false })
 useExpireOnUnauthorized(relations.error); useExpireOnUnauthorized(scope.error); useExpireOnUnauthorized(list.error); useExpireOnUnauthorized(detail.error)
 const errors = [scope.error, list.error, detail.error, relations.error, action.error], denied = errors.some(error => error instanceof ApiError && [401, 403, 404].includes(error.status))
 const canWrite = !denied && (session.user.is_admin || ['Admin', 'DE', 'DL', 'SRE'].includes(project.role)) && !['archived', 'delivered'].includes(project.state)
 const freshScope = !!scope.data?.ready && Date.parse(scope.data.valid_until) > now
 const record = detail.data
 useEffect(() => {
  const dates = [scope.data?.valid_until, record?.valid_until].map(value => value ? Date.parse(value) : NaN).filter(value => Number.isFinite(value) && value > Date.now())
  if (!dates.length) return
  const timer = window.setTimeout(() => setNow(Date.now()), Math.min(2_147_483_647, Math.max(1, Math.min(...dates) - Date.now() + 1)))
  return () => window.clearTimeout(timer)
 }, [scope.data?.valid_until, record?.valid_until, now])
 function refresh() { setNow(Date.now()); void scope.refetch(); void list.refetch(); if (selected) { void detail.refetch(); void relations.refetch() } }
 async function compare(event: FormEvent<HTMLFormElement>) {
  event.preventDefault()
  if (!scope.data || !freshScope || !canWrite) return
  if (Array.from(reason.trim()).length < 3) { setValidation(t('precollectInvalidReason')); return }
  setValidation('')
  const body = JSON.stringify({ scope_hash: scope.data.scope_hash, reason: reason.trim() })
  if (attempt.current.body !== body) attempt.current = { body, key: crypto.randomUUID() }
  const result = await action.run(() => validateTopology(project.id, asset.id, scope.data!.scope_hash, reason.trim(), session.csrf_token, attempt.current.key))
  if (result.ok && mounted.current) { queries.setQueryData([...key, 'detail', result.value.id], result.value); setSelected(result.value.id); void list.refetch(); void scope.refetch() }
 }
 const wiringPorts = [...new Map((relations.data?.rows ?? []).filter(row => row.physical_port_id).map(row => [row.physical_port_id, { id: row.physical_port_id, name: row.interface, mac: row.permanent_mac }])).values()]
 const effective = record && Date.parse(record.valid_until) <= now ? 'BLOCKED' : record?.effective_verdict
 return <AssetDetailFrame title={`${t('topologyTitle')} · ${asset.hostname}`} description={t('topologyIntro')} busy={action.busy || showWiring} embedded={embedded} onBusy={onBusy} onClose={onClose}>
  {errors.map((error, index) => error ? <ActionError key={index} error={error as ApiError} /> : null)}
  {!denied && <>
   {scope.isPending && <p role="status">{t('loadingRecords')}</p>}
   {scope.data && !scope.isError && <section><h3>{t('topologyPrerequisites')}: {t(freshScope ? 'topologyReady' : 'topologyBlocked')}</h3><p className="mono-value">{freshScope ? scope.data.code : Date.parse(scope.data.valid_until) <= now && scope.data.ready ? 'TOPOLOGY_SCOPE_EXPIRED' : scope.data.code}</p>
    {scope.data.expectation && <details><summary>{t('topologyModel')} · {scope.data.model_version_id}</summary><p>{t('topologyProvenance')}: {scope.data.expectation.provenance.kind} · {scope.data.expectation.provenance.reference}</p><pre className="ui:max-h-80 ui:overflow-auto ui:whitespace-pre-wrap ui:break-words">{JSON.stringify(scope.data.expectation, null, 2)}</pre></details>}
    {canWrite && <form onSubmit={event => void compare(event)} className="ui:grid ui:gap-2"><Label htmlFor="topology-reason">{t('topologyReason')}</Label><Input id="topology-reason" value={reason} maxLength={1000} onChange={event => setReason(event.target.value)} disabled={action.busy} />{validation && <p role="alert">{validation}</p>}<Button type="submit" disabled={action.busy || !freshScope}>{t('topologyCompare')}</Button></form>}
   </section>}
   <Button variant="outline" onClick={refresh} disabled={action.busy || scope.isFetching || list.isFetching || detail.isFetching}>{t('topologyRefresh')}</Button>
   <section><h3>{t('topologyHistory')}</h3>{list.isPending && <p role="status">{t('loadingRecords')}</p>}{list.data && !list.isError && <><Table><TableHeader><TableRow><TableHead>{t('modelVersion')}</TableHead><TableHead>{t('topologyHistorical')}</TableHead><TableHead>{t('topologyEffective')}</TableHead><TableHead>{t('topologyUntil')}</TableHead><TableHead>{t('topologyEvidence')}</TableHead></TableRow></TableHeader><TableBody>{list.data.items.map(row => <TableRow key={row.id} aria-selected={selected === row.id}><TableCell>v{row.version}</TableCell><TableCell>{t(`precollect_${row.verdict}`)}</TableCell><TableCell>{t(`precollect_${Date.parse(row.valid_until) <= now ? 'BLOCKED' : row.effective_verdict}`)}</TableCell><TableCell>{new Date(row.valid_until).toLocaleString()}</TableCell><TableCell><Button variant="outline" size="sm" onClick={() => setSelected(row.id)}>{t('topologyEvidence')}</Button></TableCell></TableRow>)}</TableBody></Table>{list.data.items.length === 0 && <p>{t('topologyEmpty')}</p>}<Pagination offset={(page - 1) * list.data.page_size} limit={list.data.page_size} total={list.data.total} busy={list.isFetching} onChange={offset => setPage(Math.floor(offset / (list.data?.page_size ?? 20)) + 1)} /></>}</section>
   {record && !detail.isError && <section><h3>{t('topologyHistorical')}: {t(`precollect_${record.verdict}`)} · {t('topologyEffective')}: {t(`precollect_${effective}`)}</h3><p>{t('topologyActualSource')}: {record.origin} · {record.precollect_id}</p>{record.invalidation_reason && <p className="mono-value">{record.invalidation_reason}</p>}<Button variant="outline" disabled={action.busy} onClick={() => void action.run(() => downloadPrecollect(project.id, asset.id, record.precollect_id))}>{t('precollectRaw')}</Button><PcieTopologyView record={record} />{relations.data && !relations.isError && <section><h3>{t('topologyRelationsTitle')}</h3><p className="field-hint">{t('topologyRelationsHelp')}</p><Table><TableHeader><TableRow>{['GPU','NIC',t('topologyNUMA'),t('topologyMatrix'),t('topologyEffective')].map(label => <TableHead key={label}>{label}</TableHead>)}</TableRow></TableHeader><TableBody>{relations.data.rows.map((row, index) => <TableRow key={`${row.gpu_key}:${row.nic_key}:${row.physical_port_id}:${index}`}><TableCell>{row.gpu_key}<code className="cell-subtitle">{row.gpu_uuid} · {row.gpu_node?.bdf}</code></TableCell><TableCell>{row.nic_key} · {row.interface}<code className="cell-subtitle">{row.permanent_mac} · {row.nic_node?.bdf}</code></TableCell><TableCell>{row.gpu_node?.numa ?? '?'} / {row.nic_node?.numa ?? '?'}</TableCell><TableCell>{row.expected_affinity} → {row.observed_affinity || '?'} ({row.computed_affinity || '?'})</TableCell><TableCell>{t(`precollect_${row.verdict}`)}</TableCell></TableRow>)}</TableBody></Table></section>}</section>}
  </>}
  {!denied && wiringPorts.length > 0 && <Button variant="outline" disabled={action.busy} onClick={() => setShowWiring(true)}>{t('wiringTitle')}</Button>}
  {showWiring && !denied && <WiringDialog key={`${project.id}:${asset.id}:${selected}`} asset={asset} ports={wiringPorts} onClose={() => setShowWiring(false)} />}
  <p className="field-hint">{t('topologyFirmwareHint')}</p><p className="field-hint">{t('topologyReadOnly')}</p>{!embedded && <Button variant="outline" disabled={action.busy} onClick={onClose}>{t('bmcClose')}</Button>}
 </AssetDetailFrame>
}
