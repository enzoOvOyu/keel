import {useState} from 'react'
import {useQuery} from '@tanstack/react-query'
import {useTranslation} from 'react-i18next'
import {useProject} from '../../components/ProjectWorkspace'
import {useSession,useExpireOnUnauthorized} from '../../lib/session'
import {managementArtifacts,manageabilityChecks,deviceSessions,deviceSession} from '../../lib/ztp-completion'
import {latestManagementPlan} from '../../lib/ztp-management'
import {ApiError} from '../../lib/api'
export function ZTPCompletion(){
 const project=useProject(),session=useSession(),{t}=useTranslation(),[chosenBatch,setBatch]=useState(''),[chosenSession,setSession]=useState('')
 const plan=useQuery({queryKey:['private',session.user.id,project.id,'ztp-management-plan'],queryFn:({signal})=>latestManagementPlan(project.id,signal)})
 const batches=[...new Set(plan.data?.assignments.map(a=>a.batch_ref)??[])],batch=batches.includes(chosenBatch)?chosenBatch:batches[0]??''
 const preview=useQuery({queryKey:['private',session.user.id,project.id,'ztp-management-artifacts',batch,plan.data?.content_hash],queryFn:({signal})=>managementArtifacts(project.id,batch,signal),enabled:Boolean(batch),retry:false})
 const checks=useQuery({queryKey:['private',session.user.id,project.id,'ztp-manageability-checks'],queryFn:({signal})=>manageabilityChecks(project.id,signal),refetchInterval:5000,retry:false})
 const sessions=useQuery({queryKey:['private',session.user.id,project.id,'ztp-device-sessions'],queryFn:({signal})=>deviceSessions(project.id,signal),refetchInterval:5000,retry:false})
 const selected=sessions.data?.items.some(v=>v.id===chosenSession)?chosenSession:sessions.data?.items[0]?.id??''
 const detail=useQuery({queryKey:['private',session.user.id,project.id,'ztp-device-session',selected],queryFn:({signal})=>deviceSession(project.id,selected,signal),enabled:Boolean(selected),refetchInterval:5000,retry:false})
 useExpireOnUnauthorized(preview.error??checks.error??sessions.error??detail.error)
 function download(){if(!preview.data)return;const url=URL.createObjectURL(new Blob([preview.data.content],{type:'application/json'})),a=document.createElement('a');a.href=url;a.download='ztp-management-manifest.json';a.click();setTimeout(()=>URL.revokeObjectURL(url),1000)}
 return <section className="panel"><h2>{t('ztpCompletionTitle')}</h2><p>{t('ztpCompletionBoundary')}</p><div className="management-plan-controls"><label>{t('ztpBatch')}<select value={batch} onChange={e=>setBatch(e.target.value)}><option value="">{t('ztpCompletionNoPlan')}</option>{batches.map(b=><option key={b}>{b}</option>)}</select></label><button className="secondary-button" disabled={!preview.data} onClick={download}>{t('ztpCompletionManifestExport')}</button><button className="secondary-button" onClick={()=>{void preview.refetch();void checks.refetch();void sessions.refetch()}}>{t('networkHealthRefresh')}</button></div>
 {preview.error&&<p role="alert">{t('ztpCompletionPreviewBlocked')}: {preview.error instanceof ApiError?preview.error.code:t('fabricUnknown')}</p>}
 {(checks.error||sessions.error||detail.error)&&<p role="alert">{t('ztpCompletionReadFailed')}</p>}
 {preview.data&&<><p>{t('ztpCompletionApproval')}: SHA256 <code>{preview.data.sha256}</code> · {preview.data.size} bytes</p><details><summary>{t('ztpCompletionReviewScripts')}</summary>{preview.data.scripts.map(script=><article key={script.switch_id}><h3>{script.serial_number}</h3><p>SHA256 {script.sha256} · {script.size} bytes · {script.credential_batch_ref}</p><pre>{script.body}</pre></article>)}</details></>}
 <h3>{t('ztpCompletionChecks')}</h3><div className="management-plan-table"><table><thead><tr>{['fabricSerial','networkHealthSource','networkPushResult','ztpCompletionAttempts','ztpCompletionNextRetry','networkHealthDecisionAt'].map(k=><th key={k}>{t(k)}</th>)}</tr></thead><tbody>{checks.data?.items.map(c=><tr key={c.id}><td>{c.proof?.serial_number??c.switch_id}</td><td>{c.evidence_source}</td><td>{c.code}</td><td>{c.attempt}</td><td>{c.next_retry_at?new Date(c.next_retry_at).toLocaleString():'—'}</td><td>{new Date(c.checked_at).toLocaleString()}</td></tr>)}</tbody></table></div>{checks.data?.items.length===0&&<p>{t('ztpCompletionNoChecks')}</p>}
 <h3>{t('ztpCompletionDeviceSessions')}</h3><div className="management-plan-table"><table><thead><tr>{['ztpBatch','ztpCompletionChannel','networkPushResult','ztpCompletionAttempts'].map(k=><th key={k}>{t(k)}</th>)}</tr></thead><tbody>{sessions.data?.items.map(s=><tr key={s.id}><td><button className="fabric-device-link" onClick={()=>setSession(s.id)}>{s.batch_ref}</button></td><td>{s.boot_channel}</td><td>{s.state}</td><td>{s.attempt}</td></tr>)}</tbody></table></div>{sessions.data?.items.length===0&&<p>{t('ztpCompletionNoRealSession')}</p>}
 {detail.data&&<details open><summary>{t('ztpCompletionTimeline')}</summary><ol>{detail.data.events.map(e=><li key={e.seq}>{e.from_state?e.from_state+' → ':''}{e.to_state} · {e.event_code} · {new Date(e.occurred_at).toLocaleString()} · {e.evidence_ref}</li>)}</ol>{detail.data.failure_cause&&<p role="alert">{detail.data.failure_phenomenon} / {detail.data.failure_cause} / {detail.data.failure_next_action}</p>}</details>}
 </section>
}
