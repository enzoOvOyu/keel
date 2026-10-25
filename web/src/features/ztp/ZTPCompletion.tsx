import { Table, TableHeader, TableRow, TableHead, TableBody, TableCell } from "@/components/ui/table";
import { Button } from "@/components/ui/button";
import { NativeSelect, NativeSelectOption } from "@/components/ui/native-select";
import { Label } from "@/components/ui/label";
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
 return <section className="panel"><h2>{t('ztpCompletionTitle')}</h2><p>{t('ztpCompletionBoundary')}</p><div className="management-plan-controls"><Label className="ui:flex-col ui:items-start ui:leading-normal">{t('ztpBatch')}<NativeSelect value={batch} onChange={e=>setBatch(e.target.value)}><NativeSelectOption value="">{t('ztpCompletionNoPlan')}</NativeSelectOption>{batches.map(b=><NativeSelectOption key={b}>{b}</NativeSelectOption>)}</NativeSelect></Label><Button variant="outline" className="secondary-button" disabled={!preview.data} onClick={download}>{t('ztpCompletionManifestExport')}</Button><Button variant="outline" className="secondary-button" onClick={()=>{void preview.refetch();void checks.refetch();void sessions.refetch()}}>{t('networkHealthRefresh')}</Button></div>
 {preview.error&&<p role="alert">{t('ztpCompletionPreviewBlocked')}: {preview.error instanceof ApiError?preview.error.code:t('fabricUnknown')}</p>}
 {(checks.error||sessions.error||detail.error)&&<p role="alert">{t('ztpCompletionReadFailed')}</p>}
 {preview.data&&<><p>{t('ztpCompletionApproval')}: SHA256 <code>{preview.data.sha256}</code> · {preview.data.size} bytes</p><details><summary>{t('ztpCompletionReviewScripts')}</summary>{preview.data.scripts.map(script=><article key={script.switch_id}><h3>{script.serial_number}</h3><p>SHA256 {script.sha256} · {script.size} bytes · {script.credential_batch_ref}</p><pre>{script.body}</pre></article>)}</details></>}
 <h3>{t('ztpCompletionChecks')}</h3><div className="management-plan-table"><Table scrollContainer={false} className="ui:text-xs"><TableHeader><TableRow>{['fabricSerial','networkHealthSource','networkPushResult','ztpCompletionAttempts','ztpCompletionNextRetry','networkHealthDecisionAt'].map(k=><TableHead className="ui:px-3 ui:py-2" key={k}>{t(k)}</TableHead>)}</TableRow></TableHeader><TableBody>{checks.data?.items.map(c=><TableRow key={c.id}><TableCell className="ui:px-3 ui:py-2">{c.proof?.serial_number??c.switch_id}</TableCell><TableCell className="ui:px-3 ui:py-2">{c.evidence_source}</TableCell><TableCell className="ui:px-3 ui:py-2">{c.code}</TableCell><TableCell className="ui:px-3 ui:py-2">{c.attempt}</TableCell><TableCell className="ui:px-3 ui:py-2">{c.next_retry_at?new Date(c.next_retry_at).toLocaleString():'—'}</TableCell><TableCell className="ui:px-3 ui:py-2">{new Date(c.checked_at).toLocaleString()}</TableCell></TableRow>)}</TableBody></Table></div>{checks.data?.items.length===0&&<p>{t('ztpCompletionNoChecks')}</p>}
 <h3>{t('ztpCompletionDeviceSessions')}</h3><div className="management-plan-table"><Table scrollContainer={false} className="ui:text-xs"><TableHeader><TableRow>{['ztpBatch','ztpCompletionChannel','networkPushResult','ztpCompletionAttempts'].map(k=><TableHead className="ui:px-3 ui:py-2" key={k}>{t(k)}</TableHead>)}</TableRow></TableHeader><TableBody>{sessions.data?.items.map(s=><TableRow key={s.id}><TableCell className="ui:px-3 ui:py-2"><Button variant="link" size="sm" className="fabric-device-link" onClick={()=>setSession(s.id)}>{s.batch_ref}</Button></TableCell><TableCell className="ui:px-3 ui:py-2">{s.boot_channel}</TableCell><TableCell className="ui:px-3 ui:py-2">{s.state}</TableCell><TableCell className="ui:px-3 ui:py-2">{s.attempt}</TableCell></TableRow>)}</TableBody></Table></div>{sessions.data?.items.length===0&&<p>{t('ztpCompletionNoRealSession')}</p>}
 {detail.data&&<details open><summary>{t('ztpCompletionTimeline')}</summary><ol>{detail.data.events.map(e=><li key={e.seq}>{e.from_state?e.from_state+' → ':''}{e.to_state} · {e.event_code} · {new Date(e.occurred_at).toLocaleString()} · {e.evidence_ref}</li>)}</ol>{detail.data.failure_cause&&<p role="alert">{detail.data.failure_phenomenon} / {detail.data.failure_cause} / {detail.data.failure_next_action}</p>}</details>}
 </section>
}
