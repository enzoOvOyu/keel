import { useEffect, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { ActionError, Pagination } from '../../components/Management'
import { listMedia, type MediaItemSummary } from '../../lib/media'
import { useExpireOnUnauthorized, useSession } from '../../lib/session'
import type { components } from '../../generated/api'

export type MediaDependencyChoice = Pick<MediaItemSummary, 'id' | 'name' | 'type' | 'version' | 'arch' | 'sha256'>

export function MediaDependencyPicker({ projectId, arch, selected, disabled, onChange }: {
  projectId: string | null; arch: string; selected: MediaDependencyChoice[]; disabled: boolean; onChange: (items: MediaDependencyChoice[]) => void
}) {
  const { t } = useTranslation(), session = useSession(), [page, setPage] = useState(1)
  const options = useQuery({ queryKey: ['private', session.user.id, 'project', projectId, 'media-dependency-options', arch, page], queryFn: ({ signal }) => listMedia(projectId, page, signal, { state: 'approved', compatible_arch: arch as 'amd64' | 'arm64' | 'noarch', stored_only: true }) })
  useEffect(() => setPage(1), [arch])
  useExpireOnUnauthorized(options.error)
  const eligible = options.data?.items.filter(item => item.state === 'approved' && item.artifact_stored && !item.artifact_quarantined && (item.arch === 'noarch' || item.arch === arch)) ?? []
  return <div className="field">
    <Label htmlFor="media-dependency">{t('mediaArtifactDependencies')}</Label>
    <p className="field-hint">{t('mediaArtifactDependenciesHint')}</p>
    <ActionError error={options.error} />
    <NativeSelect id="media-dependency" value="" disabled={disabled || options.isFetching || selected.length >= 32} onChange={event => {
      const item = eligible.find(item => item.id === event.target.value)
      if (item && !selected.some(ref => ref.id === item.id)) onChange([...selected, item])
    }}>
      <NativeSelectOption value="">{t(options.isPending ? 'loadingRecords' : 'mediaSelectDependency')}</NativeSelectOption>
      {eligible.map(item => <NativeSelectOption key={item.id} value={item.id} disabled={selected.some(ref => ref.id === item.id)}>{item.type} / {item.name} / {item.version} · {item.arch}</NativeSelectOption>)}
    </NativeSelect>
    {!options.isPending && !eligible.length && <p className="field-hint">{t('mediaNoDependenciesOnPage')}</p>}
    {options.data && <Pagination offset={(page - 1) * options.data.page_size} limit={options.data.page_size} total={options.data.total} busy={disabled || options.isFetching} onChange={offset => setPage(offset / options.data!.page_size + 1)} />}
    <div role="list" aria-label={t('mediaSelectedDependencies')}>
      {selected.map(item => <div role="listitem" className="ui:break-words" key={item.id}>
        <strong>{item.name} · {item.version} · {item.arch}</strong>
        <span className="cell-subtitle mono-value">{item.sha256}</span>
        {item.arch !== 'noarch' && item.arch !== arch && <p role="alert" className="field-error">{t('mediaDependencyArchMismatch')}</p>}
        <Button type="button" variant="ghost" size="sm" disabled={disabled} onClick={() => onChange(selected.filter(ref => ref.id !== item.id))}>{t('mediaRemoveDependency', { name: item.name })}</Button>
      </div>)}
    </div>
  </div>
}

export function MediaDependencySummary({ refs }: { refs?: components['schemas']['MediaArtifactDependency'][] }) {
  const { t } = useTranslation()
  if (!refs?.length) return null
  return <div className="field">
    <Label>{t('mediaArtifactDependencies')}</Label>
    <p className="field-hint">{t('mediaDependencyReview')}</p>
    <div role="list" aria-label={t('mediaSelectedDependencies')}>
      {refs.map(ref => <div role="listitem" className="ui:break-words" key={ref.media_id}>
        <strong>{ref.name ?? ref.media_id} · {ref.version ?? t('mediaDependencyUnknown')} · {ref.arch ?? t('mediaDependencyUnknown')}</strong>
        <span className="cell-subtitle mono-value">{ref.media_id} · {ref.sha256}</span>
      </div>)}
    </div>
  </div>
}
