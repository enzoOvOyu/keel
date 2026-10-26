import {Button} from '@/components/ui/button'
import {Textarea} from '@/components/ui/textarea'
import {Checkbox} from '@/components/ui/checkbox'
import {Label} from '@/components/ui/label'
import {useEffect,useRef,useState,type FormEvent} from 'react'
import {useQuery} from '@tanstack/react-query'
import {useTranslation} from 'react-i18next'
import {ActionDialog,ActionError} from '../../components/Management'
import {useSession,useExpireOnUnauthorized} from '../../lib/session'
import {useAction} from '../../lib/management'
import {previewRerun,rerunExecution} from '../../lib/execution-rerun'

export function RerunDialog({projectId,projectName,sourceExecutionId,mode,targetIDs=[],onClose,onDone}:{projectId:string;projectName:string;sourceExecutionId:string;mode:'failed'|'targets';targetIDs?:string[];onClose:()=>void;onDone:(executionId:string)=>void}){
 const session=useSession(),{t}=useTranslation(),action=useAction(),[reason,setReason]=useState(''),[confirmed,setConfirmed]=useState(false),key=useRef<{payload:string;key:string}|null>(null)
 const targets=[...targetIDs].sort(),preview=useQuery({queryKey:['private',session.user.id,'project',projectId,'rerun-preview',sourceExecutionId,mode,targets.join(',')],queryFn:({signal})=>previewRerun(projectId,sourceExecutionId,mode,targets,session.csrf_token,signal),retry:false})
 useExpireOnUnauthorized(preview.error)
 const data=preview.data
 useEffect(()=>setConfirmed(false),[data?.selection_hash,sourceExecutionId,projectId])
 async function submit(event:FormEvent){
  event.preventDefault();if(!data||data.targets.length===0||preview.isFetching||preview.isError||(data.requires_full_workflow_confirmation&&!confirmed))return
  const body={mode,...(mode==='targets'?{target_ids:targets}:{}),expected_version:data.source_version,selection_hash:data.selection_hash,change_reason:reason.trim(),...(data.requires_full_workflow_confirmation?{confirm_full_workflow:confirmed}:{})},payload=JSON.stringify(body)
  if(key.current?.payload!==payload)key.current={payload,key:crypto.randomUUID()}
  const requestKey=key.current.key,result=await action.run(()=>rerunExecution(projectId,sourceExecutionId,body,session.csrf_token,requestKey))
  if(result.ok)onDone(result.value.id)
 }
 const message=action.error?.code==='RERUN_SOURCE_UNSETTLED'?'rerunSourceUnsettled':action.error?.code==='RERUN_TARGET_BLOCKED'?'rerunTargetBlocked':action.error?.code==='RERUN_NO_TARGETS'?'rerunNoTargets':action.error?.code==='RERUN_OWNED_FLOW'?'rerunOwnedFlow':action.error?.status===409?'rerunSelectionChanged':''
 return <ActionDialog title={t(mode==='failed'?'rerunFailed':'rerunTarget')} description={`${projectName} · ${sourceExecutionId}`} busy={action.busy} confirm={t('rerunConfirm')} submitDisabled={!data||data.targets.length===0||(mode==='targets'&&data.excluded.length>0)||reason.trim().length<3||preview.isFetching||preview.isError||(data?.requires_full_workflow_confirmation&&!confirmed)} onClose={onClose} onSubmit={event=>void submit(event)}>
  <ActionError error={action.error??preview.error}/>{message&&<p role="alert">{t(message)}</p>}{preview.isPending&&<p role="status">{t('loadingRecords')}</p>}
  <p>{t('rerunSemantics')}</p>
  {data?.requires_full_workflow_confirmation&&<Label><Checkbox checked={confirmed} disabled={action.busy||preview.isFetching||preview.isError} onCheckedChange={value=>setConfirmed(value===true)}/>{t('rerunNativeConfirm',{transport:data.execution_mode})}</Label>}
  {data&&<><dl className="execution-view-facts"><dt>{t('flowRevisionHash')}</dt><dd><code>{data.flow_hash}</code></dd><dt>{t('rerunSourceVersion')}</dt><dd>{data.source_version}</dd><dt>{t('executionTargets')}</dt><dd>{data.targets.length}</dd><dt>{t('rerunExcluded')}</dt><dd>{data.excluded.length}</dd></dl>{data.targets.length===0&&<p role="status">{t('rerunNoTargets')}</p>}<details><summary>{t('rerunFrozenTargets')}</summary><pre>{JSON.stringify(data.targets,null,2)}</pre></details>{data.excluded.length>0&&<details><summary>{t('rerunExcludedReasons')}</summary><pre>{JSON.stringify(data.excluded,null,2)}</pre></details>}<details><summary>{t('executionViewFrozenParameters')}</summary><pre>{JSON.stringify(data.parameters,null,2)}</pre></details></>}
  <div className="field"><Label htmlFor="rerun-reason">{t('flowChangeReason')}</Label><Textarea id="rerun-reason" minLength={3} maxLength={1000} required value={reason} onChange={event=>setReason(event.target.value)}/></div>
  <Button variant="outline" type="button" className="secondary-button" disabled={preview.isFetching||action.busy} onClick={()=>void preview.refetch()}>{t('rerunRefresh')}</Button>
 </ActionDialog>
}
