import { useEffect, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { useProject, canManageProject } from '../../components/ProjectWorkspace'
import { useSession, useExpireOnUnauthorized } from '../../lib/session'
import { useAction } from '../../lib/management'
import { latestManagementPlan, latestEnvironmentReport, runEnvironmentCheck } from '../../lib/ztp-management'
export function EnvironmentChecks(){
 const project=useProject(),session=useSession(),{t}=useTranslation(),client=useQueryClient(),action=useAction()
 const [choice,setChoice]=useState(''),[channel,setChannel]=useState<'239'|'143'>('239'),[clock,setClock]=useState(Date.now())
 const plan=useQuery({queryKey:['private',session.user.id,project.id,'ztp-management-plan'],queryFn:({signal})=>latestManagementPlan(project.id,signal)})
 const batches=[...new Set(plan.data?.assignments.map(item=>item.batch_ref)??[])],batch=batches.includes(choice)?choice:batches[0]??''
 const key=['private',session.user.id,project.id,'ztp-environment',plan.data?.id,batch,channel]
 const report=useQuery({queryKey:key,queryFn:({signal})=>latestEnvironmentReport(project.id,batch,channel,signal),enabled:!!batch})
 useExpireOnUnauthorized(plan.error??report.error)
 const canWrite=canManageProject(project,session.user.is_admin)||project.role==='NDE'
 useEffect(()=>{if(!report.data)return;const timer=setInterval(()=>setClock(Date.now()),1000);return()=>clearInterval(timer)},[report.data])
 const ready=report.data?.ready&&clock<new Date(report.data.expires_at).getTime()
 return <section className="management-planning panel" aria-label={t('environmentChecks')}><h2>{t('environmentChecks')}</h2><p>{t('environmentChecksBoundary')}</p>
  <div className="management-plan-controls"><div className="field"><label htmlFor="environment-batch">{t('managementBatch')}</label><select id="environment-batch" value={batch} disabled={!batches.length||action.busy} onChange={event=>setChoice(event.target.value)}>{!batches.length&&<option value="">{t('environmentPlanRequired')}</option>}{batches.map(value=><option key={value}>{value}</option>)}</select></div>
  <div className="field"><label htmlFor="environment-channel">{t('environmentChannel')}</label><select id="environment-channel" value={channel} disabled={action.busy} onChange={event=>setChannel(event.target.value as '239'|'143')}><option value="239">239 HTTPS</option><option value="143">143 SZTP / mTLS</option></select></div>
  <button className="secondary-button" disabled={!batch||!canWrite||action.busy} title={!canWrite?t('managementReadOnly'):undefined} onClick={()=>void action.run(()=>runEnvironmentCheck(project.id,batch,channel,session.csrf_token)).then(result=>{if(result.ok){client.setQueryData(key,result.value);setClock(Date.now())}})}>{t(action.busy?'environmentChecking':'environmentRun')}</button></div>
  {(action.error||report.error||plan.error)&&<p role="alert">{t('environmentFailed')}{action.error&&` · ${action.error.code}: ${action.error.message}`}</p>}
  {report.data?<><p role="status">{t(ready?'environmentReady':'environmentBlocked')} · {t('environmentExpires')} {new Date(report.data.expires_at).toLocaleString()}</p><div className="management-plan-table"><table><thead><tr><th>{t('environmentService')}</th><th>{t('environmentState')}</th><th>{t('environmentEvidence')}</th></tr></thead><tbody>{report.data.checks.map(item=><tr key={item.service}><td>{t(`environmentService_${item.service}`)}</td><td>{t(`environmentState_${item.state}`)}</td><td>{item.code} · {item.duration_ms}ms</td></tr>)}</tbody></table></div></>:<p>{t('environmentNotRun')}</p>}
 </section>
}
