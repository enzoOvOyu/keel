import type {components} from '../generated/api'
import {request} from './api'
export type NetworkPushJob=components['schemas']['NetworkPushJob']
export type NetworkPushInput=components['schemas']['NetworkPushInput']
const path=(id:string)=>`/projects/${encodeURIComponent(id)}/fabric/push-jobs`
export const listNetworkPush=(id:string,signal:AbortSignal)=>request<{jobs:NetworkPushJob[];writes_enabled:boolean}>(path(id),{signal})
export const createNetworkPush=(id:string,input:NetworkPushInput,csrf:string)=>request<NetworkPushJob>(path(id),{method:'POST',headers:{'X-CSRF-Token':csrf},body:JSON.stringify(input)})
export const executeNetworkPush=(id:string,job:string,phase:'prepare'|'push',csrf:string,key:string)=>request<{id:string;mode:'fabric'}>(`${path(id)}/${job}/${phase}`,{method:'POST',headers:{'X-CSRF-Token':csrf,'Idempotency-Key':key}})
export const decideNetworkPush=(id:string,job:string,reviewHash:string,comment:string,decision:'approve'|'reject',csrf:string)=>request<NetworkPushJob>(`${path(id)}/${job}/decision`,{method:'POST',headers:{'X-CSRF-Token':csrf},body:JSON.stringify({review_hash:reviewHash,comment,decision})})

export const archiveNetworkPush=(id:string,job:string,csrf:string)=>request<NetworkPushJob>(`${path(id)}/${job}/archive`,{method:'POST',headers:{'X-CSRF-Token':csrf}})
