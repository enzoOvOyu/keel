import {useEffect,useState,type FormEvent} from 'react'
import {useQuery,useQueryClient} from '@tanstack/react-query'
import {useTranslation} from 'react-i18next'
import {Button} from '@/components/ui/button'
import {Label} from '@/components/ui/label'
import {Input} from '@/components/ui/input'
import {Checkbox} from '@/components/ui/checkbox'
import {NativeSelect,NativeSelectOption} from '@/components/ui/native-select'
import {ActionError} from '../../components/Management'
import {useSession,useExpireOnUnauthorized} from '../../lib/session'
import {useAction} from '../../lib/management'
import {getZTPSimSession,listZTPIdentityHistory,recordZTPIdentityHistory,type ZTPDetail,type ZTPSummary} from '../../lib/ztp'

export function ZTPIdentityHistory({projectId,current,sessions,canManage,onRecorded}:{projectId:string;current?:ZTPDetail;sessions:ZTPSummary[];canManage:boolean;onRecorded:()=>void}){
 const {t}=useTranslation(),session=useSession(),client=useQueryClient(),action=useAction()
 const [from,setFrom]=useState(''),[kind,setKind]=useState<'reinstall'|'rma'>('reinstall'),[reason,setReason]=useState(''),[confirmed,setConfirmed]=useState(false)
 const queryKey=['private',session.user.id,projectId,'ztp-identity-history']
 const history=useQuery({queryKey,queryFn:({signal})=>listZTPIdentityHistory(projectId,signal)})
 const predecessor=useQuery({queryKey:['private',session.user.id,projectId,'ztp-history-predecessor',from],queryFn:({signal})=>getZTPSimSession(projectId,from,signal),enabled:!!from&&canManage})
 useExpireOnUnauthorized(history.error??predecessor.error??action.error)
 useEffect(()=>{setFrom('');setReason('');setConfirmed(false)},[current?.id])
 useEffect(()=>{setConfirmed(false)},[from,kind,reason,current?.state_version,predecessor.data?.state_version])
 const source=predecessor.data,already=history.data?.some(item=>item.target.session_id===current?.id)
 const correctKind=source&&current&&(kind==='reinstall'?source.switch_id===current.switch_id:source.switch_id!==current.switch_id)
 const ready=!!source&&!!current&&!already&&!predecessor.isFetching&&!predecessor.isError&&!history.isError&&!history.isFetching&&source.state==='managed'&&current.state==='managed'&&source.id!==current.id&&source.mode===current.mode&&correctKind&&!!source.managed_at&&Date.parse(source.managed_at)<=Date.parse(current.started_at)&&reason.trim().length>=3&&reason.trim().length<=1000
 async function submit(event:FormEvent){event.preventDefault();if(!ready||!confirmed||!source||!current)return
  const result=await action.run(()=>recordZTPIdentityHistory(projectId,{kind,from_session_id:source.id,to_session_id:current.id,from_expected_version:source.state_version,to_expected_version:current.state_version,reason:reason.trim(),confirmed:true},session.csrf_token))
  if(result.ok){setConfirmed(false);setFrom('');setReason('');void client.invalidateQueries({queryKey});onRecorded()}
 }
 return <section className="panel"><h2>{t('ztpHistoryTitle')}</h2><p className="impact-notice">{t('ztpHistoryBoundary')}</p><ActionError error={action.error??history.error??predecessor.error}/>
  <ul>{history.data?.map(item=><li key={item.id}><strong>{t(item.kind==='rma'?'ztpHistoryRMA':'ztpHistoryReinstall')}</strong> · {item.source.serial} → {item.target.serial} · {item.mode} · <time>{new Date(item.recorded_at).toLocaleString()}</time><p>{item.reason}</p><small>{item.source.session_id} → {item.target.session_id}</small></li>)}</ul>
  {canManage&&current?.state==='managed'&&current.mode==='sim'&&!already&&<form onSubmit={event=>void submit(event)}>
   <p>{t('ztpHistoryTarget',{batch:current.batch_ref})}</p><Label htmlFor="ztp-history-kind">{t('ztpHistoryKind')}</Label><NativeSelect id="ztp-history-kind" value={kind} disabled={action.busy} onChange={event=>setKind(event.target.value as 'reinstall'|'rma')}><NativeSelectOption value="reinstall">{t('ztpHistoryReinstall')}</NativeSelectOption><NativeSelectOption value="rma">{t('ztpHistoryRMA')}</NativeSelectOption></NativeSelect>
   <Label htmlFor="ztp-history-source">{t('ztpHistoryPredecessor')}</Label><NativeSelect id="ztp-history-source" value={from} disabled={action.busy} onChange={event=>setFrom(event.target.value)}><NativeSelectOption value="">—</NativeSelectOption>{sessions.filter(item=>item.state==='managed'&&item.id!==current.id).map(item=><NativeSelectOption key={item.id} value={item.id}>{item.batch_ref} · {new Date(item.updated_at).toLocaleString()} · {item.id.slice(0,8)}</NativeSelectOption>)}</NativeSelect>
   <Label htmlFor="ztp-history-reason">{t('ztpHistoryReason')}</Label><Input id="ztp-history-reason" value={reason} required minLength={3} maxLength={1000} disabled={action.busy} onChange={event=>setReason(event.target.value)}/>
   <Label className="ui:flex ui:items-center ui:gap-2"><Checkbox checked={confirmed} disabled={action.busy||!ready} onCheckedChange={value=>setConfirmed(value===true)}/>{t('ztpHistoryConfirm')}</Label>
   <Button type="submit" disabled={action.busy||!ready||!confirmed}>{t('ztpHistoryRecord')}</Button>
  </form>}
 </section>
}
