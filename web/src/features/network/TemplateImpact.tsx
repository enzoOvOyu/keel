import {useState} from 'react'
import {useQuery} from '@tanstack/react-query'
import {Link} from '@tanstack/react-router'
import {useTranslation} from 'react-i18next'
import {Button} from '@/components/ui/button'
import {Label} from '@/components/ui/label'
import {NativeSelect,NativeSelectOption} from '@/components/ui/native-select'
import {Table,TableHeader,TableHead,TableBody,TableRow,TableCell} from '@/components/ui/table'
import {useSession,useExpireOnUnauthorized} from '../../lib/session'
import {ActionError} from '../../components/Management'
import {networkTemplateImpact,type NetworkTemplate} from '../../lib/network-templates'

export function TemplateImpact({projectId,candidate,templates,onClose}:{projectId:string;candidate:NetworkTemplate;templates:NetworkTemplate[];onClose:()=>void}){
 const {t}=useTranslation(),session=useSession(),[base,setBase]=useState(candidate.parent_version_id??''),[page,setPage]=useState(1)
 const choices=templates.filter(v=>v.name===candidate.name&&v.version<candidate.version),baseId=base||choices[0]?.id||''
 const query=useQuery({queryKey:['private',session.user.id,projectId,'network-template-impact',candidate.id,baseId,page],queryFn:({signal})=>networkTemplateImpact(projectId,candidate.id,baseId,page,signal),enabled:!!baseId,retry:false,refetchOnWindowFocus:false})
 useExpireOnUnauthorized(query.error)
 return <section className="panel" aria-label={t('networkTemplateImpact')}><header className="management-heading"><div><h2>{t('networkTemplateImpact')} · {candidate.name} v{candidate.version}</h2><p>{t('networkTemplateImpactBoundary')}</p></div><Button variant="outline" onClick={onClose}>{t('close')}</Button></header>
 <Label htmlFor="network-template-impact-base">{t('networkTemplateImpactBase')}</Label><NativeSelect id="network-template-impact-base" value={baseId} onChange={event=>{setBase(event.target.value);setPage(1)}}>{choices.map(v=><NativeSelectOption key={v.id} value={v.id}>v{v.version} · {t('networkTemplateState_'+v.state)}</NativeSelectOption>)}</NativeSelect>
 <Button variant="outline" disabled={!baseId||query.isFetching} onClick={()=>void query.refetch()}>{t('refresh')}</Button><ActionError error={query.error}/>
 {query.data&&<><p>{t('networkTemplateImpactCounts',{jobs:query.data.referencing_jobs,batches:query.data.referencing_batches,devices:query.data.referencing_devices,current:query.data.current_devices})}</p><p>{t('networkTemplateImpactTime')} {new Date(query.data.evaluated_at).toLocaleString()}</p><Table><TableHeader><TableRow>{['networkTemplateImpactDevice','networkTemplateBatch','networkTemplateImpactVerdict','networkTemplateImpactPaths','execution'].map(key=><TableHead key={key}>{t(key)}</TableHead>)}</TableRow></TableHeader><TableBody>{query.data.items.map(item=><TableRow key={item.switch_id}><TableCell>{item.serial}<small>{item.evidence_source==='protocol_fixture'&&t('networkTemplateSourceFixture')}</small></TableCell><TableCell><code>{item.batch_ref}</code></TableCell><TableCell>{t('networkTemplateImpact_'+item.verdict)}{item.reason&&<details><summary>{t('networkTemplateImpactBlocked')}</summary><code>{item.reason}</code></details>}</TableCell><TableCell>{item.changes.map(change=><p key={change.path}><code>{change.path}</code> · {change.severity}</p>)}</TableCell><TableCell><Link to="/p/$projectId/executions" params={{projectId}} search={{execution:item.execution_id}}>{t('networkTemplateImpactExecution')}</Link></TableCell></TableRow>)}</TableBody></Table></>}
 <nav className="pagination" aria-label={t('networkTemplateImpact')}><Button variant="outline" disabled={page===1||query.isFetching} onClick={()=>setPage(page-1)}>{t('previousPage')}</Button><span>{page}</span><Button variant="outline" disabled={!query.data||page*20>=query.data.current_devices||query.isFetching} onClick={()=>setPage(page+1)}>{t('nextPage')}</Button></nav></section>
}
