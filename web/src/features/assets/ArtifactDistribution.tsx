import { useEffect, useRef, useState, type FormEvent } from 'react'
import { useNavigate } from '@tanstack/react-router'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { ActionDialog, ActionError, Pagination } from '../../components/Management'
import { useProject } from '../../components/ProjectWorkspace'
import { ApiError, request } from '../../lib/api'
import type { components } from '../../generated/api'
import { createArtifactDistribution, type ArtifactCreateInput } from '../../lib/execution'
import { useAction } from '../../lib/management'
import { useSession, useExpireOnUnauthorized } from '../../lib/session'

type Target = { machine_id:string; hostname:string; serial_number:string }
type Props = { assets:Target[]; transport:'ssh'|'agent' }
function Launch({assets,transport,onClose,onDenied}:Props & {onClose:()=>void;onDenied:(error:ApiError)=>void}) {
 const {t}=useTranslation(),project=useProject(),session=useSession(),action=useAction(),navigate=useNavigate(),client=useQueryClient()
 const [targets,setTargets]=useState<string[]>([]),[media,setMedia]=useState(''),[page,setPage]=useState(1),[timeout,setTimeout]=useState('30000'),[confirmed,setConfirmed]=useState(false),[invalid,setInvalid]=useState(false)
 const attempt=useRef({body:'',key:''}),mounted=useRef(true)
 const catalog=useQuery({queryKey:['private',session.user.id,project.id,'distribution-media',page],queryFn:({signal})=>request<components['schemas']['MediaPage']>(`/projects/${encodeURIComponent(project.id)}/media?page=${page}&page_size=20&state=approved&stored_only=true`,{signal}),retry:false,refetchOnWindowFocus:false})
 const chosen=!catalog.isError && !catalog.isFetching ? catalog.data?.items.find(item=>item.id===media && item.size>0 && item.size<=1073741824 && !item.artifact_quarantined):undefined
 useExpireOnUnauthorized(catalog.error ?? action.error)
 useEffect(()=>{mounted.current=true;return()=>{mounted.current=false}},[])
 useEffect(()=>{setTargets(previous=>previous.filter(id=>assets.some(asset=>asset.machine_id===id)))},[assets])
 useEffect(()=>{setConfirmed(false)},[targets,media,timeout,catalog.dataUpdatedAt])
 useEffect(()=>{const error=action.error ?? catalog.error;if(error instanceof ApiError && [401,403,404].includes(error.status))onDenied(error)},[action.error,catalog.error])
 async function submit(event:FormEvent<HTMLFormElement>) {
  event.preventDefault()
  const timeoutMS=Number(timeout)
  if(!chosen || !confirmed || !targets.length || targets.length>32 || targets.some(id=>!assets.some(asset=>asset.machine_id===id)) || !Number.isSafeInteger(timeoutMS) || timeoutMS<100 || timeoutMS>30000){setInvalid(true);return}
  setInvalid(false)
  const body:ArtifactCreateInput={machine_ids:[...targets].sort(),media_id:chosen.id,transport,timeout_ms:timeoutMS},serialized=JSON.stringify(body)
  if(attempt.current.body!==serialized)attempt.current={body:serialized,key:crypto.randomUUID()}
  const result=await action.run(()=>createArtifactDistribution(project.id,body,session.csrf_token,attempt.current.key))
  if(result.ok && mounted.current){await client.invalidateQueries({queryKey:['private',session.user.id,'project',project.id,'executions']});if(mounted.current)await navigate({to:'/p/$projectId/executions',params:{projectId:project.id},search:{execution:result.value.id,tab:'overview'}})}
 }
 return <ActionDialog title={t('artifactDistributionTitle',{transport:transport.toUpperCase()})} description={t('artifactDistributionBoundary')} busy={action.busy} confirm={t('artifactDistributionSubmit')} submitDisabled={!targets.length || !chosen || !confirmed} onSubmit={event=>void submit(event)} onClose={onClose}>
  <ActionError error={action.error ?? catalog.error}/>
  {action.error?.code==='ARTIFACT_TRANSFERS_DISABLED' && <p role="status">{t('artifactDistributionDisabled')}</p>}
  {invalid && <p className="field-error" role="alert">{t('artifactDistributionInvalid')}</p>}
  <p>{t('artifactDistributionResume')}</p><p className="field-hint">{t('sshLaunchCurrentPage')}</p>
  <div className="ui:flex ui:flex-wrap ui:gap-2" role="group" aria-label={t('sshLaunchTargets')}>{assets.map(asset=><Button key={asset.machine_id} type="button" variant={targets.includes(asset.machine_id)?'default':'outline'} aria-pressed={targets.includes(asset.machine_id)} disabled={action.busy || !targets.includes(asset.machine_id) && targets.length>=32} onClick={()=>setTargets(previous=>previous.includes(asset.machine_id)?previous.filter(id=>id!==asset.machine_id):[...previous,asset.machine_id])}>{asset.hostname} · {asset.serial_number}</Button>)}</div>
  <p role="status">{t('sshLaunchSelected',{count:targets.length})}</p>
  <div className="field"><Label htmlFor="artifact-media">{t('sshScriptMedia')}</Label><NativeSelect id="artifact-media" value={chosen?.id ?? ''} disabled={action.busy || catalog.isFetching || catalog.isError} onChange={event=>setMedia(event.target.value)}><NativeSelectOption value="">{t('sshScriptChoose')}</NativeSelectOption>{catalog.data?.items.filter(item=>item.size>0 && item.size<=1073741824 && !item.artifact_quarantined).map(item=><NativeSelectOption key={item.id} value={item.id}>{item.name} · {item.version} · {item.size} B</NativeSelectOption>)}</NativeSelect></div>
  {catalog.data && <Pagination offset={(page-1)*catalog.data.page_size} limit={catalog.data.page_size} total={catalog.data.total} busy={action.busy || catalog.isFetching} onChange={offset=>{setPage(offset/catalog.data!.page_size+1);setMedia('')}}/>}
  {chosen && <code className="ui:break-all">{chosen.sha256}</code>}
  <div className="field"><Label htmlFor="artifact-timeout">{t('sshLaunchTimeout')}</Label><Input id="artifact-timeout" type="number" min={100} max={30000} step={1} required value={timeout} disabled={action.busy} onChange={event=>setTimeout(event.target.value)}/></div>
  <div className="ui:flex ui:items-start ui:gap-2"><Checkbox id="artifact-confirm" checked={confirmed} disabled={action.busy || !chosen || !targets.length} onCheckedChange={value=>setConfirmed(value===true)}/><Label htmlFor="artifact-confirm">{t('artifactDistributionConfirm')}</Label></div>
 </ActionDialog>
}
export function ArtifactDistribution({assets,transport}:Props){
 const {t}=useTranslation(),project=useProject(),session=useSession(),client=useQueryClient()
 const [open,setOpen]=useState(false),[denied,setDenied]=useState<ApiError|null>(null)
 const allowed=!['archived','delivered'].includes(project.state) && (session.user.is_admin || ['Admin','DE','DL','SRE'].includes(project.role))
 useEffect(()=>{if(!allowed || denied)setOpen(false)},[allowed,denied])
 useEffect(()=>{setOpen(false);setDenied(null)},[project.id,session.user.id])
 if(!allowed)return null
 function onDenied(error:ApiError){setDenied(error);void client.invalidateQueries({queryKey:['private',session.user.id,'project',project.id]})}
 return <section className="management-panel"><div className="panel-heading"><div><h2>{t('artifactDistributionTitle',{transport:transport.toUpperCase()})}</h2><p>{t('artifactDistributionIntro')}</p></div><Button type="button" disabled={!assets.length || !!denied} onClick={()=>setOpen(true)}>{t('artifactDistributionSubmit')}</Button></div><ActionError error={denied}/>{open && <Launch key={`${project.id}:${session.user.id}:${transport}`} assets={assets} transport={transport} onClose={()=>setOpen(false)} onDenied={onDenied}/>}</section>
}
