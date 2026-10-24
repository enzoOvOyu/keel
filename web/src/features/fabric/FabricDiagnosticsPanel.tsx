import { Table, TableHeader, TableBody, TableRow, TableHead, TableCell } from "@/components/ui/table";
import {useQuery} from '@tanstack/react-query'
import {useTranslation} from 'react-i18next'
import {useSession,useExpireOnUnauthorized} from '../../lib/session'
import {fabricDiagnostics} from '../../lib/fabric-diagnostics'
import {FabricControlPanel} from './FabricControls'
export function FabricDiagnosticsPanel({projectId,deviceId,type}:{projectId:string;deviceId:string;type?:'T6'}){
 const session=useSession(),{t}=useTranslation(),query=useQuery({queryKey:['private',session.user.id,projectId,'fabric-diagnostics',deviceId],queryFn:({signal})=>fabricDiagnostics(projectId,deviceId,signal),retry:false})
 useExpireOnUnauthorized(query.error)
 const rows=query.data?.ports.flatMap(p=>p.checks.filter(c=>type?c.issue_type===type:c.issue_type!=='T6').map(c=>({port:p.port,inUse:p.in_use,...c})))??[]
 return <FabricControlPanel><h3>{t(type?'fabricDiagnosticConfigTitle':'fabricDiagnosticPhysicalTitle')}</h3><p>{t('fabricDiagnosticIntro')}</p>{query.data&&<p>source=switch / {query.data.evidence_source||t('fabricUnknown')} · {query.data.stage} · NP-3 {query.data.np3} · {t('networkGate_'+query.data.gate.state)}</p>}{query.error&&<p role="alert">{t('fabricUnavailable')}</p>}{query.isPending&&<p role="status">{t('managementLoading')}</p>}<div className="fabric-diagnostic-results"><Table><TableHeader><TableRow>{['fabricPort','fabricIssue','fabricObserved','fabricReason'].map(k=><TableHead key={k}>{t(k)}</TableHead>)}</TableRow></TableHeader><TableBody>{rows.slice(0,500).map((r,i)=><TableRow key={i} data-state={r.state}><TableCell>{r.port}</TableCell><TableCell>{r.issue_type}{r.subtype&&`-${r.subtype}`}</TableCell><TableCell>{t('fabricDiagnosticState_'+r.state)}</TableCell><TableCell>{t('fabricDiagnosticReason_'+r.reason,{defaultValue:r.reason})}<small>{r.rule_ref} {r.threshold_ref}</small><small>{Object.entries(r.observed).map(([k,v])=>`${k}=${v}`).join(' · ')}</small></TableCell></TableRow>)}</TableBody></Table></div>{rows.length>500&&<p>{t('fabricDiagnosticBounded')}</p>}{query.data&&<details><summary>{t('networkHealthReferences')}</summary><pre>{JSON.stringify({policy:query.data.policy,policy_hash:query.data.policy_hash,evidence_refs:query.data.evidence_refs},null,2)}</pre></details>}</FabricControlPanel>
}
