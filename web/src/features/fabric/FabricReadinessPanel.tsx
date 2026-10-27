import {useEffect,useState} from 'react'
import {Link} from '@tanstack/react-router'
import {useQuery} from '@tanstack/react-query'
import {useTranslation} from 'react-i18next'
import {Button} from '@/components/ui/button'
import {Table,TableHeader,TableHead,TableBody,TableRow,TableCell} from '@/components/ui/table'
import {ActionError} from '../../components/Management'
import {useSession,useExpireOnUnauthorized} from '../../lib/session'
import {networkReadiness,networkCables} from '../../lib/fabric'

export function FabricReadinessPanel({projectId}:{projectId:string}){
 const session=useSession(),{t}=useTranslation(),[page,setPage]=useState(1),[now,setNow]=useState(()=>Date.now())
 const readiness=useQuery({queryKey:['private',session.user.id,projectId,'fabric-readiness'],queryFn:({signal})=>networkReadiness(projectId,signal),retry:false,refetchOnWindowFocus:false})
 const cables=useQuery({queryKey:['private',session.user.id,projectId,'fabric-cables',page],queryFn:({signal})=>networkCables(projectId,page,signal),retry:false,refetchOnWindowFocus:false})
 useExpireOnUnauthorized(readiness.error);useExpireOnUnauthorized(cables.error)
 // Expiry display is local. It does not poll expensive server projections or
 // manufacture a new readiness proof every time the clock advances.
 useEffect(()=>{const timer=setInterval(()=>setNow(Date.now()),1000);return()=>clearInterval(timer)},[])
 const expired=!readiness.data||Date.parse(readiness.data.valid_until)<=now
 const state=(value:string)=>t(value==='PASS'?'fabricPrecheckPass':value==='FAIL'?'fabricAnomaly':value==='WARN'?'fabricWarning':'fabricPending')
 return <section className="panel">
  <header className="management-heading"><div><h2>{t('fabricReadiness')}</h2><p>{t('fabricReadinessHint')}</p></div><Button variant="outline" disabled={readiness.isFetching} onClick={()=>void readiness.refetch()}>{t('fabricRefreshReadiness')}</Button></header>
  <ActionError error={readiness.error}/>
  {readiness.data&&<><p role="status">{expired?t('fabricReadinessExpired'):t('fabricReadinessCurrent')} · {readiness.data.ttl_seconds}s · {new Date(readiness.data.readiness_at).toLocaleTimeString()}</p><dl className="ui:grid ui:grid-cols-[auto_1fr] ui:gap-x-4 ui:gap-y-2">{readiness.data.gates.map(g=><div key={g.id} className="ui:contents"><dt>{t('fabricGate_'+g.id)}</dt><dd>{expired&&g.state==='PASS'?t('fabricPending'):g.state==='PASS'?t('fabricGatePass'):state(g.state)}{g.state!=='PASS'&&<p>{t('fabricGateHelp_'+g.id)}</p>}</dd></div>)}</dl></>}
  <header className="management-heading"><div><h2>{t('fabricCurrentCables')}</h2><p>{t('fabricTargetedScopeHint')}</p></div><Button variant="outline" disabled={cables.isFetching} onClick={()=>void cables.refetch()}>{t('fabricRefreshCables')}</Button></header>
  <ActionError error={cables.error}/>
  {cables.isSuccess&&!cables.data.items.length&&<p>{t('fabricNoCable')}</p>}
  <Table><TableHeader><TableRow><TableHead>{t('fabricCable')}</TableHead><TableHead>{t('fabricObserved')}</TableHead><TableHead>{t('fabricSource')}</TableHead><TableHead>{t('fabricLastSample')}</TableHead><TableHead>{t('fabricEvidenceLinks')}</TableHead></TableRow></TableHeader><TableBody>{cables.data?.items.map(c=><TableRow key={c.cable_id}><TableCell><code>{c.cable_id}</code></TableCell><TableCell>{state(Date.parse(c.valid_until)<=now?'BLOCKED':c.gate.state)}<p>{c.rule_ref}</p></TableCell><TableCell>{t(c.evidence_scope==='targeted_link'?'fabricTargetedLink':'fabricSnapshot')} · {t(c.evidence_source==='device'?'fabricDeviceSource':c.evidence_source==='protocol_fixture'?'fabricFixture':'fabricUnknown')}</TableCell><TableCell>{c.collected_at?new Date(c.collected_at).toLocaleString():t('fabricUnknown')}</TableCell><TableCell>{c.issue_id&&<Link to="/issues" search={{project_id:projectId,issue_id:c.issue_id}}>{t('fabricRelatedIssue')}</Link>}{c.execution_id&&<><br/><Link to="/p/$projectId/executions" params={{projectId}} search={{execution:c.execution_id}}>{t('fabricRelatedExecution')}</Link></>}</TableCell></TableRow>)}</TableBody></Table>
  <nav className="pagination" aria-label={t('fabricCurrentCables')}><Button variant="outline" disabled={page===1||cables.isFetching} onClick={()=>setPage(page-1)}>{t('previousPage')}</Button><span>{page} · {cables.data?.total??0}</span><Button variant="outline" disabled={!cables.data||page*20>=cables.data.total||cables.isFetching} onClick={()=>setPage(page+1)}>{t('nextPage')}</Button></nav>
 </section>
}
