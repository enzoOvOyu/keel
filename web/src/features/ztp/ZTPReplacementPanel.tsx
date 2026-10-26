import {useEffect,useState,type FormEvent} from 'react'
import {useQuery,useQueryClient} from '@tanstack/react-query'
import {useTranslation} from 'react-i18next'
import {Button} from '@/components/ui/button'
import {Label} from '@/components/ui/label'
import {Input} from '@/components/ui/input'
import {Checkbox} from '@/components/ui/checkbox'
import {NativeSelect,NativeSelectOption} from '@/components/ui/native-select'
import {ActionError} from '../../components/Management'
import {useProject} from '../../components/ProjectWorkspace'
import {useAction} from '../../lib/management'
import {useSession,useExpireOnUnauthorized} from '../../lib/session'
import {latestFabric} from '../../lib/fabric'
import {approveZTPReplacement,rejectZTPReplacement,createZTPReplacement,listZTPReplacements,type ZTPDetail} from '../../lib/ztp'

export function ZTPReplacementPanel({projectId,current,canManage}:{projectId:string;current?:ZTPDetail;canManage:boolean}){
 const {t}=useTranslation(),session=useSession(),project=useProject(),client=useQueryClient(),action=useAction()
 const [target,setTarget]=useState(''),[reason,setReason]=useState(''),[confirmed,setConfirmed]=useState(false),[review,setReview]=useState(''),[approved,setApproved]=useState(false)
 const queryKey=['private',session.user.id,projectId,'ztp-replacements']
 const requests=useQuery({queryKey,queryFn:({signal})=>listZTPReplacements(projectId,signal)})
 const inventory=useQuery({queryKey:['private',session.user.id,projectId,'ztp-replacement-inventory'],queryFn:({signal})=>latestFabric(projectId,signal),enabled:canManage})
 useExpireOnUnauthorized(requests.error??inventory.error??action.error)
 useEffect(()=>{setTarget('');setReason('');setConfirmed(false)},[current?.id])
 useEffect(()=>{setConfirmed(false)},[target,reason,current?.state_version])
 const selected=requests.data?.find(item=>item.id===review)
 useEffect(()=>setApproved(false),[review,selected?.state_version,selected?.snapshot_sha256])
 const ready=canManage&&current?.state==='managed'&&!!target&&target!==current.switch_id&&inventory.data?.inventory.some(item=>item.id===target)&&!inventory.isFetching&&!inventory.isError&&reason.trim().length>=3&&reason.trim().length<=1000
 async function create(event:FormEvent){event.preventDefault();if(!ready||!confirmed||!current)return
  const result=await action.run(()=>createZTPReplacement(projectId,{from_session_id:current.id,from_expected_version:current.state_version,to_switch_id:target,reason:reason.trim(),confirmed:true},session.csrf_token))
  if(result.ok){setConfirmed(false);setTarget('');setReason('');void client.invalidateQueries({queryKey})}
 }
 async function approve(reject=false){if(!selected||!approved)return
  const result=await action.run(()=>(reject?rejectZTPReplacement:approveZTPReplacement)(projectId,selected.id,{expected_version:selected.state_version,snapshot_sha256:selected.snapshot_sha256,confirmed:true},session.csrf_token))
  if(result.ok){setApproved(false);setReview('');void client.invalidateQueries({queryKey})}
 }
 const canApprove=session.user.is_admin||project.role==='Admin'
 return <section><h3>{t('ztpRMATitle')}</h3><p className="impact-notice">{t('ztpRMABoundary')}</p><ActionError error={requests.error??inventory.error??action.error}/>
  <ul>{requests.data?.map(item=><li key={item.id}>{item.snapshot.old.serial} → {item.snapshot.new.serial} · {item.snapshot.mode} · {item.state}<p>{item.snapshot.old.mgmt_mac} → {item.snapshot.new.mgmt_mac} · {JSON.stringify(item.snapshot.new.position)}</p><p>{item.reason}</p>{canApprove&&item.state==='requested'&&<Button variant="outline" disabled={action.busy} onClick={()=>setReview(item.id)}>{t('ztpRMAReview')}</Button>}</li>)}</ul>
  {selected?.state==='requested'&&canApprove&&<div><p>{selected.snapshot.old.serial} → {selected.snapshot.new.serial} · {selected.snapshot.mode}</p><Label><Checkbox checked={approved} disabled={action.busy||requests.isFetching} onCheckedChange={value=>setApproved(value===true)}/>{t('ztpRMAApproveConfirm')}</Label><Button disabled={action.busy||requests.isFetching||!approved||selected.created_by===session.user.id} onClick={()=>void approve()}>{t('ztpRMAApprove')}</Button><Button variant="outline" disabled={action.busy||requests.isFetching||!approved} onClick={()=>void approve(true)}>{t('ztpRMAReject')}</Button></div>}
  {canManage&&current?.state==='managed'&&<form onSubmit={event=>void create(event)}><Label htmlFor="ztp-rma-target">{t('ztpRMANewDevice')}</Label><NativeSelect id="ztp-rma-target" value={target} disabled={action.busy||inventory.isFetching} onChange={event=>setTarget(event.target.value)}><NativeSelectOption value="">—</NativeSelectOption>{inventory.data?.inventory.filter(item=>item.id!==current.switch_id).map(item=><NativeSelectOption key={item.id} value={item.id}>{item.serial} · {item.hostname} · {item.rack}</NativeSelectOption>)}</NativeSelect><Label htmlFor="ztp-rma-reason">{t('ztpHistoryReason')}</Label><Input id="ztp-rma-reason" value={reason} minLength={3} maxLength={1000} required disabled={action.busy} onChange={event=>setReason(event.target.value)}/><Label><Checkbox checked={confirmed} disabled={action.busy||!ready} onCheckedChange={value=>setConfirmed(value===true)}/>{t('ztpRMARequestConfirm')}</Label><Button type="submit" disabled={action.busy||!ready||!confirmed}>{t('ztpRMARequest')}</Button></form>}
 </section>
}
