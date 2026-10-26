import { useEffect, useRef, useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Checkbox } from '@/components/ui/checkbox'
import { Label } from '@/components/ui/label'
import { ActionError } from '../../components/Management'
import { ApiError } from '../../lib/api'
import { requestScriptNonstartProof, type ScriptNonstartProofRequest } from '../../lib/execution'
import { useAction } from '../../lib/management'
import { useSession, useExpireOnUnauthorized } from '../../lib/session'

export function ScriptNonstartProof({ projectId, executionId, attemptId, version, onDenied, onRequested }: { projectId: string; executionId: string; attemptId: string; version: number; onDenied: (error: ApiError) => void; onRequested: () => void }) {
  const { t } = useTranslation(), session = useSession(), action = useAction()
  const [confirmed, setConfirmed] = useState(false), [requested, setRequested] = useState<ScriptNonstartProofRequest | null>(null)
  const submission = useRef({ body: '', key: '' }), mounted = useRef(true)
  useEffect(() => { mounted.current = true; return () => { mounted.current = false } }, [])
  useExpireOnUnauthorized(action.error)
  useEffect(() => { if (action.error instanceof ApiError && [401, 403, 404].includes(action.error.status)) onDenied(action.error) }, [action.error])
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!confirmed || requested) return
    const body = { expected_version: version, confirmed: true as const }
    const serialized = JSON.stringify({ projectId, executionId, attemptId, ...body })
    if (submission.current.body !== serialized) submission.current = { body: serialized, key: crypto.randomUUID() }
    const result = await action.run(() => requestScriptNonstartProof(projectId, executionId, attemptId, body, session.csrf_token, submission.current.key))
    if (result.ok && mounted.current) { setRequested(result.value); setConfirmed(false); onRequested() }
  }
  useEffect(() => {
    if (!requested) return
    const remaining = Date.parse(requested.expires_at) - Date.now()
    const timer = window.setTimeout(() => { setRequested(null); submission.current = { body: '', key: '' } }, Math.max(1, Math.min(remaining + 1, 2_147_483_647)))
    return () => window.clearTimeout(timer)
  }, [requested])
  return <form onSubmit={event => void submit(event)} aria-busy={action.busy}>
    <ActionError error={action.error} />
    <p className="impact-notice">{t('scriptNonstartBoundary')}</p>
    {requested ? <p role="status">{t('scriptNonstartRequested', { expires: new Date(requested.expires_at).toLocaleTimeString() })}</p> : <>
      <div className="ui:flex ui:items-start ui:gap-2"><Checkbox id="script-nonstart-confirm" checked={confirmed} disabled={action.busy} onCheckedChange={value => setConfirmed(value === true)} /><Label htmlFor="script-nonstart-confirm">{t('scriptNonstartConfirm')}</Label></div>
      <Button type="submit" variant="outline" disabled={!confirmed || action.busy}>{t('scriptNonstartRequest')}</Button>
    </>}
  </form>
}
