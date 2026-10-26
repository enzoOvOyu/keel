import type { KeyRepairBatchMode } from './KeyRepairBatch'
import { useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { ActionDialog, ActionError } from '../../components/Management'
import { useProject } from '../../components/ProjectWorkspace'
import { useAction } from '../../lib/management'
import { useSession } from '../../lib/session'
import { actProvisionKeyRepair, requestProvisionKeyRepair, type ProvisionInstallation, type ProvisionInstallationProgress, type ProvisionKeyRepair } from '../../lib/provision'

type Choice = { mode:'create'|'approve'|'revoke'|'query'; version:number; repair?:ProvisionKeyRepair }
export function KeyRepair({item,progress,refresh,onSelectBatch,selectedBatchMode,canSelectBatch}:{item:ProvisionInstallation;progress:ProvisionInstallationProgress;refresh:()=>Promise<void>;onSelectBatch?:(mode:KeyRepairBatchMode)=>void;selectedBatchMode?:KeyRepairBatchMode;canSelectBatch?:(mode:KeyRepairBatchMode)=>boolean}) {
 const {t}=useTranslation(),session=useSession(),project=useProject(),[choice,setChoice]=useState<Choice>()
 const allowed=(session.user.is_admin || ['Admin','DL','SRE'].includes(project.role)) && !['archived','delivered'].includes(project.state)
 const repair=progress.key_repair, held=progress.state==='awaiting_first_boot' && progress.resource.lease_state==='held' && !progress.resource.lease_expired
 const active=repair && !['revoked','succeeded'].includes(repair.state)
 const live=!!repair && new Date(repair.expires_at).getTime()>Date.now()
 const approval=allowed && (session.user.is_admin || ['Admin','DL'].includes(project.role)) && repair?.created_by!==session.user.id
 const current=held && !!choice && progress.state_version===choice.version && (choice.mode==='create' ? !active && progress.ssh_repair?.status==='available' : !!repair && repair.id===choice.repair?.id && repair.state_version===choice.repair?.state_version)
 return <div className="ui:space-y-2"><h4>{t('keyRepairTitle')}</h4><p className="cell-subtitle">{t('keyRepairHint')}</p>{repair && <><p>{t(`keyRepairState_${repair.state}`)} · v{repair.state_version}</p><p className="mono-value ui:break-all">{repair.snapshot.host_key}</p><p>{t('keyRepairFrozenKey')}: {repair.snapshot.credential.id} · {repair.snapshot.credential.credential_version}</p><p>{t('keyRepairRequester')}: {repair.created_by}{repair.approved_by && <> · {t('keyRepairApprover')}: {repair.approved_by}</>}</p><p>{t('provisionExpiry')}: {new Date(repair.expires_at).toLocaleString()}</p></>}
 {allowed && held && !active && progress.ssh_repair?.status==='available' && <Button variant="outline" size="sm" onClick={()=>setChoice({mode:'create',version:progress.state_version})}>{t('keyRepairCreate')}</Button>}
 {allowed && held && repair && <div className="ui:flex ui:flex-wrap ui:gap-2">{approval && live && repair.state==='pending' && <Button variant="outline" size="sm" onClick={()=>setChoice({mode:'approve',version:progress.state_version,repair})}>{t('keyRepairApprove')}</Button>}{['pending','approved'].includes(repair.state) && <Button variant="outline" size="sm" onClick={()=>setChoice({mode:'revoke',version:progress.state_version,repair})}>{t('keyRepairRevoke')}</Button>}{live && ['consumed','unknown'].includes(repair.state) && <Button variant="outline" size="sm" onClick={()=>setChoice({mode:'query',version:progress.state_version,repair})}>{t('keyRepairQuery')}</Button>}</div>}
 {allowed && held && onSelectBatch && <div className="ui:flex ui:flex-wrap ui:gap-2">{(['create','approve','revoke','query'] as const).filter(mode=>mode==='create' ? !active && progress.ssh_repair?.status==='available' : !!repair && (mode==='approve' ? approval && live && repair.state==='pending' : mode==='revoke' ? ['pending','approved'].includes(repair.state) : live && ['consumed','unknown'].includes(repair.state))).map(mode=><Button key={mode} size="sm" variant={selectedBatchMode===mode?'default':'outline'} aria-pressed={selectedBatchMode===mode} disabled={selectedBatchMode!==mode && !!canSelectBatch && !canSelectBatch(mode)} onClick={()=>onSelectBatch(mode)}>{t(selectedBatchMode===mode?'keyRepairBatchRemove':`keyRepairBatchSelect_${mode}`)}</Button>)}</div>}
 {choice && <RepairDialog key={`${choice.mode}:${choice.repair?.id ?? progress.ssh_repair?.event_id}`} choice={choice} item={item} progress={progress} current={current} close={()=>setChoice(undefined)} done={async()=>{setChoice(undefined);await refresh()}}/>}
 </div>
}
function RepairDialog({choice,item,progress,current,close,done}:{choice:Choice;item:ProvisionInstallation;progress:ProvisionInstallationProgress;current:boolean;close:()=>void;done:()=>Promise<void>}) {
 const {t}=useTranslation(),session=useSession(),action=useAction(),key=useRef(crypto.randomUUID()),[reason,setReason]=useState(''),[serial,setSerial]=useState('')
 const labels={create:'keyRepairCreate',approve:'keyRepairApprove',revoke:'keyRepairRevoke',query:'keyRepairQuery'}
 const create=choice.mode==='create',repairEvent=progress.ssh_repair?.event_id
 const valid=current && !!reason.trim() && (!create || (!!repairEvent && !!item.spec?.expected.serial_number && serial===item.spec.expected.serial_number))
 return <ActionDialog title={t(labels[choice.mode])} description={item.id} busy={action.busy} danger confirm={t(labels[choice.mode])} submitDisabled={!valid} onClose={close} onSubmit={event=>{
 event.preventDefault();if(!valid)return
 void action.run(()=>create && repairEvent ? requestProvisionKeyRepair(item.project_id,item.id,{expected_version:choice.version,repair_event_id:repairEvent,confirm_serial:serial,reason:reason.trim()},session.csrf_token,key.current) : choice.mode!=='create' && choice.repair ? actProvisionKeyRepair(choice.repair,choice.mode,reason.trim(),session.csrf_token,key.current) : Promise.reject(new Error('Repair selection changed'))).then(async result=>{if(result.ok)await done()})
 }}><p className="impact-notice">{t('keyRepairImpact')}</p><p>{t('keyRepairPrerequisite')}</p>{choice.repair && <dl><dt>{t('keyRepairFrozenKey')}</dt><dd className="mono-value ui:break-all">{choice.repair.snapshot.credential.id} · {choice.repair.snapshot.credential.credential_version}</dd><dt>{t('keyRepairHost')}</dt><dd className="mono-value ui:break-all">{choice.repair.snapshot.host_key}</dd></dl>}{!current && <p role="status">{t('installVersionChanged')}</p>}<ActionError error={action.error}/><div className="field"><Label htmlFor="key-repair-reason">{t('installReason')}</Label><Input id="key-repair-reason" maxLength={500} required disabled={action.busy} value={reason} onChange={event=>{setReason(event.target.value);key.current=crypto.randomUUID()}}/></div>{create && <div className="field"><Label htmlFor="key-repair-serial">{t('installSerialConfirm')} · {item.spec?.expected.serial_number}</Label><Input id="key-repair-serial" required autoComplete="off" disabled={action.busy} value={serial} onChange={event=>{setSerial(event.target.value);key.current=crypto.randomUUID()}}/></div>}</ActionDialog>
}
