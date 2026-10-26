import { useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { NativeCheckbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { ActionDialog, ActionError } from '../../components/Management'
import { useAction } from '../../lib/management'
import { useSession } from '../../lib/session'
import { requestProvisionKeyRepairBatch, type ProvisionKeyRepairBatchInput, type ProvisionInstallation, type ProvisionInstallationProgress } from '../../lib/provision'

export type KeyRepairBatchMode = ProvisionKeyRepairBatchInput['action']
export type KeyRepairBatchChoice = { item:ProvisionInstallation; progress:ProvisionInstallationProgress; mode:KeyRepairBatchMode }
export function KeyRepairBatch({items,project,onClose,onDone}:{items:KeyRepairBatchChoice[];project:string;onClose:()=>void;onDone:()=>Promise<void>}) {
 const {t}=useTranslation(),session=useSession(),action=useAction(),key=useRef(crypto.randomUUID()),[reason,setReason]=useState(''),[confirmed,setConfirmed]=useState<string[]>([])
 const mode=items[0]?.mode ?? 'create'
 const valid=items.length>0 && items.length<=32 && items.every(({item,progress,mode:choice})=>item.project_id===project && progress.installation_id===item.id && choice===mode && progress.state==='awaiting_first_boot' && progress.resource.lease_state==='held' && !progress.resource.lease_expired && (mode==='create' ? !!item.spec?.expected.serial_number && progress.ssh_repair?.status==='available' && (!progress.key_repair || ['revoked','succeeded'].includes(progress.key_repair.state)) : !!progress.key_repair && (mode==='approve' ? progress.key_repair.state==='pending' && progress.key_repair.created_by!==session.user.id : mode==='revoke' ? ['pending','approved'].includes(progress.key_repair.state) : ['consumed','unknown'].includes(progress.key_repair.state))))
 return <ActionDialog title={t(`keyRepairBatch_${mode}`)} description={t('keyRepairBatchDetail')} busy={action.busy} danger confirm={t(`keyRepairBatch_${mode}`)} submitDisabled={!valid || !reason.trim() || confirmed.length!==items.length} onClose={onClose} onSubmit={event=>{
  event.preventDefault();if(!valid || !reason.trim() || confirmed.length!==items.length)return
  const selected=items.map(({item,progress})=>mode==='create' ? {installation_id:item.id,expected_version:progress.state_version,repair_event_id:progress.ssh_repair!.event_id,confirm_serial:item.spec!.expected.serial_number} : {installation_id:item.id,expected_version:progress.key_repair!.state_version,repair_id:progress.key_repair!.id})
  void action.run(()=>requestProvisionKeyRepairBatch(project,{action:mode,items:selected,reason:reason.trim()},session.csrf_token,key.current)).then(async result=>{if(result.ok)await onDone()})
 }}><p className="impact-notice">{t('keyRepairBatchImpact')}</p><p>{t('keyRepairImpact')}</p><ActionError error={action.error}/><ul className="ui:space-y-3">{items.map(({item,progress})=><li key={item.id}><div className="checkbox-field"><NativeCheckbox id={`key-repair-batch-confirm-${item.id}`} checked={confirmed.includes(item.id)} disabled={action.busy} onChange={event=>setConfirmed(values=>event.target.checked?[...values,item.id]:values.filter(id=>id!==item.id))}/><Label htmlFor={`key-repair-batch-confirm-${item.id}`}>{t('keyRepairBatchConfirm')} · {item.spec?.expected.serial_number ?? item.machine_id} · {item.id} · v{mode==='create'?progress.state_version:progress.key_repair?.state_version}</Label></div>{progress.key_repair && mode!=='create' && <><p className="mono-value ui:break-all">{progress.key_repair.snapshot.host_key}</p><p>{progress.key_repair.snapshot.credential.id} · {progress.key_repair.snapshot.credential.credential_version} · {t('keyRepairRequester')}: {progress.key_repair.created_by}</p></>}</li>)}</ul><div className="field"><Label htmlFor="key-repair-batch-reason">{t('installReason')}</Label><Input id="key-repair-batch-reason" required maxLength={500} disabled={action.busy} value={reason} onChange={event=>{setReason(event.target.value);key.current=crypto.randomUUID()}}/></div></ActionDialog>
}
