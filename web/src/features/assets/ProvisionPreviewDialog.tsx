import { useEffect, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Label } from '@/components/ui/label'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { ActionDialog, ActionError, Pagination } from '../../components/Management'
import { useAction } from '../../lib/management'
import { request } from '../../lib/api'
import { listMedia, type MediaItemSummary } from '../../lib/media'
import { listSSHDomainKeys, type ProjectAsset } from '../../lib/bom'
import { useExpireOnUnauthorized, useSession } from '../../lib/session'
import type { components } from '../../generated/api'

export function ProvisionPreviewDialog({ projectId, asset, onClose }: { projectId: string; asset: ProjectAsset; onClose: () => void }) {
 const { t } = useTranslation(), session = useSession(), action = useAction()
 const [mediaPage,setMediaPage] = useState(1), [keyPage,setKeyPage] = useState(1)
 const [media,setMedia] = useState<MediaItemSummary>(), [keyId,setKeyId] = useState(''), [portId,setPortId] = useState('')
 const [preview,setPreview] = useState<components['schemas']['ProvisionPreview']>()
 const base = `/projects/${encodeURIComponent(projectId)}/assets/${encodeURIComponent(asset.id)}/provision`
 const target = useQuery({ queryKey: ['private',session.user.id,'project',projectId,'provision-target',asset.id,asset.version], queryFn: ({ signal }) => request<components['schemas']['ProvisionTarget']>(base + '/target',{ signal }), retry: false })
 const images = useQuery({ queryKey: ['private',session.user.id,'project',projectId,'provision-images',target.data?.arch,mediaPage], enabled: !!target.data, queryFn: ({ signal }) => listMedia(projectId,mediaPage,signal,{ type: 'os',state: 'approved',compatible_arch: target.data!.arch,stored_only: true,boot_only: true }) })
 const keys = useQuery({ queryKey: ['private',session.user.id,'project',projectId,'ssh-domain-keys',keyPage], queryFn: ({ signal }) => listSSHDomainKeys(projectId,keyPage,signal) })
 useExpireOnUnauthorized(target.error);useExpireOnUnauthorized(images.error);useExpireOnUnauthorized(keys.error)
 const lookupError = target.error ?? images.error ?? keys.error
 useEffect(() => { if (lookupError) { setPreview(undefined);setMedia(undefined);setKeyId('');setPortId('') } },[lookupError])
 const activeKeys = keys.data?.items.filter(key => !key.revoked_at && Date.parse(key.expires_at) >= Date.now() + 3_600_000) ?? []
 const change = () => setPreview(undefined)
 return <ActionDialog title={t('provisionPreviewTitle')} description={`${asset.hostname} · ${asset.serial_number}`} busy={action.busy} submitDisabled={!target.data || !media || !keyId || !portId || !!lookupError} confirm={t('provisionPreviewGenerate')} onClose={onClose} onSubmit={event => {
  event.preventDefault();setPreview(undefined)
  if (!target.data || !media || !keyId || !portId || lookupError) return
  void action.run(() => request<components['schemas']['ProvisionPreview']>(base + '/preview',{ method: 'POST',headers: { 'X-CSRF-Token':session.csrf_token },body: JSON.stringify({ asset_version:target.data.asset_version,boot_port_id:portId,media_id:media.id,domain_key_id:keyId } satisfies components['schemas']['ProvisionPreviewInput']) })).then(result => { if (result.ok) setPreview(result.value) })
 }}>
  <p className="impact-notice">{t('provisionPreviewNotice')}</p>
  <ActionError error={lookupError ?? action.error} />
  {target.isPending && <p role="status">{t('loadingRecords')}</p>}
  {target.data && !lookupError && <>
   <p>{target.data.hostname} · {target.data.arch} · v{target.data.asset_version}</p>
   <div className="field"><Label htmlFor="provision-boot-port">{t('provisionBootPort')}</Label><NativeSelect id="provision-boot-port" required value={portId} disabled={action.busy} onChange={event => {change();setPortId(event.target.value)}}><NativeSelectOption value="">{t('provisionSelectPort')}</NativeSelectOption>{target.data.ports.map(port => <NativeSelectOption key={port.id} value={port.id}>{port.name} · {port.mac}</NativeSelectOption>)}</NativeSelect></div>
   <div className="field"><Label htmlFor="provision-image">{t('provisionOSImage')}</Label><NativeSelect id="provision-image" required value={media?.id ?? ''} disabled={action.busy || images.isFetching} onChange={event => {change();setMedia(images.data?.items.find(item => item.id === event.target.value))}}><NativeSelectOption value="">{t('provisionSelectImage')}</NativeSelectOption>{media && !images.data?.items.some(item => item.id === media.id) && <NativeSelectOption value={media.id}>{media.name} · {media.version} · {media.arch}</NativeSelectOption>}{images.data?.items.map(item => <NativeSelectOption key={item.id} value={item.id}>{item.name} · {item.version} · {item.arch}</NativeSelectOption>)}</NativeSelect></div>
  </>}
  {images.data && <Pagination offset={(mediaPage-1)*images.data.page_size} limit={images.data.page_size} total={images.data.total} busy={action.busy || images.isFetching} onChange={offset => setMediaPage(offset/images.data!.page_size+1)} />}
  <div className="field"><Label htmlFor="provision-domain-key">{t('provisionDomainKey')}</Label><NativeSelect id="provision-domain-key" required value={keyId} disabled={action.busy || keys.isFetching || !!lookupError} onChange={event => {change();setKeyId(event.target.value)}}><NativeSelectOption value="">{t('provisionSelectKey')}</NativeSelectOption>{!lookupError && activeKeys.map(key => <NativeSelectOption key={key.id} value={key.id}>{key.credential_ref} · {key.credential_version}</NativeSelectOption>)}</NativeSelect><p className="field-hint">{t('provisionKeyWindow')}</p></div>
  {keys.data && <Pagination offset={(keyPage-1)*keys.data.page_size} limit={keys.data.page_size} total={keys.data.total} busy={action.busy || keys.isFetching} onChange={offset => {setKeyId('');change();setKeyPage(offset/keys.data!.page_size+1)}} />}
  {preview && !lookupError && <div aria-live="polite"><p className="mono-value ui:break-all">{t('provisionSnapshot')}: {preview.snapshot_sha256}</p><details open><summary>user-data</summary><pre className="ui:overflow-auto ui:whitespace-pre-wrap ui:break-words">{preview.seed.user_data}</pre></details><details><summary>meta-data</summary><pre className="ui:overflow-auto ui:whitespace-pre-wrap ui:break-words">{preview.seed.meta_data}</pre></details></div>}
 </ActionDialog>
}
