import { useEffect, useRef, useState, type FormEvent } from 'react'
import { useNavigate } from '@tanstack/react-router'
import { useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { ActionDialog, ActionError } from '../../components/Management'
import { useProject } from '../../components/ProjectWorkspace'
import { ApiError } from '../../lib/api'
import type { ProjectAsset } from '../../lib/bom'
import { createSSHExecution, type SSHCreateInput } from '../../lib/execution'
import { useAction } from '../../lib/management'
import { useSession } from '../../lib/session'

function Launch({ assets, onClose, onDenied }: { assets: ProjectAsset[]; onClose: () => void; onDenied: (error: ApiError) => void }) {
  const { t } = useTranslation(), project = useProject(), session = useSession(), action = useAction(), navigate = useNavigate(), client = useQueryClient()
  const [selected, setSelected] = useState<string[]>([]), [operation, setOperation] = useState<SSHCreateInput['operation']>('host.identity')
  const [timeout, setTimeout] = useState('5000'), [wait, setWait] = useState('1000'), [validation, setValidation] = useState('')
  const attempt = useRef({ body: '', key: '' })
  const mounted = useRef(true)
  useEffect(() => { mounted.current = true; return () => { mounted.current = false } }, [])
  const eligible = assets.filter(asset => asset.state === 'provisioned' && asset.ssh_trust === 'established' && asset.ssh_trust_source === 'ssh')
  useEffect(() => { setSelected(previous => previous.filter(id => eligible.some(asset => asset.machine_id === id))) }, [assets])
  useEffect(() => { if (action.error && [401, 403, 404].includes(action.error.status)) onDenied(action.error) }, [action.error])
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const timeoutMS = Number(timeout), waitMS = operation === 'host.wait' ? Number(wait) : 0
    if (!selected.length || selected.length > 32 || !Number.isSafeInteger(timeoutMS) || timeoutMS < 100 || timeoutMS > 30000
      || !Number.isSafeInteger(waitMS) || waitMS < 0 || waitMS >= timeoutMS || selected.some(id => !eligible.some(asset => asset.machine_id === id))) { setValidation(t('sshLaunchInvalid')); return }
    setValidation('')
    const body: SSHCreateInput = { machine_ids: [...selected].sort(), operation, timeout_ms: timeoutMS, wait_ms: waitMS }
    const serialized = JSON.stringify(body)
    if (attempt.current.body !== serialized) attempt.current = { body: serialized, key: crypto.randomUUID() }
    const result = await action.run(() => createSSHExecution(project.id, body, session.csrf_token, attempt.current.key))
    if (result.ok && mounted.current) {
      await client.invalidateQueries({ queryKey: ['private', session.user.id, 'project', project.id, 'executions'] })
      if (!mounted.current) return
      await navigate({ to: '/p/$projectId/executions', params: { projectId: project.id }, search: { execution: result.value.id, tab: 'overview' } })
    }
  }
  const bindingRequired = action.error && ['SSH_DOMAIN_BINDING_REQUIRED', 'SSH_TARGET_UNAVAILABLE', 'SSH_INSTALL_CREDENTIAL_UNAVAILABLE'].includes(action.error.code)
  return <ActionDialog title={t('sshLaunchTitle')} description={t('sshLaunchBoundary')} busy={action.busy} confirm={t('sshLaunchConfirm')} submitDisabled={!selected.length} onSubmit={event => void submit(event)} onClose={onClose}>
    <ActionError error={action.error} />
    {action.error?.code === 'CACHE_INVENTORY_DISABLED' && <p role="status">{t('cacheInventoryDisabled')}</p>}
    {action.error?.code === 'SSH_DISABLED' && <p className="impact-notice">{t('sshLaunchDisabled')}</p>}
    {bindingRequired && <p className="impact-notice">{t('sshLaunchBindingRequired')}</p>}
    {validation && <p className="field-error" role="alert">{validation}</p>}
    <p className="field-hint">{t('sshLaunchCurrentPage')}</p>
    <div className="ui:flex ui:flex-wrap ui:gap-2" role="group" aria-label={t('sshLaunchTargets')}>
      {eligible.map(asset => <Button key={asset.machine_id} type="button" className="ui:h-auto ui:min-h-9 ui:min-w-0 ui:max-w-full ui:whitespace-normal ui:break-all ui:text-left" variant={selected.includes(asset.machine_id) ? 'default' : 'outline'} aria-pressed={selected.includes(asset.machine_id)} disabled={action.busy || !selected.includes(asset.machine_id) && selected.length >= 32} onClick={() => setSelected(previous => previous.includes(asset.machine_id) ? previous.filter(id => id !== asset.machine_id) : [...previous, asset.machine_id])}>{asset.hostname} · {asset.serial_number}</Button>)}
    </div>
    <p role="status">{t('sshLaunchSelected', { count: selected.length })}</p>
    <div className="field"><Label htmlFor="ssh-launch-operation">{t('sshLaunchOperation')}</Label><NativeSelect id="ssh-launch-operation" value={operation} disabled={action.busy} onChange={event => setOperation(event.target.value === 'host.wait' ? 'host.wait' : event.target.value === 'host.cache.inventory' ? 'host.cache.inventory' : 'host.identity')}><NativeSelectOption value="host.identity">{t('sshLaunchIdentity')}</NativeSelectOption><NativeSelectOption value="host.wait">{t('sshLaunchWait')}</NativeSelectOption><NativeSelectOption value="host.cache.inventory">{t('cacheInventoryOperation')}</NativeSelectOption></NativeSelect></div>
    <div className="field"><Label htmlFor="ssh-launch-timeout">{t('sshLaunchTimeout')}</Label><Input id="ssh-launch-timeout" type="number" inputMode="numeric" min={100} max={30000} step={1} required value={timeout} disabled={action.busy} onChange={event => setTimeout(event.target.value)} /></div>
    {operation === 'host.cache.inventory' && <p className="field-hint">{t('cacheInventoryBoundary')}</p>}
    {operation === 'host.wait' && <div className="field"><Label htmlFor="ssh-launch-wait">{t('sshLaunchWaitDuration')}</Label><Input id="ssh-launch-wait" type="number" inputMode="numeric" min={0} max={Math.max(0, Number(timeout) - 1)} step={1} required value={wait} disabled={action.busy} onChange={event => setWait(event.target.value)} /></div>}
    <p className="impact-notice">{t('sshLaunchAdmission')}</p>
  </ActionDialog>
}

export function SSHExecutions({ assets }: { assets: ProjectAsset[] }) {
  const { t } = useTranslation(), project = useProject(), session = useSession(), client = useQueryClient()
  const [open, setOpen] = useState(false), [denied, setDenied] = useState<ApiError | null>(null)
  const allowed = !['archived', 'delivered'].includes(project.state) && (session.user.is_admin || ['Admin', 'DE', 'DL', 'SRE'].includes(project.role))
  const eligible = assets.filter(asset => asset.state === 'provisioned' && asset.ssh_trust === 'established' && asset.ssh_trust_source === 'ssh')
  useEffect(() => { if (!allowed || denied) setOpen(false) }, [allowed, denied])
  if (!allowed) return null
  function onDenied(error: ApiError) { setDenied(error); void client.invalidateQueries({ queryKey: ['private', session.user.id, 'project', project.id] }) }
  return <section className="management-panel">
    <div className="panel-heading"><div><h2>{t('sshLaunchTitle')}</h2><p>{t('sshLaunchIntro')}</p></div><Button type="button" disabled={!eligible.length || !!denied} onClick={() => setOpen(true)}>{t('sshLaunchOpen')}</Button></div>
    <ActionError error={denied} />
    {!eligible.length && <p className="impact-notice">{t('sshLaunchNoTargets')}</p>}
    {open && <Launch assets={assets} onClose={() => setOpen(false)} onDenied={onDenied} />}
  </section>
}
