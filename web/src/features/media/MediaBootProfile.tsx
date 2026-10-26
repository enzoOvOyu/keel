import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Label } from '@/components/ui/label'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { ActionDialog, ActionError } from '../../components/Management'
import { useExpireOnUnauthorized, useSession } from '../../lib/session'
import { resolveMediaBootPlan, type MediaItemSummary } from '../../lib/media'
import type { components } from '../../generated/api'
import type { MediaDependencyChoice } from './MediaDependencyPicker'

export type MediaOSBootProfile = components['schemas']['MediaOSBootProfile']
const roles = ['kernel_media_id', 'initrd_media_id', 'template_media_id'] as const
const recipeRoles = ['uefi_media_id', 'legacy_media_id', 'preflight_media_id', 'ssh_helper_media_id'] as const

export function MediaBootProfileEditor({ value, onChange, dependencies, arch, disabled }: {
 value?: MediaOSBootProfile; onChange: (value?: MediaOSBootProfile) => void; dependencies: MediaDependencyChoice[]; arch: string; disabled: boolean
}) {
 const { t } = useTranslation()
 return <div className="field">
  <Label htmlFor="media-boot-profile">{t('mediaBootProfile')}</Label>
  <p className="field-hint">{t('mediaBootProfileHint')}</p>
  <NativeSelect id="media-boot-profile" disabled={disabled} value={value?.installer ?? ''} onChange={event => onChange(event.target.value ? { schema: 'keel.os-boot/v1', installer: 'cloud-init', kernel_media_id: '', initrd_media_id: '', template_media_id: '' } : undefined)}>
   <NativeSelectOption value="">{t('mediaBootNotConfigured')}</NativeSelectOption>
   <NativeSelectOption value="cloud-init" disabled={arch === 'noarch'}>cloud-init</NativeSelectOption>
  </NativeSelect>
  {value && roles.map(role => {
   const template = role === 'template_media_id'
   const eligible = dependencies.filter(item => item.type === (template ? 'config' : 'os') && (item.arch === arch || (template && item.arch === 'noarch')))
   return <div className="field" key={role}>
    <Label htmlFor={'media-boot-' + role}>{t('mediaBoot_' + role)}</Label>
    <NativeSelect id={'media-boot-' + role} required value={value[role]} disabled={disabled} onChange={event => onChange({ ...value, [role]: event.target.value })}>
     <NativeSelectOption value="">{t('mediaBootSelectRole')}</NativeSelectOption>
     {value[role] && !eligible.some(item => item.id === value[role]) && <NativeSelectOption value={value[role]} disabled>{t('mediaBootRoleUnavailable')}</NativeSelectOption>}
     {eligible.map(item => <NativeSelectOption key={item.id} value={item.id} disabled={roles.some(other => other !== role && value[other] === item.id) || recipeRoles.some(other => value.recipe?.[other] === item.id)}>{item.name} · {item.version} · {item.arch}</NativeSelectOption>)}
    </NativeSelect>
   </div>
  })}
  {value && <MediaBootRecipeEditor value={value} onChange={onChange} dependencies={dependencies} arch={arch} disabled={disabled} />}
 </div>
}

export function MediaBootProfileSummary({ profile }: { profile?: MediaOSBootProfile }) {
 const { t } = useTranslation()
 if (!profile) return null
 return <div className="field"><Label>{t('mediaBootProfile')}</Label><p>{profile.installer}{profile.recipe ? ' · ' + profile.recipe.protocol : ''}</p><dl>{roles.map(role => <div key={role}><dt>{t('mediaBoot_' + role)}</dt><dd className="mono-value ui:break-all">{profile[role]}</dd></div>)}{profile.recipe && recipeRoles.map(role => profile.recipe?.[role] ? <div key={role}><dt>{t("mediaBoot_" + role)}</dt><dd className="mono-value ui:break-all">{profile.recipe[role]}</dd></div> : null)}</dl></div>
}

export function MediaBootPlanDialog({ projectId, item, onClose }: { projectId: string | null; item: MediaItemSummary; onClose: () => void }) {
 const { t } = useTranslation(), session = useSession()
 const query = useQuery({ queryKey: ['private',session.user.id,'project',projectId,'media-boot-plan',item.id], queryFn: ({ signal }) => resolveMediaBootPlan(projectId,item.id,signal), staleTime: 0, retry: false })
 useExpireOnUnauthorized(query.error)
 return <ActionDialog title={t('mediaBootResolve')} description={`${item.name} · ${item.version}`} busy={false} confirm={t('mediaMatrixClose')} onClose={onClose} onSubmit={event => { event.preventDefault(); onClose() }}>
  <p className="impact-notice">{t('mediaBootResolveHint')}</p>
  {query.isPending && <p role="status">{t('loadingRecords')}</p>}
  <ActionError error={query.error} />
  {!query.error && query.data?.artifacts.map(artifact => <div className="field" key={artifact.role}><Label>{t('mediaBootRole_' + artifact.role)}</Label><strong>{artifact.name} · {artifact.version} · {artifact.arch}</strong><span className="cell-subtitle mono-value ui:break-all">{artifact.media_id} · {artifact.sha256}</span><span className="cell-subtitle">{artifact.size} B · {artifact.scope_key}</span></div>)}
 </ActionDialog>
}


function MediaBootRecipeEditor({ value, onChange, dependencies, arch, disabled }: {
 value: MediaOSBootProfile; onChange: (value: MediaOSBootProfile) => void; dependencies: MediaDependencyChoice[]; arch: string; disabled: boolean
}) {
 const { t } = useTranslation(), recipe = value.recipe
 const eligible = dependencies.filter(item => item.type === 'os' && item.arch === arch)
 const selected = [...roles.map(role => value[role]), ...recipeRoles.map(role => recipe?.[role])].filter(Boolean)
 return <div className="field">
  <Label htmlFor="media-boot-recipe">{t('mediaBootRecipe')}</Label>
  <p className="field-hint">{t('mediaBootRecipeHint')}</p>
  <NativeSelect id="media-boot-recipe" value={recipe?.protocol ?? ''} disabled={disabled} onChange={event => onChange({ ...value, recipe: event.target.value ? { schema: 'keel.boot-recipe/v1', protocol: 'keel-initrd/v1', rootfs_format: 'squashfs', uefi_media_id: '', preflight_media_id: '', ssh_helper_media_id: '', preflight_sha256: '', ssh_helper_sha256: '' } : undefined })}>
   <NativeSelectOption value="">{t('mediaBootPreparationOnly')}</NativeSelectOption>
   <NativeSelectOption value="keel-initrd/v1">keel-initrd/v1 · squashfs</NativeSelectOption>
  </NativeSelect>
  {recipe && recipeRoles.filter(role => role !== 'legacy_media_id' || arch === 'amd64').map(role => <div className="field" key={role}>
   <Label htmlFor={'media-boot-' + role}>{t('mediaBoot_' + role)}</Label>
   <NativeSelect id={'media-boot-' + role} required={role !== 'legacy_media_id'} disabled={disabled} value={recipe[role] ?? ''} onChange={event => {
    const item = eligible.find(item => item.id === event.target.value)
    onChange({ ...value, recipe: { ...recipe, [role]: event.target.value || undefined, ...(role === 'preflight_media_id' ? { preflight_sha256: item?.sha256 ?? '' } : {}), ...(role === 'ssh_helper_media_id' ? { ssh_helper_sha256: item?.sha256 ?? '' } : {}) } })
   }}>
    <NativeSelectOption value="">{t(role === 'legacy_media_id' ? 'mediaBootLegacyDisabled' : 'mediaBootSelectRole')}</NativeSelectOption>
    {recipe[role] && !eligible.some(item => item.id === recipe[role]) && <NativeSelectOption value={recipe[role]} disabled>{t('mediaBootRoleUnavailable')}</NativeSelectOption>}
    {eligible.map(item => <NativeSelectOption key={item.id} value={item.id} disabled={selected.includes(item.id) && recipe[role] !== item.id}>{item.name} · {item.version} · {item.arch}</NativeSelectOption>)}
   </NativeSelect>
  </div>)}
 </div>
}
