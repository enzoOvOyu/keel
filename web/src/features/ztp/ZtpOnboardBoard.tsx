import { useEffect, useState, type FormEvent } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { ProjectWorkspace, canManageProject, useProject } from '../../components/ProjectWorkspace'
import { useSession, useExpireOnUnauthorized } from '../../lib/session'
import { useAction } from '../../lib/management'
import { actZTPSimSession, runZTPSimSession, createZTPSimSession, getZTPSimSession, getZTPIsolationScriptManifest, listZTPSimSessions, listZTPDeviceObservations, listZTPIdentityIssues, listZTPCredentialIssues, resolveZTPIdentityIssue, stageZTPMockKea, commitZTPMockKea, deactivateZTPMockKea, type ZTPMockKeaPlan, type ZTPAction } from '../../lib/ztp'

const stages = ['powered', 'dhcp', 'booturl', 'identity', 'lookup', 'fetch', 'config', 'restart', 'managed']
function Board() {
  const { t } = useTranslation(), project = useProject(), session = useSession(), client = useQueryClient(), action = useAction()
  const [selectedId, setSelectedId] = useState(''), [batch, setBatch] = useState(''), [channel, setChannel] = useState<'239' | '143' | '60-43'>('239')
  const [serial, setSerial] = useState(''), [mgmtMac, setMgmtMac] = useState(''), [baseMac, setBaseMac] = useState('')
  const [evidence, setEvidence] = useState(''), [phenomenon, setPhenomenon] = useState(''), [cause, setCause] = useState(''), [nextAction, setNextAction] = useState('')
  const [clock, setClock] = useState(Date.now())
  const [pool, setPool] = useState(''), [bootURL, setBootURL] = useState(''), [redirectURI, setRedirectURI] = useState('')
  const [vci, setVci] = useState('cumulus-linux x86_64'), [nos, setNos] = useState('cumulus-linux'), [versionIntent, setVersionIntent] = useState('')
  const [linkIP, setLinkIP] = useState(''), [plan, setPlan] = useState<ZTPMockKeaPlan | null>(null)
  const [deactivateReason, setDeactivateReason] = useState(''), [deactivateConfirmed, setDeactivateConfirmed] = useState(false)
  const [issueResolutionReason, setIssueResolutionReason] = useState('')
  const [credentialVerdict, setCredentialVerdict] = useState<'reachable' | 'expired' | 'unreachable'>('reachable')
  const canManage = canManageProject(project, session.user.is_admin)
  const scriptManifest = useQuery({ queryKey: ['private', session.user.id, project.id, 'ztp-isolation-script'], queryFn: ({ signal }) => getZTPIsolationScriptManifest(project.id, signal) })
  const list = useQuery({ queryKey: ['private', session.user.id, project.id, 'ztp-sims'], queryFn: ({ signal }) => listZTPSimSessions(project.id, signal), refetchInterval: 5000 })
  const observations = useQuery({ queryKey: ['private', session.user.id, project.id, 'ztp-device-observations'], queryFn: ({ signal }) => listZTPDeviceObservations(project.id, signal) })
  const issues = useQuery({ queryKey: ['private', session.user.id, project.id, 'ztp-identity-issues'], queryFn: ({ signal }) => listZTPIdentityIssues(project.id, signal) })
  const credentialIssues = useQuery({ queryKey: ['private', session.user.id, project.id, 'ztp-credential-issues'], queryFn: ({ signal }) => listZTPCredentialIssues(project.id, signal) })
  const detail = useQuery({ queryKey: ['private', session.user.id, project.id, 'ztp-sim', selectedId], queryFn: ({ signal }) => getZTPSimSession(project.id, selectedId, signal), enabled: !!selectedId, refetchInterval: selectedId ? 2000 : false })
  useExpireOnUnauthorized(scriptManifest.error ?? list.error ?? detail.error ?? observations.error ?? issues.error ?? credentialIssues.error)
  async function stagePlan(event: FormEvent) {
    event.preventDefault()
    const result = await action.run(() => stageZTPMockKea(project.id, { batch_ref: batch.trim(), stage_ref: 'N-S4', management_cidr: pool.trim(), option_239_url: bootURL.trim(), option_143_uris: [redirectURI.trim()], option_60_vci: vci.trim(), option_43: { nos: nos.trim(), version_intent: versionIntent.trim() } }, session.csrf_token))
    if (result.ok) setPlan(result.value)
  }
  async function commitPlan() {
    if (!plan?.staged_hash) return
    const candidate = plan, digest = plan.staged_hash
    const result = await action.run(() => commitZTPMockKea(project.id, { batch_ref: candidate.batch_ref, stage_ref: candidate.stage_ref, staged_hash: digest, expected_version: candidate.version }, session.csrf_token))
    if (result.ok) setPlan(result.value)
  }
  async function deactivatePlan() {
    if (!plan?.active_hash || !deactivateConfirmed || !deactivateReason.trim()) return
    const result = await action.run(() => deactivateZTPMockKea(project.id, { batch_ref: plan.batch_ref, stage_ref: plan.stage_ref, active_hash: plan.active_hash!, expected_version: plan.version, reason: deactivateReason.trim() }, session.csrf_token))
    if (result.ok) { setPlan(result.value); setDeactivateReason(''); setDeactivateConfirmed(false) }
  }
  async function resolveIssue(issueId: string) {
    if (!issueResolutionReason.trim()) return
    const result = await action.run(() => resolveZTPIdentityIssue(project.id, issueId, issueResolutionReason.trim(), session.csrf_token))
    if (result.ok) { setIssueResolutionReason(''); await issues.refetch() }
  }
  async function create(event: FormEvent) {
    event.preventDefault()
    const result = await action.run(() => createZTPSimSession(project.id, { batch_ref: batch.trim(), boot_channel: channel, identity: { serial: serial.trim(), mgmt_mac: mgmtMac.trim(), base_mac: baseMac.trim() } }, session.csrf_token))
    if (!result.ok) return
    setSelectedId(result.value.id); client.setQueryData(['private', session.user.id, project.id, 'ztp-sim', result.value.id], result.value)
    void list.refetch()
  }
  async function change(kind: ZTPAction['action']) {
    if (!detail.data) return
    const payload: ZTPAction = { action: kind, expected_version: detail.data.state_version, ...(kind === 'advance' && detail.data.state === 'powered' ? { dora: { batch_ref: detail.data.batch_ref, stage_ref: 'N-S4', interface: 'eth0', link_ip: linkIP.trim(), vendor_class: vci.trim() } } : {}), ...(kind === 'advance' && detail.data.state !== 'powered' ? { evidence_ref: evidence.trim() } : {}), ...(kind === 'advance' && detail.data.state === 'restart' ? { credential_verdict: credentialVerdict } : {}), ...(kind === 'fail' ? { phenomenon: phenomenon.trim(), cause: cause.trim(), next_action: nextAction.trim() } : {}) }
    const result = await action.run(() => actZTPSimSession(project.id, detail.data!.id, payload, session.csrf_token))
    if (!result.ok) { void detail.refetch(); return }
    client.setQueryData(['private', session.user.id, project.id, 'ztp-sim', result.value.id], result.value)
    setEvidence(''); void list.refetch(); void credentialIssues.refetch()
  }
  async function runAll() {
    if (!detail.data) return
    const current = detail.data
    const result = await action.run(() => runZTPSimSession(project.id, current.id, { expected_version: current.state_version, dora: { batch_ref: current.batch_ref, stage_ref: 'N-S4', interface: 'eth0', link_ip: linkIP.trim(), vendor_class: vci.trim() }, credential_verdict: credentialVerdict }, session.csrf_token))
    if (!result.ok) { void detail.refetch(); return }
    client.setQueryData(['private', session.user.id, project.id, 'ztp-sim', result.value.id], result.value)
    void list.refetch(); void credentialIssues.refetch()
  }
  const current = detail.data, canAdvance = current && stages.indexOf(current.state) >= 0 && current.state !== 'managed'
  useEffect(() => {
    if (current?.state !== 'failed' || !current.next_retry_at) return
    const timer = window.setInterval(() => setClock(Date.now()), 1000)
    return () => window.clearInterval(timer)
  }, [current?.state, current?.next_retry_at])
  return <main className="page-panel">
    <header><h1>{t('ztpTitle')}</h1><p>{t('ztpBoundary')}</p></header>
    {scriptManifest.data && <section className="panel"><h2>{t('ztpIsolationManifest')}</h2><p>{t('ztpIsolationApproval')}</p><p>SHA256: <code>{scriptManifest.data.sha256}</code> · {scriptManifest.data.size} bytes</p><details><summary>{t('ztpIsolationReview')}</summary><pre>{scriptManifest.data.script}</pre></details></section>}
    {canManage && <form onSubmit={(event) => void stagePlan(event)} className="panel">
      <h2>{t('ztpKeaTitle')}</h2><p>{t('ztpKeaBoundary')}</p>
      <div className="field"><label htmlFor="kea-batch">{t('ztpBatch')}</label><input id="kea-batch" required value={batch} onChange={e => { setBatch(e.target.value); setPlan(null) }} /></div>
      <div className="field"><label htmlFor="kea-pool">{t('ztpKeaPool')}</label><input id="kea-pool" required value={pool} onChange={e => { setPool(e.target.value); setPlan(null) }} placeholder="192.0.2.0/24" /></div>
      <div className="field"><label htmlFor="kea-url">{t('ztpKea239')}</label><input id="kea-url" required value={bootURL} onChange={e => { setBootURL(e.target.value); setPlan(null) }} placeholder={`https://boot.example.invalid/ztp?project=${project.id}&batch=...&stage=N-S4&policy=APPROVED_MEDIA_ID`} /></div>
      <div className="field"><label htmlFor="kea-redirect">{t('ztpKea143')}</label><input id="kea-redirect" required value={redirectURI} onChange={e => { setRedirectURI(e.target.value); setPlan(null) }} placeholder="https://boot.example.invalid" /></div>
      <div className="field"><label htmlFor="kea-vci">{t('ztpKea60')}</label><input id="kea-vci" required value={vci} onChange={e => { setVci(e.target.value); setPlan(null) }} /></div>
      <div className="field"><label htmlFor="kea-nos">{t('ztpKeaNOS')}</label><input id="kea-nos" required value={nos} onChange={e => { setNos(e.target.value); setPlan(null) }} /></div>
      <div className="field"><label htmlFor="kea-version">{t('ztpKeaVersion')}</label><input id="kea-version" required value={versionIntent} onChange={e => { setVersionIntent(e.target.value); setPlan(null) }} /></div>
      <button disabled={action.busy}>{t('ztpKeaStage')}</button>
      {plan?.staged_hash && <p>{t('ztpKeaStaged')}: <code>{plan.staged_hash}</code> <button type="button" disabled={action.busy} onClick={() => void commitPlan()}>{t('ztpKeaCommit')}</button></p>}
      {plan?.active_hash && <><p>{t('ztpKeaActive')}: <code>{plan.active_hash}</code></p><details><summary>{t('ztpKeaDeactivate')}</summary><p>{t('ztpKeaDeactivateBoundary')}</p><div className="field"><label htmlFor="ztp-deactivate-reason">{t('ztpKeaDeactivateReason')}</label><input id="ztp-deactivate-reason" maxLength={300} value={deactivateReason} onChange={e => setDeactivateReason(e.target.value)} /></div><label><input type="checkbox" checked={deactivateConfirmed} onChange={e => setDeactivateConfirmed(e.target.checked)} />{t('ztpKeaDeactivateConfirm')}</label><button type="button" className="secondary-button" disabled={action.busy || !deactivateConfirmed || !deactivateReason.trim()} onClick={() => void deactivatePlan()}>{t('ztpKeaDeactivate')}</button></details></>}
    </form>}
    {canManage && <form onSubmit={(event) => void create(event)} className="panel">
      <h2>{t('ztpCreate')}</h2>
      <div className="field"><label htmlFor="ztp-batch">{t('ztpBatch')}</label><input id="ztp-batch" required maxLength={160} value={batch} onChange={e => { setBatch(e.target.value); setPlan(null) }} /></div>
      <div className="field"><label htmlFor="ztp-channel">{t('ztpChannel')}</label><select id="ztp-channel" value={channel} onChange={e => setChannel(e.target.value as typeof channel)}><option value="239">239</option><option value="143">143</option><option value="60-43">60/43</option></select></div>
      <div className="field"><label htmlFor="ztp-serial">{t('ztpSerial')}</label><input id="ztp-serial" value={serial} onChange={e => setSerial(e.target.value)} /></div>
      <div className="field"><label htmlFor="ztp-mgmt">{t('ztpMgmtMac')}</label><input id="ztp-mgmt" value={mgmtMac} onChange={e => setMgmtMac(e.target.value)} /></div>
      <div className="field"><label htmlFor="ztp-base">{t('ztpBaseMac')}</label><input id="ztp-base" value={baseMac} onChange={e => setBaseMac(e.target.value)} /></div>
      <button disabled={action.busy || !batch.trim() || !(serial.trim() || mgmtMac.trim() || baseMac.trim())}>{t('ztpCreate')}</button>
    </form>}
    <section className="panel"><h2>{t('ztpSessions')}</h2><button type="button" className="secondary-button" onClick={() => void list.refetch()}>{t('ztpRefresh')}</button>
      {list.isError && <p role="alert">{t('ztpLoadError')}</p>}
      {list.data?.length === 0 && <p>{t('ztpNone')}</p>}
      <ul>{list.data?.map(item => <li key={item.id}><button type="button" className="secondary-button" onClick={() => setSelectedId(item.id)}>{item.batch_ref} · {item.state} · {item.identity_verdict}</button></li>)}</ul>
    </section>
    <section className="panel"><h2>{t('ztpIdentityIssues')}</h2><p>{t('ztpIdentityIssuesBoundary')}</p><button type="button" className="secondary-button" onClick={() => void issues.refetch()}>{t('ztpRefresh')}</button>
      {issues.isError && <p role="alert">{t('ztpLoadError')}</p>}
      {issues.data?.length === 0 && <p>{t('ztpIdentityIssuesNone')}</p>}
      {canManage && <div className="field"><label htmlFor="ztp-issue-resolution">{t('ztpIdentityIssueResolutionReason')}</label><input id="ztp-issue-resolution" maxLength={300} value={issueResolutionReason} onChange={e => setIssueResolutionReason(e.target.value)} /></div>}
      <ol>{issues.data?.map(item => <li key={item.id}><time>{new Date(item.last_seen_at).toLocaleString()}</time> · {item.serial_number || item.identity_key} · {item.batch_ref} · {item.boot_channel} · {item.reasons.join(', ')} · {item.event_count} · {item.state}{item.resolution_reason && <> · {item.resolution_reason}</>}{canManage && item.state === 'open' && <button type="button" disabled={action.busy || !issueResolutionReason.trim()} onClick={() => void resolveIssue(item.id)}>{t('ztpIdentityIssueResolve')}</button>}</li>)}</ol>
    </section>
    <section className="panel"><h2>{t('ztpCredentialIssues')}</h2><p>{t('ztpCredentialIssuesBoundary')}</p><button type="button" className="secondary-button" onClick={() => void credentialIssues.refetch()}>{t('ztpRefresh')}</button>
      {credentialIssues.isError && <p role="alert">{t('ztpLoadError')}</p>}
      <ol>{credentialIssues.data?.map(item => <li key={item.id}><time>{new Date(item.occurred_at).toLocaleString()}</time> · {item.credential_batch_ref} · {item.verdict} · {t('ztpAttempt')} {item.attempt} · <button type="button" className="secondary-button" onClick={() => setSelectedId(item.session_id)}>{item.session_id}</button></li>)}</ol>
    </section>
    <section className="panel"><h2>{t('ztpDeviceObservations')}</h2><p>{t('ztpDeviceObservationBoundary')}</p><button type="button" className="secondary-button" onClick={() => void observations.refetch()}>{t('ztpRefresh')}</button>
      {observations.isError && <p role="alert">{t('ztpLoadError')}</p>}
      {observations.data?.length === 0 && <p>{t('ztpDeviceObservationsNone')}</p>}
      <ol>{observations.data?.map(item => <li key={item.id}><time>{new Date(item.occurred_at).toLocaleString()}</time> · {item.serial_number} · {item.batch_ref} · {item.event_type}{item.progress_type && <> · {item.progress_type}</>}{item.message && <> · {item.message}</>} · <code>{item.evidence_sha256}</code></li>)}</ol>
    </section>
    {detail.isError && <p role="alert">{t('ztpLoadError')}</p>}
    {current && <section className="panel"><h2>{current.batch_ref} · {current.id}</h2>
      <p>{t('ztpState')}: <strong>{current.state}</strong> · {t('ztpVerdict')}: {current.identity_resolution.verdict} · {t('ztpAttempt')}: {current.attempt}/{current.retry_max + 1}</p>
      {current.next_retry_at && <p>{t('ztpRetryAt')}: {new Date(current.next_retry_at).toLocaleString()}</p>}
      {current.failure_phenomenon && <p>{t('ztpPhenomenon')}: {current.failure_phenomenon} · {t('ztpCause')}: {current.failure_cause} · {t('ztpNextAction')}: {current.failure_next_action}</p>}
      {canManage && <div>
        {canAdvance && <><button type="button" disabled={action.busy || !linkIP.trim() || !vci.trim()} onClick={() => void runAll()}>{t('ztpRunAll')}</button>{current.state === 'restart' && <div className="field"><label htmlFor="ztp-credential-verdict">{t('ztpCredentialVerdict')}</label><select id="ztp-credential-verdict" value={credentialVerdict} onChange={e => setCredentialVerdict(e.target.value as typeof credentialVerdict)}><option value="reachable">{t('ztpCredentialReachable')}</option><option value="expired">{t('ztpCredentialExpired')}</option><option value="unreachable">{t('ztpCredentialUnreachable')}</option></select><small>{t('ztpCredentialBoundary')}</small></div>}{current.state === 'powered' ? <div className="field"><label htmlFor="ztp-link-ip">{t('ztpKeaLinkIP')}</label><input id="ztp-link-ip" value={linkIP} onChange={e => setLinkIP(e.target.value)} placeholder="192.0.2.1" /></div> : <div className="field"><label htmlFor="ztp-evidence">{t('ztpEvidence')}</label><input id="ztp-evidence" value={evidence} onChange={e => setEvidence(e.target.value)} /></div>}<button disabled={action.busy || (current.state === 'powered' ? !linkIP.trim() || !vci.trim() : !evidence.trim())} onClick={() => void change('advance')}>{t('ztpAdvance')}</button></>}
        {canAdvance && <><div className="field"><label htmlFor="ztp-phenomenon">{t('ztpPhenomenon')}</label><input id="ztp-phenomenon" value={phenomenon} onChange={e => setPhenomenon(e.target.value)} /></div><div className="field"><label htmlFor="ztp-cause">{t('ztpCause')}</label><input id="ztp-cause" value={cause} onChange={e => setCause(e.target.value)} /></div><div className="field"><label htmlFor="ztp-next">{t('ztpNextAction')}</label><input id="ztp-next" value={nextAction} onChange={e => setNextAction(e.target.value)} /></div><button className="secondary-button" disabled={action.busy || !phenomenon.trim() || !cause.trim() || !nextAction.trim()} onClick={() => void change('fail')}>{t('ztpFail')}</button></>}
        {current.state === 'failed' && current.next_retry_at && <><div className="field"><label htmlFor="ztp-retry-link-ip">{t('ztpKeaLinkIP')}</label><input id="ztp-retry-link-ip" value={linkIP} onChange={e => setLinkIP(e.target.value)} placeholder="192.0.2.1" /></div><div className="field"><label htmlFor="ztp-retry-credential">{t('ztpCredentialVerdict')}</label><select id="ztp-retry-credential" value={credentialVerdict} onChange={e => setCredentialVerdict(e.target.value as typeof credentialVerdict)}><option value="reachable">{t('ztpCredentialReachable')}</option><option value="expired">{t('ztpCredentialExpired')}</option><option value="unreachable">{t('ztpCredentialUnreachable')}</option></select></div><button type="button" disabled={action.busy || !linkIP.trim()} onClick={() => void runAll()}>{t('ztpUpdateRetryFixture')}</button><button disabled={action.busy || clock < new Date(current.next_retry_at).getTime()} onClick={() => void change('retry')}>{t('ztpRetry')}</button></>}
      </div>}
      {action.error && <p role="alert">{t('ztpActionError')}: {action.error.code}</p>}
      <h3>{t('ztpTimeline')}</h3><ol>{current.events.map(event => <li key={event.seq}><time>{new Date(event.occurred_at).toLocaleString()}</time> · {event.to_state} · {event.event_code}{event.evidence_ref && <> · {event.evidence_ref}</>}</li>)}</ol>
    </section>}
  </main>
}
export function ZtpOnboardBoard({ projectId }: { projectId: string }) { return <ProjectWorkspace projectId={projectId}><Board /></ProjectWorkspace> }
