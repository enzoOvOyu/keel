import type {components} from '../generated/api'
import {request} from './api'
export type RuleCatalog=components['schemas']['NetworkRuleCatalog']
export type RuleInput=components['schemas']['NetworkRuleInput']
export type RuleVersion=components['schemas']['NetworkRuleVersion']
export type RulePreview=components['schemas']['NetworkRulePreview']
const path=(id:string)=>`/projects/${encodeURIComponent(id)}/fabric/rules`
export const networkRules=(id:string,signal:AbortSignal)=>request<RuleCatalog>(path(id),{signal})
function post<T>(url:string,csrf:string,body:unknown){return request<T>(url,{method:'POST',headers:{'X-CSRF-Token':csrf},body:JSON.stringify(body)})}
export const previewRule=(id:string,csrf:string,input:RuleInput)=>post<RulePreview>(path(id)+'/preview',csrf,input)
export const saveRule=(id:string,csrf:string,input:RuleInput)=>post<RuleVersion>(path(id),csrf,input)
export const approveRule=(id:string,csrf:string,version:RuleVersion,role:'NE'|'NH')=>post<RuleVersion>(`${path(id)}/${version.id}/approve`,csrf,{approval_role:role,preview_hash:version.preview_hash})
export const publishRule=(id:string,csrf:string,version:RuleVersion)=>post<RuleVersion>(`${path(id)}/${version.id}/publish`,csrf,{preview_hash:version.preview_hash})
export const rollbackRule=(id:string,version:string,target='default')=>request<RuleInput>(`${path(id)}/${version}/rollback-candidate?target=${encodeURIComponent(target)}`)
