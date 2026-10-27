import { lazy, Suspense, useEffect, useState } from 'react'
import { Link } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Label } from '@/components/ui/label'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { ActionError } from '../../components/Management'
import { ProjectWorkspace, useProject } from '../../components/ProjectWorkspace'
import { ApiError } from '../../lib/api'
import { gateDashboard } from '../../lib/gates'
import { useExpireOnUnauthorized, useSession } from '../../lib/session'
import { useMonitorVisible } from '../monitor/visibility'
import type { ServerDetailTab, ServerDetailTarget } from '../assets/ServerDetailDrawer'

import { GateWaivers } from './GateWaivers'

const ServerDetailDrawer = lazy(() => import('../assets/ServerDetailDrawer'))
export function GatesPage({ projectId }: { projectId: string }) { return <ProjectWorkspace projectId={projectId}><GatesContent key={projectId} /></ProjectWorkspace> }
function GatesContent() {
 const { t } = useTranslation(), project = useProject(), session = useSession(), visible = useMonitorVisible()
 const [assetId, setAssetId] = useState(''), [now, setNow] = useState(Date.now()), [target, setTarget] = useState<{ asset: ServerDetailTarget; tab: ServerDetailTab } | null>(null)
 const [writing, setWriting] = useState(false)
 const query = useQuery({ queryKey: ['private', session.user.id, 'project', project.id, 'gates'], queryFn: ({ signal }) => gateDashboard(project.id, signal), enabled: visible, retry: false, refetchOnWindowFocus: false })
 useExpireOnUnauthorized(query.error)
 useEffect(() => { if (!visible) return; setNow(Date.now()); const timer = globalThis.setInterval(() => setNow(Date.now()), 5000); return () => globalThis.clearInterval(timer) }, [visible])
 const denied = query.error instanceof ApiError && [401, 403, 404].includes(query.error.status), data = denied ? undefined : query.data
 const selected = data?.items.find(item => item.asset_id === assetId) || data?.items[0]
 const stale = !!data && now - Date.parse(data.as_of) > 30000
 return <section className="ui:grid ui:gap-4"><h1>{t('gatesTitle')}</h1><p>{t('gatesEvidenceHelp')}</p><ActionError error={query.error} /><Button variant="outline" disabled={query.isFetching} onClick={() => { setNow(Date.now()); void query.refetch() }}>{t('topologyRefresh')}</Button>{query.isPending && <p role="status">{t('loadingRecords')}</p>}
  {data && <><section className="ui:grid ui:gap-3 ui:sm:grid-cols-3" aria-label={t('gatesKPI')}><p>{t('gatesNetworkKPI')}: {stale ? '—' : data.kpi.network_normal}/{data.kpi.assets - data.kpi.maintained}<span className="cell-subtitle">{t('gatesNetworkCounts', { abnormal: data.kpi.network_abnormal, unknown: data.kpi.network_unknown, maintained: data.kpi.maintained })}</span></p><p><Link to="/issues" search={{ project_id: project.id }}>{t('gatesOpenIssues')}: {data.kpi.open_issues}</Link></p><p>{t('gatesTrustAnomalies')}: {data.kpi.trust_anomalies}</p></section><p>{t('gatesAsOf')}: {new Date(data.as_of).toLocaleString()}{stale && <> · {t('monitor_stale')}</>}</p>
   <Label htmlFor="gate-asset">{t('bomHostname')}</Label><NativeSelect disabled={writing} id="gate-asset" value={selected?.asset_id || ''} onChange={event => setAssetId(event.target.value)}>{data.items.map(item => <NativeSelectOption key={item.asset_id} value={item.asset_id}>{item.hostname}</NativeSelectOption>)}</NativeSelect>
   {selected && <section className="ui:grid ui:gap-3 ui:sm:grid-cols-2 ui:xl:grid-cols-3">{selected.cards.map(card => {
    const expired = !!card.valid_until && Date.parse(card.valid_until) <= now
    const effective = expired || stale ? 'BLOCKED' : card.effective_verdict
    return <article key={card.gate} className="ui:rounded-lg ui:border ui:p-4"><h2>{card.gate} · {t(`gatesName_${card.gate}`)}</h2><p>{t('gatesEffective')}: {effective}</p><p>{t('gatesOriginal')}: {card.original_verdict || '—'} · {card.origin}</p>{card.reason && <code className="ui:break-all">{card.reason}</code>}{expired && <p>{t('gatesExpired')}</p>}<p>{t('wiringValidUntil')}: {card.valid_until ? new Date(card.valid_until).toLocaleString() : '—'}</p><code className="cell-subtitle ui:break-all">{card.evidence_id || '—'}</code><code className="cell-subtitle ui:break-all">{card.scope_hash}</code>{['G1', 'G2', 'G3'].includes(card.gate) && <Button variant="link" onClick={() => setTarget({ asset: { id: selected.asset_id, hostname: selected.hostname }, tab: card.gate === 'G3' ? 'mapping' : card.gate === 'G2' ? 'topology' : 'ledger' })}>{t('topologyEvidence')}</Button>}</article>
   })}</section>}
   {selected && <GateWaivers key={selected.asset_id} assetId={selected.asset_id} onBusy={setWriting} currentWaiverId={stale ? '' : selected.cards.find(card => card.gate === 'G1')?.waiver_id || ''} />}
   <Table><TableHeader><TableRow><TableHead>{t('bomHostname')}</TableHead>{['G1', 'G2', 'G3', 'G4', 'G5', 'G6'].map(gate => <TableHead key={gate}>{gate}</TableHead>)}</TableRow></TableHeader><TableBody>{data.items.map(item => <TableRow key={item.asset_id}><TableCell><Button variant="link" disabled={writing} onClick={() => setAssetId(item.asset_id)}>{item.hostname}</Button></TableCell>{item.cards.map(card => <TableCell key={card.gate}>{stale || card.valid_until && Date.parse(card.valid_until) <= now ? 'BLOCKED' : card.effective_verdict}</TableCell>)}</TableRow>)}</TableBody></Table>
  </>}
  {target && <Suspense fallback={<p role="status">{t('loadingRecords')}</p>}><ServerDetailDrawer key={`${project.id}:${target.asset.id}`} target={target.asset} assets={(data?.items ?? []).map(item => ({ id: item.asset_id, hostname: item.hostname }))} initialTab={target.tab} onSelect={(asset, tab) => setTarget({ asset, tab })} onClose={() => setTarget(null)} /></Suspense>}
 </section>
}
