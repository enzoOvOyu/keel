import { Table, TableHeader, TableRow, TableHead, TableBody, TableCell } from "@/components/ui/table";
import { Button } from "@/components/ui/button";
import { NativeSelect, NativeSelectOption } from "@/components/ui/native-select";
import { Label } from "@/components/ui/label";
import { useState } from 'react'
import { Link } from '@tanstack/react-router'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { useProject, canManageProject } from '../../components/ProjectWorkspace'
import { useSession, useExpireOnUnauthorized } from '../../lib/session'
import { useAction } from '../../lib/management'
import { latestManagementPlan } from '../../lib/ztp-management'
import { listKeaDeployments, stageKeaDeployment, executeKeaDeployment } from '../../lib/kea-deployments'
export function KeaDeployments(){
 const project=useProject(),session=useSession(),{t}=useTranslation(),client=useQueryClient(),action=useAction()
 const [selected,setSelected]=useState(''),[policy,setPolicy]=useState('')
 const plan=useQuery({queryKey:['private',session.user.id,project.id,'ztp-management-plan'],queryFn:({signal})=>latestManagementPlan(project.id,signal)})
 const key=['private',session.user.id,project.id,'kea-deployments']
 const catalog=useQuery({queryKey:key,queryFn:({signal})=>listKeaDeployments(project.id,signal),refetchInterval:5000})
 useExpireOnUnauthorized(catalog.error??plan.error)
 const batches=[...new Set(plan.data?.assignments.map(a=>a.batch_ref)??[])],batch=batches.includes(selected)?selected:batches[0]??''
 const policies=catalog.data?.policies??[],media=policies.some(p=>p.id===policy)?policy:policies[0]?.id??''
 const canWrite=canManageProject(project,session.user.is_admin)||project.role==='NDE'
 async function stage(){const result=await action.run(()=>stageKeaDeployment(project.id,batch,media,session.csrf_token));if(result.ok)await client.invalidateQueries({queryKey:key})}
 async function execute(id:string){const result=await action.run(()=>executeKeaDeployment(project.id,id,session.csrf_token));if(result.ok)await client.invalidateQueries({queryKey:key})}
 return <section className="management-planning panel" aria-label={t('keaDeployments')}><h2>{t('keaDeployments')}</h2><p>{t('keaDeploymentBoundary')}</p>
  {catalog.data&&!catalog.data.enabled&&<p role="status">{t('keaWriteDisabled')}</p>}
  <div className="management-plan-controls"><div className="field"><Label className="ui:block ui:leading-normal" htmlFor="kea-live-batch">{t('managementBatch')}</Label><NativeSelect id="kea-live-batch" value={batch} disabled={action.busy||!batches.length} onChange={e=>setSelected(e.target.value)}>{!batches.length&&<NativeSelectOption value="">{t('environmentPlanRequired')}</NativeSelectOption>}{batches.map(value=><NativeSelectOption key={value}>{value}</NativeSelectOption>)}</NativeSelect></div>
  <div className="field"><Label className="ui:block ui:leading-normal" htmlFor="kea-live-policy">{t('keaApprovedPolicy')}</Label><NativeSelect id="kea-live-policy" value={media} disabled={action.busy||!policies.length} onChange={e=>setPolicy(e.target.value)}>{!policies.length&&<NativeSelectOption value="">{t('keaNoApprovedPolicy')}</NativeSelectOption>}{policies.map(p=><NativeSelectOption key={p.id} value={p.id}>{p.name} · {p.version}</NativeSelectOption>)}</NativeSelect></div>
  <Button variant="outline" className="secondary-button" disabled={action.busy||!canWrite||!catalog.data?.enabled||!batch||!media} onClick={()=>void stage()}>{t('keaStageLive')}</Button></div>
  {(action.error||catalog.error)&&<p role="alert">{t('keaOperationFailed')}{action.error&&` · ${action.error.code}: ${action.error.message}`}</p>}
  <div className="management-plan-table"><Table scrollContainer={false} className="ui:text-xs"><TableHeader><TableRow><TableHead className="ui:px-3 ui:py-2">{t('managementBatch')}</TableHead><TableHead className="ui:px-3 ui:py-2">{t('environmentState')}</TableHead><TableHead className="ui:px-3 ui:py-2">{t('keaCandidate')}</TableHead><TableHead className="ui:px-3 ui:py-2">{t('actions')}</TableHead></TableRow></TableHeader><TableBody>{catalog.data?.deployments.map(job=><TableRow key={job.id}><TableCell className="ui:px-3 ui:py-2">{job.spec.options.batch_ref}</TableCell><TableCell className="ui:px-3 ui:py-2"><span>{t('keaState_'+job.state)}</span>{job.failure_code&&<small> · {job.failure_code}</small>}{job.verification_code&&<small> · {t('keaVerification')}: {job.verification_code}</small>}</TableCell><TableCell className="ui:px-3 ui:py-2"><code title={job.candidate_hash}>{job.candidate_hash.slice(0,16)}…</code><details><summary>{t('keaReview')}</summary><p>{t('keaBeforeHash')}: <code>{job.before_hash}</code></p><p>{t('keaCandidate')}: <code>{job.candidate_hash}</code></p><p>{job.spec.options.option_239_url}</p><p>{t(job.spec.vendor_profile?'keaVendorProfile':'keaVendorNotConfigured')}</p>{job.spec.vendor_profile&&<pre>{JSON.stringify(job.spec.vendor_profile,null,2)}</pre>}<pre>{JSON.stringify(job.spec.bindings,null,2)}</pre>{job.spec.canary&&<><p>{t('keaCanary')}</p><pre>{JSON.stringify(job.spec.canary,null,2)}</pre></>}{job.dora&&<><p>{t('keaDoraProof')}</p><pre>{JSON.stringify(job.dora,null,2)}</pre></>}</details></TableCell><TableCell className="ui:px-3 ui:py-2">{job.state==='staged'&&<Button variant="outline" className="secondary-button" disabled={action.busy||!canWrite||!catalog.data?.enabled} onClick={()=>void execute(job.id)}>{t('keaExecute')}</Button>}{job.execution_id&&<Link to="/p/$projectId/executions" params={{projectId:project.id}} search={{execution:job.execution_id}}>{t('keaExecutionEvidence')}</Link>}</TableCell></TableRow>)}</TableBody></Table></div>
  {catalog.data?.deployments.length===0&&<p>{t('keaNoDeployments')}</p>}
 </section>
}
