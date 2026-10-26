import { OSDependencies } from './OSDependencies'
import { useCallback, useEffect, useRef, useState } from 'react'
import { Link } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { NativeCheckbox } from '@/components/ui/checkbox'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { ActionDialog, ActionError, Pagination } from '../../components/Management'
import { listProjectAssets, type ProjectAsset } from '../../lib/bom'
import { listMedia, type MediaItemSummary } from '../../lib/media'
import { useAction } from '../../lib/management'
import { useSession, useExpireOnUnauthorized } from '../../lib/session'
import { createProvisionRackRequests, getProvisionTarget, provisionPublicKeys, type ProvisionRackRequestItem, type ProvisionRackRequests } from '../../lib/provision'
import type { components } from '../../generated/api'

type PublicKey = components['schemas']['SSHDomainKey']

function RackAssetChoices({ project, asset, keys, busy, onChange, onRemove }: { project: string; asset: ProjectAsset; keys: PublicKey[]; busy: boolean; onChange: (id: string, item?: ProvisionRackRequestItem) => void; onRemove: () => void }) {
 const { t } = useTranslation(), session = useSession()
 const [mediaPage, setMediaPage] = useState(1), [media, setMedia] = useState<MediaItemSummary>(), [key, setKey] = useState<PublicKey>(), [port, setPort] = useState(''), [serial, setSerial] = useState('')
 const prefix = ['private', session.user.id, 'project', project]
 const target = useQuery({ queryKey: [...prefix, 'provision-target', asset.id, asset.version], queryFn: ({ signal }) => getProvisionTarget(project, asset.id, signal), retry: false })
 const images = useQuery({ queryKey: [...prefix, 'provision-images', target.data?.arch, mediaPage], enabled: !!target.data, queryFn: ({ signal }) => listMedia(project, mediaPage, signal, { type: 'os', state: 'approved', compatible_arch: target.data!.arch, stored_only: true, boot_only: true }), retry: false })
 useExpireOnUnauthorized(target.error); useExpireOnUnauthorized(images.error)
 const changed = !!target.data && (target.data.asset_version !== asset.version || target.data.serial_number !== asset.serial_number)
 useEffect(() => {
  const value = target.data
  const valid = value && !changed && !target.error && !images.error && !target.isFetching && !images.isFetching && media && key && Date.parse(key.expires_at) >= Date.now() + 3_600_000 && !key.revoked_at && serial === value.serial_number && value.ports.some(item => item.id === port)
  onChange(asset.id, valid ? { asset_id: asset.id, asset_version: value.asset_version, boot_port_id: port, media_id: media.id, domain_key_id: key.id, confirm_serial: serial } : undefined)
 }, [asset.id, changed, images.error, images.isFetching, key, media, onChange, port, serial, target.data, target.error, target.isFetching])
 return <fieldset className="ui:rounded-md ui:border ui:border-border ui:p-3 ui:space-y-3" disabled={busy}>
  <legend>{asset.hostname} · {asset.serial_number}</legend>
  <div className="ui:flex ui:flex-wrap ui:items-center ui:justify-between ui:gap-2"><span>{asset.rack} · {asset.unit_position} · v{asset.version}</span><Button type="button" size="sm" variant="outline" onClick={onRemove}>{t('rackPrepareRemove')}</Button></div>
  <ActionError error={target.error ?? images.error} />
  {target.isPending && <p role="status">{t('loadingRecords')}</p>}
  {changed && <p role="status">{t('rackPrepareChanged')}</p>}
  {target.data && !changed && <>
   <p>{target.data.arch} · {target.data.hostname}</p>
   <div className="field"><Label htmlFor={`rack-port-${asset.id}`}>{t('provisionBootPort')}</Label><NativeSelect id={`rack-port-${asset.id}`} value={port} required onChange={event => setPort(event.target.value)}><NativeSelectOption value="">{t('provisionSelectPort')}</NativeSelectOption>{target.data.ports.map(item => <NativeSelectOption key={item.id} value={item.id}>{item.name} · {item.mac}</NativeSelectOption>)}</NativeSelect></div>
   <div className="field"><Label htmlFor={`rack-media-${asset.id}`}>{t('provisionOSImage')}</Label><NativeSelect id={`rack-media-${asset.id}`} required value={media?.id ?? ''} disabled={images.isFetching} onChange={event => setMedia(images.data?.items.find(item => item.id === event.target.value))}><NativeSelectOption value="">{t('provisionSelectImage')}</NativeSelectOption>{media && !images.data?.items.some(item => item.id === media.id) && <NativeSelectOption value={media.id}>{media.name} · {media.version} · {media.arch}</NativeSelectOption>}{images.data?.items.map(item => <NativeSelectOption key={item.id} value={item.id}>{item.name} · {item.version} · {item.arch}</NativeSelectOption>)}</NativeSelect></div>
   {media && <OSDependencies projectId={project} mediaId={media.id}/>}
   {images.data && <Pagination offset={(mediaPage - 1) * images.data.page_size} limit={images.data.page_size} total={images.data.total} busy={busy || images.isFetching} onChange={offset => setMediaPage(offset / images.data!.page_size + 1)} />}
   <div className="field"><Label htmlFor={`rack-key-${asset.id}`}>{t('provisionDomainKey')}</Label><NativeSelect id={`rack-key-${asset.id}`} required value={key?.id ?? ''} onChange={event => setKey(keys.find(item => item.id === event.target.value))}><NativeSelectOption value="">{t('provisionSelectKey')}</NativeSelectOption>{key && !keys.some(item => item.id === key.id) && <NativeSelectOption value={key.id}>{key.credential_ref} · {key.credential_version}</NativeSelectOption>}{keys.map(item => <NativeSelectOption key={item.id} value={item.id}>{item.credential_ref} · {item.credential_version}</NativeSelectOption>)}</NativeSelect><p className="field-hint">{t('provisionKeyWindow')}</p></div>
   <div className="field"><Label htmlFor={`rack-serial-${asset.id}`}>{t('provisionSerialConfirmation')}</Label><Input id={`rack-serial-${asset.id}`} required maxLength={256} value={serial} onChange={event => setSerial(event.target.value)} autoComplete="off" /></div>
  </>}
 </fieldset>
}

export function RackRequestsDialog({ project, onClose, onDone }: { project: string; onClose: () => void; onDone: () => Promise<void> }) {
 const { t } = useTranslation(), session = useSession(), action = useAction(), key = useRef(crypto.randomUUID())
 const submittedPayload = useRef('')
 const [rack, setRack] = useState(''), [reason, setReason] = useState(''), [page, setPage] = useState(1), [keyPage, setKeyPage] = useState(1)
 const [selected, setSelected] = useState<ProjectAsset[]>([]), [choices, setChoices] = useState<Record<string, ProvisionRackRequestItem | undefined>>({}), [created, setCreated] = useState<ProvisionRackRequests>()
 const assets = useQuery({ queryKey: ['private', session.user.id, 'project', project, 'assets', page], queryFn: ({ signal }) => listProjectAssets(project, page, signal), retry: false })
 const keys = useQuery({ queryKey: ['private', session.user.id, 'project', project, 'provision-public-keys', keyPage], queryFn: ({ signal }) => provisionPublicKeys(project, keyPage, signal), retry: false })
 useExpireOnUnauthorized(assets.error); useExpireOnUnauthorized(keys.error)
 const onChoice = useCallback((id: string, item?: ProvisionRackRequestItem) => {
  setChoices(previous => { if (JSON.stringify(previous[id]) === JSON.stringify(item)) return previous; return { ...previous, [id]: item } })
 }, [])
 const items = selected.map(asset => choices[asset.id]).filter((item): item is ProvisionRackRequestItem => !!item)
 const valid = rack.trim() && reason.trim() && selected.length > 0 && selected.length <= 50 && items.length === selected.length && !assets.error && !keys.error && !assets.isFetching && !keys.isFetching
 const activeKeys = keys.data?.items.filter(item => !item.revoked_at && Date.parse(item.expires_at) >= Date.now() + 3_600_000) ?? []
 const remove = (id: string) => { setSelected(previous => previous.filter(asset => asset.id !== id)); onChoice(id) }
 return <ActionDialog title={t('rackPrepareTitle')} description={t('rackPrepareDescription')} busy={action.busy} confirm={t('rackPrepareSubmit')} submitDisabled={!valid || !!created} onClose={onClose} onSubmit={event => {
  event.preventDefault(); if (!valid || created) return
  const input = { rack, reason: reason.trim(), items: [...items].sort((a, b) => a.asset_id.localeCompare(b.asset_id)) }
  const payload = JSON.stringify(input)
  if (submittedPayload.current !== payload) { submittedPayload.current = payload; key.current = crypto.randomUUID() }
  void action.run(() => createProvisionRackRequests(project, input, session.csrf_token, key.current)).then(async result => { if (result.ok) { setCreated(result.value); await onDone() } })
 }}>
  <p className="impact-notice">{t('rackPrepareImpact')}</p><ActionError error={assets.error ?? keys.error ?? action.error} />
  {created ? <div role="status" className="ui:space-y-3"><p>{t('rackPrepareCreated')} <span className="mono-value ui:break-all">{created.batch_id}</span></p><p>{created.rack} · {created.requests.length}</p><ul>{created.requests.map(item => <li key={item.id}><Link to="/p/$projectId/deploy/provision" params={{ projectId: project }} search={{ request: item.id }} onClick={onClose}>{item.snapshot.target.hostname} · {item.snapshot.target.serial_number} · {t('rackPrepareOpenRequest')}</Link></li>)}</ul></div> : <>
   <div className="field"><Label htmlFor="rack-prepare-rack">{t('rackPrepareRack')}</Label><Input id="rack-prepare-rack" maxLength={120} required value={rack} disabled={action.busy} onChange={event => { setRack(event.target.value); setSelected([]); setChoices({}) }} /><p className="field-hint">{t('rackPrepareRackHint')}</p></div>
   {assets.isPending && <p role="status">{t('loadingRecords')}</p>}
   {assets.data && !assets.error && <><p>{t('rackPrepareSelectionHint')} ({selected.length}/50)</p><ul className="ui:space-y-2">{assets.data.items.filter(asset => asset.rack === rack && !!rack.trim()).map(asset => <li key={asset.id}><Label><NativeCheckbox checked={selected.some(item => item.id === asset.id)} disabled={action.busy || (selected.length >= 50 && !selected.some(item => item.id === asset.id))} onChange={event => { if (event.target.checked) { setSelected(previous => [...previous, asset]) } else remove(asset.id) }} /> {asset.hostname} · {asset.serial_number} · {asset.unit_position}</Label></li>)}</ul>{!assets.data.items.some(asset => asset.rack === rack && !!rack.trim()) && <p>{t('rackPrepareNoAssets')}</p>}<Pagination offset={(page - 1) * assets.data.page_size} limit={assets.data.page_size} total={assets.data.total} busy={action.busy || assets.isFetching} onChange={offset => setPage(offset / assets.data!.page_size + 1)} /></>}
   {selected.map(asset => <RackAssetChoices key={asset.id} project={project} asset={asset} keys={activeKeys} busy={action.busy} onChange={onChoice} onRemove={() => remove(asset.id)} />)}
   {keys.data && <div><p>{t('rackPrepareKeyPages')}</p><Pagination offset={(keyPage - 1) * keys.data.page_size} limit={keys.data.page_size} total={keys.data.total} busy={action.busy || keys.isFetching} onChange={offset => setKeyPage(offset / keys.data!.page_size + 1)} /></div>}
   <div className="field"><Label htmlFor="rack-prepare-reason">{t('provisionReason')}</Label><Input id="rack-prepare-reason" maxLength={500} required disabled={action.busy} value={reason} onChange={event => { setReason(event.target.value) }} /></div>
  </>}
 </ActionDialog>
}
