import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Label } from '@/components/ui/label'
import { useTranslation } from 'react-i18next'
import type { CatalogKey } from '../lib/catalog'

export function ParamSelect({ projectId, entry, value, arch, disabled, required, onChange }: {
  projectId: string; required: boolean; entry: CatalogKey; value: string; arch?: string; disabled: boolean; onChange: (value: string) => void
}) {
  const { t } = useTranslation()
  const media = entry.media_options.filter(m => !arch || m.arch === arch || m.arch === 'noarch')
  const options = entry.value_type === 'version' ? [...new Set(media.map(m => m.version))].sort() : entry.options
  const id = `execution-${entry.key}`
  return <div className="field">
    <Label htmlFor={id}>{entry.key}</Label>
    <NativeSelect id={id} value={value} disabled={disabled} required={required} onChange={event => onChange(event.target.value)}>
      <NativeSelectOption value="">—</NativeSelectOption>
      {value && !options.includes(value) && <NativeSelectOption value={value} disabled>{value} · {t('catalogUnavailable')}</NativeSelectOption>}
      {options.map(option => <NativeSelectOption key={option} value={option}>{option}{entry.value_type === 'version' ? ` · ${media.filter(m => m.version === option).map(m => `${m.arch}${m.hw_scope ? ' / ' + m.hw_scope : ''} [approved]`).join(', ')}` : ''}</NativeSelectOption>)}
    </NativeSelect>
    {entry.value_type === 'version' && <a href={`/p/${projectId}/media`}>{t('catalogRegisterMedia')}</a>}
  </div>
}
