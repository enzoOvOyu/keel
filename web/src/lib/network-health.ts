import type {components} from '../generated/api'
import {request} from './api'
export type NetworkReadiness=components['schemas']['NetworkReadiness']
export type NetworkDeviceHealth=components['schemas']['NetworkDeviceHealth']
const path=(id:string)=>`/projects/${encodeURIComponent(id)}/fabric`
export const networkReadiness=(id:string,signal:AbortSignal)=>request<NetworkReadiness>(`${path(id)}/readiness`,{signal})
export const networkDeviceHealth=(id:string,device:string,signal:AbortSignal)=>request<NetworkDeviceHealth>(`${path(id)}/devices/${encodeURIComponent(device)}/health`,{signal})

export type NetworkLogReport=components['schemas']['NetworkLogReport']
type LogQuery={hours:number;page:number;level:string;keyword:string}
function logPath(id:string,device:string,q:LogQuery){return `${path(id)}/devices/${encodeURIComponent(device)}/logs?${new URLSearchParams({hours:String(q.hours),page:String(q.page),level:q.level,keyword:q.keyword})}`}
export const networkLogs=(id:string,device:string,q:LogQuery,signal:AbortSignal)=>request<NetworkLogReport>(logPath(id,device,q),{signal})
export async function exportNetworkLogs(id:string,device:string,q:LogQuery,format:'json'|'csv'){
 const response=await fetch('/api/v1'+logPath(id,device,q)+'&format='+format,{credentials:'same-origin'})
 if(!response.ok)throw new Error('Log evidence export unavailable')
 const url=URL.createObjectURL(await response.blob()),a=document.createElement('a');a.href=url;a.download=`network-logs.${format}`;a.click();setTimeout(()=>URL.revokeObjectURL(url),1000)
}
