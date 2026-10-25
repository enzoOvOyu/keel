import { useEffect, useState, type FormEvent } from 'react'
import { Link, useNavigate } from '@tanstack/react-router'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { NativeSelect, NativeSelectOption } from '@/components/ui/native-select'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { ActionDialog, ActionError, Pagination, SuccessNotice } from '../../components/Management'
import { useProject } from '../../components/ProjectWorkspace'
import { ApiError } from '../../lib/api'
import { useAction } from '../../lib/management'
import { useExpireOnUnauthorized, useSession } from '../../lib/session'
import { approveSSHTrustRequest, createSSHTrustRequest, executeSSHTrustRequest, listSSHTrustRequests, listSSHDomainKeys, revokeSSHTrustRequest, type ProjectAsset, type SSHTrustRequest } from '../../lib/bom'

function TrustError({ error }: { error: unknown }) {
  const { t } = useTranslation()
  const code = error instanceof ApiError ? error.code : ''
  const explanation = ({ SSH_DISABLED: 'sshTrustDisabled', SSH_TARGET_UNAVAILABLE: 'sshTrustTargetMissing', SSH_TRUST_TARGET_UNAVAILABLE: 'sshTrustTargetMissing', SSH_TRUST_EXPIRED: 'sshTrustExpiredDetail', SSH_TRUST_REQUEST_EXPIRED: 'sshTrustExpiredDetail', SSH_TRUST_CREDENTIAL_INVALID: 'sshTrustCredentialInvalid', SSH_TRUST_BINDING_INVALID: 'sshTrustTargetMissing', SSH_TRUST_HOST_KEY_MISMATCH: 'sshTrustHostKeyMismatch', SSH_TRUST_REAL_VERIFICATION_REQUIRED: 'sshTrustRealRequired', SSH_TRUST_TARGET_CHANGED: 'sshTrustChanged', SSH_TRUST_BINDING_CHANGED: 'sshTrustChanged', SSH_TRUST_INDEPENDENT_APPROVAL_REQUIRED: 'sshTrustIndependent', SSH_TRUST_SELF_APPROVAL: 'sshTrustIndependent', SSH_BINDING_CHANGED: 'sshTrustChanged', SSH_INSTALL_CREDENTIAL_UNAVAILABLE: 'sshTrustInstallKeyUnavailable', SSH_INSTALL_CREDENTIAL_CHANGED: 'sshTrustInstallKeyUnavailable' } as Record<string, string>)[code]
  return <><ActionError error={error} />{explanation && <p className="impact-notice">{t(explanation)}</p>}</>
}

function TrustRequestCreate({ projectId, assets, onClose, onDone, onDenied }: { projectId: string; assets: ProjectAsset[]; onClose: () => void; onDone: () => Promise<void>; onDenied: (error: ApiError) => void }) {
  const { t } = useTranslation(), session = useSession(), action = useAction()
  const [targets] = useState(assets)
  const [machineId, setMachineId] = useState(targets[0]?.machine_id ?? '')
  const asset = targets.find(item => item.machine_id === machineId)
  const [hostKey, setHostKey] = useState(''), [reason, setReason] = useState(''), [minutes, setMinutes] = useState('15'), [validation, setValidation] = useState('')
  const [installKeyId, setInstallKeyId] = useState(''), [keyPage, setKeyPage] = useState(1)
  const keys = useQuery({ queryKey: ['private', session.user.id, 'project', projectId, 'ssh-domain-keys', keyPage], queryFn: ({ signal }) => listSSHDomainKeys(projectId, keyPage, signal) })
  const selectedKey = keys.data?.items.find(item => item.id === installKeyId)
  const dirty = machineId !== targets[0]?.machine_id || hostKey !== '' || reason !== '' || minutes !== '15' || installKeyId !== ''
  useEffect(() => { if (action.error && [401, 403, 404].includes(action.error.status)) onDenied(action.error) }, [action.error])
  useEffect(() => { if (keys.error instanceof ApiError && [401, 403, 404].includes(keys.error.status)) onDenied(keys.error) }, [keys.error])
  function close() { if (!dirty || window.confirm(t('sshTrustDiscard'))) onClose() }
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); setValidation('')
    if (!asset || !/^SHA256:[A-Za-z0-9+/]{43}$/.test(hostKey.trim()) || !reason.trim() || !Number.isInteger(Number(minutes)) || Number(minutes) < 5 || Number(minutes) > 60) { setValidation(t('sshTrustInvalid')); return }
    if (installKeyId && (keys.isError || !selectedKey || selectedKey.revoked_at || Date.parse(selectedKey.expires_at) <= Date.now() + Number(minutes) * 60_000)) { setValidation(t('sshTrustInstallKeyUnavailable')); return }
    const result = await action.run(() => createSSHTrustRequest(projectId, {
      machine_id: asset.machine_id, action: asset.ssh_trust !== 'unverified' ? 'trust.repair' : 'trust.bootstrap', expected_asset_version: asset.version,
      host_key: hostKey.trim(), reason: reason.trim(), expires_in_seconds: Number(minutes) * 60,
      install_key_id: installKeyId || undefined,
    }, session.csrf_token, crypto.randomUUID()))
    if (result.ok) await onDone()
  }
  return <ActionDialog title={t('sshTrustCreate')} description={t('sshTrustCreateDetail')} busy={action.busy} confirm={t('sshTrustCreate')} onSubmit={event => void submit(event)} onClose={close} submitDisabled={!asset}>
    <TrustError error={action.error} />
    {validation && <p className="field-error" role="alert">{validation}</p>}
    <div className="field"><Label htmlFor="ssh-trust-machine">{t('bomHostname')}</Label><NativeSelect id="ssh-trust-machine" value={machineId} onChange={event => setMachineId(event.target.value)} disabled={action.busy} required>{targets.map(item => <NativeSelectOption key={item.machine_id} value={item.machine_id}>{item.hostname} · {item.serial_number}</NativeSelectOption>)}</NativeSelect></div>
    {asset && <dl className="action-target"><dt>{t('sshTrustFrozenAsset')}</dt><dd>{asset.hostname} · v{asset.version} · {t(`trust_state_${asset.ssh_trust}`)}</dd><dt>{t('trustAction')}</dt><dd>{t(asset.ssh_trust !== 'unverified' ? 'sshTrustRepair' : 'sshTrustBootstrap')}</dd></dl>}
    <div className="field"><Label htmlFor="ssh-trust-host-key">{t('trustFingerprint')}</Label><Input id="ssh-trust-host-key" value={hostKey} onChange={event => setHostKey(event.target.value)} placeholder="SHA256:…" maxLength={50} required disabled={action.busy} className="mono-value" /><p className="field-hint">{t('sshTrustHostKeyHint')}</p></div>
    <div className="field"><Label htmlFor="ssh-trust-reason">{t('sshTrustReason')}</Label><Textarea id="ssh-trust-reason" value={reason} onChange={event => setReason(event.target.value)} maxLength={500} required disabled={action.busy} /></div>
    <div className="field"><Label htmlFor="ssh-trust-minutes">{t('sshTrustValidity')}</Label><Input id="ssh-trust-minutes" type="number" inputMode="numeric" min={5} max={60} step={1} value={minutes} onChange={event => setMinutes(event.target.value)} required disabled={action.busy} /><p className="field-hint">{t('sshTrustValidityHint')}</p></div>
    <div className="field"><Label htmlFor="ssh-trust-install-key">{t('sshTrustInstallKey')}</Label><NativeSelect id="ssh-trust-install-key" value={installKeyId} onChange={event => setInstallKeyId(event.target.value)} disabled={action.busy || keys.isPending || keys.isError}><NativeSelectOption value="">{t('sshTrustVerifyExisting')}</NativeSelectOption>{(keys.isError ? [] : keys.data?.items ?? []).filter(item => !item.revoked_at && Date.parse(item.expires_at) > Date.now() + Number(minutes) * 60_000).map(item => <NativeSelectOption key={item.id} value={item.id}>{item.credential_ref} · {item.credential_version}</NativeSelectOption>)}</NativeSelect><ActionError error={keys.error} /><p className="field-hint">{t('sshTrustInstallChoice')}</p></div>
    {keys.data && !keys.isError && keys.data.total > keys.data.page_size && <Pagination offset={(keyPage - 1) * keys.data.page_size} limit={keys.data.page_size} total={keys.data.total} busy={keys.isFetching} onChange={offset => { setInstallKeyId(''); setKeyPage(offset / keys.data.page_size + 1) }} />}
    {selectedKey && !keys.isError && <><code className="ui:block ui:whitespace-pre-wrap ui:break-all">{selectedKey.public_key}</code><p className="impact-notice">{t('sshTrustInstallBoundary')}</p></>}
    <p className="impact-notice">{t('sshTrustIndependent')}</p>
  </ActionDialog>
}

type RequestAction = 'approve' | 'execute' | 'revoke'
function TrustRequestAction({ request, kind, onClose, onDone, onDenied }: { request: SSHTrustRequest; kind: RequestAction; onClose: () => void; onDone: (executionId?: string) => Promise<void>; onDenied: (error: ApiError) => void }) {
  const { t } = useTranslation(), project = useProject(), session = useSession(), action = useAction()
  const [reason, setReason] = useState(''), [validation, setValidation] = useState('')
  const conflict = action.error?.status === 409
  const expired = Date.parse(request.expires_at) <= Date.now()
  useEffect(() => { if (action.error && [401, 403, 404].includes(action.error.status)) onDenied(action.error) }, [action.error])
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); setValidation('')
    if (kind !== 'revoke' && expired) { setValidation(t('sshTrustExpiredDetail')); return }
    if (kind === 'revoke' && !reason.trim()) { setValidation(t('sshTrustReasonRequired')); return }
    if (kind === 'execute') {
      const result = await action.run(() => executeSSHTrustRequest(project.id, request.id, request.state_version, session.csrf_token, crypto.randomUUID()))
      if (result.ok) await onDone(result.value.id)
    } else {
      const result = await action.run(() => kind === 'approve'
        ? approveSSHTrustRequest(project.id, request.id, request.state_version, session.csrf_token, crypto.randomUUID())
        : revokeSSHTrustRequest(project.id, request.id, request.state_version, reason.trim(), session.csrf_token, crypto.randomUUID()))
      if (result.ok) await onDone()
    }
  }
  function close() { if (!reason || window.confirm(t('sshTrustDiscard'))) onClose() }
  return <ActionDialog title={t(`sshTrust_${kind}`)} description={t(`sshTrust_${kind}Detail`)} busy={action.busy} confirm={t(`sshTrust_${kind}`)} danger={kind === 'revoke'} submitDisabled={conflict || kind !== 'revoke' && expired} onSubmit={event => void submit(event)} onClose={close}>
    <TrustError error={action.error} />{validation && <p className="field-error" role="alert">{validation}</p>}
    <dl className="action-target"><dt>{t('projectName')}</dt><dd>{project.name}</dd><dt>{t('sshTrustRequestId')}</dt><dd className="mono-value ui:break-all">{request.id} · v{request.state_version}</dd><dt>{t('sshTrustMachineId')}</dt><dd className="mono-value ui:break-all">{request.machine_id}</dd><dt>{t('trustFingerprint')}</dt><dd className="mono-value ui:break-all">{request.host_key}</dd><dt>{t('sshTrustExpiry')}</dt><dd>{new Date(request.expires_at).toLocaleString()}</dd><dt>{t('sshTrustReason')}</dt><dd className="ui:whitespace-pre-wrap ui:break-words">{request.reason}</dd></dl>
    {request.install_key && <dl className="action-target"><dt>{t('sshTrustInstallKey')}</dt><dd className="mono-value">{request.install_key.credential_ref} · {request.install_key.credential_version}</dd><dt>{t('sshDomainPublicKey')}</dt><dd className="mono-value ui:whitespace-pre-wrap ui:break-all">{request.install_key.public_key}</dd></dl>}
    {kind === 'revoke' && <div className="field"><Label htmlFor="ssh-trust-revoke-reason">{t('sshTrustRevokeReason')}</Label><Textarea id="ssh-trust-revoke-reason" value={reason} onChange={event => setReason(event.target.value)} maxLength={500} required disabled={action.busy} /></div>}
    <p className="impact-notice">{t(kind === 'approve' ? 'sshTrustIndependent' : kind === 'execute' ? request.install_key ? 'sshTrustInstallBoundary' : 'sshTrustExecutionBoundary' : 'sshTrustRevokeBoundary')}</p>
    {request.install_key && kind === 'approve' && <p className="impact-notice">{t('sshTrustInstallBoundary')}</p>}
    {conflict && <p className="impact-notice">{t('sshTrustConflict')}</p>}
    {kind !== 'revoke' && expired && <p className="impact-notice">{t('sshTrustExpiredDetail')}</p>}
  </ActionDialog>
}

export function SSHTrustRequests({ assets }: { assets: ProjectAsset[] }) {
  const { t } = useTranslation(), project = useProject(), session = useSession(), client = useQueryClient(), navigate = useNavigate()
  const [page, setPage] = useState(1), [creating, setCreating] = useState(false), [chosen, setChosen] = useState<{ request: SSHTrustRequest; kind: RequestAction } | null>(null), [success, setSuccess] = useState(''), [now, setNow] = useState(Date.now())
  const [writeDenied, setWriteDenied] = useState<ApiError | null>(null)
  const canWrite = !['archived', 'delivered'].includes(project.state) && (session.user.is_admin || ['Admin', 'DL', 'SRE'].includes(project.role))
  const canApprove = canWrite && (session.user.is_admin || ['Admin', 'DL'].includes(project.role))
  const eligible = assets.filter(asset => asset.state === 'provisioned' && (['unverified', 'broken'].includes(asset.ssh_trust) || asset.ssh_trust === 'established' && asset.ssh_trust_source !== 'ssh'))
  const key = ['private', session.user.id, 'project', project.id, 'ssh-trust-requests']
  const records = useQuery({ queryKey: [...key, page], queryFn: ({ signal }) => listSSHTrustRequests(project.id, page, signal) })
  const readable = !records.isError && !writeDenied
  useExpireOnUnauthorized(records.error)
  useEffect(() => {
    const next = Math.min(...(records.data?.items ?? []).filter(item => ['pending', 'approved'].includes(item.state)).map(item => Date.parse(item.expires_at)).filter(expiry => expiry > now))
    if (!Number.isFinite(next)) return
    const timer = window.setTimeout(() => setNow(Date.now()), Math.max(1, Math.min(next - Date.now() + 1, 2_147_483_647)))
    return () => window.clearTimeout(timer)
  }, [records.data, now])
  useEffect(() => { if (!readable || !canWrite) { setCreating(false); setChosen(null); setSuccess('') } }, [readable, canWrite])
  function denied(error: ApiError) { setWriteDenied(error); void client.invalidateQueries({ queryKey: ['private', session.user.id, 'project', project.id] }) }
  async function refresh() { setWriteDenied(null); await client.invalidateQueries({ queryKey: key }); await client.invalidateQueries({ queryKey: ['private', session.user.id, 'project', project.id, 'assets'] }) }
  async function done(executionId?: string) {
    setCreating(false); setChosen(null); setSuccess(t(executionId ? 'sshTrustExecutionCreated' : 'sshTrustSaved')); await refresh()
    if (executionId) void navigate({ to: '/p/$projectId/executions', params: { projectId: project.id }, search: { execution: executionId, tab: 'overview' } })
  }
  return <section className="management-panel ui:mt-6" aria-labelledby="ssh-trust-heading" aria-busy={records.isFetching}>
    <div className="page-heading"><div><h2 id="ssh-trust-heading">{t('sshTrustRequestsTitle')}</h2><p>{t('sshTrustRequestsDetail')}</p></div><div className="table-actions"><Button type="button" variant="outline" disabled={records.isFetching} onClick={() => { setNow(Date.now()); void refresh() }}>{t('refresh')}</Button>{canWrite && <Button type="button" disabled={!eligible.length || !readable} onClick={() => { setSuccess(''); setCreating(true) }}>{t('sshTrustCreate')}</Button>}</div></div>
    <TrustError error={records.error ?? writeDenied} />{success && readable && <SuccessNotice>{success}</SuccessNotice>}
    {canWrite && !eligible.length && <p className="impact-notice">{t('sshTrustNoEligible')}</p>}
    {records.isPending && <p className="table-loading" role="status">{t('loadingRecords')}</p>}
    {records.data && readable && <>
      {records.data.items.length ? <Table aria-label={t('sshTrustRequestsTitle')}><TableHeader><TableRow><TableHead>{t('sshTrustRequestId')}</TableHead><TableHead>{t('sshTrustMachineId')}</TableHead><TableHead>{t('trustAction')}</TableHead><TableHead>{t('status')}</TableHead><TableHead>{t('sshTrustExpiry')}</TableHead><TableHead>{t('sshTrustApprover')}</TableHead><TableHead>{t('actions')}</TableHead></TableRow></TableHeader><TableBody>{records.data.items.map(item => {
        const expired = Date.parse(item.expires_at) <= now
        return <TableRow key={item.id} data-ssh-trust-request={item.id}><TableCell><span className="mono-value">{item.id.slice(0, 8)}</span><span className="cell-subtitle">v{item.state_version}</span></TableCell><TableCell><span className="mono-value">{assets.find(asset => asset.machine_id === item.machine_id)?.hostname ?? item.machine_id}</span><details><summary>{t('sshTrustDetails')}</summary><dl className="ui:max-w-96 ui:whitespace-normal ui:break-words"><dt>{t('trustFingerprint')}</dt><dd className="mono-value ui:break-all">{item.host_key}</dd><dt>{t('sshTrustReason')}</dt><dd>{item.reason}</dd><dt>{t('sshTrustCreator')}</dt><dd className="mono-value ui:break-all">{item.created_by}</dd><dt>{t('sshTrustCredential')}</dt><dd className="mono-value ui:break-all">{item.credential_ref} · {item.credential_version || '—'}</dd></dl></details></TableCell><TableCell>{t(item.install_key ? 'sshTrustInstallAction' : item.action === 'trust.repair' ? 'sshTrustRepair' : 'sshTrustBootstrap')}</TableCell><TableCell><span className={`status-label ${item.state === 'succeeded' ? 'enabled' : ['failed', 'unknown', 'revoked'].includes(item.state) ? 'disabled' : ''}`}>{t(`sshTrustState_${item.state}`)}</span>{expired && ['pending', 'approved'].includes(item.state) && <span className="cell-subtitle">{t('sshTrustExpired')}</span>}</TableCell><TableCell>{new Date(item.expires_at).toLocaleString()}</TableCell><TableCell className="mono-value ui:break-all">{item.approved_by || '—'}</TableCell><TableCell><div className="table-actions">{canApprove && item.state === 'pending' && !expired && item.created_by !== session.user.id && <Button type="button" variant="outline" size="sm" onClick={() => setChosen({ request: item, kind: 'approve' })}>{t('sshTrust_approve')}</Button>}{canWrite && item.state === 'approved' && !expired && <Button type="button" size="sm" onClick={() => setChosen({ request: item, kind: 'execute' })}>{t('sshTrust_execute')}</Button>}{canWrite && ['pending', 'approved'].includes(item.state) && <Button type="button" variant="outline" size="sm" onClick={() => setChosen({ request: item, kind: 'revoke' })}>{t('sshTrust_revoke')}</Button>}{item.execution_id && <Link to="/p/$projectId/executions" params={{ projectId: project.id }} search={{ execution: item.execution_id, tab: 'overview' }}>{t('sshTrustViewExecution')}</Link>}</div></TableCell></TableRow>
      })}</TableBody></Table> : <p className="table-loading">{t('sshTrustEmpty')}</p>}
      <Pagination offset={(page - 1) * records.data.page_size} limit={records.data.page_size} total={records.data.total} busy={records.isFetching} onChange={offset => setPage(offset / records.data.page_size + 1)} />
    </>}
    {creating && readable && canWrite && <TrustRequestCreate projectId={project.id} assets={eligible} onClose={() => setCreating(false)} onDone={() => done()} onDenied={denied} />}
    {chosen && readable && canWrite && (chosen.kind !== 'approve' || canApprove) && <TrustRequestAction request={chosen.request} kind={chosen.kind} onClose={() => { setChosen(null); void refresh() }} onDone={done} onDenied={denied} />}
  </section>
}
