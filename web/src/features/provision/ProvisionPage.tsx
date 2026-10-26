import { useEffect, useRef, useState } from 'react'
import { RackRequestsDialog } from './RackRequestsDialog'
import { Link, useSearch } from '@tanstack/react-router'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Table,TableBody,TableCell,TableHead,TableHeader,TableRow } from '@/components/ui/table'
import { ProjectWorkspace,useProject } from '../../components/ProjectWorkspace'
import { ActionDialog,ActionError,Pagination,PermissionDenied } from '../../components/Management'
import { useSession,useExpireOnUnauthorized } from '../../lib/session'
import { ApiError } from '../../lib/api'
import { useAction } from '../../lib/management'
import { actProvisionRequest,getProvisionRequest,listProvisionRequests,prepareProvisionRequest,type ProvisionRequest } from '../../lib/provision'
import { BootScopesPanel } from './BootScopesPanel'
import { InstallationsPanel, StartInstallation } from './InstallationsPanel'

function RequestAction({ item,kind,onClose,onDone }: { item:ProvisionRequest;kind:'approve'|'cancel'|'prepare';onClose:()=>void;onDone:()=>Promise<void> }) {
 const {t}=useTranslation(),session=useSession(),action=useAction(),key=useRef(crypto.randomUUID())
 const [reason,setReason]=useState('')
 return <ActionDialog title={t(kind==='approve'?'provisionReview':kind==='cancel'?'provisionCancel':'provisionPrepare')} description={`${item.snapshot.target.hostname} · ${item.snapshot.target.serial_number}`} busy={action.busy} confirm={t('confirm')} submitDisabled={kind!=='prepare'&&!reason.trim()} onClose={onClose} onSubmit={event=>{
  event.preventDefault()
  void action.run(async()=>{if(kind==='prepare')await prepareProvisionRequest(item,session.csrf_token,key.current);else await actProvisionRequest(item,kind,reason.trim(),session.csrf_token,key.current)}).then(async result=>{if(result.ok)await onDone()})
 }}><p className="impact-notice">{t('provisionPreparationOnly')}</p><p className="mono-value ui:break-all">{item.snapshot_sha256}</p><ActionError error={action.error}/>{action.error instanceof ApiError&&action.error.code==='PROVISION_PREPARATION_DISABLED'&&<p role="status">{t('provisionPendingWorker')}</p>}{kind!=='prepare'&&<div className="field"><Label htmlFor="provision-action-reason">{t('provisionActionReason')}</Label><Input id="provision-action-reason" maxLength={500} value={reason} disabled={action.busy} required onChange={event=>{setReason(event.target.value);key.current=crypto.randomUUID()}}/></div>}</ActionDialog>
}
function Board() {
 const {t}=useTranslation(),project=useProject(),session=useSession(),client=useQueryClient()
 const {request:requestId}=useSearch({from:'/p_/$projectId/deploy/provision'})
 const [rackOpen,setRackOpen]=useState(false)
 const [page,setPage]=useState(1),[chosen,setChosen]=useState<{item:ProvisionRequest;kind:'approve'|'cancel'|'prepare'|'install'}>()
 const readable=session.user.is_admin||['Admin','DE','DL','SRE'].includes(project.role)
 const writable=readable&&!['archived','delivered'].includes(project.state)
 const approver=writable&&(session.user.is_admin||['Admin','DL'].includes(project.role))
 const prefix=['private',session.user.id,'project',project.id,'provision-requests']
 const records=useQuery({queryKey:[...prefix,page],enabled:readable&&!requestId,queryFn:({signal})=>listProvisionRequests(project.id,page,signal),retry:false})
 const focused=useQuery({queryKey:[...prefix,'detail',requestId],enabled:readable&&!!requestId,queryFn:({signal})=>getProvisionRequest(project.id,requestId!,signal),retry:false})
 const data=requestId?(focused.data?{items:[focused.data],total:1,page:1,page_size:1}:undefined):records.data
 const loadError=requestId?focused.error:records.error,isFetching=requestId?focused.isFetching:records.isFetching,isPending=requestId?focused.isPending:records.isPending
 useExpireOnUnauthorized(loadError)
 useEffect(()=>{setPage(1);setChosen(undefined);setRackOpen(false)},[project.id,requestId])
 useEffect(()=>{if(!writable||loadError){setChosen(undefined);setRackOpen(false)}},[writable,loadError])
 const refresh=async()=>{await client.invalidateQueries({queryKey:prefix})}
 if(!readable)return <PermissionDenied/>
 return <section className="management-page"><div className="page-heading"><div><h1>{t('provisionTitle')}</h1><p>{t('provisionDetail')}</p></div><div className="table-actions"><Button variant="outline" disabled={isFetching} onClick={()=>void refresh()}>{t('refresh')}</Button>{writable&&<Button variant="outline" onClick={()=>setRackOpen(true)}>{t('rackPrepareTitle')}</Button>}{requestId&&<Link to="/p/$projectId/deploy/provision" params={{projectId:project.id}} search={{request:undefined}}>{t('rackPrepareAllRequests')}</Link>}{writable&&<Link to="/p/$projectId/assets" params={{projectId:project.id}}>{t('provisionFromBOM')}</Link>}</div></div><p className="impact-notice">{t('provisionPreparationOnly')}</p><ActionError error={loadError}/>{isPending&&<p role="status">{t('loadingRecords')}</p>}{data&&!loadError&&<>{!data.items.length&&<p>{t('provisionNoRequests')}</p>}<div className="table-scroll" role="region" aria-label={t('provisionTitle')} tabIndex={0}><Table><TableHeader><TableRow><TableHead>{t('provisionIdentity')}</TableHead><TableHead>{t('status')}</TableHead><TableHead>{t('provisionExpiry')}</TableHead><TableHead>{t('actions')}</TableHead></TableRow></TableHeader><TableBody>{data.items.map(item=>{
 const expired=Date.parse(item.expires_at)<=Date.now()
 return <TableRow key={item.id} data-provision-request={item.id}><TableCell><span>{item.snapshot.target.hostname}</span><span className="cell-subtitle mono-value">{item.snapshot.target.serial_number} · v{item.state_version}</span><details><summary>{t('provisionProof')}</summary><dl className="ui:max-w-96 ui:whitespace-normal ui:break-words"><dt>{t('provisionReason')}</dt><dd>{item.reason}</dd><dt>{t('provisionSnapshot')}</dt><dd className="mono-value ui:break-all">{item.snapshot_sha256}</dd><dt>{t('provisionBootPort')}</dt><dd>{item.snapshot.boot_port.mac}</dd><dt>{t('provisionOSImage')}</dt><dd>{item.snapshot.media.root_id}</dd><dt>{t('provisionDomainKey')}</dt><dd>{item.snapshot.domain_key.credential_ref} · {item.snapshot.domain_key.credential_version}</dd></dl></details></TableCell><TableCell>{t(`provisionState_${item.state}`)}{expired&&['requested','approved','prepared'].includes(item.state)&&<span className="cell-subtitle">{t('provisionExpired')}</span>}</TableCell><TableCell>{new Date(item.expires_at).toLocaleString()}</TableCell><TableCell><div className="table-actions">{approver&&!expired&&item.state==='requested'&&item.created_by!==session.user.id&&<Button size="sm" variant="outline" onClick={()=>setChosen({item,kind:'approve'})}>{t('provisionReview')}</Button>}{writable&&!expired&&item.state==='approved'&&<Button size="sm" onClick={()=>setChosen({item,kind:'prepare'})}>{t('provisionPrepare')}</Button>}{writable&&!expired&&item.state==='prepared'&&<Button size="sm" onClick={()=>setChosen({item,kind:'install'})}>{t('installStart')}</Button>}{writable&&['requested','approved','prepared'].includes(item.state)&&<Button size="sm" variant="outline" onClick={()=>setChosen({item,kind:'cancel'})}>{t('provisionCancel')}</Button>}{item.execution_id&&<Link to="/p/$projectId/executions" params={{projectId:project.id}} search={{execution:item.execution_id,tab:'overview'}}>{t('sshTrustViewExecution')}</Link>}</div></TableCell></TableRow>
 })}</TableBody></Table></div><Pagination offset={(page-1)*data.page_size} limit={data.page_size} total={data.total} busy={isFetching} onChange={offset=>setPage(offset/data!.page_size+1)}/></>}{chosen&&chosen.item.project_id===project.id&&writable&&!loadError&&(chosen.kind!=='approve'||approver)&&chosen.kind!=='install'&&<RequestAction key={`${chosen.item.id}:${chosen.kind}`} item={chosen.item} kind={chosen.kind} onClose={()=>{setChosen(undefined);void refresh()}} onDone={async()=>{setChosen(undefined);await refresh()}}/>}{chosen?.kind==='install'&&chosen.item.project_id===project.id&&writable&&!loadError&&<StartInstallation key={chosen.item.id} item={chosen.item} onClose={()=>{setChosen(undefined);void refresh()}} onDone={async()=>{setChosen(undefined);await Promise.all([refresh(),client.invalidateQueries({queryKey:['private',session.user.id,'project',project.id,'provision-installations']})])}}/>}{rackOpen&&writable&&!loadError&&<RackRequestsDialog key={project.id} project={project.id} onClose={()=>setRackOpen(false)} onDone={refresh}/>}</section>
}
function ProvisionContent() {
 const project=useProject(),session=useSession()
 const readable=session.user.is_admin||['Admin','DE','DL','SRE'].includes(project.role)
 return <>{readable&&<Board/>}<InstallationsPanel/><BootScopesPanel/></>
}
export function ProvisionPage({projectId}:{projectId:string}) {return <ProjectWorkspace projectId={projectId}><ProvisionContent/></ProjectWorkspace>}
