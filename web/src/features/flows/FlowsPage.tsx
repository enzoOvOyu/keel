import {ControlFlowLaunchDialog} from './ControlFlowLaunchDialog'
import {ApiError} from '../../lib/api'
import {HostFlowLaunchDialog} from './HostFlowLaunchDialog'
import { Label } from '@/components/ui/label'
import { NativeCheckbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Button } from '@/components/ui/button'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Textarea } from '@/components/ui/textarea'
import { CatalogInputs } from '../execution/CatalogInputs'
import type { CatalogValues } from '../../lib/catalog'
import {lazy,Suspense,useEffect,useRef,useState,type FormEvent} from 'react'
import {Link,useBlocker} from '@tanstack/react-router'
import {useQuery,useQueryClient} from '@tanstack/react-query'
import {useTranslation} from 'react-i18next'
import {ProjectWorkspace,useProject} from '../../components/ProjectWorkspace'
import {ActionDialog,ActionError,Pagination,SuccessNotice} from '../../components/Management'
import {useAction} from '../../lib/management'
import {useSession,useExpireOnUnauthorized} from '../../lib/session'
import {createExecution,listFlows,listSimTargets,publishFlow,type FlowVersion} from '../../lib/execution'
import {getFlowDiff,listFlowVersions,publishFlowVersion,rollbackFlow,validateFlow,type FlowSearch,type FlowValidation} from '../../lib/flow-workbench'
import '../../styles/flow-workbench.css'
const SourceEditor=lazy(()=>import('./FlowEditor').then(m=>({default:m.FlowEditor}))),DiffEditor=lazy(()=>import('./FlowEditor').then(m=>({default:m.FlowDiffEditor})))
const sample=`api_version: keel.sim/v1
inputs:
  profile:
    type: enum
    required: true
    enum: [smoke, full]
    default: smoke
tasks:
  - id: collect
    type: sim.read
    retry: 0
    idempotent: true
    timeout_ms: 1000
    resources: [machine]
    sim: {outcome: success, duration_ms: 100, seed: 42}
  - id: verify
    type: sim.read
    depends_on: [collect]
    retry: 0
    idempotent: true
    timeout_ms: 1000
    resources: [machine]
    sim: {outcome: success, duration_ms: 100, seed: 42}
`
const source=(flow:FlowVersion)=>flow.yaml_source||JSON.stringify(flow.definition,null,2)
function useKey(){const ref=useRef<{payload:string;key:string}|null>(null);return(body:unknown)=>{const payload=JSON.stringify(body);if(ref.current?.payload!==payload)ref.current={payload,key:crypto.randomUUID()};return ref.current.key}}
function LaunchDialog({flow,onClose,onDone}:{flow:FlowVersion;onClose:()=>void;onDone:(id:string)=>void}){
 const {t}=useTranslation(),session=useSession(),action=useAction(),keyFor=useKey()
 const [page,setPage]=useState(1),[ids,setIds]=useState<string[]>([]),[concurrency,setConcurrency]=useState(16),[breaker,setBreaker]=useState(''),[parameters,setParameters]=useState<CatalogValues>(()=>Object.fromEntries(Object.entries(flow.definition.inputs).filter(([,v])=>v.default!==undefined).map(([key,v])=>[key,v.default!])))
 const [catalogReady,setCatalogReady]=useState(false)
 const targets=useQuery({queryKey:['private',session.user.id,'project',flow.project_id,'flow-targets',page],queryFn:({signal})=>listSimTargets(flow.project_id,page,signal,100)})
 useExpireOnUnauthorized(targets.error)
 async function submit(e:FormEvent){e.preventDefault();if(!catalogReady)return;const body={mode:'sim' as const,flow_version_id:flow.id,target_ids:ids,parameters:Object.fromEntries(Object.entries(parameters).filter((pair):pair is [string,string|number|boolean]=>['string','number','boolean'].includes(typeof pair[1]))),concurrency,circuit_breaker_percent:Number(breaker)};const result=await action.run(()=>createExecution(flow.project_id,body,session.csrf_token,keyFor(body)));if(result.ok)onDone(result.value.id)}
 return <ActionDialog title={t('flowLaunch')} description={`${flow.name} · v${flow.version} · ${flow.project_id}`} busy={action.busy} confirm={t('startExecution')} submitDisabled={!ids.length||!catalogReady} onClose={onClose} onSubmit={e=>void submit(e)}><ActionError error={action.error??targets.error}/><p className="impact-notice">{t('flowSimNotice')}</p><p>{t('flowExplicitTargets',{count:ids.length})}</p><div className="flow-target-list">{targets.data?.items.map(target=><Label key={target.id}><NativeCheckbox checked={ids.includes(target.id)} onChange={e=>setIds(current=>e.target.checked?[...current,target.id]:current.filter(id=>id!==target.id))}/>{target.name}</Label>)}</div>{targets.data&&<Pagination offset={(page-1)*100} limit={100} total={targets.data.total} busy={targets.isFetching} onChange={offset=>setPage(offset/100+1)}/>}
 <CatalogInputs projectId={flow.project_id} flow={flow} values={parameters} disabled={action.busy} onChange={setParameters} onReady={setCatalogReady} />
 <div className="field"><Label className="ui:grid ui:gap-1.5" htmlFor="flow-concurrency">{t('concurrency')}</Label><Input className="ui:h-11" id="flow-concurrency" type="number" min={1} max={256} value={concurrency} onChange={e=>setConcurrency(Number(e.target.value))} required/></div><div className="field"><Label className="ui:grid ui:gap-1.5" htmlFor="flow-breaker">{t('circuitBreaker')}</Label><Input className="ui:h-11" id="flow-breaker" type="number" min={1} max={100} value={breaker} aria-describedby="flow-breaker-help" onChange={e=>setBreaker(e.target.value)}/></div><p id="flow-breaker-help">{t('flowBreakerDefault')}</p></ActionDialog>
}
function FlowContent({search,onSearch}:{search:FlowSearch;onSearch:(value:Partial<FlowSearch>)=>void}){
 const project=useProject(),session=useSession(),{t}=useTranslation(),client=useQueryClient(),action=useAction(),keyFor=useKey()
 const canPublish=session.user.is_admin||['Admin','SRE'].includes(project.role),canLaunch=session.user.is_admin||['Admin','SRE','DE','DL'].includes(project.role)
 const page=search.page??1,versionPage=search.versionPage??1,tab=search.tab??'overview'
 const flows=useQuery({queryKey:['private',session.user.id,'project',project.id,'flow-workbench',page],queryFn:({signal})=>listFlows(project.id,page,signal)})
 const flowId=search.flow??flows.data?.items[0]?.flow_id??''
 const versions=useQuery({queryKey:['private',session.user.id,'project',project.id,'flow-versions',flowId,versionPage],queryFn:({signal})=>listFlowVersions(project.id,flowId,versionPage,signal),enabled:Boolean(flowId)})
 const head=useQuery({queryKey:['private',session.user.id,'project',project.id,'flow-versions',flowId,1],queryFn:({signal})=>listFlowVersions(project.id,flowId,1,signal),enabled:Boolean(flowId)})
 const latest=flows.data?.items.find(flow=>flow.flow_id===flowId)??head.data?.items[0]
 const [newFlow,setNewFlow]=useState(false),[name,setName]=useState(''),[draft,setDraft]=useState(sample),[initial,setInitial]=useState(sample),[validation,setValidation]=useState<FlowValidation|null>(null),[confirm,setConfirm]=useState<'publish'|'rollback'|null>(null),[reason,setReason]=useState(''),[launch,setLaunch]=useState(false),[success,setSuccess]=useState(''),[executionId,setExecutionId]=useState('')
 const [diagnosticSource,setDiagnosticSource]=useState('')
 const errorLine=!action.busy&&diagnosticSource===draft&&action.error instanceof ApiError?Number(action.error.details.find(v=>v.startsWith('workflow_yaml_line:'))?.split(':')[1])||undefined:undefined
 const dirty=draft!==initial
 const editable=newFlow||['keel.sim/v1','keel.host/v1','keel.control/v1'].includes(latest?.definition.api_version??'')
 useEffect(()=>{if(!newFlow&&latest){setDraft(source(latest));setInitial(source(latest));setName(latest.name);setValidation(null)}},[latest?.id,newFlow])
 useBlocker({shouldBlockFn:({current,next})=>current.pathname!==next.pathname&&dirty&&!window.confirm(t('flowDiscard')),enableBeforeUnload:()=>dirty})
 useEffect(()=>{if(!dirty)return;const listener=(event:BeforeUnloadEvent)=>{event.preventDefault();event.returnValue=''};window.addEventListener('beforeunload',listener);return()=>window.removeEventListener('beforeunload',listener)},[dirty])
 const left=search.left??versions.data?.items[1]?.id??versions.data?.items[0]?.id??'',right=search.right??versions.data?.items[0]?.id??''
 const diff=useQuery({queryKey:['private',session.user.id,'project',project.id,'flow-diff',flowId,left,right],queryFn:({signal})=>getFlowDiff(project.id,flowId,left,right,signal),enabled:tab==='revisions'&&Boolean(left&&right)})
 useExpireOnUnauthorized(flows.error??versions.error??diff.error)
 async function refresh(flow?:FlowVersion){await client.invalidateQueries({queryKey:['private',session.user.id,'project',project.id]});if(flow){setNewFlow(false);setDraft(source(flow));setInitial(source(flow));setName(flow.name);onSearch({flow:flow.flow_id,versionPage:1,right:undefined,left:undefined})}}
 async function check(){setDiagnosticSource(draft);const result=await action.run(()=>validateFlow(project.id,draft,{},session.csrf_token));if(result.ok)setValidation(result.value)}
 async function publish(e:FormEvent){e.preventDefault();setDiagnosticSource(draft);const body={name:name.trim(),yaml_source:draft,expected_version:newFlow?0:latest?.version??0};const result=await action.run(()=>newFlow?publishFlow(project.id,body,session.csrf_token,keyFor(body)):publishFlowVersion(project.id,flowId,body,session.csrf_token,keyFor(body)));if(result.ok){setConfirm(null);setSuccess(t('flowPublished',{version:result.value.version}));await refresh(result.value)}}
 async function rollback(e:FormEvent){e.preventDefault();if(!latest||!left)return;const body={source_version_id:left,expected_version:latest.version,change_reason:reason};const result=await action.run(()=>rollbackFlow(project.id,flowId,body,session.csrf_token,keyFor(body)));if(result.ok){setConfirm(null);setReason('');setSuccess(t('flowRolledBack',{version:result.value.version}));await refresh(result.value)}}
 function choose(flow:string){if(dirty&&!window.confirm(t('flowDiscard')))return;setNewFlow(false);setSuccess('');setInitial(draft);onSearch({flow,versionPage:1,left:undefined,right:undefined})}
 function controlSample(){if(dirty&&!window.confirm(t('flowDiscard')))return;setNewFlow(true);setName('');setDraft(`api_version: keel.control/v1
inputs: {}
tasks:
  - id: remember
    type: kv.put
    retry: 0
    idempotent: false
    timeout_ms: 2000
    resources: [project]
    kv:
      key: delivery.note
      value: "Ready for review"
      expected_version: 0
  - id: inspect
    type: kv.get
    depends_on: [remember]
    retry: 0
    idempotent: true
    timeout_ms: 2000
    resources: [project]
    kv:
      key: delivery.note
`);setInitial('');setValidation(null)}
 function hostSample(){if(dirty&&!window.confirm(t('flowDiscard')))return;setNewFlow(true);setName('');const value=`api_version: keel.host/v1
inputs: {}
tasks:
  - id: identify
    type: host.identity
    retry: 0
    idempotent: true
    timeout_ms: 5000
    resources: [device]
  - id: wait
    type: host.wait
    depends_on: [identify]
    retry: 0
    idempotent: true
    timeout_ms: 5000
    wait_ms: 1000
    resources: [device]
`;setDraft(value);setInitial(value);setValidation(null);setSuccess('');onSearch({tab:'source'})}
 function create(){if(dirty&&!window.confirm(t('flowDiscard')))return;setNewFlow(true);setName('');setDraft(sample);setInitial(sample);setValidation(null);setSuccess('');onSearch({tab:'source'})}
 return <div className="flow-workbench"><header><div><h1>{t('flowWorkbenchTitle')}</h1><p>{t('flowWorkbenchIntro')}</p></div><div className="flow-actions"><Link className="secondary-button" to="/p/$projectId/executions" params={{projectId:project.id}}>{t('executionsTab')}</Link>{canPublish&&<Button variant="outline" onClick={controlSample}>{t('flowNewControl')}</Button>}{canPublish&&<Button variant="outline" onClick={hostSample}>{t('flowNewHost')}</Button>}{canPublish&&<Button variant="default" className="primary-button ui:min-h-11" onClick={create}>{t('flowNew')}</Button>}</div></header><p className="impact-notice">{t('flowWorkbenchNotice')}</p>{success&&<SuccessNotice>{success}</SuccessNotice>}{executionId&&<Link to="/p/$projectId/executions" params={{projectId:project.id}} search={{execution:executionId}}>{t('viewExecution')} · {executionId}</Link>}<ActionError error={flows.error??versions.error??diff.error??action.error}/>
 <div className="flow-columns"><aside className="panel flow-list" aria-label={t('flowWorkbenchTitle')}>{flows.isPending&&<p role="status">{t('loadingRecords')}</p>}{flows.data?.items.map(flow=><Button variant={!newFlow&&flow.flow_id===flowId?'secondary':'outline'} aria-pressed={!newFlow&&flow.flow_id===flowId} className={('secondary-button '+(!newFlow&&flow.flow_id===flowId?'flow-selected':'')) + " ui:h-auto ui:min-h-11 ui:whitespace-normal ui:justify-start ui:text-left"} key={flow.flow_id} onClick={()=>choose(flow.flow_id)}><span>{flow.name}<br/>v{flow.version} · {flow.definition.api_version}</span></Button>)}{flows.data&&!flows.data.items.length&&<p>{t('noFlows')}</p>}{flows.data&&<Pagination offset={(page-1)*20} limit={20} total={flows.data.total} busy={flows.isFetching} onChange={offset=>onSearch({page:offset/20+1})}/>}</aside>
 <div className="flow-detail">{(newFlow||latest)&&<><div className="flow-actions"><h2>{newFlow?t('flowNew'):latest?.name}</h2>{latest&&!newFlow&&<span>v{latest.version}</span>}{!newFlow&&latest&&canLaunch&&editable&&<Button variant="default" className="primary-button ui:min-h-11" onClick={()=>setLaunch(true)}>{t('flowLaunch')}</Button>}</div>
 <div className="flow-tabs" role="tablist" aria-label={t('flowViews')}>{(['overview','source','revisions'] as const).filter(value=>!newFlow||value==='source').map(value=><Button className="ui:h-auto ui:rounded-none ui:border-0 ui:border-b-2 ui:border-transparent ui:bg-transparent ui:px-4 ui:py-3 ui:text-muted-foreground ui:aria-selected:border-primary ui:aria-selected:text-primary" variant="ghost" role="tab" id={'flow-tab-'+value} aria-controls="flow-tab-panel" aria-selected={tab===value} tabIndex={tab===value?0:-1} key={value} onClick={()=>onSearch({tab:value})} onKeyDown={event=>{if(event.key==='ArrowRight'||event.key==='ArrowLeft'){event.preventDefault();const tabs=['overview','source','revisions'] as const,index=tabs.indexOf(tab),next=tabs[(index+(event.key==='ArrowRight'?1:2))%3];onSearch({tab:next});requestAnimationFrame(()=>document.getElementById('flow-tab-'+next)?.focus())}}}>{t('flowTab_'+value)}</Button>)}</div>
 <section className="panel" id="flow-tab-panel" role="tabpanel" aria-labelledby={'flow-tab-'+tab}>
 {tab==='overview'&&latest&&<><h3>{t('flowTaskGraph')}</h3><p>{t('flowRevisionHash')}: <code>{latest.content_hash}</code></p><ol className="flow-task-list">{latest.definition.tasks.map(task=><li key={task.id}><strong>{task.id}</strong> · {task.type}<p>{t('flowDependencies')}: {task.depends_on?.join(' → ')||'—'} · {t('flowTimeout')}: {task.timeout_ms}ms · retry {task.retry}</p></li>)}</ol><details><summary>{t('flowInputSchema')}</summary><pre>{JSON.stringify(latest.definition.inputs,null,2)}</pre></details></>}
 {tab==='source'&&<><div className="flow-new-form"><Label className="ui:grid ui:gap-1.5">{t('flowName')}<Input className="ui:h-11" value={name} maxLength={120} disabled={!newFlow} onChange={e=>setName(e.target.value)}/></Label>{!editable&&<p>{t('flowOwnedNotice')}</p>}<Suspense fallback={<p role="status">{t('loadingRecords')}</p>}><SourceEditor errorLine={errorLine} value={draft} onChange={value=>{setDraft(value);setValidation(null)}} readOnly={!canPublish||!editable} label={t('flowYAML')}/></Suspense><div className="flow-actions">{editable&&<Button variant="outline" className="secondary-button ui:min-h-11" disabled={action.busy} onClick={()=>void check()}>{t('flowValidate')}</Button>}{canPublish&&editable&&<Button variant="default" className="primary-button ui:min-h-11" disabled={action.busy||!name.trim()} onClick={()=>setConfirm('publish')}>{t('flowPublish')}</Button>}{dirty&&<span>{t('flowUnsaved')}</span>}</div>{validation&&<div role="status"><strong>{t('flowValid')}</strong><p>{validation.task_order.join(' → ')}</p><p>{t('flowRevisionHash')}: <code>{validation.content_hash}</code></p><pre>{JSON.stringify(validation.parameters,null,2)}</pre></div>}</div></>}
 {tab==='revisions'&&latest&&<><h3>{t('flowRevisions')}</h3><div className="flow-revision-select"><Label className="ui:grid ui:gap-1.5">{t('flowBefore')}<NativeSelect className="ui:h-11" value={left} onChange={e=>onSearch({left:e.target.value})}>{versions.data?.items.map(v=><NativeSelectOption key={v.id} value={v.id}>v{v.version} · {new Date(v.created_at).toLocaleString()}</NativeSelectOption>)}</NativeSelect></Label><Label className="ui:grid ui:gap-1.5">{t('flowAfter')}<NativeSelect className="ui:h-11" value={right} onChange={e=>onSearch({right:e.target.value})}>{versions.data?.items.map(v=><NativeSelectOption key={v.id} value={v.id}>v{v.version} · {new Date(v.created_at).toLocaleString()}</NativeSelectOption>)}</NativeSelect></Label></div>{versions.data&&<Pagination offset={(versionPage-1)*20} limit={20} total={versions.data.total} busy={versions.isFetching} onChange={offset=>onSearch({versionPage:offset/20+1,left:undefined,right:undefined})}/>}{diff.data&&<><p>{t('flowChangeCount',{count:diff.data.changes.length})}</p><Suspense fallback={<p role="status">{t('loadingRecords')}</p>}><DiffEditor before={source(diff.data.before)} after={source(diff.data.after)} label={t('flowDiff')}/></Suspense><ul>{diff.data.changes.map(change=><li key={change.path}>{change.kind} · <code>{change.path}</code></li>)}</ul></>}{canPublish&&editable&&left&&<Button variant="destructive" className="danger-button ui:min-h-11" disabled={action.busy} onClick={()=>setConfirm('rollback')}>{t('flowRollback')}</Button>}<p>{t('flowRollbackHint')}</p></>}
 </section></>}</div></div>
 {confirm==='publish'&&<ActionDialog title={t('flowPublish')} description={`${project.name} · ${name} · v${newFlow?1:(latest?.version??0)+1}`} busy={action.busy} confirm={t('flowPublish')} onClose={()=>setConfirm(null)} onSubmit={e=>void publish(e)}><ActionError error={action.error}/><p>{t('flowPublishHint')}</p></ActionDialog>}
 {confirm==='rollback'&&latest&&<ActionDialog title={t('flowRollback')} description={`${project.name} · ${latest.name} · v${latest.version} → v${latest.version+1}`} danger busy={action.busy} submitDisabled={reason.trim().length<3} confirm={t('flowRollback')} onClose={()=>setConfirm(null)} onSubmit={e=>void rollback(e)}><ActionError error={action.error}/><p>{t('flowRollbackHint')}</p><code>{left}</code><div className="field"><Label className="ui:grid ui:gap-1.5" htmlFor="flow-rollback-reason">{t('flowChangeReason')}</Label><Textarea className="ui:min-h-24" id="flow-rollback-reason" value={reason} minLength={3} maxLength={1000} required onChange={e=>setReason(e.target.value)}/></div></ActionDialog>}
 {launch&&latest&&(latest.definition.api_version==='keel.control/v1'?<ControlFlowLaunchDialog flow={latest} onClose={()=>setLaunch(false)} onDone={id=>{setLaunch(false);setExecutionId(id);setSuccess(t('executionCreated'))}}/>:latest.definition.api_version==='keel.host/v1'?<HostFlowLaunchDialog flow={latest} onClose={()=>setLaunch(false)} onDone={id=>{setLaunch(false);setExecutionId(id);setSuccess(t('executionCreated'))}}/>:<LaunchDialog flow={latest} onClose={()=>setLaunch(false)} onDone={id=>{setLaunch(false);setExecutionId(id);setSuccess(t('executionCreated'))}}/>)}
 </div>
}
export function FlowsPage({projectId,search,onSearch}:{projectId:string;search:FlowSearch;onSearch:(value:Partial<FlowSearch>)=>void}){return <ProjectWorkspace projectId={projectId}><FlowContent search={search} onSearch={onSearch}/></ProjectWorkspace>}
