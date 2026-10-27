import { useEffect, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { ActionError } from '../../components/Management'
import { useProject } from '../../components/ProjectWorkspace'
import { ApiError } from '../../lib/api'
import { monitorSnapshot, monitorTrend, type MonitorObservation } from '../../lib/monitor'
import { useExpireOnUnauthorized, useSession } from '../../lib/session'
import { useMonitorVisible } from './visibility'

export default function MonitorPanel({ assetId }: { assetId: string }) {
 const { t } = useTranslation(), project = useProject(), session = useSession(), visible = useMonitorVisible(), cursor = useRef(0)
 const [selected, setSelected] = useState<MonitorObservation | null>(null), [window, setWindow] = useState('1h'), [now, setNow] = useState(Date.now())
 const key = ['private', session.user.id, 'project', project.id, 'monitor', assetId]
 const snapshot = useQuery({ queryKey: key, queryFn: async ({ signal }) => { const value = await monitorSnapshot(project.id, assetId, cursor.current, signal); cursor.current = value.cursor; return value }, enabled: visible, retry: false, refetchOnWindowFocus: false, refetchInterval: visible ? 30000 : false })
 const trend = useQuery({ queryKey: [...key, 'trend', selected?.sample.key, selected?.sample.entity, selected?.source, selected?.method, selected?.origin, window], queryFn: ({ signal }) => monitorTrend(project.id, assetId, selected!, window, signal), enabled: visible && !!selected, retry: false, refetchOnWindowFocus: false })
 useExpireOnUnauthorized(snapshot.error); useExpireOnUnauthorized(trend.error)
 useEffect(() => { if (!visible) return; setNow(Date.now()); const timer = globalThis.setInterval(() => setNow(Date.now()), 15000); return () => globalThis.clearInterval(timer) }, [visible])
 const denied = [snapshot.error, trend.error].some(error => error instanceof ApiError && [401, 403, 404].includes(error.status))
 const data = snapshot.data
 function freshness(item: MonitorObservation) { return item.freshness === 'stopped' ? 'stopped' : item.freshness === 'stale' || Date.parse(item.sample.observed_at) + item.stale_after_seconds * 1000 <= now || Date.parse(item.received_at) + item.stale_after_seconds * 1000 <= now ? 'stale' : 'fresh' }
 return <section className="ui:grid ui:gap-3"><h3>{t('monitorDetail')}</h3><p>{t('monitorSourceHelp')}</p><ActionError error={snapshot.error} /><ActionError error={trend.error} />
  {!denied && <><Button variant="outline" disabled={snapshot.isFetching} onClick={() => { setNow(Date.now()); void snapshot.refetch(); if (selected) void trend.refetch() }}>{t('topologyRefresh')}</Button>
   {snapshot.isPending && <p role="status">{t('loadingRecords')}</p>}
   {data && <>
    {!data.collection_enabled && <p role="status">{t('monitorDisabled')}</p>}
    <p>{data.stage} · {t(`monitorAgent_${data.agent_state}`)}{data.last_heartbeat && <> · {t('monitorLastHeartbeat')}: {new Date(data.last_heartbeat).toLocaleString()} · {Math.max(0, Math.floor((now - Date.parse(data.last_heartbeat)) / 60000))}min</>}</p>
    <Table><TableHeader><TableRow><TableHead>{t('monitorMetric')}</TableHead><TableHead>{t('monitorSource')}</TableHead><TableHead>{t('monitorValue')}</TableHead><TableHead>{t('monitorFreshness')}</TableHead><TableHead>{t('monitorTrend')}</TableHead></TableRow></TableHeader><TableBody>{data.items.map(item => <TableRow key={`${item.source}:${item.method}:${item.sample.key}:${item.sample.entity}`}><TableCell>{item.sample.key}<span className="cell-subtitle">{item.sample.entity} · {item.dtype}</span></TableCell><TableCell>{item.source} / {item.method} / {item.origin}</TableCell><TableCell>{item.sample.quality === 'good' ? item.sample.value ?? item.sample.state : '—'} {item.unit}<span className="cell-subtitle">{item.sample.quality}</span></TableCell><TableCell>{t(`monitor_${freshness(item)}`)}<span className="cell-subtitle">{new Date(item.sample.observed_at).toLocaleString()} · TTL {item.stale_after_seconds}s</span></TableCell><TableCell><Button variant="outline" size="sm" onClick={() => setSelected(item)}>{t('monitorTrend')}</Button></TableCell></TableRow>)}</TableBody></Table>
    {!data.items.length && <p>{t('monitorNoSamples')}</p>}
    {!!data.gaps.length && <section><h4>{t('monitorGaps')}</h4>{data.gaps.map((gap, i) => <p key={i}>{gap.source}/{gap.method} · {new Date(gap.started_at).toLocaleString()}—{new Date(gap.ended_at).toLocaleString()} · {gap.reason}</p>)}</section>}
   </>}
   {selected && <section><h4>{selected.sample.key} · {selected.sample.entity} · {selected.source}/{selected.method}</h4><Label htmlFor="monitor-window">{t('monitorWindow')}</Label><NativeSelect id="monitor-window" value={window} onChange={event => setWindow(event.target.value)}>{['1h', '24h', '7d'].map(value => <NativeSelectOption key={value} value={value}>{value}</NativeSelectOption>)}</NativeSelect>
    {trend.isPending && <p role="status">{t('loadingRecords')}</p>}
    {trend.data && <><p>{t('monitorCoverageHelp')}</p><Table><TableHeader><TableRow><TableHead>{t('monitorTime')}</TableHead><TableHead>Min / Avg / Max ({trend.data.unit})</TableHead><TableHead>{t('mappingCoverage')}</TableHead><TableHead>up / down / unknown</TableHead></TableRow></TableHeader><TableBody>{trend.data.items.map(bucket => <TableRow key={bucket.at}><TableCell>{new Date(bucket.at).toLocaleString()}</TableCell><TableCell>{bucket.minimum ?? '—'} / {bucket.average ?? '—'} / {bucket.maximum ?? '—'}</TableCell><TableCell>{Math.round(bucket.coverage * 100)}% · {bucket.good_samples}/{bucket.expected_samples || '?'}</TableCell><TableCell>{bucket.up_samples} / {bucket.down_samples} / {bucket.unknown_samples}</TableCell></TableRow>)}</TableBody></Table></>}
   </section>}
  </>}
 </section>
}
