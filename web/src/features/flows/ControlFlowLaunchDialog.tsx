import {useRef,useState,type FormEvent} from 'react'
import {useTranslation} from 'react-i18next'
import {Label} from '@/components/ui/label'
import {NativeCheckbox} from '@/components/ui/checkbox'
import {ActionDialog,ActionError} from '../../components/Management'
import {useAction} from '../../lib/management'
import {useSession,useExpireOnUnauthorized} from '../../lib/session'
import {createControlFlowExecution} from '../../lib/flow-workbench'
import type {FlowVersion} from '../../lib/execution'

export function ControlFlowLaunchDialog({flow,onClose,onDone}:{flow:FlowVersion;onClose:()=>void;onDone:(id:string)=>void}){
 const {t}=useTranslation(),session=useSession(),action=useAction(),key=useRef(crypto.randomUUID())
 const [confirmed,setConfirmed]=useState(false)
 useExpireOnUnauthorized(action.error)
 async function submit(event:FormEvent){event.preventDefault();if(!confirmed||action.busy)return
  const result=await action.run(()=>createControlFlowExecution(flow.project_id,{flow_version_id:flow.id},session.csrf_token,key.current))
  if(result.ok)onDone(result.value.id)
 }
 return <ActionDialog title={t('flowLaunch')} description={`${flow.name} · v${flow.version} · ${flow.project_id}`} busy={action.busy} confirm={t('startExecution')} submitDisabled={!confirmed} onClose={onClose} onSubmit={event=>void submit(event)}>
  <ActionError error={action.error}/><p className="impact-notice">{t('flowControlBoundary')}</p>
  <ul>{flow.definition.tasks.map(task=><li key={task.id}><strong>{task.id}</strong> · {task.type} · <code>{task.kv?.key}</code>{task.type==='kv.put'&&<> · {t('flowControlExpectedVersion',{version:task.kv?.expected_version})}</>}</li>)}</ul>
  <Label><NativeCheckbox checked={confirmed} disabled={action.busy} onChange={event=>setConfirmed(event.target.checked)}/>{t('flowControlConfirm')}</Label>
 </ActionDialog>
}
