import { useEffect, useState } from 'react'
import { Link } from '@tanstack/react-router'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { ActionError, Pagination } from '../../components/Management'
import { useProject } from '../../components/ProjectWorkspace'
import { ApiError } from '../../lib/api'
import { portMonitorBase, portMonitorEvents, portMonitorSnapshot, type PortMonitorItem, type PortMonitorSnapshot } from '../../lib/port-monitor'
import { useSession, useExpireOnUnauthorized } from '../../lib/session'
import { useMonitorVisible } from './visibility'

const lampClass: Record<string, string> = { up: 'ui:bg-emerald-500', down: 'ui:bg-red-500', flapping: 'ui:bg-red-500 ui:motion-safe:animate-pulse', degraded: 'ui:bg-amber-500', disabled: 'ui:bg-slate-400', maintained: 'ui:bg-blue-500', unknown: 'ui:bg-slate-400' }

export default function PortPanel({ assetId }: { assetId: string }) {
 const { t } = useTranslation(), session = useSession(), project = useProject(), visible = useMonitorVisible(), queries = useQueryClient()
 const [port, setPort] = useState(''), [page, setPage] = useState(1), [connected, setConnected] = useState(false), [now, setNow] = useState(Date.now())
 const key = ['private', session.user.id, 'project', project.id, 'port-monitor', assetId]
 const snapshot = useQuery({ queryKey: key, queryFn: ({ signal }) => portMonitorSnapshot(project.id, assetId, signal), enabled: visible, retry: false, refetchOnWindowFocus: false })
 const events = useQuery({ queryKey: [...key, 'events', port, page], queryFn: ({ signal }) => portMonitorEvents(project.id, assetId, port, page, signal), enabled: visible && !!port, retry: false, refetchOnWindowFocus: false })
 useExpireOnUnauthorized(snapshot.error); useExpireOnUnauthorized(events.error)
 const denied = [snapshot.error, events.error].some(error => error instanceof ApiError && [401, 403, 404].includes(error.status))
 useEffect(() => {
  if (!visible || denied || !snapshot.isSuccess) return
  let active = true
  const stream = new EventSource(`/api/v1${portMonitorBase(project.id, assetId)}/stream`)
  stream.addEventListener('snapshot', event => {
   if (!active) return
   try {
    const value = JSON.parse((event as MessageEvent).data) as PortMonitorSnapshot
    if (value.project_id !== project.id || value.asset_id !== assetId || !Array.isArray(value.items)) throw new Error('scope')
    queries.setQueryData(key, value); setConnected(true); setNow(Date.now())
   } catch { setConnected(false); stream.close() }
  })
  stream.addEventListener('access_revoked', () => { stream.close(); setConnected(false); queries.removeQueries({ queryKey: key }); void snapshot.refetch() })
  stream.addEventListener('unavailable', () => { setConnected(false) })
  stream.onerror = () => { if (active) setConnected(false) }
  return () => { active = false; stream.close(); setConnected(false) }
 }, [visible, denied, snapshot.isSuccess, session.user.id, project.id, assetId, queries])
 useEffect(() => { if (!visible) return; setNow(Date.now()); const timer = globalThis.setInterval(() => setNow(Date.now()), 5000); return () => globalThis.clearInterval(timer) }, [visible])
 const data = snapshot.data, selected = data?.items.find(item => item.physical_port_id === port)
 function state(item: PortMonitorItem) {
  if (['disabled', 'maintained'].includes(item.state)) return item.state
  if (!data || now - Date.parse(data.as_of) > 30000) return 'unknown'
  if (item.state === 'up' && !item.observations.some(value => value.source === 'host' && value.sample.key === 'link.state' && value.sample.state === 'up' && Date.parse(value.sample.observed_at) + value.stale_after_seconds * 1000 > now && Date.parse(value.received_at) + value.stale_after_seconds * 1000 > now)) return 'unknown'
  return item.state
 }
 function open(item: PortMonitorItem) { setPort(item.physical_port_id); setPage(1) }
 return <section className="ui:grid ui:gap-3"><h3>{t('portMonitorTitle')}</h3><p>{t('portMonitorHelp')}</p><ActionError error={snapshot.error} /><ActionError error={events.error} />
  {!denied && <><div className="action-row"><Button variant="outline" disabled={snapshot.isFetching} onClick={() => { void snapshot.refetch(); if (port) void events.refetch() }}>{t('topologyRefresh')}</Button><span role="status">{t(connected ? 'portMonitorLive' : 'portMonitorReconnecting')}</span></div>
   {snapshot.isPending && <p role="status">{t('loadingRecords')}</p>}
   {data && <><p>{t('mappingTitle')}: {data.mapping_state} · <code>{data.mapping_revision_id || '—'}</code>{data.mapping_reason && <> · {data.mapping_reason}</>}</p><p>{t('portMonitorSummary', data.summary)}</p>
    <div className="ui:flex ui:flex-wrap ui:gap-2" aria-label={t('portMonitorPorts')}>{data.items.map(item => <Button key={item.physical_port_id} variant={port === item.physical_port_id ? 'default' : 'outline'} onClick={() => open(item)} aria-label={`${item.plan.name || item.plan.port_index || item.physical_port_id}: ${t(`portMonitorState_${state(item)}`)}`}><span aria-hidden="true" className={`ui:inline-block ui:size-3 ui:rounded-full ${lampClass[state(item)] || lampClass.unknown}`} />{item.plan.name || item.plan.port_index || item.physical_port_id.slice(0, 8)} · {t(`portMonitorState_${state(item)}`)}</Button>)}</div>
    {!data.items.length && <p>{t('portMonitorNoPorts')}</p>}
    <Table><TableHeader><TableRow><TableHead>{t('wiringPort')}</TableHead><TableHead>{t('monitorState')}</TableHead><TableHead>OS / MAC / BDF</TableHead><TableHead>{t('mappingSwitchPort')}</TableHead><TableHead>{t('monitorIssue')}</TableHead></TableRow></TableHeader><TableBody>{data.items.map(item => <TableRow key={item.physical_port_id}><TableCell><Button variant="link" onClick={() => open(item)}>{item.plan.name || item.plan.port_index || '—'}</Button><code className="cell-subtitle">{item.physical_port_id}</code><span className="cell-subtitle">{item.plan.role || '—'} · Rail {item.plan.rail || '—'} · {t('portMonitorEvents')}: {item.event_count}</span></TableCell><TableCell>{t(`portMonitorState_${state(item)}`)}<span className="cell-subtitle">{item.reason}</span></TableCell><TableCell>{item.binding?.observed.interface || '—'}<code className="cell-subtitle">{item.binding?.observed.permanent_mac}</code><code className="cell-subtitle">{item.binding?.observed.bdf}</code></TableCell><TableCell>{item.binding?.observed.switch_serial || '—'} / {item.binding?.observed.switch_port || '—'}<code className="cell-subtitle">{item.binding?.switch_port_id}</code></TableCell><TableCell>{item.open_issue_ids.map(id => <Link key={id} to="/issues" search={{ project_id: project.id, issue_id: id }} className="cell-subtitle">{id.slice(0, 8)}</Link>)}</TableCell></TableRow>)}</TableBody></Table>
   </>}
   {selected && <section className="ui:grid ui:gap-3"><h4>{selected.plan.name || selected.physical_port_id}</h4><p>{t('portMonitorSourceHelp')}</p><Table><TableHeader><TableRow><TableHead>{t('monitorMetric')}</TableHead><TableHead>{t('monitorSource')}</TableHead><TableHead>{t('monitorValue')}</TableHead><TableHead>{t('monitorFreshness')}</TableHead></TableRow></TableHeader><TableBody>{selected.observations.map(item => <TableRow key={`${item.source}:${item.method}:${item.sample.key}:${item.sample.entity}`}><TableCell>{item.sample.key}</TableCell><TableCell>{item.source}/{item.method}/{item.origin}</TableCell><TableCell>{item.sample.quality === 'good' ? item.sample.value ?? item.sample.state : '—'} {item.unit}</TableCell><TableCell>{item.freshness} · {new Date(item.sample.observed_at).toLocaleString()} · TTL {item.stale_after_seconds}s</TableCell></TableRow>)}</TableBody></Table>
    <h4>{t('portMonitorEvents')}</h4>{events.isPending && <p role="status">{t('loadingRecords')}</p>}{events.data && <><Table><TableHeader><TableRow><TableHead>{t('monitorTime')}</TableHead><TableHead>{t('monitorSource')}</TableHead><TableHead>{t('monitorState')}</TableHead><TableHead>{t('monitorMetric')}</TableHead></TableRow></TableHeader><TableBody>{events.data.items.map(item => <TableRow key={item.id}><TableCell>{new Date(item.observed_at).toLocaleString()}</TableCell><TableCell>{item.source}/{item.method}/{item.origin}</TableCell><TableCell>{item.severity} · {item.state}</TableCell><TableCell>{item.rule_id}<code className="cell-subtitle">{item.mapping_revision_id}</code></TableCell></TableRow>)}</TableBody></Table><Pagination offset={(page - 1) * events.data.page_size} limit={events.data.page_size} total={events.data.total} busy={events.isFetching} onChange={offset => setPage(Math.floor(offset / events.data!.page_size) + 1)} /></>}
   </section>}
  </>}
 </section>
}
