import { useState } from 'react'
import { Link } from '@tanstack/react-router'
import { useQuery,useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { ProjectWorkspace,useProject,canManageProject } from '../../components/ProjectWorkspace'
import { useSession,useExpireOnUnauthorized } from '../../lib/session'
import { useAction } from '../../lib/management'
import { latestFabric,collectFabric } from '../../lib/fabric'
import { NetworkDeviceDrawer,type FabricTab } from './NetworkDeviceDrawer'
import type {DDMSearch} from './DDMTrendsPanel'
import './fabric.css'
export type FabricSearch=DDMSearch&{pod:string;role:string;state:string;device:string;tab:FabricTab;abnormal:boolean;descending:boolean}
function Board({search,onSearch}:{search:FabricSearch;onSearch:(value:Partial<FabricSearch>)=>void}){
 const project=useProject(),session=useSession(),{t}=useTranslation(),client=useQueryClient(),action=useAction(),[scroll,setScroll]=useState(0)
 const key=['private',session.user.id,project.id,'fabric-collection']
 const query=useQuery({queryKey:key,queryFn:({signal})=>latestFabric(project.id,signal),refetchInterval:60000})
 useExpireOnUnauthorized(query.error)
 const report=query.data?.report,devices=(report&&!report.stale?report.devices:query.data?.inventory)??[],canCollect=canManageProject(project,session.user.is_admin)||(project.role==='NDE'||project.role==='NE')
 const filtered=devices.filter(d=>(!search.pod||d.pod===search.pod)&&(!search.role||d.role===search.role)&&(!search.state||d.state===search.state)).sort((a,b)=>(search.descending?-1:1)*a.serial.localeCompare(b.serial))
 const selected=devices.find(d=>d.id===search.device),position=filtered.findIndex(d=>d.id===search.device)
 const start=Math.max(0,Math.min(Math.floor(scroll/44)-8,Math.max(0,filtered.length-60))),visible=filtered.slice(start,start+60)
 const findingState=(value:string)=>t(value==='precheck_pass'?'fabricPrecheckPass':value==='anomaly'?'fabricAnomaly':'fabricPending')
 async function collect(){const result=await action.run(()=>collectFabric(project.id,session.csrf_token));if(result.ok){await client.invalidateQueries({queryKey:key});await client.invalidateQueries({queryKey:['private',session.user.id,project.id,'fabric-device']})}}
 function filter(value:Partial<FabricSearch>){setScroll(0);onSearch(value)}
 return <div className="fabric-page"><header className="management-heading"><div><h1>{t('fabricTitle')}</h1><p>{t('fabricIntro')}</p></div><div className="fabric-toolbar"><Link className="secondary-button" to="/p/$projectId/ztp" params={{projectId:project.id}}>{t('ztpTab')}</Link><button className="primary-button" disabled={!canCollect||!query.data?.configured||action.busy} onClick={()=>void collect()}>{t('fabricCollect')}</button></div></header>
 {query.data&&!query.data.configured&&<p role="status">{t('fabricNotConfigured')}</p>}
 {(query.error||action.error)&&<p role="alert">{t('fabricCollectionFailed')}{action.error&&` · ${action.error.code}: ${action.error.message}`}</p>}
 {report?.stale&&<p className="fabric-stale" role="status">{t('fabricStale')}</p>}
 {report&&<p className="fabric-evidence-source">{t('fabricSource')}: {t(report.evidence_source==='protocol_fixture'?'fabricFixture':'fabricDeviceSource')} · {t('fabricSnapshot')}: <code>{report.id}</code> · {t('fabricNP3')}</p>}
 <section className="panel"><div className="fabric-filters">{(['pod','role','state'] as const).map(field=><label key={field}>{t(field==='pod'?'fabricPod':field==='role'?'fabricRole':'fabricObserved')}<select value={search[field]} onChange={e=>filter({[field]:e.target.value})}><option value="">{t('fabricFilterAll')}</option>{[...new Set(devices.map(d=>d[field]))].filter(Boolean).sort().map(value=><option key={value} value={value}>{field==='state'?t(value==='observed'?'fabricObservedState':'fabricUnknown'):value}</option>)}</select></label>)}<button className="secondary-button" onClick={()=>filter({descending:!search.descending})}>{t('fabricSortSerial')} {search.descending?'↓':'↑'}</button></div>
 <div className="fabric-device-table" onScroll={e=>setScroll(e.currentTarget.scrollTop)}><table><thead><tr>{['fabricHostname','fabricSerial','fabricRole','fabricPod','fabricRack','fabricAddress','fabricNOS','fabricObserved','fabricPorts','fabricLastSample'].map(name=><th key={name}>{t(name)}</th>)}</tr></thead><tbody>{start>0&&<tr aria-hidden="true"><td colSpan={10} style={{height:start*44,padding:0,border:0}} /></tr>}{visible.map(d=><tr key={d.id}><td><button className="fabric-device-link" onClick={()=>onSearch({device:d.id,tab:'basic'})}>{d.hostname||d.serial}</button></td><td>{d.serial}</td><td>{d.role||'—'}</td><td>{d.pod||'—'}</td><td>{d.rack||'—'}</td><td>{d.management_ip||'—'}</td><td>{d.nos||t('fabricUnknown')}</td><td>{t(d.state==='observed'?'fabricObservedState':'fabricUnknown')}</td><td>{d.port_count}</td><td>{d.collected_at?new Date(d.collected_at).toLocaleString():'—'}</td></tr>)}{filtered.length>start+visible.length&&<tr aria-hidden="true"><td colSpan={10} style={{height:(filtered.length-start-visible.length)*44,padding:0,border:0}} /></tr>}</tbody></table></div>
 {query.isPending&&<p role="status">{t('managementLoading')}</p>}{!query.isPending&&!report&&<p>{t('fabricNoEvidence')}</p>}{report&&!filtered.length&&<p>{t('fabricNoMatch')}</p>}</section>
 <section className="panel fabric-precheck"><header><h2>{t('fabricPrecheck')}</h2><label><input type="checkbox" checked={search.abnormal} onChange={e=>onSearch({abnormal:e.target.checked})}/>{t('fabricOnlyAbnormal')}</label></header><div className="management-plan-table"><table><thead><tr>{['fabricCable','fabricObserved','fabricIssue','fabricReason','fabricRule'].map(v=><th key={v}>{t(v)}</th>)}</tr></thead><tbody>{report?.findings.filter(f=>!search.abnormal||f.verdict!=='precheck_pass').map(f=><tr key={f.cable_uid}><td><code>{f.cable_uid}</code></td><td>{findingState(f.verdict)}</td><td>{f.issue_type||'—'}</td><td>{f.reason}</td><td>{f.rule_ref} · source={f.source}</td></tr>)}</tbody></table></div>{report?.findings.length===0&&<p>{t('fabricNoCable')}</p>}</section>
 {selected&&<NetworkDeviceDrawer trendSearch={search} onTrendSearch={onSearch} projectId={project.id} device={selected} tab={search.tab} onTab={tab=>onSearch({tab})} onClose={()=>onSearch({device:''})} onPrevious={position>0?()=>onSearch({device:filtered[position-1].id}):undefined} onNext={position>=0&&position<filtered.length-1?()=>onSearch({device:filtered[position+1].id}):undefined}/>}</div>
}
export function FabricPage({projectId,search,onSearch}:{projectId:string;search:FabricSearch;onSearch:(value:Partial<FabricSearch>)=>void}){return <ProjectWorkspace projectId={projectId}><Board search={search} onSearch={onSearch}/></ProjectWorkspace>}
