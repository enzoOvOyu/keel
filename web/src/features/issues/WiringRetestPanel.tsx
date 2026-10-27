import { useEffect, useRef, useState, type FormEvent } from 'react'
import { Link } from '@tanstack/react-router'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { ActionError, Pagination } from '../../components/Management'
import { ApiError } from '../../lib/api'
import type { Issue } from '../../lib/issues'
import { useAction } from '../../lib/management'
import { useExpireOnUnauthorized, useSession } from '../../lib/session'
import { createWiringRetest, listWiringRetests, previewWiringRetest } from '../../lib/wiring'

export default function WiringRetestPanel({ issue }: { issue: Issue }) {
 const { t } = useTranslation(), { session } = useSession(), client = useQueryClient(), action = useAction()
 const [page, setPage] = useState(1), [reason, setReason] = useState(''), [validation, setValidation] = useState('')
 const mounted = useRef(true), attempt = useRef({ body: '', key: '' })
 useEffect(() => { mounted.current = true; return () => { mounted.current = false } }, [])
 const key = ['private', session.user.id, 'wiring-retests', issue.project_id, issue.id]
 const scope = useQuery({ queryKey: [...key, 'scope', issue.version], queryFn: ({ signal }) => previewWiringRetest(issue.id, signal), retry: false, refetchOnWindowFocus: false })
 const history = useQuery({ queryKey: [...key, 'history', issue.version, page], queryFn: ({ signal }) => listWiringRetests(issue.id, page, signal), retry: false, refetchOnWindowFocus: false })
 useExpireOnUnauthorized(scope.error); useExpireOnUnauthorized(history.error)
 const errors = [scope.error, history.error, action.error], denied = errors.some(error => error instanceof ApiError && [401, 403, 404].includes(error.status))
 async function submit(event: FormEvent<HTMLFormElement>) {
  event.preventDefault()
  if (!scope.data?.ready || denied) return
  if (Array.from(reason.trim()).length < 3) { setValidation(t('precollectInvalidReason')); return }
  setValidation('')
  const body = { version: scope.data.issue_version, report_id: scope.data.report_id, reason: reason.trim() }, encoded = JSON.stringify(body)
  if (attempt.current.body !== encoded) attempt.current = { body: encoded, key: crypto.randomUUID() }
  const result = await action.run(() => createWiringRetest(issue.id, body, session.csrf_token, attempt.current.key))
  if (result.ok && mounted.current) { await client.invalidateQueries({ queryKey: ['private', session.user.id, 'issue', issue.id] }); await client.invalidateQueries({ queryKey: ['private', session.user.id, 'issues'] }); void scope.refetch(); void history.refetch() }
 }
 return <section className="ui:grid ui:gap-3"><h2>{t('wiringRetestTitle')}</h2><p>{t('wiringRetestHelp')}</p>{errors.map((error, index) => error ? <ActionError key={index} error={error as ApiError} /> : null)}{!denied && <>
  {scope.isPending && <p role="status">{t('loadingRecords')}</p>}
  {scope.data && !scope.isError && <><p>{t('wiringRetestRepair')}: v{scope.data.repair_version} · {scope.data.repaired_at ? new Date(scope.data.repaired_at).toLocaleString() : '—'}</p><ul>{scope.data.blockers.map(code => <li key={code} className="mono-value">{t(`wiringBlock_${code}`, { defaultValue: code })}</li>)}</ul><Link to="/p/$projectId/assets" params={{ projectId: issue.project_id }} className="text-link">{t('wiringRetestAssets')}</Link>{scope.data.report_id && <p>{t('wiringRetestCandidate')}: {t(`precollect_${scope.data.verdict}`)}</p>}
   <form onSubmit={event => void submit(event)} className="ui:grid ui:gap-2"><Label htmlFor={`wiring-retest-reason-${issue.id}`}>{t('wiringReason')}</Label><Input id={`wiring-retest-reason-${issue.id}`} value={reason} maxLength={1000} disabled={action.busy} onChange={event => setReason(event.target.value)} />{validation && <p role="alert">{validation}</p>}<Button type="submit" disabled={action.busy || !scope.data.ready}>{t('wiringRetestSubmit')}</Button></form>
  </>}
  <Button variant="outline" disabled={action.busy || scope.isFetching || history.isFetching} onClick={() => { void scope.refetch(); void history.refetch() }}>{t('wiringRefreshReports')}</Button>
  {history.data && !history.isError && <><h3>{t('wiringRetestHistory')}</h3>{history.data.items.length === 0 && <p>{t('wiringRetestEmpty')}</p>}{history.data.items.map(row => <article key={row.id} className="ui:grid ui:gap-2"><strong>{t(`precollect_${row.verdict}`)} · {row.stale ? t('precollect_invalidated') : t('precollect_fresh')}</strong><p>{new Date(row.created_at).toLocaleString()} · {row.reason}</p><p>{t('wiringRetestRepair')}: v{row.repair_version}</p>{row.can_close && <p>{t('wiringRetestCanClose')}</p>}{row.escalated && <p role="alert">{t('wiringRetestEscalated')}{row.recipient_ids.length === 0 && ` · ${t('wiringRetestNoDL')}`}</p>}<Link to="/p/$projectId/executions" params={{ projectId: issue.project_id }} search={{ execution: row.execution_id }}>{t('bmcViewExecution')}</Link></article>)}<Pagination offset={(page - 1) * history.data.page_size} limit={history.data.page_size} total={history.data.total} busy={history.isFetching} onChange={offset => setPage(Math.floor(offset / (history.data?.page_size ?? 10)) + 1)} /></>}
 </>}</section>
}
