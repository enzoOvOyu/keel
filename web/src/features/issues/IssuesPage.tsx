import { useEffect, useRef, useState, type FormEvent } from 'react'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Workspace } from '../../components/Workspace'
import { ActionDialog, ActionError } from '../../components/Management'
import { Button } from '../../components/ui/button'
import { Input } from '../../components/ui/input'
import { Textarea } from '../../components/ui/textarea'
import { Label } from '../../components/ui/label'
import { NativeSelect, NativeSelectOption } from '../../components/ui/native-select'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '../../components/ui/table'
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription } from '../../components/ui/dialog'
import { useAction } from '../../lib/management'
import { useExpireOnUnauthorized, useSession } from '../../lib/session'
import { actOnIssue, createIssue, getIssue, issueAssignees, issueLabels, issueSources, issueStates, issueTargets, listIssues, listFabricIssueObservations, listPortIssueEvidence, type Issue, type IssueAction, type IssueSearch, type NewIssue } from '../../lib/issues'
import '../../styles/issues.css'
function useLabels(){ const {i18n}=useTranslation(); const labels=i18n.language.startsWith('en')?issueLabels.en:issueLabels.zh; return (key:string)=>labels[key as keyof typeof labels]??key }
const repairActions=['reseat','replace_cable','replace_optic','replace_nic','rma_machine','switch_repair','configuration_fix','flash_bios','rma_board'] as const
function CreateDialog({project,onClose,onDone}:{project:string;onClose:()=>void;onDone:(id:string)=>Promise<void>}){
 const l=useLabels(), session=useSession(), action=useAction()
 const [title,setTitle]=useState(''),[category,setCategory]=useState<NewIssue['category']>('T1'),[source,setSource]=useState<NewIssue['source']>('fabric'),[phase,setPhase]=useState<NewIssue['phase']>('delivery')
 const [q,setQ]=useState(''),[target,setTarget]=useState(''),[expected,setExpected]=useState(''),[actual,setActual]=useState('')
 const [observedAt,setObservedAt]=useState(()=>{const now=new Date();return new Date(now.getTime()-now.getTimezoneOffset()*60000).toISOString().slice(0,16)})
 const key=useRef({body:'',key:''})
 const targets=useQuery({queryKey:['private',session.user.id,'issue-targets',project,q],queryFn:({signal})=>issueTargets(project,q,signal)})
 useExpireOnUnauthorized(targets.error)
 async function submit(event:FormEvent<HTMLFormElement>){
  event.preventDefault();const selected=targets.data?.find(v=>v.kind+':'+v.id===target);if(!selected)return
  const body:NewIssue={title,category,source,phase,anchors:[{kind:selected.kind,id:selected.id}],evidence:{observed_at:new Date(observedAt).toISOString(),source_version:'operator-report/v1',expected,actual}}
  const serialized=JSON.stringify(body);if(key.current.body!==serialized)key.current={body:serialized,key:crypto.randomUUID()}
  const result=await action.run(()=>createIssue(project,body,key.current.key,session.csrf_token));if(result.ok)await onDone(result.value.id)
 }
 return <ActionDialog title={l('new')} description={l('reported')} busy={action.busy} confirm={l('save')} onSubmit={e=>void submit(e)} onClose={onClose}>
  <ActionError error={action.error}/><ActionError error={targets.error}/>
  <Label>{l('name')}<Input value={title} onChange={e=>setTitle(e.target.value)} maxLength={240} required disabled={action.busy}/></Label>
  <Label>{l('category')}<NativeSelect value={category} onChange={e=>setCategory(e.target.value as NewIssue['category'])}>{['T1','T2','T3','T4','T5','T6','T7'].map(v=><NativeSelectOption key={v}>{v}</NativeSelectOption>)}</NativeSelect></Label>
  <Label>{l('source')}<NativeSelect value={source} onChange={e=>{const v=e.target.value as NewIssue['source'];setSource(v);if(v!=='bmc')setPhase('delivery')}}>{issueSources.map(v=><NativeSelectOption key={v} value={v}>{l(v)}</NativeSelectOption>)}</NativeSelect></Label>
  <Label>{l('phase')}<NativeSelect value={phase} onChange={e=>setPhase(e.target.value as NewIssue['phase'])}><NativeSelectOption value="delivery">{l('delivery')}</NativeSelectOption>{source==='bmc'&&<NativeSelectOption value="post-delivery">{l('post-delivery')}</NativeSelectOption>}</NativeSelect></Label>
  <Label>{l('search')}<Input value={q} onChange={e=>{setQ(e.target.value);setTarget('')}} maxLength={120}/></Label>
  <Label>{l('target')}<NativeSelect value={target} onChange={e=>setTarget(e.target.value)} required><NativeSelectOption value="" disabled>{l('target')}</NativeSelectOption>{targets.data?.map(v=><NativeSelectOption key={v.kind+v.id} value={v.kind+':'+v.id}>{v.label} ({v.kind})</NativeSelectOption>)}</NativeSelect></Label>
  <p className="field-hint">{l('targetHelp')}{targets.isSuccess&&!targets.data.length&&' '+l('noTargets')}</p>
  <Label>{l('observedAt')}<Input type="datetime-local" value={observedAt} onChange={e=>setObservedAt(e.target.value)} required/></Label>
  <Label>{l('expected')}<Textarea value={expected} onChange={e=>setExpected(e.target.value)} maxLength={4000}/></Label>
  <Label>{l('actual')}<Textarea value={actual} onChange={e=>setActual(e.target.value)} maxLength={4000} required/></Label>
 </ActionDialog>
}
function ActionForm({issue,kind,onClose,onDone}:{issue:Issue;kind:string;onClose:()=>void;onDone:()=>Promise<void>}){
 const l=useLabels(),session=useSession(),action=useAction()
 const [reason,setReason]=useState(''),[assignee,setAssignee]=useState(issue.assignee_id),[repair,setRepair]=useState<IssueAction['repair_action']>('reseat'),[oldSerial,setOldSerial]=useState(''),[newSerial,setNewSerial]=useState('')
 const assignees=useQuery({queryKey:['private',session.user.id,'issue-assignees',issue.project_id],queryFn:({signal})=>issueAssignees(issue.project_id,signal),enabled:kind==='assign'})
 useExpireOnUnauthorized(assignees.error)
 const replacement=['replace_cable','replace_optic','replace_nic','rma_machine','rma_board'].includes(repair??'')
 async function submit(event:FormEvent<HTMLFormElement>){event.preventDefault();const body:IssueAction={version:issue.version,reason,...(kind==='assign'?{assignee_id:assignee}:kind==='repair'?{repair_action:repair,...(replacement?{old_serial:oldSerial,new_serial:newSerial}:{})}:{})};const result=await action.run(()=>actOnIssue(issue.id,kind,body,session.csrf_token));if(result.ok)await onDone()}
 return <ActionDialog title={l(kind)} description={kind==='repair'?l('physicalOnly'):issue.title} busy={action.busy} confirm={l('save')} onSubmit={e=>void submit(e)} onClose={onClose}>
  <p>{l('version')}: {issue.version}</p><ActionError error={action.error}/><ActionError error={assignees.error}/>
  {action.error?.status===409&&<div role="alert"><p>{l('conflict')}</p><Button type="button" variant="outline" onClick={()=>void onDone()}>{l('reload')}</Button></div>}
  {kind==='assign'&&<><Label>{l('assignee')}<NativeSelect value={assignee} onChange={e=>setAssignee(e.target.value)} required><NativeSelectOption value="" disabled>{l('assignee')}</NativeSelectOption>{assignees.data?.map(v=><NativeSelectOption key={v.id} value={v.id}>{v.display_name} ({v.username})</NativeSelectOption>)}</NativeSelect></Label>{assignees.isSuccess&&!assignees.data.length&&<p>{l('noAssignees')}</p>}</>}
  {kind==='repair'&&<><Label>{l('repairAction')}<NativeSelect value={repair} onChange={e=>setRepair(e.target.value as IssueAction['repair_action'])}>{repairActions.filter(v=>v!=='flash_bios'||issue.category==='T7').map(v=><NativeSelectOption key={v} value={v}>{l(v)}</NativeSelectOption>)}</NativeSelect></Label>{replacement&&<><Label>{l('oldSerial')}<Input value={oldSerial} onChange={e=>setOldSerial(e.target.value)} maxLength={240} required/></Label><Label>{l('newSerial')}<Input value={newSerial} onChange={e=>setNewSerial(e.target.value)} maxLength={240} required/></Label></>}</>}
  <Label>{l('reason')}<Textarea value={reason} onChange={e=>setReason(e.target.value)} maxLength={2000} required disabled={action.busy}/></Label>
 </ActionDialog>
}
function FabricObservations({id}:{id:string}){
 const l=useLabels(),session=useSession(),[page,setPage]=useState(1)
 const query=useQuery({queryKey:['private',session.user.id,'issue-fabric-observations',id,page],queryFn:({signal})=>listFabricIssueObservations(id,page,signal),refetchInterval:15000})
 useExpireOnUnauthorized(query.error)
 return <section><h2>{l('observations')}</h2><p>{l('nS5Only')}</p><ActionError error={query.error}/>{query.isPending&&<p role="status">{l('loading')}</p>}{query.isSuccess&&!query.data.items.length&&<p>{l('noObservations')}</p>}<ol>{query.data?.items.map(v=><li key={v.id}><strong>{l(v.evidence_origin)}</strong> · {l(v.stale||Date.parse(v.valid_until)<=Date.now()?'stale':'current')}<p>{v.finding.issue_type} · {v.finding.reason} · {v.finding.rule_ref}</p><p>{l('observedAt')}: {new Date(v.observed_at).toLocaleString()}</p><p>{l('run')}: <code>{v.run_id}</code></p></li>)}</ol><nav className="pagination" aria-label={l('observations')}><Button variant="outline" disabled={page===1||query.isFetching} onClick={()=>setPage(page-1)}>{l('previous')}</Button><span>{page} · {query.data?.total??0}</span><Button variant="outline" disabled={!query.data||page*10>=query.data.total||query.isFetching} onClick={()=>setPage(page+1)}>{l('next')}</Button></nav></section>
}
function PortDiagnosticEvidence({id}:{id:string}){
 const l=useLabels(),session=useSession(),[page,setPage]=useState(1)
 const query=useQuery({queryKey:['private',session.user.id,'issue-port-diagnostics',id,page],queryFn:({signal})=>listPortIssueEvidence(id,page,signal)})
 useExpireOnUnauthorized(query.error)
 return <section><h2>{l('diagnosticEvidence')}</h2><ActionError error={query.error}/>{query.isSuccess&&!query.data.items.length&&<p>{l('noDiagnosticEvidence')}</p>}{query.data?.items.map(v=><details key={v.id}><summary>{l(v.report.evidence_source)} · {l(v.stale||Date.parse(v.report.valid_until)<=Date.now()?'stale':'current')} · {new Date(v.recorded_at).toLocaleString()}</summary><p>{l('run')}: {v.report.collection_id}</p><pre>{JSON.stringify({ports:v.report.ports,policy:v.report.policy,ddm_limits:v.report.ddm_limits,optics:v.report.optics_snapshot,intent:v.report.intent_snapshot,evidence_refs:v.report.evidence_refs},null,2)}</pre></details>)}<nav className="pagination" aria-label={l('diagnosticEvidence')}><Button variant="outline" disabled={page===1||query.isFetching} onClick={()=>setPage(page-1)}>{l('previous')}</Button><span>{page} · {query.data?.total??0}</span><Button variant="outline" disabled={!query.data||page*10>=query.data.total||query.isFetching} onClick={()=>setPage(page+1)}>{l('next')}</Button></nav></section>
}
function Detail({id,onClose,onAct}:{id:string;onClose:()=>void;onAct:(issue:Issue,kind:string)=>void}){
 const l=useLabels(),session=useSession()
 const detail=useQuery({queryKey:['private',session.user.id,'issue',id],queryFn:({signal})=>getIssue(id,signal)})
 useExpireOnUnauthorized(detail.error)
 const v=detail.data?.issue
 return <Dialog open onOpenChange={open=>{if(!open)onClose()}}><DialogContent className="issue-drawer"><DialogHeader><DialogTitle>{v?.title??l('detail')}</DialogTitle><DialogDescription>{v?`${v.category} · ${l(v.source)} · ${l(v.phase)} · ${l(v.state)}`:l('loading')}</DialogDescription></DialogHeader>
  <ActionError error={detail.error}/>{v&&<><dl className="action-target"><dt>{l('version')}</dt><dd>{v.version}</dd><dt>{l('responsible_domain')}</dt><dd>{l(v.responsible_domain)}</dd><dt>{l('expected')}</dt><dd>{v.evidence.expected||'—'}</dd><dt>{l('actual')}</dt><dd>{v.evidence.actual}</dd><dt>{l('sourceVersion')}</dt><dd>{v.evidence.source_version}</dd><dt>{l('observedAt')}</dt><dd>{new Date(v.evidence.observed_at).toLocaleString()}</dd><dt>{l('target')}</dt><dd>{v.anchors.map(a=><div key={a.kind+a.id}>{a.kind}: {a.id}</div>)}</dd></dl><p className="impact-notice">{l('revalidation')}</p><div className="issue-actions">{v.allowed_transitions.map(kind=><Button key={kind} variant="outline" onClick={()=>onAct(v,kind)}>{l(kind)}</Button>)}<Button variant="outline" onClick={()=>void detail.refetch()}>{l('reload')}</Button></div>
  {v.source==='fabric'&&<><FabricObservations key={v.id} id={v.id}/><PortDiagnosticEvidence key={v.id+'port'} id={v.id}/></>}<h2>{l('timeline')}</h2><ol>{detail.data?.events.map(ev=><li key={ev.id}><strong>{l(ev.action==='create'?'created':ev.action)}</strong> · {new Date(ev.created_at).toLocaleString()} · {l('version')} {ev.version}{typeof ev.data.reason==='string'&&<p>{ev.data.reason}</p>}{typeof ev.data.repair_action==='string'&&ev.data.repair_action&&<p>{l(ev.data.repair_action)}</p>}{typeof ev.data.old_serial==='string'&&ev.data.old_serial&&<p>{l('oldSerial')}: {ev.data.old_serial}</p>}{typeof ev.data.new_serial==='string'&&ev.data.new_serial&&<p>{l('newSerial')}: {ev.data.new_serial}</p>}</li>)}</ol></>}
 </DialogContent></Dialog>
}
function IssuesContent(){
 const l=useLabels(),session=useSession(),client=useQueryClient(),navigate=useNavigate()
 const search=useSearch({from:'/issues'}),page=search.page??1
 const [dragging,setDragging]=useState<string|null>(null)
 const [view,setView]=useState<'list'|'board'>('list'),[creating,setCreating]=useState<string|null>(null),[selection,setSelection]=useState<{issue:Issue;kind:string}|null>(null)
 const detail=search.issue_id??null
 const setDetail=(id:string|null)=>void navigate({to:'/issues',search:{...search,issue_id:id??undefined}})
 const filtersForList={...search,issue_id:undefined}
 const list=useQuery({queryKey:['private',session.user.id,'issues',filtersForList],queryFn:({signal})=>listIssues(filtersForList,signal),refetchInterval:5000})
 useExpireOnUnauthorized(list.error)
 const project=session.projects.find(v=>v.id===search.project_id),canCreate=!!project&&(session.user.is_admin||['Admin','DL','NE'].includes(project.role))
 useEffect(()=>{document.title=`${l('title')} · Keel`},[l('title')])
 const filter=(key:keyof IssueSearch,value:string)=>void navigate({to:'/issues',search:{...search,[key]:value||undefined,page:undefined}})
 async function done(id?:string){setCreating(null);setSelection(null);if(id)setDetail(id);await client.invalidateQueries({queryKey:['private',session.user.id,'issues']});await client.invalidateQueries({queryKey:['private',session.user.id,'issue']})}
 function card(v:Issue){return <article key={v.id} className="issue-card" draggable={v.allowed_transitions.some(a=>a==='assign'||a==='start')} onDragStart={event=>{event.dataTransfer.setData('application/x-keel-issue',v.id);setDragging(v.id)}} onDragEnd={()=>setDragging(null)}><Button variant="link" onClick={()=>setDetail(v.id)}>{v.title}</Button><p>{v.category} · {l(v.source)} · {l(v.phase)}</p><div className="issue-actions">{v.allowed_transitions.map(kind=><Button key={kind} variant="outline" onClick={()=>{setDetail(null);setSelection({issue:v,kind})}}>{l(kind)}</Button>)}</div></article>}
 const filters:{key:keyof IssueSearch;values:readonly string[]}[]=[{key:'state',values:issueStates},{key:'source',values:issueSources},{key:'phase',values:['delivery','post-delivery']},{key:'category',values:['T1','T2','T3','T4','T5','T6','T7']},{key:'responsible_domain',values:['host','fabric','unknown']}]
 return <section className="issue-page" aria-label={l('title')}><header className="page-heading"><h1>{l('title')}</h1><div className="issue-actions"><Button variant="outline" onClick={()=>setView(view==='list'?'board':'list')}>{l(view==='list'?'board':'list')}</Button><Button variant="outline" onClick={()=>void list.refetch()}>{l('reload')}</Button>{canCreate&&<Button onClick={()=>setCreating(project.id)}>{l('new')}</Button>}</div></header>
  <section className="issue-filters" aria-label={l('title')}><Label>{l('project')}<NativeSelect value={search.project_id??''} onChange={e=>filter('project_id',e.target.value)}><NativeSelectOption value="">{l('all')}</NativeSelectOption>{session.projects.map(v=><NativeSelectOption key={v.id} value={v.id}>{v.name}</NativeSelectOption>)}</NativeSelect></Label>{filters.map(({key,values})=><Label key={key}>{l(key)}<NativeSelect value={String(search[key]??'')} onChange={e=>filter(key,e.target.value)}><NativeSelectOption value="">{l('all')}</NativeSelectOption>{values.map(v=><NativeSelectOption key={v} value={v}>{l(v)}</NativeSelectOption>)}</NativeSelect></Label>)}</section>
  <ActionError error={list.error}/><p className="field-hint">{l('pageScope')}</p>{list.isPending&&<p role="status">{l('loading')}</p>}{list.isSuccess&&!list.data.items.length&&<p>{l('empty')}</p>}
  {view==='board'?<div className="issue-board">{issueStates.map(state=><section key={state} className={dragging?'issue-drop-lane':''} onDragOver={event=>{const item=list.data?.items.find(v=>v.id===dragging);const action=state==='assigned'?'assign':state==='in_repair'?'start':'';if(item&&item.allowed_transitions.includes(action as Issue['allowed_transitions'][number]))event.preventDefault()}} onDrop={event=>{event.preventDefault();const item=list.data?.items.find(v=>v.id===event.dataTransfer.getData('application/x-keel-issue'));const kind=state==='assigned'?'assign':state==='in_repair'?'start':'';setDragging(null);if(item&&item.allowed_transitions.includes(kind as Issue['allowed_transitions'][number]))setSelection({issue:item,kind})}}><h2>{l(state)}</h2>{list.data?.items.filter(v=>v.state===state).map(card)}</section>)}</div>:<Table><TableHeader><TableRow>{['name','category','source','phase','state','responsible_domain','version'].map(k=><TableHead key={k}>{l(k)}</TableHead>)}</TableRow></TableHeader><TableBody>{list.data?.items.map(v=><TableRow key={v.id}><TableCell><Button variant="link" onClick={()=>setDetail(v.id)}>{v.title}</Button></TableCell>{[v.category,l(v.source),l(v.phase),l(v.state),l(v.responsible_domain),v.version].map((value,i)=><TableCell key={i}>{value}</TableCell>)}</TableRow>)}</TableBody></Table>}
  <nav className="pagination" aria-label={l('title')}><Button variant="outline" disabled={page<=1||list.isFetching} onClick={()=>void navigate({to:'/issues',search:{...search,page:page-1}})}>{l('previous')}</Button><span>{page} · {list.data?.total??0}</span><Button variant="outline" disabled={!list.data||page*20>=list.data.total||list.isFetching} onClick={()=>void navigate({to:'/issues',search:{...search,page:page+1}})}>{l('next')}</Button></nav>
  {creating&&<CreateDialog project={creating} onClose={()=>setCreating(null)} onDone={done}/>}{detail&&<Detail id={detail} onClose={()=>setDetail(null)} onAct={(issue,kind)=>{setDetail(null);setSelection({issue,kind})}}/>}{selection&&<ActionForm key={selection.issue.id+selection.kind} issue={selection.issue} kind={selection.kind} onClose={()=>setSelection(null)} onDone={()=>done(selection.issue.id)}/>}
 </section>
}
export function IssuesPage(){const search=useSearch({from:'/issues'});return <Workspace projectId={search.project_id}><IssuesContent/></Workspace>}
