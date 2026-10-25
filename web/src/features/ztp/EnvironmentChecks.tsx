import { Table, TableHeader, TableRow, TableHead, TableBody, TableCell } from "@/components/ui/table";
import { Button } from "@/components/ui/button";
import { NativeSelect, NativeSelectOption } from "@/components/ui/native-select";
import { Label } from "@/components/ui/label";
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
  <div className="management-plan-controls"><div className="field"><Label className="ui:block ui:leading-normal" htmlFor="environment-batch">{t('managementBatch')}</Label><NativeSelect id="environment-batch" value={batch} disabled={!batches.length||action.busy} onChange={event=>setChoice(event.target.value)}>{!batches.length&&<NativeSelectOption value="">{t('environmentPlanRequired')}</NativeSelectOption>}{batches.map(value=><NativeSelectOption key={value}>{value}</NativeSelectOption>)}</NativeSelect></div>
  <div className="field"><Label className="ui:block ui:leading-normal" htmlFor="environment-channel">{t('environmentChannel')}</Label><NativeSelect id="environment-channel" value={channel} disabled={action.busy} onChange={event=>setChannel(event.target.value as '239'|'143')}><NativeSelectOption value="239">239 HTTPS</NativeSelectOption><NativeSelectOption value="143">143 SZTP / mTLS</NativeSelectOption></NativeSelect></div>
  <Button variant="outline" className="secondary-button" disabled={!batch||!canWrite||action.busy} title={!canWrite?t('managementReadOnly'):undefined} onClick={()=>void action.run(()=>runEnvironmentCheck(project.id,batch,channel,session.csrf_token)).then(result=>{if(result.ok){client.setQueryData(key,result.value);setClock(Date.now())}})}>{t(action.busy?'environmentChecking':'environmentRun')}</Button></div>
  {(action.error||report.error||plan.error)&&<p role="alert">{t('environmentFailed')}{action.error&&` · ${action.error.code}: ${action.error.message}`}</p>}
  {report.data?<><p role="status">{t(ready?'environmentReady':'environmentBlocked')} · {t('environmentExpires')} {new Date(report.data.expires_at).toLocaleString()}</p><div className="management-plan-table"><Table scrollContainer={false} className="ui:text-xs"><TableHeader><TableRow><TableHead className="ui:px-3 ui:py-2">{t('environmentService')}</TableHead><TableHead className="ui:px-3 ui:py-2">{t('environmentState')}</TableHead><TableHead className="ui:px-3 ui:py-2">{t('environmentEvidence')}</TableHead></TableRow></TableHeader><TableBody>{report.data.checks.map(item=><TableRow key={item.service}><TableCell className="ui:px-3 ui:py-2">{t(`environmentService_${item.service}`)}</TableCell><TableCell className="ui:px-3 ui:py-2">{t(`environmentState_${item.state}`)}</TableCell><TableCell className="ui:px-3 ui:py-2">{item.code} · {item.duration_ms}ms</TableCell></TableRow>)}</TableBody></Table></div></>:<p>{t('environmentNotRun')}</p>}
 </section>
}
