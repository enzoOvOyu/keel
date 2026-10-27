import {useState} from 'react'
import {useQuery,useQueryClient} from '@tanstack/react-query'
import {Link} from '@tanstack/react-router'
import {useTranslation} from 'react-i18next'
import {Button} from '@/components/ui/button'
import {Table,TableHeader,TableHead,TableBody,TableRow,TableCell} from '@/components/ui/table'
import {useSession,useExpireOnUnauthorized} from '../../lib/session'
import {useProject} from '../../components/ProjectWorkspace'
import {useAction} from '../../lib/management'
import {ActionDialog,ActionError} from '../../components/Management'
import {networkTemplateChanges,type NetworkTemplateChange} from '../../lib/network-templates'
import {createNetworkPush} from '../../lib/network-push'

export function TemplateChanges(){
 const p=useProject(),session=useSession(),{t}=useTranslation(),client=useQueryClient(),action=useAction()
 const [page,setPage]=useState(1),[selected,setSelected]=useState<{projectId:string;change:NetworkTemplateChange}|null>(null)
 const key=['private',session.user.id,p.id,'network-template-changes']
 const query=useQuery({queryKey:[...key,page],queryFn:({signal})=>networkTemplateChanges(p.id,page,signal),retry:false,refetchOnWindowFocus:false})
 useExpireOnUnauthorized(query.error)
 const canPlan=session.user.is_admin||p.role==='NDE'
 async function create(){if(!selected||selected.projectId!==p.id)return;const frozen=selected;const result=await action.run(()=>createNetworkPush(frozen.projectId,frozen.change.plan_input,session.csrf_token));if(result.ok){setSelected(null);await Promise.all([client.invalidateQueries({queryKey:key}),client.invalidateQueries({queryKey:['private',session.user.id,p.id,'network-push-jobs']})])}}
 return <section className="panel" aria-label={t('networkTemplateChanges')}><header className="management-heading"><div><h2>{t('networkTemplateChanges')}</h2><p>{t('networkTemplateChangesBoundary')}</p></div><Button variant="outline" disabled={query.isFetching||action.busy} onClick={()=>void query.refetch()}>{t('refreshProjects')}</Button></header><ActionError error={query.error??action.error}/>
 {query.data&&!query.data.total&&<p>{t('networkTemplateChangesEmpty')}</p>}
 <Table><TableHeader><TableRow>{['networkTemplateImpactDevice','networkTemplateName','networkTemplateBatch','networkTemplateImpactVerdict','networkTemplateImpactPaths','actions'].map(key=><TableHead key={key}>{t(key)}</TableHead>)}</TableRow></TableHeader><TableBody>{query.data?.items.map(item=><TableRow key={item.id}><TableCell>{item.assessment.serial}{item.assessment.evidence_source==='protocol_fixture'&&<small>{t('networkTemplateSourceFixture')}</small>}</TableCell><TableCell>{item.candidate_name} v{item.candidate_version}</TableCell><TableCell><code>{item.assessment.batch_ref}</code></TableCell><TableCell>{t('networkTemplateChange_'+item.state)}{item.assessment.reason&&<details><summary>{t('networkTemplateImpactBlocked')}</summary><code>{item.assessment.reason}</code></details>}</TableCell><TableCell>{item.assessment.changes.map(change=><p key={change.path}><code>{change.path}</code> · {change.severity}</p>)}</TableCell><TableCell>{item.state==='DIFF'&&<Button variant="outline" disabled={!canPlan||action.busy} onClick={()=>setSelected({projectId:p.id,change:item})}>{t('networkTemplateChangeCreateJob')}</Button>}{item.linked_job_id&&<p><a href="#network-push-jobs">{t('networkTemplateChangeLinkedJob')}: <code>{item.linked_job_id}</code></a></p>}<Link to="/p/$projectId/executions" params={{projectId:p.id}} search={{execution:item.source_execution_id}}>{t('networkTemplateImpactExecution')}</Link></TableCell></TableRow>)}</TableBody></Table>
 <nav className="pagination" aria-label={t('networkTemplateChanges')}><Button variant="outline" disabled={page===1||query.isFetching} onClick={()=>setPage(page-1)}>{t('previousPage')}</Button><span>{page} · {query.data?.total??0}</span><Button variant="outline" disabled={!query.data||page*20>=query.data.total||query.isFetching} onClick={()=>setPage(page+1)}>{t('nextPage')}</Button></nav>
 {selected&&<ActionDialog title={t('networkTemplateChangeCreateJob')} description={t('networkTemplateChangeCreateConfirm',{name:selected.change.candidate_name,version:selected.change.candidate_version,batch:selected.change.assessment.batch_ref,count:selected.change.assessment.target_count})} confirm={t('networkTemplateChangeCreateJob')} busy={action.busy} submitDisabled={selected.projectId!==p.id} onClose={()=>setSelected(null)} onSubmit={()=>void create()}><p>{t('networkTemplateChangeCreateBoundary')}</p><ActionError error={action.error}/></ActionDialog>}</section>
}
