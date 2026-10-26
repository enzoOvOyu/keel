import {useEffect,useRef,useState,type FormEvent} from 'react'
import {useQuery} from '@tanstack/react-query'
import {useTranslation} from 'react-i18next'
import {Button} from '@/components/ui/button'
import {Label} from '@/components/ui/label'
import {Input} from '@/components/ui/input'
import {NativeCheckbox} from '@/components/ui/checkbox'
import {NativeSelect,NativeSelectOption} from '@/components/ui/native-select'
import {ActionDialog,ActionError,Pagination} from '../../components/Management'
import {useSession,useExpireOnUnauthorized} from '../../lib/session'
import {useAction} from '../../lib/management'
import {listProjectAssets} from '../../lib/bom'
import {listAgentTargets,type FlowVersion} from '../../lib/execution'
import {createHostFlowExecution} from '../../lib/flow-workbench'

export function HostFlowLaunchDialog({flow,onClose,onDone}:{flow:FlowVersion;onClose:()=>void;onDone:(id:string)=>void}){
 const {t}=useTranslation(),session=useSession(),action=useAction()
 const [transport,setTransport]=useState<'ssh'|'agent'>('ssh'),[page,setPage]=useState(1),[ids,setIds]=useState<string[]>([]),[concurrency,setConcurrency]=useState(4),[breaker,setBreaker]=useState(100),[confirmed,setConfirmed]=useState(false)
 const submission=useRef({body:'',key:''}),alive=useRef(true)
 useEffect(()=>{alive.current=true;return()=>{alive.current=false}},[])
 const targets=useQuery({queryKey:['private',session.user.id,'project',flow.project_id,'host-flow-targets',transport,page],queryFn:async({signal})=>{
  if(transport==='agent'){const result=await listAgentTargets(flow.project_id,page,signal);return{total:result.total,items:result.items.filter(v=>v.connected&&Date.parse(v.credential_expires_at)>Date.now()).map(v=>({id:v.machine_id,name:v.hostname,serial:v.serial_number}))}}
  const result=await listProjectAssets(flow.project_id,page,signal);return{total:result.total,items:result.items.filter(v=>v.state==='provisioned'&&v.ssh_trust==='established'&&v.ssh_trust_source==='ssh').map(v=>({id:v.machine_id,name:v.hostname,serial:v.serial_number}))}
 }})
 useExpireOnUnauthorized(targets.error??action.error)
 useEffect(()=>{setConfirmed(false)},[transport,page,ids,concurrency,breaker])
 const ready=!targets.isFetching&&!targets.isError&&ids.length>0&&ids.length<=32&&ids.every(id=>targets.data?.items.some(v=>v.id===id))&&Number.isSafeInteger(concurrency)&&concurrency>=1&&concurrency<=32&&Number.isSafeInteger(breaker)&&breaker>=1&&breaker<=100
 async function submit(event:FormEvent){event.preventDefault();if(!ready||!confirmed)return
  const body={flow_version_id:flow.id,transport,machine_ids:[...ids].sort(),concurrency,circuit_breaker_percent:breaker},serialized=JSON.stringify(body)
  if(submission.current.body!==serialized)submission.current={body:serialized,key:crypto.randomUUID()}
  const result=await action.run(()=>createHostFlowExecution(flow.project_id,body,session.csrf_token,submission.current.key));if(result.ok&&alive.current)onDone(result.value.id)
 }
 return <ActionDialog title={t('flowLaunch')} description={`${flow.name} · v${flow.version} · ${flow.project_id}`} busy={action.busy} confirm={t('startExecution')} submitDisabled={!ready||!confirmed} onClose={onClose} onSubmit={event=>void submit(event)}>
  <ActionError error={action.error??targets.error}/><p className="impact-notice">{t('flowHostBoundary')}</p>
  <Label>{t('flowHostTransport')}<NativeSelect value={transport} disabled={action.busy} onChange={event=>{setTransport(event.target.value as 'ssh'|'agent');setPage(1);setIds([])}}><NativeSelectOption value="ssh">SSH</NativeSelectOption><NativeSelectOption value="agent">Agent</NativeSelectOption></NativeSelect></Label>
  <p>{t('flowExplicitTargets',{count:ids.length})}</p><div className="flow-target-list">{targets.data?.items.map(target=><Button key={target.id} type="button" variant={ids.includes(target.id)?'default':'outline'} disabled={action.busy||targets.isFetching} aria-pressed={ids.includes(target.id)} onClick={()=>setIds(current=>current.includes(target.id)?current.filter(id=>id!==target.id):[...current,target.id])}>{target.name} · {target.serial}</Button>)}</div>
  {targets.data?.items.length===0&&<p>{t('flowHostNoTargets')}</p>}{targets.data&&<Pagination offset={(page-1)*20} limit={20} total={targets.data.total} busy={action.busy||targets.isFetching} onChange={offset=>{setIds([]);setPage(offset/20+1)}}/>}
  <Label htmlFor="host-flow-concurrency">{t('concurrency')}</Label><Input id="host-flow-concurrency" type="number" min={1} max={32} required disabled={action.busy} value={concurrency} onChange={event=>setConcurrency(Number(event.target.value))}/>
  <Label htmlFor="host-flow-breaker">{t('circuitBreaker')}</Label><Input id="host-flow-breaker" type="number" min={1} max={100} required disabled={action.busy} value={breaker} onChange={event=>setBreaker(Number(event.target.value))}/>
  <Label><NativeCheckbox checked={confirmed} disabled={action.busy||!ready} onChange={event=>setConfirmed(event.target.checked)}/>{t('flowHostConfirm',{count:ids.length,transport})}</Label>
 </ActionDialog>
}
