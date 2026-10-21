import { useEffect, useState, type FormEvent } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { ProjectWorkspace, canManageProject, useProject } from '../../components/ProjectWorkspace'
import { useSession, useExpireOnUnauthorized } from '../../lib/session'
import { useAction } from '../../lib/management'
import { actZTPSimSession, createZTPSimSession, getZTPSimSession, listZTPSimSessions, type ZTPAction } from '../../lib/ztp'

const stages = ['powered', 'dhcp', 'booturl', 'identity', 'lookup', 'fetch', 'config', 'restart', 'managed']
function Board() {
  const { t } = useTranslation(), project = useProject(), session = useSession(), client = useQueryClient(), action = useAction()
  const [selectedId, setSelectedId] = useState(''), [batch, setBatch] = useState(''), [channel, setChannel] = useState<'239' | '143' | '60-43'>('239')
  const [serial, setSerial] = useState(''), [mgmtMac, setMgmtMac] = useState(''), [baseMac, setBaseMac] = useState('')
  const [evidence, setEvidence] = useState(''), [phenomenon, setPhenomenon] = useState(''), [cause, setCause] = useState(''), [nextAction, setNextAction] = useState('')
  const [clock, setClock] = useState(Date.now())
  const canManage = canManageProject(project, session.user.is_admin)
  const list = useQuery({ queryKey: ['private', session.user.id, project.id, 'ztp-sims'], queryFn: ({ signal }) => listZTPSimSessions(project.id, signal) })
  const detail = useQuery({ queryKey: ['private', session.user.id, project.id, 'ztp-sim', selectedId], queryFn: ({ signal }) => getZTPSimSession(project.id, selectedId, signal), enabled: !!selectedId })
  useExpireOnUnauthorized(list.error ?? detail.error)
  async function create(event: FormEvent) {
    event.preventDefault()
    const result = await action.run(() => createZTPSimSession(project.id, { batch_ref: batch.trim(), boot_channel: channel, identity: { serial: serial.trim(), mgmt_mac: mgmtMac.trim(), base_mac: baseMac.trim() } }, session.csrf_token))
    if (!result.ok) return
    setSelectedId(result.value.id); client.setQueryData(['private', session.user.id, project.id, 'ztp-sim', result.value.id], result.value)
    void list.refetch()
  }
  async function change(kind: ZTPAction['action']) {
    if (!detail.data) return
    const payload: ZTPAction = { action: kind, expected_version: detail.data.state_version, ...(kind === 'advance' ? { evidence_ref: evidence.trim() } : {}), ...(kind === 'fail' ? { phenomenon: phenomenon.trim(), cause: cause.trim(), next_action: nextAction.trim() } : {}) }
    const result = await action.run(() => actZTPSimSession(project.id, detail.data!.id, payload, session.csrf_token))
    if (!result.ok) { void detail.refetch(); return }
    client.setQueryData(['private', session.user.id, project.id, 'ztp-sim', result.value.id], result.value)
    setEvidence(''); void list.refetch()
  }
  const current = detail.data, canAdvance = current && stages.indexOf(current.state) >= 0 && current.state !== 'managed'
  useEffect(() => {
    if (current?.state !== 'failed' || !current.next_retry_at) return
    const timer = window.setInterval(() => setClock(Date.now()), 1000)
    return () => window.clearInterval(timer)
  }, [current?.state, current?.next_retry_at])
  return <main className="page-panel">
    <header><h1>{t('ztpTitle')}</h1><p>{t('ztpBoundary')}</p></header>
    {canManage && <form onSubmit={(event) => void create(event)} className="panel">
      <h2>{t('ztpCreate')}</h2>
      <div className="field"><label htmlFor="ztp-batch">{t('ztpBatch')}</label><input id="ztp-batch" required maxLength={160} value={batch} onChange={e => setBatch(e.target.value)} /></div>
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
    {detail.isError && <p role="alert">{t('ztpLoadError')}</p>}
    {current && <section className="panel"><h2>{current.batch_ref} · {current.id}</h2>
      <p>{t('ztpState')}: <strong>{current.state}</strong> · {t('ztpVerdict')}: {current.identity_resolution.verdict} · {t('ztpAttempt')}: {current.attempt}/{current.retry_max + 1}</p>
      {current.next_retry_at && <p>{t('ztpRetryAt')}: {new Date(current.next_retry_at).toLocaleString()}</p>}
      {current.failure_phenomenon && <p>{t('ztpPhenomenon')}: {current.failure_phenomenon} · {t('ztpCause')}: {current.failure_cause} · {t('ztpNextAction')}: {current.failure_next_action}</p>}
      {canManage && <div>
        {canAdvance && <><div className="field"><label htmlFor="ztp-evidence">{t('ztpEvidence')}</label><input id="ztp-evidence" value={evidence} onChange={e => setEvidence(e.target.value)} /></div><button disabled={action.busy || !evidence.trim()} onClick={() => void change('advance')}>{t('ztpAdvance')}</button></>}
        {canAdvance && <><div className="field"><label htmlFor="ztp-phenomenon">{t('ztpPhenomenon')}</label><input id="ztp-phenomenon" value={phenomenon} onChange={e => setPhenomenon(e.target.value)} /></div><div className="field"><label htmlFor="ztp-cause">{t('ztpCause')}</label><input id="ztp-cause" value={cause} onChange={e => setCause(e.target.value)} /></div><div className="field"><label htmlFor="ztp-next">{t('ztpNextAction')}</label><input id="ztp-next" value={nextAction} onChange={e => setNextAction(e.target.value)} /></div><button className="secondary-button" disabled={action.busy || !phenomenon.trim() || !cause.trim() || !nextAction.trim()} onClick={() => void change('fail')}>{t('ztpFail')}</button></>}
        {current.state === 'failed' && current.next_retry_at && <button disabled={action.busy || clock < new Date(current.next_retry_at).getTime()} onClick={() => void change('retry')}>{t('ztpRetry')}</button>}
      </div>}
      {action.error && <p role="alert">{t('ztpActionError')}: {action.error.code}</p>}
      <h3>{t('ztpTimeline')}</h3><ol>{current.events.map(event => <li key={event.seq}><time>{new Date(event.occurred_at).toLocaleString()}</time> · {event.to_state} · {event.event_code}{event.evidence_ref && <> · {event.evidence_ref}</>}</li>)}</ol>
    </section>}
  </main>
}
export function ZtpOnboardBoard({ projectId }: { projectId: string }) { return <ProjectWorkspace projectId={projectId}><Board /></ProjectWorkspace> }
