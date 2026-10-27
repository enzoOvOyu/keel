import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from "@/components/ui/table";
import {Button} from '@/components/ui/button'
import {Link} from '@tanstack/react-router'
import {useState} from 'react'
import {ActionError} from '../../components/Management'
import {useAction} from '../../lib/management'
import {issueLabels} from '../../lib/issues'
import type {components} from '../../generated/api'
import {useQuery} from '@tanstack/react-query'
import {useTranslation} from 'react-i18next'
import {useSession,useExpireOnUnauthorized} from '../../lib/session'
import {fabricDiagnostics,recordFabricDiagnostics} from '../../lib/fabric-diagnostics'
import {FabricControlPanel} from './FabricControls'
export function FabricDiagnosticsPanel({projectId,deviceId,type}:{projectId:string;deviceId:string;type?:'T6'}){
 const session=useSession(),{t,i18n}=useTranslation(),action=useAction(),[record,setRecord]=useState<components['schemas']['FabricDiagnosticRecord']|null>(null),query=useQuery({queryKey:['private',session.user.id,projectId,'fabric-diagnostics',deviceId],queryFn:({signal})=>fabricDiagnostics(projectId,deviceId,signal),retry:false})
 useExpireOnUnauthorized(query.error)
 const labels=i18n.language.startsWith('en')?issueLabels.en:issueLabels.zh
 const role=session.projects.find(p=>p.id===projectId)?.role,canRecord=session.user.is_admin||['Admin','NDE','NE'].includes(role??'')
 async function recordCurrent(){const result=await action.run(()=>recordFabricDiagnostics(projectId,deviceId,session.csrf_token));if(result.ok)setRecord(result.value)}
 const visibleRecord=record?.report.project_id===projectId&&record.report.device_id===deviceId?record:null
 const rows=query.data?.ports.flatMap(p=>p.checks.filter(c=>type?c.issue_type===type:c.issue_type!=='T6').map(c=>({port:p.port,inUse:p.in_use,...c})))??[]
 return <FabricControlPanel><h3>{t(type?'fabricDiagnosticConfigTitle':'fabricDiagnosticPhysicalTitle')}</h3><p>{t('fabricDiagnosticIntro')}</p><p>{labels.recordDiagnosticHint}</p>{canRecord&&<Button disabled={action.busy||query.isPending} onClick={()=>void recordCurrent()}>{labels.recordDiagnostic}</Button>}<ActionError error={action.error}/>{visibleRecord&&<section><p>{labels.diagnosticEvidence} · {visibleRecord.stale?labels.stale:labels.current}</p>{visibleRecord.issue_ids.map(id=><Link key={id} to="/issues" search={{project_id:projectId,issue_id:id}}>{labels.openIssue} {id.slice(0,8)}</Link>)}</section>}{query.data&&<p>source=switch / {query.data.evidence_source||t('fabricUnknown')} · {query.data.stage} · NP-3 {query.data.np3} · {t('networkGate_'+query.data.gate.state)}</p>}{query.error&&<p role="alert">{t('fabricUnavailable')}</p>}{query.isPending&&<p role="status">{t('managementLoading')}</p>}<div className="fabric-diagnostic-results"><Table><TableHeader><TableRow>{['fabricPort','fabricIssue','fabricObserved','fabricReason'].map(k=><TableHead key={k}>{t(k)}</TableHead>)}</TableRow></TableHeader><TableBody>{rows.slice(0,500).map((r,i)=><TableRow key={i} data-state={r.state}><TableCell>{r.port}</TableCell><TableCell>{r.issue_type}{r.subtype&&`-${r.subtype}`}</TableCell><TableCell>{t('fabricDiagnosticState_'+r.state)}</TableCell><TableCell>{t('fabricDiagnosticReason_'+r.reason,{defaultValue:r.reason})}<small>{r.rule_ref} {r.threshold_ref}</small><small>{Object.entries(r.observed).map(([k,v])=>`${k}=${v}`).join(' · ')}</small></TableCell></TableRow>)}</TableBody></Table></div>{rows.length>500&&<p>{t('fabricDiagnosticBounded')}</p>}{query.data&&<details><summary>{t('networkHealthReferences')}</summary><pre>{JSON.stringify({policy:query.data.policy,policy_hash:query.data.policy_hash,evidence_refs:query.data.evidence_refs},null,2)}</pre></details>}</FabricControlPanel>
}
