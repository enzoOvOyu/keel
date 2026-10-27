import { useRef, useState, type FormEvent } from 'react'
import { Link } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Checkbox } from '@/components/ui/checkbox'
import { ActionDialog, ActionError } from '../../components/Management'
import { useAction } from '../../lib/management'
import type { Issue } from '../../lib/issues'
import { useExpireOnUnauthorized, useSession } from '../../lib/session'
import { approveFirmwarePlan, createFirmwarePlan, executeFirmwarePlan, getFirmwareWorkspace, type FirmwarePlan } from '../../lib/topology-firmware'

export default function TopologyFirmwarePanel({ issue }: { issue: Issue }) {
 const { t } = useTranslation(), session = useSession(), action = useAction()
 const [media, setMedia] = useState(''), [kind, setKind] = useState('flash_bios'), [runbook, setRunbook] = useState(''), [recovery, setRecovery] = useState(''), [reason, setReason] = useState(''), [reboot, setReboot] = useState(false)
 const [pending, setPending] = useState<{ plan: FirmwarePlan; kind: 'approve' | 'execute' } | null>(null), [approvalReason, setApprovalReason] = useState('')
 const attempt = useRef({ body: '', key: '' })
 const query = useQuery({ queryKey: ['private', session.user.id, 'firmware-mock', issue.project_id, issue.id, issue.version], queryFn: ({ signal }) => getFirmwareWorkspace(issue.id, signal), retry: false, refetchOnWindowFocus: false })
 useExpireOnUnauthorized(query.error)
 const workspace = query.data
 function key(body: unknown) { const encoded = JSON.stringify(body); if (encoded !== attempt.current.body) attempt.current = { body: encoded, key: crypto.randomUUID() }; return attempt.current.key }
 async function propose(event: FormEvent<HTMLFormElement>) {
  event.preventDefault()
  const body = { issue_version: issue.version, media_id: media, action: kind, runbook: runbook.trim(), recovery_plan: recovery.trim(), reason: reason.trim(), reboot_approved: reboot }
  const result = await action.run(() => createFirmwarePlan(issue.id, body, session.csrf_token, key(body)))
  if (result.ok) { setReason(''); setRunbook(''); setRecovery(''); setReboot(false); void query.refetch() }
 }
 async function confirm(event: FormEvent<HTMLFormElement>) {
  event.preventDefault(); if (!pending) return
  const body = { scope_hash: pending.plan.scope_hash, reason: approvalReason.trim() }, selected = pending
  const result = await action.run(async () => { if (selected.kind === 'approve') await approveFirmwarePlan(issue.id, selected.plan.id, body, session.csrf_token); else await executeFirmwarePlan(issue.id, selected.plan.id, body, session.csrf_token, key({ plan: selected.plan.id, ...body })) })
  if (result.ok) { setPending(null); setApprovalReason(''); void query.refetch() }
 }
 return <section className="ui:grid ui:gap-3"><h2>{t('topologyFirmwareMockTitle')}</h2><p>{t('topologyFirmwareMockHelp')}</p><ActionError error={query.error} /><ActionError error={action.error} />
  {workspace && !query.isError && <>
   {!workspace.enabled && <p>{t('topologyFirmwareMockDisabled')}</p>}
   {workspace.enabled && workspace.can_propose && <form onSubmit={event => void propose(event)} className="ui:grid ui:gap-2">
    <Label>{t('topologyFirmwareAction')}<NativeSelect value={kind} disabled={action.busy} onChange={event => setKind(event.target.value)}><NativeSelectOption value="flash_bios">BIOS</NativeSelectOption><NativeSelectOption value="update_nvswitch">NVSwitch</NativeSelectOption></NativeSelect></Label>
    <Label>{t('topologyFirmwareMedia')}<NativeSelect value={media} disabled={action.busy} required onChange={event => setMedia(event.target.value)}><NativeSelectOption value="">—</NativeSelectOption>{workspace.media.map(item => <NativeSelectOption key={item.id} value={item.id}>{item.name} · {item.version} · {item.arch} · {item.hw_scope}</NativeSelectOption>)}</NativeSelect></Label>
    <p className="field-hint">{t('topologyFirmwareCompatibility')}</p>
    <Label>{t('topologyFirmwareRunbook')}<Textarea value={runbook} minLength={3} maxLength={2000} required disabled={action.busy} onChange={event => setRunbook(event.target.value)} /></Label>
    <Label>{t('topologyFirmwareRecovery')}<Textarea value={recovery} minLength={3} maxLength={2000} required disabled={action.busy} onChange={event => setRecovery(event.target.value)} /></Label>
    <Label>{t('precollectReason')}<Textarea value={reason} minLength={3} maxLength={2000} required disabled={action.busy} onChange={event => setReason(event.target.value)} /></Label>
    <Label className="ui:flex ui:items-center ui:gap-2"><Checkbox checked={reboot} disabled={action.busy} onCheckedChange={value => setReboot(value === true)} />{t('topologyFirmwareReboot')}</Label>
    <Button type="submit" disabled={action.busy || !reboot || !media}>{t('topologyFirmwarePropose')}</Button>
   </form>}
   <Button variant="outline" disabled={action.busy || query.isFetching} onClick={() => void query.refetch()}>{t('topologyRefresh')}</Button>
   {workspace.plans.map(plan => <article key={plan.id} className="ui:grid ui:gap-2"><strong>{plan.action} · {plan.artifact.name} · {plan.artifact.version} · SIM</strong><p>{plan.serial} · {t('modelVersion')} {plan.model_version_id}</p><p>{t('topologyUntil')}: {new Date(plan.valid_until).toLocaleString()}</p><p>{t('topologyFirmwareRunbook')}: {plan.runbook}</p><p>{t('topologyFirmwareRecovery')}: {plan.recovery_plan}</p><code className="ui:break-all">{plan.scope_hash}</code>{!plan.execution_id && !plan.approved_by && workspace.enabled && workspace.can_approve && plan.created_by !== session.user.id && Date.parse(plan.valid_until) > Date.now() && <Button variant="outline" disabled={action.busy} onClick={() => { setApprovalReason(''); setPending({ plan, kind: 'approve' }) }}>{t('topologyFirmwareApprove')}</Button>}{!plan.execution_id && plan.approved_by && workspace.enabled && workspace.can_propose && Date.parse(plan.valid_until) > Date.now() && <Button variant="outline" disabled={action.busy} onClick={() => { setApprovalReason(''); setPending({ plan, kind: 'execute' }) }}>{t('topologyFirmwareExecute')}</Button>}{plan.execution_id && <Link to="/p/$projectId/executions" params={{ projectId: issue.project_id }} search={{ execution: plan.execution_id }}>{t('bmcViewExecution')}</Link>}</article>)}
  </>}
  {pending && <ActionDialog title={t(pending.kind === 'approve' ? 'topologyFirmwareApprove' : 'topologyFirmwareExecute')} description={t('topologyFirmwareMockHelp')} busy={action.busy} onClose={() => setPending(null)} onSubmit={event => void confirm(event)} confirm={t(pending.kind === 'approve' ? 'topologyFirmwareApprove' : 'topologyFirmwareExecute')} submitDisabled={Array.from(approvalReason.trim()).length < 3}><p>{pending.plan.serial} · {pending.plan.artifact.name} · {pending.plan.artifact.version}</p><code className="ui:break-all">{pending.plan.scope_hash}</code><Label>{t('precollectReason')}<Textarea value={approvalReason} required minLength={3} maxLength={2000} disabled={action.busy} onChange={event => setApprovalReason(event.target.value)} /></Label><ActionError error={action.error} /></ActionDialog>}
 </section>
}
