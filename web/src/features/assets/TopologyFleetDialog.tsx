import { useEffect, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Label } from '@/components/ui/label'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { ActionError, Pagination } from '../../components/Management'
import { useProject } from '../../components/ProjectWorkspace'
import { useExpireOnUnauthorized, useSession } from '../../lib/session'
import { listTopologyFleet } from '../../lib/topology'
import TopologyDialog from './TopologyDialog'

export default function TopologyFleetDialog({ onClose }: { onClose: () => void }) {
 const { t } = useTranslation(), { session } = useSession(), { project } = useProject()
 const [verdict, setVerdict] = useState(''), [page, setPage] = useState(1), [now, setNow] = useState(Date.now()), [target, setTarget] = useState<{ id: string; hostname: string } | null>(null)
 const query = useQuery({ queryKey: ['private', session.user.id, 'project', project.id, 'topology-fleet', verdict, page], queryFn: ({ signal }) => listTopologyFleet(project.id, verdict, page, signal), retry: false, refetchOnWindowFocus: false })
 useExpireOnUnauthorized(query.error)
 useEffect(() => { const deadlines = (query.data?.items ?? []).map(row => row.valid_until ? Date.parse(row.valid_until) : NaN).filter(value => Number.isFinite(value) && value > Date.now()); if (!deadlines.length) return; const timer = window.setTimeout(() => setNow(Date.now()), Math.min(2_147_483_647, Math.max(1, Math.min(...deadlines) - Date.now() + 1))); return () => window.clearTimeout(timer) }, [query.data, now])
 return <><Dialog open onOpenChange={open => { if (!open) onClose() }}><DialogContent className="ui:max-h-[90vh] ui:overflow-y-auto ui:sm:max-w-5xl"><DialogHeader><DialogTitle>{t('topologyFleetTitle')}</DialogTitle><DialogDescription>{t('topologyFleetHelp')}</DialogDescription></DialogHeader><ActionError error={query.error} />
  <Label htmlFor="topology-fleet-filter">{t('topologyEffective')}</Label><NativeSelect id="topology-fleet-filter" value={verdict} onChange={event => { setVerdict(event.target.value); setPage(1) }}><NativeSelectOption value="">{t('topologyFleetAll')}</NativeSelectOption>{['PASS', 'FAIL', 'BLOCKED', 'UNAVAILABLE'].map(value => <NativeSelectOption key={value} value={value}>{value === 'UNAVAILABLE' ? t('topologyFleetUncollected') : t(`precollect_${value}`)}</NativeSelectOption>)}</NativeSelect>
  <Button variant="outline" disabled={query.isFetching} onClick={() => { setNow(Date.now()); void query.refetch() }}>{t('topologyRefresh')}</Button>
  {query.isPending && <p role="status">{t('loadingRecords')}</p>}
  {query.data && !query.isError && <><Table><TableHeader><TableRow>{[t('topologyFleetHost'), 'SN', t('topologyHistorical'), t('topologyEffective'), t('topologyUntil')].map(label => <TableHead key={label}>{label}</TableHead>)}</TableRow></TableHeader><TableBody>{query.data.items.map(row => <TableRow key={row.asset_id}><TableCell><Button variant="link" onClick={() => setTarget({ id: row.asset_id, hostname: row.hostname })}>{row.hostname}</Button></TableCell><TableCell>{row.serial}</TableCell><TableCell>{row.historical_verdict ? t(`precollect_${row.historical_verdict}`) : '—'}</TableCell><TableCell>{row.effective_verdict === 'UNAVAILABLE' ? t('topologyFleetUncollected') : t(`precollect_${row.valid_until && Date.parse(row.valid_until) <= now ? 'BLOCKED' : row.effective_verdict}`)}{row.reason && <code className="cell-subtitle">{row.reason}</code>}</TableCell><TableCell>{row.valid_until ? new Date(row.valid_until).toLocaleString() : '—'}</TableCell></TableRow>)}</TableBody></Table>{!query.data.items.length && <p>{t('topologyFleetEmpty')}</p>}<Pagination offset={(page - 1) * query.data.page_size} limit={query.data.page_size} total={query.data.total} busy={query.isFetching} onChange={offset => setPage(Math.floor(offset / (query.data?.page_size ?? 20)) + 1)} /></>}
  <p className="field-hint">{t('topologyFleetExpiry')}</p><Button variant="outline" onClick={onClose}>{t('bmcClose')}</Button>
 </DialogContent></Dialog>{target && <TopologyDialog asset={target} onClose={() => { setTarget(null); void query.refetch() }} />}</>
}
