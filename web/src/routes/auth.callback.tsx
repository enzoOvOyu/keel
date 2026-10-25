import { useEffect, useState } from 'react'
import { createFileRoute, Link } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { getSession, safeNext } from '../lib/api'

export const Route = createFileRoute('/auth/callback')({
  validateSearch: (search: Record<string, unknown>) => ({ status: search.status === 'ok' ? 'ok' : 'failed', next: typeof search.next === 'string' ? search.next : '/projects' }),
  component: Callback,
})
function Callback() {
  const { t } = useTranslation()
  const { status, next } = Route.useSearch()
  const [failed, setFailed] = useState(status !== 'ok')
  useEffect(() => {
    if (status !== 'ok') return
    let active = true
    void getSession().then((session) => {
      if (!active) return
      if (session) window.location.replace(safeNext(next, session))
      else setFailed(true)
    }).catch(() => { if (active) setFailed(true) })
    return () => { active = false }
  }, [status, next])
  return <main className="fallback"><h1>Keel</h1><p role={failed ? 'alert' : 'status'}>{t(failed ? 'organizationSignInFailed' : 'signingIn')}</p>{failed && <Link to="/login">{t('signIn')}</Link>}</main>
}
