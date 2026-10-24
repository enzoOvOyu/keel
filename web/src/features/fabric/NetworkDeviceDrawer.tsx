import {DDMTrendsPanel,type DDMSearch} from './DDMTrendsPanel'
import {FabricDiagnosticsPanel} from './FabricDiagnosticsPanel'
import { useEffect, useRef } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { useExpireOnUnauthorized,useSession } from '../../lib/session'
import { deviceEvidence,type FabricDevice } from '../../lib/fabric'
export const fabricTabs=['basic','config','script','events','runtime','lossless','optics','logs'] as const
export type FabricTab=typeof fabricTabs[number]
export function NetworkDeviceDrawer({projectId,device,tab,onTab,onClose,onPrevious,onNext,trendSearch={},onTrendSearch=()=>{}}:{trendSearch?:DDMSearch;onTrendSearch?:(v:Partial<DDMSearch>)=>void;projectId:string;device:FabricDevice;tab:FabricTab;onTab:(tab:FabricTab)=>void;onClose:()=>void;onPrevious?:()=>void;onNext?:()=>void}){
 const {t}=useTranslation(),session=useSession(),root=useRef<HTMLElement>(null)
 const result=useQuery({queryKey:['private',session.user.id,projectId,'fabric-device',device.id],queryFn:({signal})=>deviceEvidence(projectId,device.id,signal),retry:false})
 useExpireOnUnauthorized(result.error)
 useEffect(()=>{const prior=document.activeElement as HTMLElement|null;root.current?.querySelector<HTMLButtonElement>('button')?.focus();return()=>prior?.focus()},[])
 const evidence=result.data,sample=evidence?.snapshot
 const state=(v:boolean|null|undefined)=>v==null?t('fabricUnknown'):v?t('fabricUp'):t('fabricDown')
 const unavailable=<p role="status">{t('fabricUnavailable')}</p>
 return <div className="fabric-drawer-backdrop" onMouseDown={e=>{if(e.target===e.currentTarget)onClose()}}><aside ref={root} className="network-device-drawer" role="dialog" aria-modal="true" aria-label={t('fabricDrawer')} onKeyDown={e=>{if(e.key==='Escape'){e.stopPropagation();onClose()}if(e.key==='Tab'){const nodes=root.current?.querySelectorAll<HTMLElement>('button:not(:disabled),a[href],select,input,[tabindex="0"]');if(!nodes?.length)return;const first=nodes[0],last=nodes[nodes.length-1];if(e.shiftKey&&document.activeElement===first){e.preventDefault();last.focus()}else if(!e.shiftKey&&document.activeElement===last){e.preventDefault();first.focus()}}}}>
 <header><div><h2>{device.hostname||device.serial}</h2><p>{device.serial} · {device.pod} · {device.role}</p></div><button className="secondary-button" onClick={onClose}>{t('fabricClose')}</button></header>
 <div className="fabric-drawer-actions"><button className="secondary-button" disabled={!onPrevious} onClick={onPrevious}>{t('fabricPrevious')}</button><button className="secondary-button" disabled={!onNext} onClick={onNext}>{t('fabricNext')}</button></div>
 <div role="tablist" aria-label={t('fabricDrawer')} className="fabric-drawer-tabs">{fabricTabs.map(value=><button key={value} role="tab" aria-selected={tab===value} aria-controls={'fabric-tab-'+value} id={'fabric-tab-button-'+value} onClick={()=>onTab(value)}>{t('fabricDrawer_'+value)}</button>)}</div>
 <section role="tabpanel" id={'fabric-tab-'+tab} aria-labelledby={'fabric-tab-button-'+tab} className="fabric-drawer-content">
 {evidence?.stale&&<p role="status" className="fabric-stale">{t('fabricStale')}</p>}
 {tab==='basic'&&<dl>{[[t('fabricSerial'),device.serial],[t('fabricHostname'),device.hostname],[t('fabricPod'),device.pod],[t('fabricRack'),device.rack],[t('fabricAddress'),device.management_ip],[t('fabricNOS'),device.nos],[t('fabricLastSample'),device.collected_at],[t('fabricSampleHash'),device.content_hash]].map(([key,value])=><div key={key}><dt>{key}</dt><dd>{value||t('fabricUnknown')}</dd></div>)}</dl>}
 {tab==='config'&&<FabricDiagnosticsPanel projectId={projectId} deviceId={device.id} type="T6"/>}{tab==='script'&&<p>{t('fabricScriptPending')}</p>}{tab==='events'&&<p>{t('fabricEventsPending')}</p>}{tab==='logs'&&<p>{t('fabricLogsPending')}</p>}
 {tab==='runtime'&&<FabricDiagnosticsPanel projectId={projectId} deviceId={device.id}/>}{tab==='runtime'&&(sample?<><p>{t('fabricBGP')}: {state(sample.bgp_up)} · {t('fabricPTP')}: {state(sample.ptp_locked)}</p><table><thead><tr><th>{t('fabricPort')}</th><th>{t('fabricLink')}</th><th>{t('fabricNeighbor')}</th></tr></thead><tbody>{sample.ports.map(p=><tr key={p.name}><td>{p.name}</td><td>{state(p.link_up)}</td><td>{p.lldp_readable&&p.neighbor?`${p.neighbor.serial||p.neighbor.sysname||p.neighbor.chassis_id} · ${p.neighbor.port_id}`:t('fabricUnknown')}</td></tr>)}</tbody></table><details><summary>FDB / ARP · source=switch</summary><pre>{JSON.stringify({fdb:sample.fdb,arp:sample.arp},null,2)}</pre></details></>:unavailable)}
 {tab==='lossless'&&(sample?<><p>{t('fabricMetrics')} · source=switch</p><pre>{JSON.stringify({capabilities:sample.capabilities,ports:sample.ports.map(p=>({port:p.name,metrics:p.metrics}))},null,2)}</pre></>:unavailable)}
 {tab==='optics'&&(evidence?<><h3>{t('fabricDDM')}</h3><table><thead><tr><th>{t('fabricPort')}</th><th>Rx / Tx (dBm)</th><th>°C / V / mA</th><th>{t('fabricDDMState')}</th></tr></thead><tbody>{evidence.optics.map(o=><tr key={o.port}><td>{o.port}<small>{o.data?.vendor} {o.data?.model} {o.data?.serial}</small></td><td>{o.data?.rx_dbm??'—'} / {o.data?.tx_dbm??'—'}</td><td>{o.data?.temperature_c??'—'} / {o.data?.voltage_v??'—'} / {o.data?.bias_ma??'—'}</td><td>{!o.in_use?t('fabricDDMUnused'):t(o.state==='normal'?'fabricNormal':o.state==='critical'?'fabricCritical':o.state==='warning'?'fabricWarning':'fabricUnknown')}<small>{o.rule_ref} · {o.reasons.join(', ')}</small></td></tr>)}</tbody></table><DDMTrendsPanel projectId={projectId} deviceId={device.id} ports={evidence.optics.map(o=>o.port)} search={trendSearch} onSearch={onTrendSearch}/></>:unavailable)}
 {result.isPending&&<p role="status">{t('managementLoading')}</p>}
 {result.isError&&<p role="status">{t('fabricUnavailable')}</p>}
 </section></aside></div>
}
