import { useEffect, useState, type FormEvent } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { ActionError, Pagination } from '../../components/Management'
import { useProject } from '../../components/ProjectWorkspace'
import { ApiError } from '../../lib/api'
import { mappingFleet } from '../../lib/mapping'
import { useExpireOnUnauthorized, useSession } from '../../lib/session'
import MappingDialog from './MappingDialog'

export default function MappingFleetDialog({ onClose }: { onClose: () => void }) {
 const { t } = useTranslation(), project = useProject(), session = useSession()
 const [rail, setRail] = useState(''), [draftRail, setDraftRail] = useState(''), [page, setPage] = useState(1), [now, setNow] = useState(Date.now()), [selected, setSelected] = useState<{ id: string; hostname: string; assessment: string } | null>(null)
 const query = useQuery({ queryKey: ['private', session.user.id, 'project', project.id, 'mapping-fleet', rail, page], queryFn: ({ signal }) => mappingFleet(project.id, rail, page, signal), retry: false, refetchOnWindowFocus: false })
 useExpireOnUnauthorized(query.error)
 const denied = query.error instanceof ApiError && [401, 403, 404].includes(query.error.status)
 useEffect(() => {
  const dates = query.data?.items.map(item => item.revision?.candidate.valid_until).map(value => value ? Date.parse(value) : NaN).filter(value => Number.isFinite(value) && value > Date.now()) ?? []
  if (!dates.length) return
  const timer = window.setTimeout(() => setNow(Date.now()), Math.min(2_147_483_647, Math.max(1, Math.min(...dates) - Date.now() + 1)))
  return () => window.clearTimeout(timer)
 }, [query.data, now])
 function filter(event: FormEvent<HTMLFormElement>) { event.preventDefault(); setPage(1); setRail(draftRail.trim()) }
 return <><Dialog open onOpenChange={open => { if (!open) onClose() }}><DialogContent className="ui:max-h-[90vh] ui:overflow-y-auto ui:sm:max-w-6xl"><DialogHeader><DialogTitle>{t('mappingFleetTitle')}</DialogTitle><DialogDescription>{t('mappingFleetHelp')}</DialogDescription></DialogHeader>
  <ActionError error={query.error} />
  {!denied && <>
   <form onSubmit={filter} className="action-row"><Label htmlFor="mapping-fleet-rail">Rail</Label><Input id="mapping-fleet-rail" value={draftRail} maxLength={64} onChange={event => setDraftRail(event.target.value)} /><Button type="submit" disabled={query.isFetching}>{t('mappingFilterRail')}</Button><Button type="button" variant="outline" onClick={() => { setRail(''); setDraftRail(''); setPage(1) }}>{t('mappingAllServers')}</Button><Button type="button" variant="outline" disabled={query.isFetching} onClick={() => { setNow(Date.now()); void query.refetch() }}>{t('topologyRefresh')}</Button></form>
   {query.isPending && <p role="status">{t('loadingRecords')}</p>}
   {query.data && <><p className="field-hint">{new Date(query.data.as_of).toLocaleString()} · {query.data.total} · {rail || t('mappingAllServers')}</p><Table><TableHeader><TableRow><TableHead>{t('bomHostname')}</TableHead><TableHead>{t('mappingPublished')}</TableHead><TableHead>{t('mappingCoverage')}</TableHead><TableHead>{t('mappingViewDetail')}</TableHead></TableRow></TableHeader><TableBody>{query.data.items.map(item => {
    const revision = item.revision, expired = !!revision?.candidate.valid_until && Date.parse(revision.candidate.valid_until) <= now
    return <TableRow key={item.asset_id}><TableCell>{item.hostname}<span className="cell-subtitle">{item.serial}</span></TableCell><TableCell>{revision ? <>{`v${revision.version}`} · {t(`precollect_${expired ? 'BLOCKED' : revision.effective_verdict}`)}<code className="cell-subtitle">{revision.id}</code>{revision.invalidation_reason && <code>{revision.invalidation_reason}</code>}</> : t('mappingNotBuilt')}</TableCell><TableCell>{revision && <>GPU {revision.candidate.coverage.mapped_gpus}/{revision.candidate.coverage.required_gpus} · NIC {revision.candidate.coverage.mapped_nics}/{revision.candidate.coverage.required_nics} · {revision.candidate.coverage.mapped_ports}/{revision.candidate.coverage.required_ports}</>}</TableCell><TableCell><Button variant="outline" size="sm" onClick={() => setSelected({ id: item.asset_id, hostname: item.hostname, assessment: revision?.assessment_id ?? '' })}>{t('mappingViewDetail')}</Button>{revision && <details><summary>{t('mappingAllPorts')}</summary>{revision.candidate.bindings.filter(binding => !rail || binding.plan.rail === rail).map(binding => <p key={binding.plan.physical_port_id}>{binding.logical_name} · Rail {binding.plan.rail || '—'} · {binding.observed.interface} → {binding.observed.switch_serial}/{binding.observed.switch_port}<code className="cell-subtitle">{binding.plan.physical_port_id}</code></p>)}</details>}</TableCell></TableRow>
   })}</TableBody></Table>{!query.data.items.length && <p>{t('mappingNoFleetRows')}</p>}<Pagination offset={(page - 1) * query.data.page_size} limit={query.data.page_size} total={query.data.total} busy={query.isFetching} onChange={offset => setPage(Math.floor(offset / query.data!.page_size) + 1)} /></>}
  </>}
  <Button variant="outline" onClick={onClose}>{t('bmcClose')}</Button>
 </DialogContent></Dialog>{selected && <MappingDialog key={`${project.id}:${selected.id}:${selected.assessment}`} asset={selected} initialAssessmentId={selected.assessment} onClose={() => setSelected(null)} />}</>
}
