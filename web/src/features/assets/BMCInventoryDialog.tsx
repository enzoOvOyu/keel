import { BMCPolicyPanel } from './BMCPolicyPanel'
import { useEffect, useRef, useState, type FormEvent } from 'react'
import { Link, useNavigate } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Label } from '@/components/ui/label'
import { Input } from '@/components/ui/input'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Table, TableHeader, TableHead, TableBody, TableRow, TableCell } from '@/components/ui/table'
import { ActionError, Pagination } from '../../components/Management'
import { useProject } from '../../components/ProjectWorkspace'
import { createBMCCollection, downloadBMCRaw, getBMCManifest, getBMCResource, listBMCCollections, listBMCResources, type BMCInput, type BMCManifest } from '../../lib/bmc'
import type { ProjectAsset } from '../../lib/bom'
import { useAction } from '../../lib/management'
import { useExpireOnUnauthorized, useSession } from '../../lib/session'

function Freshness({ record }: { record: BMCManifest }) {
 const { t } = useTranslation(), [now, setNow] = useState(Date.now())
 const collected = Date.parse(record.collected_at), maxAge = record.freshness_max_age_seconds * 1000
 const eligible = record.identity_matched && record.asset_current && record.state !== 'failed' && Number.isFinite(collected) && collected <= now && maxAge > 0
 const age = now - collected, freshness = !eligible ? record.freshness : age >= maxAge ? 'stale' : age >= maxAge / 2 ? 'aging' : 'fresh'
 useEffect(() => { if (!eligible || age >= maxAge) return; const remaining = (age < maxAge / 2 ? maxAge / 2 : maxAge) - age; const timer = window.setTimeout(() => setNow(Date.now()), Math.max(1, Math.min(remaining + 1, 86400000))); return () => window.clearTimeout(timer) }, [record.id, now, eligible, age, maxAge])
 return <span>{t(`bmc_freshness_${freshness}`)}</span>
}
export function BMCInventoryDialog({ asset, onClose }: { asset: ProjectAsset; onClose: () => void }) {
 const { t } = useTranslation(), project = useProject(), session = useSession(), action = useAction(), navigate = useNavigate()
 const [page, setPage] = useState(1), [collection, setCollection] = useState(''), [resourcePage, setResourcePage] = useState(1), [resourcePath, setResourcePath] = useState('')
 const [level, setLevel] = useState<BMCInput['level']>('L1'), [reason, setReason] = useState(''), [validation, setValidation] = useState('')
 const [showPolicies, setShowPolicies] = useState(false), [policyBusy, setPolicyBusy] = useState(false)
 const busy = action.busy || policyBusy
 const attempt = useRef({ body: '', key: '' }), mounted = useRef(true)
 useEffect(() => {mounted.current=true;return () => {mounted.current=false}}, [])
 const allowed = session.user.is_admin || ['Admin', 'DE', 'DL', 'SRE'].includes(project.role)
 const canWrite = allowed && !['archived', 'delivered'].includes(project.state)
 const key = ['private', session.user.id, 'project', project.id, 'bmc', asset.id]
 const list = useQuery({ queryKey: [...key, 'history', page], queryFn: ({ signal }) => listBMCCollections(project.id, asset.id, page, signal), retry: false, refetchOnWindowFocus: false })
 const manifest = useQuery({ queryKey: [...key, collection, 'manifest'], queryFn: ({ signal }) => getBMCManifest(project.id, asset.id, collection, signal), enabled: !!collection, retry: false, refetchOnWindowFocus: false })
 const resources = useQuery({ queryKey: [...key, collection, 'resources', resourcePage], queryFn: ({ signal }) => listBMCResources(project.id, asset.id, collection, resourcePage, signal), enabled: !!collection, retry: false, refetchOnWindowFocus: false })
 const resource = useQuery({ queryKey: [...key, collection, 'resource', resourcePath], queryFn: ({ signal }) => getBMCResource(project.id, asset.id, collection, resourcePath, signal), enabled: !!collection && !!resourcePath, retry: false, gcTime: 60000, refetchOnWindowFocus: false })
 useExpireOnUnauthorized(list.error); useExpireOnUnauthorized(manifest.error); useExpireOnUnauthorized(resources.error); useExpireOnUnauthorized(resource.error)
 const denied = [list.error, manifest.error, resources.error, resource.error, action.error].some(e => e && 'status' in e && [401, 403, 404].includes(Number(e.status)))
 async function start(event: FormEvent) {
  event.preventDefault(); if (!canWrite || denied || busy) return
  if (reason.trim().length < 3) { setValidation(t('bmcInvalidReason')); return }; setValidation('')
  const body: BMCInput = { level, reason: reason.trim(), expected_asset_version: asset.version }, serialized = JSON.stringify(body)
  if (attempt.current.body !== serialized) attempt.current = { body: serialized, key: crypto.randomUUID() }
  const result = await action.run(() => createBMCCollection(project.id, asset.id, body, session.csrf_token, attempt.current.key))
  if (result.ok && mounted.current) await navigate({ to: '/p/$projectId/executions', params: { projectId: project.id }, search: { execution: result.value.id, tab: 'overview' } })
 }
 if (denied) return <Dialog open onOpenChange={open=>{if(!open)onClose()}}><DialogContent><DialogHeader><DialogTitle>{t('bmcTitle')}</DialogTitle><DialogDescription>{t('bmcIntro')}</DialogDescription></DialogHeader><ActionError error={action.error ?? list.error ?? manifest.error ?? resources.error ?? resource.error} /><Button type="button" onClick={onClose}>{t('bmcClose')}</Button></DialogContent></Dialog>
 const record = manifest.data
 const disabled = action.error && ['BMC_INVENTORY_DISABLED', 'BMC_TARGET_UNCONFIGURED'].includes(action.error.code)
 return <Dialog open onOpenChange={open => { if (!open && !busy) onClose() }}><DialogContent showCloseButton={false} className="ui:w-[min(1100px,calc(100vw-32px))] ui:max-w-none ui:sm:max-w-none ui:max-h-[calc(100svh-48px)] ui:overflow-y-auto">
  <DialogHeader><DialogTitle>{t('bmcTitle')} · {asset.hostname}</DialogTitle><DialogDescription>{t('bmcIntro')}</DialogDescription></DialogHeader>
  <p className="mono-value">{asset.serial_number} · v{asset.version}</p>
  <ActionError error={action.error} />{disabled && <p role="status">{t('bmcDisabled')}</p>}
  {canWrite && <form className="ui:grid ui:gap-3" onSubmit={event => void start(event)}>
   <p className="field-hint">{t('bmcPrerequisite')}</p>
   <Label htmlFor="bmc-level">{t('bmcLevel')}</Label><NativeSelect id="bmc-level" value={level} onChange={e => setLevel(e.target.value as BMCInput['level'])} disabled={busy || denied}>{(['L1', 'L2', 'L3'] as const).map(value => <NativeSelectOption key={value} value={value}>{t(`bmc${value}`)}</NativeSelectOption>)}</NativeSelect>
   <Label htmlFor="bmc-reason">{t('bmcReason')}</Label><Input id="bmc-reason" required minLength={3} maxLength={1000} value={reason} onChange={e => setReason(e.target.value)} disabled={busy || denied} />{validation && <p role="alert">{validation}</p>}
   <Button type="submit" disabled={busy || denied || reason.trim().length < 3}>{t('bmcStart')}</Button>
  </form>}
  <Button type="button" variant="outline" disabled={busy || denied} aria-expanded={showPolicies} onClick={() => setShowPolicies(!showPolicies)}>{t('bmcPolicies')}</Button>
  {showPolicies && <BMCPolicyPanel asset={asset.id} canWrite={canWrite} onBusyChange={setPolicyBusy} />}
  <div className="panel-heading"><h3>{t('bmcHistory')}</h3><Button type="button" variant="outline" disabled={list.isFetching || denied} onClick={() => void list.refetch()}>{t('bmcRefresh')}</Button></div><ActionError error={list.error} />
  {list.isPending && <p role="status">{t('loadingRecords')}</p>}
  {list.data && <><Table><TableHeader><TableRow><TableHead>{t('bmcCollectedAt')}</TableHead><TableHead>{t('bmcLevel')}</TableHead><TableHead>{t('bmcQuality')}</TableHead><TableHead>{t('bmcSource')}</TableHead><TableHead>{t('bmcCollection')}</TableHead></TableRow></TableHeader><TableBody>{list.data.items.map(row => <TableRow key={row.id} data-bmc-collection={row.id}><TableCell>{new Date(row.collected_at).toLocaleString()}</TableCell><TableCell>{row.level}</TableCell><TableCell>{t(`bmc_state_${row.state}`)}</TableCell><TableCell>BMC · {t(`bmcOrigin_${row.origin}`)}</TableCell><TableCell><Button type="button" variant={row.id === collection ? 'default' : 'outline'} size="sm" disabled={denied} onClick={() => { setCollection(row.id); setResourcePage(1); setResourcePath('') }}>{t('bmcDetails')}</Button></TableCell></TableRow>)}</TableBody></Table>{!list.data.items.length && <p>{t('bmcEmpty')}</p>}<Pagination offset={(page - 1) * list.data.page_size} limit={list.data.page_size} total={list.data.total} busy={list.isFetching} onChange={offset => setPage(Math.floor(offset / (list.data?.page_size ?? 20)) + 1)} /></>}
  <ActionError error={manifest.error} /><ActionError error={resources.error} />
  {record && !denied && <section className="ui:grid ui:gap-3" aria-label={t('bmcCollection')}>
   <dl className="ui:grid ui:grid-cols-[max-content_minmax(0,1fr)] ui:gap-x-4 ui:gap-y-2"><dt>{t('bmcQuality')}</dt><dd>{t(`bmc_state_${record.state}`)}</dd><dt>{t('bmcFreshness')}</dt><dd><Freshness key={record.id} record={record} /></dd><dt>{t('bmcSource')}</dt><dd>BMC · {t(`bmcOrigin_${record.origin}`)}</dd><dt>{t('bmcIdentity')}</dt><dd>{t(record.identity_matched ? 'bmcIdentityConfirmed' : 'bmcIdentityUnknown')}</dd><dt>{t('bmcCredentialVersion')}</dt><dd className="mono-value ui:break-all">{record.credential_batch_ref} / {record.credential_version}</dd><dt>{t('bmcSnapshotHash')}</dt><dd className="mono-value ui:break-all">{record.content_hash}</dd></dl>
   {record.fallback && <p className="impact-notice">{t('bmcFallback')} · {t(`bmcFallback_${record.fallback.reason}`)} · IPMI · {record.fallback.resource_count} · {record.fallback.parser_version}</p>}
   {!record.asset_current && <p className="impact-notice">{t('bmcAssetChanged')}</p>}
   <Link to="/p/$projectId/executions" params={{ projectId: project.id }} search={{ execution: record.execution_id, tab: 'overview' }}>{t('bmcViewExecution')}</Link>
   <details><summary>{t('bmcCapabilities')}</summary><dl className="ui:grid ui:grid-cols-2 ui:gap-2">{Object.entries(record.capabilities).sort(([a], [b]) => a.localeCompare(b)).map(([feature, state]) => <div key={feature}><dt>{feature}</dt><dd>{t(`bmcFeature_${state}`)}</dd></div>)}</dl></details>
   <details open={record.failures.length > 0}><summary>{t('bmcFailures')} · {record.failures.length}</summary>{record.failures.length ? <ul>{record.failures.map((failure, index) => <li key={`${failure.path}:${failure.code}:${index}`}><strong>{t(`bmcClass_${failure.class}`)}</strong> · <span className="mono-value ui:break-all">{failure.path} · {failure.code}</span>{failure.retryable && <span> · {t('bmcRetryable')}</span>}</li>)}</ul> : <p>{t('bmcNoFailure')}</p>}</details>
   <h3>{t('bmcResources')}</h3>{resources.isFetching && <p role="status">{t('loadingRecords')}</p>}
   {resources.data && <><Table><TableHeader><TableRow><TableHead>{t('bmcResourcePath')}</TableHead><TableHead>{t('bmcResourceType')}</TableHead><TableHead>{t('bmcResources')}</TableHead></TableRow></TableHeader><TableBody>{resources.data.items.map(row => <TableRow key={row.path}><TableCell className="mono-value ui:break-all">{row.path}</TableCell><TableCell className="mono-value ui:break-all">{row.type || '—'}</TableCell><TableCell><Button type="button" variant="outline" size="sm" onClick={() => setResourcePath(row.path)}>{t('bmcViewResource')}</Button><Button type="button" variant="ghost" size="sm" disabled={busy} onClick={() => void action.run(() => downloadBMCRaw(project.id, asset.id, collection, row.path))}>{t('bmcRaw')}</Button></TableCell></TableRow>)}</TableBody></Table>{!resources.data.items.length && <p>{t('bmcNoResource')}</p>}<Pagination offset={(resourcePage - 1) * resources.data.page_size} limit={resources.data.page_size} total={resources.data.total} busy={resources.isFetching} onChange={offset => { setResourcePage(Math.floor(offset / (resources.data?.page_size ?? 20)) + 1); setResourcePath('') }} /></>}
   <ActionError error={resource.error} />{resource.isFetching && <p role="status">{t('loadingRecords')}</p>}
   {resource.data && <><h4>{t('bmcStructured')} · <span className="mono-value ui:break-all">{resource.data.path}</span></h4><pre className="ui:max-h-96 ui:overflow-auto ui:whitespace-pre-wrap ui:break-words">{JSON.stringify(resource.data.data, null, 2)}</pre><details><summary>{t('bmcEvidence')}</summary><pre className="ui:overflow-auto ui:whitespace-pre-wrap ui:break-words">{JSON.stringify({ received_at: resource.data.received_at, data_sha256: resource.data.data_sha256, raw: resource.data.raw }, null, 2)}</pre></details></>}
  </section>}
  <p className="field-hint">{t('bmcReadOnly')}</p><Button type="button" variant="outline" disabled={busy} onClick={onClose}>{t('bmcClose')}</Button>
 </DialogContent></Dialog>
}
