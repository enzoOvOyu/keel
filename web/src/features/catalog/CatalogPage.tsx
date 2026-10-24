import { useState, type FormEvent } from 'react'
import { Link } from '@tanstack/react-router'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Workspace } from '../../components/Workspace'
import { ProjectWorkspace, useProject } from '../../components/ProjectWorkspace'
import { ActionDialog, ActionError, Pagination, SuccessNotice, PermissionDenied } from '../../components/Management'
import { useAction } from '../../lib/management'
import { ApiError } from '../../lib/api'
import { catalogApprovedMedia, listCatalogManagement, saveCatalogKey, retireCatalogKey, type CatalogEdit, type CatalogKey } from '../../lib/catalog'
import { useExpireOnUnauthorized, useSession } from '../../lib/session'

function EditCatalogDialog({ projectId, entry, onClose, onDone }: { projectId: string | null; entry?: CatalogKey; onClose: () => void; onDone: () => Promise<void> }) {
 const session = useSession(), { t } = useTranslation(), action = useAction()
 const [key, setKey] = useState(entry?.key ?? ''), [type, setType] = useState<CatalogEdit['value_type']>(entry?.value_type ?? 'enum')
 const [source, setSource] = useState(entry?.source_media_type || 'driver'), [options, setOptions] = useState(entry?.value_type === 'version' ? '' : entry?.options?.join('\n') ?? '')
 const [defaultValue, setDefaultValue] = useState(entry?.default_value ?? ''), [deps, setDeps] = useState(JSON.stringify(entry?.deps ?? {}, null, 2)), [error, setError] = useState('')
 const media = useQuery({ queryKey: ['private', session.user.id, projectId === null ? 'global' : 'project', projectId, 'catalog-approved-media'], queryFn: ({ signal }) => catalogApprovedMedia(projectId, signal), enabled: type === 'version', staleTime: 0 })
 useExpireOnUnauthorized(media.error)
 const choices = type === 'version' ? [...new Set((media.data ?? []).filter(item => item.type === source).map(item => item.version))].sort() : options.split('\n').map(value => value.trim()).filter(Boolean)
 async function submit(event: FormEvent<HTMLFormElement>) {
  event.preventDefault(); setError('')
  let dependency: CatalogEdit['deps']
  try { const parsed: unknown = JSON.parse(deps); if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed) || new TextEncoder().encode(deps).length > 65536) throw new Error(); dependency = parsed as CatalogEdit['deps'] } catch { setError(t('catalogInvalidDependencies')); return }
  const result = await action.run(() => saveCatalogKey(projectId, { key, value_type: type, options: type === 'version' ? [] : choices, default_value: defaultValue, source_media_type: type === 'version' ? source : '', deps: dependency, ...(entry ? { expected_updated_at: entry.updated_at } : { create_only: true }) }, session.csrf_token))
  if (result.ok) await onDone()
 }
 return <ActionDialog title={t(entry ? 'catalogEdit' : 'catalogCreate')} description={t('catalogEditorDetail')} busy={action.busy} confirm={t('catalogSave')} submitDisabled={!!defaultValue && !choices.includes(defaultValue) || type === 'version' && (media.isFetching || media.isError)} onSubmit={event => void submit(event)} onClose={onClose}>
  {action.error?.code === 'PARAM_KEY_CONFLICT' ? <p role="alert" className="field-error">{t('catalogConflict')}</p> : <ActionError error={action.error} />}
  {error && <p role="alert" className="field-error">{error}</p>}
  <div className="field"><Label htmlFor="catalog-key">{t('catalogKey')}</Label><Input id="catalog-key" value={key} onChange={event => setKey(event.target.value)} disabled={!!entry || action.busy} required maxLength={119} pattern={'[a-z0-9][a-z0-9._\\-]*'} /></div>
  <div className="field"><Label htmlFor="catalog-type">{t('catalogType')}</Label><NativeSelect id="catalog-type" value={type} disabled={action.busy} onChange={event => { setType(event.target.value as CatalogEdit['value_type']); setDefaultValue('') }}>{['enum', 'string', 'version'].map(value => <NativeSelectOption key={value} value={value}>{value}</NativeSelectOption>)}</NativeSelect></div>
  {type === 'version' ? <>
   <div className="field"><Label htmlFor="catalog-source">{t('catalogSource')}</Label><NativeSelect id="catalog-source" value={source} disabled={action.busy} onChange={event => { setSource(event.target.value); setDefaultValue('') }}>{['driver', 'firmware', 'os', 'nccl', 'tool', 'config'].map(value => <NativeSelectOption key={value} value={value}>{value}</NativeSelectOption>)}</NativeSelect></div>
   <p className="field-hint">{t('catalogVersionSource')}</p><ActionError error={media.error} />
   <>{projectId === null ? <p className="field-hint">{t('catalogGlobalMediaHint')}</p> : <Link to="/p/$projectId/media" params={{ projectId }}>{t('catalogRegisterMedia')}</Link>}</>
  </> : <div className="field"><Label htmlFor="catalog-options">{t('catalogOptions')}</Label><Textarea id="catalog-options" value={options} onChange={event => { setOptions(event.target.value); setDefaultValue('') }} required disabled={action.busy} maxLength={32000} rows={4} /></div>}
  <div className="field"><Label htmlFor="catalog-default">{t('catalogDefault')}</Label><NativeSelect id="catalog-default" value={defaultValue} disabled={action.busy || type === 'version' && !media.data} onChange={event => setDefaultValue(event.target.value)}><NativeSelectOption value="">{t('catalogNoDefault')}</NativeSelectOption>{defaultValue && !choices.includes(defaultValue) && <NativeSelectOption value={defaultValue} disabled>{defaultValue} · {t('catalogUnavailable')}</NativeSelectOption>}{choices.map((value, index) => <NativeSelectOption key={`${value}:${index}`} value={value}>{value}</NativeSelectOption>)}</NativeSelect></div>
  <div className="field"><Label htmlFor="catalog-deps">{t('catalogDependencies')}</Label><Textarea id="catalog-deps" value={deps} onChange={event => setDeps(event.target.value)} disabled={action.busy} maxLength={65536} rows={7} /><p className="field-hint">{t('catalogDependencyHelp')}</p></div>
 </ActionDialog>
}

function RetireDialog({ projectId, projectName, entry, onClose, onDone }: { projectId: string | null; projectName: string; entry: CatalogKey; onClose: () => void; onDone: () => Promise<void> }) {
 const session = useSession(), { t } = useTranslation(), action = useAction()
 return <ActionDialog title={t('catalogRetire')} description={t(projectId === null ? 'catalogGlobalRetireDetail' : 'catalogRetireDetail', { key: entry.key, project: projectName })} busy={action.busy} confirm={t('catalogRetire')} danger onClose={onClose} onSubmit={event => { event.preventDefault(); void action.run(() => retireCatalogKey(projectId, entry, session.csrf_token)).then(result => { if (result.ok) return onDone() }) }}>
  {action.error instanceof ApiError && action.error.code === 'PARAM_KEY_CONFLICT' ? <p role="alert" className="field-error">{t('catalogConflict')}</p> : <ActionError error={action.error} />}
  <p className="impact-notice">{t(projectId === null ? 'catalogGlobalRetireImpact' : 'catalogRetireImpact')}</p>
 </ActionDialog>
}

function CatalogContent({ projectId, projectName, projectRole }: { projectId: string | null; projectName: string; projectRole?: string }) {
 const session = useSession(), client = useQueryClient(), { t } = useTranslation()
 const [page, setPage] = useState(1), [edit, setEdit] = useState<CatalogKey | 'new' | null>(null), [retire, setRetire] = useState<CatalogKey | null>(null), [success, setSuccess] = useState('')
 const manage = session.user.is_admin || projectId !== null && projectRole === 'Admin', canView = manage || projectId !== null && projectRole === 'DL'
 const query = useQuery({ queryKey: ['private', session.user.id, projectId === null ? 'global' : 'project', projectId, 'catalog-management', page], queryFn: ({ signal }) => listCatalogManagement(projectId, page, signal), enabled: canView, staleTime: 0 })
 useExpireOnUnauthorized(query.error)
 async function done() { setEdit(null); setRetire(null); setSuccess(t('catalogSaved')); await client.invalidateQueries({ queryKey: projectId === null ? ['private', session.user.id] : ['private', session.user.id, 'project', projectId] }) }
 if (!canView) return <PermissionDenied />
 return <>
  <header className="management-heading"><div><h1>{t(projectId === null ? 'catalogGlobalTitle' : 'catalogTitle')}</h1><p>{t(projectId === null ? 'catalogGlobalIntro' : 'catalogIntro')}</p></div><div className="table-actions"><Button variant="outline" onClick={() => { setSuccess(''); void query.refetch() }} disabled={query.isFetching}>{t('catalogRefresh')}</Button>{manage && <Button onClick={() => { setSuccess(''); setEdit('new') }}>{t('catalogCreate')}</Button>}</div></header>
  {success && <SuccessNotice>{success}</SuccessNotice>}<ActionError error={query.error} />
  {query.isPending && <p role="status">{t('loading')}</p>}
  {query.data && <>
   <Table aria-label={t(projectId === null ? 'catalogGlobalTitle' : 'catalogTitle')}><TableHeader><TableRow>{['catalogKey', 'catalogType', 'catalogValues', 'catalogDefault', 'catalogScope', 'catalogState', 'catalogActions'].map(key => <TableHead key={key}>{t(key)}</TableHead>)}</TableRow></TableHeader><TableBody>{query.data.items.map(entry => <TableRow key={entry.id}>
    <TableCell><span className="mono-value">{entry.key}</span><details><summary>{t('catalogDependencies')}</summary><pre className="ui:max-w-md ui:whitespace-pre-wrap ui:break-words">{JSON.stringify(entry.deps, null, 2)}</pre></details></TableCell><TableCell>{entry.value_type}{entry.source_media_type && <div>{entry.source_media_type}</div>}</TableCell><TableCell className="ui:max-w-xs ui:whitespace-normal ui:break-words">{entry.options?.join(' / ') || '—'}{entry.value_type === 'version' && <div>{t('catalogVersionSource')}</div>}</TableCell><TableCell>{entry.default_value || '—'}</TableCell><TableCell>{t(entry.scope_key === 'global' ? projectId === null ? 'catalogGlobalEditable' : 'catalogGlobal' : 'catalogProject')}</TableCell><TableCell>{t(entry.state === 'active' ? 'catalogActive' : 'catalogRetired')}</TableCell><TableCell>{manage && entry.scope_key === (projectId === null ? 'global' : 'project:' + projectId) && entry.state === 'active' && <div className="table-actions"><Button variant="outline" onClick={() => { setSuccess(''); setEdit(entry) }}>{t('catalogEdit')}</Button><Button variant="outline" onClick={() => { setSuccess(''); setRetire(entry) }}>{t('catalogRetire')}</Button></div>}</TableCell>
   </TableRow>)}</TableBody></Table>
   {!query.data.items.length && <p className="empty-state">{t('noRecords')}</p>}
   <Pagination offset={(page - 1) * query.data.page_size} limit={query.data.page_size} total={query.data.total} busy={query.isFetching} onChange={offset => setPage(offset / query.data.page_size + 1)} />
  </>}
  {edit && <EditCatalogDialog projectId={projectId} key={projectId + (edit === 'new' ? ':new' : edit.id)} entry={edit === 'new' ? undefined : edit} onClose={() => setEdit(null)} onDone={done} />}
  {retire && <RetireDialog projectId={projectId} projectName={projectName} key={projectId + retire.id} entry={retire} onClose={() => setRetire(null)} onDone={done} />}
 </>
}
export function CatalogPage({ projectId }: { projectId: string }) { return <ProjectWorkspace projectId={projectId}><ProjectCatalogContent key={projectId} /></ProjectWorkspace> }

function ProjectCatalogContent() { const project = useProject(); return <CatalogContent projectId={project.id} projectName={project.name} projectRole={project.role} /> }
export function GlobalCatalogPage() { return <Workspace><CatalogContent projectId={null} projectName="" /></Workspace> }
