import { useEffect, useRef, useState, type FormEvent } from 'react'
import { useNavigate } from '@tanstack/react-router'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { ActionDialog, ActionError, Pagination } from '../../components/Management'
import { useProject } from '../../components/ProjectWorkspace'
import { ApiError } from '../../lib/api'
import type { ProjectAsset } from '../../lib/bom'
import { createAgentScriptExecution, createSSHScriptExecution, listTaskScripts, type SSHScriptCreateInput } from '../../lib/execution'
import { useAction } from '../../lib/management'
import { useSession, useExpireOnUnauthorized } from '../../lib/session'

type ScriptTarget = Pick<ProjectAsset, 'machine_id' | 'hostname' | 'serial_number'>
type Transport = 'ssh' | 'agent'

function LaunchScript({ assets, transport, onClose, onDenied }: { assets: ScriptTarget[]; transport: Transport; onClose: () => void; onDenied: (error: ApiError) => void }) {
  const { t } = useTranslation(), project = useProject(), session = useSession(), action = useAction(), navigate = useNavigate(), client = useQueryClient()
  const [targets, setTargets] = useState<string[]>([]), [media, setMedia] = useState(''), [page, setPage] = useState(1), [timeout, setTimeout] = useState('5000'), [confirmed, setConfirmed] = useState(false), [invalid, setInvalid] = useState(false)
  const [resultFormat, setResultFormat] = useState<'' | 'json'>('')
  const [preferCache, setPreferCache] = useState(false)
  const attempt = useRef({ body: '', key: '' }), mounted = useRef(true)
  const catalog = useQuery({ queryKey: ['private', session.user.id, project.id, 'task-scripts', transport, page], queryFn: ({ signal }) => listTaskScripts(project.id, page, signal, transport), retry: false, refetchOnWindowFocus: false })
  useExpireOnUnauthorized(catalog.error ?? action.error)
  const eligible = assets
  const pageSize = catalog.data?.page_size ?? 20
  const chosen = !catalog.isError && !catalog.isFetching ? catalog.data?.items.find(item => item.media_id === media) : undefined
  useEffect(() => { mounted.current = true; return () => { mounted.current = false } }, [])
  useEffect(() => { setTargets(previous => previous.filter(id => eligible.some(asset => asset.machine_id === id))) }, [assets])
  useEffect(() => { setConfirmed(false) }, [targets, media, timeout, resultFormat, preferCache, catalog.dataUpdatedAt])
  useEffect(() => { if (catalog.isError) { setMedia(''); setConfirmed(false) } }, [catalog.isError])
  useEffect(() => { const error = action.error ?? catalog.error; if (error instanceof ApiError && [401, 403, 404].includes(error.status)) onDenied(error) }, [action.error, catalog.error])
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const timeoutMS = Number(timeout)
    if (!catalog.data?.enabled || !chosen || !confirmed || !targets.length || targets.length > 32 || targets.some(id => !eligible.some(asset => asset.machine_id === id)) || !Number.isSafeInteger(timeoutMS) || timeoutMS < 100 || timeoutMS > 30000) { setInvalid(true); return }
    setInvalid(false)
    const body: SSHScriptCreateInput = { machine_ids: [...targets].sort(), media_id: chosen.media_id, timeout_ms: timeoutMS, result_format: resultFormat, prefer_cache: preferCache }
    const serialized = JSON.stringify(body)
    if (attempt.current.body !== serialized) attempt.current = { body: serialized, key: crypto.randomUUID() }
    const result = await action.run(() => (transport === 'agent' ? createAgentScriptExecution : createSSHScriptExecution)(project.id, body, session.csrf_token, attempt.current.key))
    if (result.ok && mounted.current) {
      await client.invalidateQueries({ queryKey: ['private', session.user.id, 'project', project.id, 'executions'] })
      if (mounted.current) await navigate({ to: '/p/$projectId/executions', params: { projectId: project.id }, search: { execution: result.value.id, tab: 'overview' } })
    }
  }
  return <ActionDialog title={t(transport === 'agent' ? 'agentScriptTitle' : 'sshScriptTitle')} description={t(transport === 'agent' ? 'agentScriptBoundary' : 'sshScriptBoundary')} busy={action.busy} confirm={t('sshScriptSubmit')} submitDisabled={!targets.length || !chosen || !confirmed || !catalog.data?.enabled} onSubmit={event => void submit(event)} onClose={onClose}>
    <ActionError error={action.error ?? catalog.error} />
    {(['SSH_SCRIPTS_DISABLED', 'AGENT_SCRIPTS_DISABLED', 'AGENT_DISABLED'].includes(action.error?.code ?? '') || catalog.data && !catalog.data.enabled) && <p role="status">{t(transport === 'agent' ? 'agentScriptDisabled' : 'sshScriptDisabled')}</p>}
    {invalid && <p className="field-error" role="alert">{t('sshScriptInvalid')}</p>}
    <p className="field-hint">{t(transport === 'agent' ? 'agentLaunchCurrentPage' : 'sshLaunchCurrentPage')}</p>
    <div className="ui:flex ui:flex-wrap ui:gap-2" role="group" aria-label={t('sshLaunchTargets')}>
      {eligible.map(asset => <Button key={asset.machine_id} type="button" variant={targets.includes(asset.machine_id) ? 'default' : 'outline'} aria-pressed={targets.includes(asset.machine_id)} disabled={action.busy || !targets.includes(asset.machine_id) && targets.length >= 32} onClick={() => setTargets(previous => previous.includes(asset.machine_id) ? previous.filter(id => id !== asset.machine_id) : [...previous, asset.machine_id])}>{asset.hostname} · {asset.serial_number}</Button>)}
    </div>
    <p role="status">{t('sshLaunchSelected', { count: targets.length })}</p>
    <div className="field"><Label htmlFor="ssh-script-media">{t('sshScriptMedia')}</Label><NativeSelect id="ssh-script-media" value={chosen?.media_id ?? ''} disabled={action.busy || catalog.isFetching || catalog.isError || !catalog.data?.enabled} onChange={event => setMedia(event.target.value)}><NativeSelectOption value="">{t('sshScriptChoose')}</NativeSelectOption>{catalog.data?.items.map(item => <NativeSelectOption key={item.media_id} value={item.media_id}>{item.name} · {item.version} · {item.interpreter}</NativeSelectOption>)}</NativeSelect></div>
    {catalog.data && <Pagination offset={(page - 1) * catalog.data.page_size} limit={catalog.data.page_size} total={catalog.data.total} busy={action.busy || catalog.isFetching} onChange={offset => { setPage(offset / pageSize + 1); setMedia('') }} />}
    {catalog.data?.enabled && !catalog.data.items.length && <p>{t('sshScriptEmpty')}</p>}
    {chosen && <div className="impact-notice"><p>{chosen.name} · {chosen.version} · {chosen.size} B</p><code className="ui:break-all">{chosen.sha256}</code></div>}
    <div className="field"><Label htmlFor="script-result-format">{t('scriptResultFormat')}</Label><NativeSelect id="script-result-format" value={resultFormat} disabled={action.busy} onChange={event => setResultFormat(event.target.value === 'json' ? 'json' : '')}><NativeSelectOption value="">{t('scriptResultText')}</NativeSelectOption><NativeSelectOption value="json">{t('scriptResultJSON')}</NativeSelectOption></NativeSelect><p className="field-hint">{t(resultFormat === 'json' ? 'scriptResultJSONBoundary' : 'scriptResultTextBoundary')}</p></div>
    <div className="ui:flex ui:items-start ui:gap-2"><Checkbox id="script-prefer-cache" checked={preferCache} disabled={action.busy} onCheckedChange={value => setPreferCache(value === true)} /><div><Label htmlFor="script-prefer-cache">{t('scriptPreferCache')}</Label><p className="field-hint">{t('scriptPreferCacheHint')}</p></div></div>
    <div className="field"><Label htmlFor="ssh-script-timeout">{t('sshLaunchTimeout')}</Label><Input id="ssh-script-timeout" type="number" min={100} max={30000} step={1} required value={timeout} disabled={action.busy} onChange={event => setTimeout(event.target.value)} /></div>
    <div className="ui:flex ui:items-start ui:gap-2"><Checkbox id="ssh-script-confirm" checked={confirmed} disabled={action.busy || !chosen || !targets.length} onCheckedChange={value => setConfirmed(value === true)} /><Label htmlFor="ssh-script-confirm">{t('sshScriptConfirm')}</Label></div>
  </ActionDialog>
}

export function SSHScripts({ assets }: { assets: ProjectAsset[] }) {
  return <ApprovedScripts assets={assets.filter(asset => asset.state === 'provisioned' && asset.ssh_trust === 'established' && asset.ssh_trust_source === 'ssh')} transport="ssh" />
}

export function ApprovedScripts({ assets, transport }: { assets: ScriptTarget[]; transport: Transport }) {
  const { t } = useTranslation(), project = useProject(), session = useSession(), client = useQueryClient()
  const [open, setOpen] = useState(false), [denied, setDenied] = useState<ApiError | null>(null)
  const allowed = !['archived', 'delivered'].includes(project.state) && (session.user.is_admin || ['Admin', 'DE', 'DL', 'SRE'].includes(project.role))
  const eligible = assets.length > 0
  useEffect(() => { if (!allowed || denied) setOpen(false) }, [allowed, denied])
  useEffect(() => { setOpen(false); setDenied(null) }, [project.id, session.user.id])
  if (!allowed) return null
  function onDenied(error: ApiError) { setDenied(error); void client.invalidateQueries({ queryKey: ['private', session.user.id, 'project', project.id] }) }
  return <section className="management-panel"><div className="panel-heading"><div><h2>{t(transport === 'agent' ? 'agentScriptTitle' : 'sshScriptTitle')}</h2><p>{t(transport === 'agent' ? 'agentScriptIntro' : 'sshScriptIntro')}</p></div><Button type="button" disabled={!eligible || !!denied} onClick={() => setOpen(true)}>{t('sshScriptOpen')}</Button></div><ActionError error={denied} />{open && <LaunchScript key={`${project.id}:${session.user.id}:${transport}`} assets={assets} transport={transport} onClose={() => setOpen(false)} onDenied={onDenied} />}</section>
}
