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
  <div className="management-plan-controls"><div className="field"><label htmlFor="kea-live-batch">{t('managementBatch')}</label><select id="kea-live-batch" value={batch} disabled={action.busy||!batches.length} onChange={e=>setSelected(e.target.value)}>{!batches.length&&<option value="">{t('environmentPlanRequired')}</option>}{batches.map(value=><option key={value}>{value}</option>)}</select></div>
  <div className="field"><label htmlFor="kea-live-policy">{t('keaApprovedPolicy')}</label><select id="kea-live-policy" value={media} disabled={action.busy||!policies.length} onChange={e=>setPolicy(e.target.value)}>{!policies.length&&<option value="">{t('keaNoApprovedPolicy')}</option>}{policies.map(p=><option key={p.id} value={p.id}>{p.name} · {p.version}</option>)}</select></div>
  <button className="secondary-button" disabled={action.busy||!canWrite||!catalog.data?.enabled||!batch||!media} onClick={()=>void stage()}>{t('keaStageLive')}</button></div>
  {(action.error||catalog.error)&&<p role="alert">{t('keaOperationFailed')}{action.error&&` · ${action.error.code}: ${action.error.message}`}</p>}
  <div className="management-plan-table"><table><thead><tr><th>{t('managementBatch')}</th><th>{t('environmentState')}</th><th>{t('keaCandidate')}</th><th>{t('actions')}</th></tr></thead><tbody>{catalog.data?.deployments.map(job=><tr key={job.id}><td>{job.spec.options.batch_ref}</td><td><span>{t('keaState_'+job.state)}</span>{job.failure_code&&<small> · {job.failure_code}</small>}{job.verification_code&&<small> · {t('keaVerification')}: {job.verification_code}</small>}</td><td><code title={job.candidate_hash}>{job.candidate_hash.slice(0,16)}…</code><details><summary>{t('keaReview')}</summary><p>{t('keaBeforeHash')}: <code>{job.before_hash}</code></p><p>{t('keaCandidate')}: <code>{job.candidate_hash}</code></p><p>{job.spec.options.option_239_url}</p><pre>{JSON.stringify(job.spec.bindings,null,2)}</pre>{job.spec.canary&&<><p>{t('keaCanary')}</p><pre>{JSON.stringify(job.spec.canary,null,2)}</pre></>}{job.dora&&<><p>{t('keaDoraProof')}</p><pre>{JSON.stringify(job.dora,null,2)}</pre></>}</details></td><td>{job.state==='staged'&&<button className="secondary-button" disabled={action.busy||!canWrite||!catalog.data?.enabled} onClick={()=>void execute(job.id)}>{t('keaExecute')}</button>}{job.execution_id&&<Link to="/p/$projectId/executions" params={{projectId:project.id}} search={{execution:job.execution_id}}>{t('keaExecutionEvidence')}</Link>}</td></tr>)}</tbody></table></div>
  {catalog.data?.deployments.length===0&&<p>{t('keaNoDeployments')}</p>}
 </section>
}
