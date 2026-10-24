import { useEffect } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { ParamSelect } from '../../components/ParamSelect'
import { listCatalog, validateCatalog, type CatalogValues } from '../../lib/catalog'
import { useExpireOnUnauthorized, useSession } from '../../lib/session'
import type { FlowVersion } from '../../lib/execution'

export function CatalogInputs({ projectId, flow, values, disabled, onChange, onReady }: {
  projectId: string; flow: FlowVersion; values: CatalogValues; disabled: boolean;
  onChange: (values: CatalogValues) => void; onReady: (valid: boolean) => void
}) {
  const session = useSession(), { t } = useTranslation()
  const catalog = useQuery({ queryKey: ['private', session.user.id, 'project', projectId, 'catalog'], queryFn: ({ signal }) => listCatalog(projectId, signal), staleTime: 0 })
  const check = useQuery({ queryKey: ['private', session.user.id, 'project', projectId, 'catalog-validation', values], queryFn: ({ signal }) => validateCatalog(projectId, values, session.csrf_token, signal), enabled: !!catalog.data, staleTime: 0 })
  useExpireOnUnauthorized(catalog.error); useExpireOnUnauthorized(check.error)
  const ready = !!catalog.data && !!check.data?.valid && !check.isFetching && !catalog.isError && !check.isError
  useEffect(() => onReady(ready), [ready, onReady])
  const change = (key: string, value: string | number | boolean) => { onReady(false); onChange({ ...values, [key]: value }) }
  return <>
    {(catalog.isError || check.isError) && <p role="alert" className="field-error">{t('catalogLoadError')}</p>}
    {check.data && !check.data.valid && <div role="alert" className="field-error">{t('catalogMissing')}{check.data.issues.map((issue, index) => <div key={index}><p>{issue.key}: {issue.dependency ? `${issue.dependency} ∈ ${issue.expected?.join(', ')}` : issue.message}{issue.matrix && <> · {issue.matrix.source}@{issue.matrix.version} · {issue.matrix.report_ref}</>}</p>{issue.code === 'PARAM_COMBINATION_REJECTED' && issue.matrix?.combinations && <div><p>{t('catalogCompleteCombination')}</p><Table aria-label={t('catalogCombinations')}><TableHeader><TableRow>{Object.keys(issue.matrix.combinations[0]).sort().map(key => <TableHead key={key}>{key}</TableHead>)}</TableRow></TableHeader><TableBody>{issue.matrix.combinations.map((row, rowIndex) => <TableRow key={rowIndex}>{Object.keys(row).sort().map(key => <TableCell key={key}>{row[key]}</TableCell>)}</TableRow>)}</TableBody></Table></div>}</div>)}</div>}
    {Object.entries(flow.definition.inputs).map(([key, spec]) => {
      const entry = catalog.data?.find(item => item.key === key)
      const value = values[key] ?? ''
      if (entry) return <ParamSelect projectId={projectId} required={spec.required} key={key} entry={entry} value={String(value)} arch={String(values['cpu.arch'] ?? '')} disabled={disabled || !catalog.data} onChange={value => change(key, value)} />
      const id = `execution-${key}`
      if (key.endsWith('.version')) return <div className="field" key={key}><Label htmlFor={id}>{key}</Label><NativeSelect id={id} disabled value=""><NativeSelectOption value="">{t('catalogRegisterKey')}</NativeSelectOption></NativeSelect></div>
      if (spec.type === 'enum' || spec.type === 'boolean') return <div className="field" key={key}><Label htmlFor={id}>{key}</Label><NativeSelect id={id} disabled={disabled} value={String(value)} required={spec.required} onChange={event => change(key, spec.type === 'boolean' ? event.target.value === 'true' : event.target.value)}><NativeSelectOption value="">—</NativeSelectOption>{(spec.type === 'boolean' ? ['true', 'false'] : spec.enum ?? []).map(option => <NativeSelectOption key={option} value={option}>{option}</NativeSelectOption>)}</NativeSelect></div>
      return <div className="field" key={key}><Label htmlFor={id}>{key === 'profile' ? t('parameterProfile') : key}</Label><Input id={id} type={spec.type === 'number' ? 'number' : 'text'} value={String(value)} maxLength={4096} required={spec.required} disabled={disabled || key.endsWith('.version')} onChange={event => change(key, spec.type === 'number' ? Number(event.target.value) : event.target.value)} />{key.endsWith('.version') && <p className="field-hint">{t('catalogRegisterKey')}</p>}</div>
    })}
    <p className="impact-notice">{t('catalogFreeze')}</p>
  </>
}
