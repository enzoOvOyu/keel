import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { useProject, canManageProject } from '../../components/ProjectWorkspace'
import { useSession, useExpireOnUnauthorized } from '../../lib/session'
import { useAction } from '../../lib/management'
import { ApiError } from '../../lib/api'
import { latestManagementPlan, previewManagementPlan, commitManagementPlan, importNetworkBaseline, approveNetworkBaseline, type ManagementPlan, type NetworkBaselineInput } from '../../lib/ztp-management'

export function ManagementPlanning() {
 const project=useProject(), session=useSession(), {t}=useTranslation(), client=useQueryClient(), action=useAction()
 const [ref,setRef]=useState(''), [preview,setPreview]=useState<ManagementPlan|null>(null)
 const [baseline,setBaseline]=useState<NetworkBaselineInput|null>(null), [fileError,setFileError]=useState(false)
 const [customer,setCustomer]=useState(''), [network,setNetwork]=useState(''), [imported,setImported]=useState(false), [approved,setApproved]=useState(false)
 const admin=canManageProject(project,session.user.is_admin), canWrite=admin||project.role==='NDE'
 const key=['private',session.user.id,project.id,'ztp-management-plan']
 const latest=useQuery({queryKey:key,queryFn:({signal})=>latestManagementPlan(project.id,signal)})
 useExpireOnUnauthorized(latest.error)
 const shown=preview??latest.data
 async function load(file?:File) {
  setBaseline(null);setImported(false);setApproved(false);setFileError(false)
  if(!file)return
  try { if(file.size>65536)throw new Error();const value=JSON.parse(await file.text()) as NetworkBaselineInput
   if(!value.version_ref||!value.source_ref||!value.engineering_limits?.management)throw new Error()
   setBaseline(value);setRef(value.version_ref);setPreview(null)
  } catch {setFileError(true)}
 }
 async function runPreview(){const result=await action.run(()=>previewManagementPlan(project.id,ref,session.csrf_token));if(result.ok)setPreview(result.value)}
 async function save(){if(!preview)return;const candidate=preview;const result=await action.run(()=>commitManagementPlan(project.id,candidate,session.csrf_token));if(result.ok){client.setQueryData(key,result.value);setPreview(null)}}
 function download(){if(!shown)return;const url=URL.createObjectURL(new Blob([JSON.stringify(shown,null,2)],{type:'application/json'}));const link=document.createElement('a');link.href=url;link.download='ztp-management-plan.json';link.click();URL.revokeObjectURL(url)}
 return <section className="management-planning panel" aria-label={t('managementPlanning')}>
  <h2>{t('managementPlanning')}</h2><p>{t('managementPlanningBoundary')}</p>
  {admin&&<details><summary>{t('managementImport')}</summary><p>{t('managementImportHint')}</p>
   <div className="field"><label htmlFor="management-baseline-file">{t('managementBaselineFile')}</label><input id="management-baseline-file" type="file" accept=".json,application/json" disabled={action.busy} onChange={event=>void load(event.target.files?.[0])}/></div>
   {fileError&&<p role="alert">{t('managementFileInvalid')}</p>}
   {baseline&&<><p>{baseline.version_ref} · {baseline.source_ref}</p><pre>{JSON.stringify(baseline.engineering_limits.management,null,2)}</pre><button className="secondary-button" disabled={action.busy||imported} onClick={()=>void action.run(()=>importNetworkBaseline(project.id,baseline,session.csrf_token)).then(result=>{if(result.ok)setImported(true)})}>{t('managementImportDraft')}</button></>}
   <div className="field"><label htmlFor="management-customer-approval">{t('managementCustomerApproval')}</label><input id="management-customer-approval" value={customer} maxLength={300} onChange={event=>setCustomer(event.target.value)}/></div>
   <div className="field"><label htmlFor="management-network-approval">{t('managementNetworkApproval')}</label><input id="management-network-approval" value={network} maxLength={300} onChange={event=>setNetwork(event.target.value)}/></div>
   <button className="secondary-button" disabled={action.busy||approved||!ref||!customer.trim()||!network.trim()} onClick={()=>void action.run(()=>approveNetworkBaseline(project.id,ref,customer.trim(),network.trim(),session.csrf_token)).then(result=>{if(result.ok){setApproved(true);setPreview(null)}})}>{t('managementApprove')}</button>
   {approved&&<p role="status">{t('managementApproved')}</p>}
  </details>}
  <div className="management-plan-controls"><div className="field"><label htmlFor="management-baseline-ref">{t('managementBaselineRef')}</label><input id="management-baseline-ref" value={ref} maxLength={120} onChange={event=>{setRef(event.target.value);setPreview(null);setApproved(false)}}/></div><button className="secondary-button" disabled={action.busy||!ref.trim()} onClick={()=>void runPreview()}>{t('managementPreview')}</button><button className="secondary-button" disabled={!shown} onClick={download}>{t('managementExport')}</button></div>
  {action.error&&<p role="alert">{t('managementBlocked')}: {action.error.code} · {action.error.message}</p>}
  {latest.error&&<p role="alert">{t('managementLoadFailed')}{latest.error instanceof ApiError?` (${latest.error.code})`:''}</p>}
  {latest.isPending&&<p role="status">{t('managementLoading')}</p>}
  {!shown&&!latest.isPending&&<p>{t('managementEmpty')}</p>}
  {shown&&<><p>{shown.id?t('managementSaved'):t('managementPreviewOnly')} · {shown.baseline_ref} · {t('managementBatchSize')}: {shown.batch_size} · {shown.assignments.length} {t('managementDevices')}</p><p className="management-plan-digest">SHA256: <code>{shown.content_hash}</code></p>
   <div className="management-plan-table"><table><thead><tr>{['managementSerial','managementHostname','managementAddress','managementGateway','managementBatch'].map(label=><th key={label}>{t(label)}</th>)}</tr></thead><tbody>{shown.assignments.map(item=><tr key={item.switch_id}><td>{item.serial_number}</td><td>{item.hostname}</td><td>{item.management_ip}</td><td>{item.gateway}</td><td>{item.batch_ref}</td></tr>)}</tbody></table></div>
   <button disabled={action.busy||!preview||!canWrite} title={!canWrite?t('managementReadOnly'):undefined} onClick={()=>void save()}>{t('managementCommit')}</button>
  </>}
 </section>
}
