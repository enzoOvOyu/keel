import { useEffect, useRef, useState, type FormEvent } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { Brand } from '../../components/Brand'
import { KeelStructure } from '../../components/KeelStructure'
import { Preferences } from '../../components/Preferences'
import { Icon } from '../../components/Icon'
import { ApiError, getAuthProviders, getCSRF, login, safeNext, type Session } from '../../lib/api'
import { sessionKey, useAuthSession } from '../../lib/session'

export function LoginPage() {
  const { t, i18n } = useTranslation()
  const navigate = useNavigate()
  const search = useSearch({ from: '/login' })
  const client = useQueryClient()
  const [showPassword, setShowPassword] = useState(false)
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [busy, setBusy] = useState(false)
  const busyRef = useRef(false)
  const [error, setError] = useState<ApiError | null>(null)
  const [invalid, setInvalid] = useState<'username' | 'password' | null>(null)
  const [retryAt, setRetryAt] = useState(0)
  const [now, setNow] = useState(Date.now())
  const retrySeconds = Math.max(0, Math.ceil((retryAt - now) / 1000))
  const providers = useQuery({ queryKey: ['public', 'auth', 'providers'], queryFn: ({ signal }) => getAuthProviders(signal), staleTime: 30_000 })
  const available = providers.data?.status === 'ready' && providers.data.methods.includes('local')
  const auth = useAuthSession(available)
  const pending = providers.isPending
  const contractError = providers.error instanceof ApiError && providers.error.code === 'INVALID_RESPONSE'
  const statusTitle = pending ? 'loading' : contractError ? 'unsupported' : providers.isError ? 'loadError' : providers.data?.status === 'setup_required' ? 'setupRequired' : 'notConfigured'
  const statusDetail = `${statusTitle}Detail`
  const stages = t('stages', { returnObjects: true }) as string[]

  useEffect(() => {
    document.title = `${t('title')} · Keel`
    document.documentElement.lang = i18n.language === 'en' ? 'en' : 'zh-CN'
  }, [i18n.language, t])
  useEffect(() => {
    if (!retryAt) return
    const interval = window.setInterval(() => setNow(Date.now()), 500)
    return () => window.clearInterval(interval)
  }, [retryAt])
  async function enter(session: Session) {
    const next = safeNext(search.next, session)
    if (next.startsWith('/p/')) {
      const [, , projectId, section] = next.split('/')
      const to = section === 'members' ? '/p/$projectId/members' : section === 'audits' ? '/p/$projectId/audits' : '/p/$projectId'
      await navigate({ to, params: { projectId }, replace: true })
    }
    else if (next === '/account/security' || next === '/admin/users' || next === '/admin/audits') await navigate({ to: next, replace: true })
    else await navigate({ to: '/projects', replace: true })
  }
  useEffect(() => { if (available && auth.data && !auth.isFetching && !busyRef.current) void enter(auth.data) }, [available, auth.data, auth.isFetching])

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!available || busyRef.current || retrySeconds > 0) return
    const missing = !username.trim() ? 'username' : !password ? 'password' : null
    if (missing) { setInvalid(missing); document.getElementById(missing)?.focus(); return }
    busyRef.current = true; setBusy(true); setError(null); setInvalid(null)
    try {
      const pre = await getCSRF()
      const session = await login({ method: 'local', username: username.trim(), password }, pre.csrf_token)
      setPassword('')
      await client.cancelQueries()
      client.clear()
      client.setQueryData(sessionKey, session)
      await enter(session)
    } catch (cause) {
      setPassword('')
      const failure = cause instanceof ApiError ? cause : new ApiError(0, 'NETWORK_ERROR')
      setError(failure)
      if (failure.status === 429) { setNow(Date.now()); setRetryAt(Date.now() + Math.max(1, failure.retryAfter) * 1000) }
      if (failure.code === 'SETUP_REQUIRED' || failure.code === 'AUTH_NOT_CONFIGURED') void providers.refetch()
      window.requestAnimationFrame(() => document.getElementById('login-error')?.focus())
    } finally { busyRef.current = false; setBusy(false) }
  }
  const errorKey = error?.status === 401 ? 'invalidCredentials' : error?.status === 429 ? 'rateLimited'
    : error?.status === 403 ? 'loginContextExpired' : 'loginUnavailable'

  return <div className="login-shell">
    <a className="skip-link" href="#login-form">{t('skip')}</a>
    <header className="login-header"><Brand /><Preferences /></header>
    <p className="mobile-slogan">Knowledge-led Execution,<br /> Enforcement &amp; Lifecycle</p>
    <main className="login-main">
      <section className="brand-panel" aria-labelledby="brand-heading">
        <div className="brand-copy"><p className="eyebrow">THE FOUNDATION OF DELIVERY</p>
          <h2 id="brand-heading" className="brand-slogan"><span>Knowledge-led Execution,</span>{' '}<span>Enforcement &amp; Lifecycle</span></h2>
          <p className="brand-description">{t('brandBody')}</p>
        </div>
        <KeelStructure />
        <div className="lifecycle" aria-label={t('lifecycle')}>{stages.map((stage, index) => <span className="lifecycle-stage" key={stage}><span className="stage-number" aria-hidden="true">0{index + 1}</span>{stage}{index < stages.length - 1 && <span className="stage-connector" aria-hidden="true" />}</span>)}</div>
        <p className="brand-assurance"><Icon name="shield" />{t('tagline')}</p>
      </section>
      <section className="form-panel" aria-labelledby="login-heading">
        <div className="login-card">
          <div className="form-heading"><span className="workspace-label">KEEL WORKSPACE</span><h1 id="login-heading">{t('welcome')}</h1><p>{t('intro')}</p>{providers.data?.environment && <span className="environment-label">{t(`environment_${providers.data.environment}`, { defaultValue: providers.data.environment })}</span>}</div>
          {!available && <div className={`identity-notice ${providers.isError ? 'notice-error' : ''}`} id="identity-status" role="status" aria-live="polite" aria-busy={providers.isFetching}>
            <Icon name={providers.isError ? 'refresh' : 'shield'} className="notice-icon" />
            <div><strong>{t(statusTitle)}</strong><p>{t(statusDetail)}</p>
              {!pending && <button type="button" className="text-button" onClick={() => void providers.refetch()} disabled={providers.isFetching}><Icon name="refresh" className={providers.isFetching ? 'rotating' : ''} />{t(providers.isFetching ? 'retrying' : 'retry')}</button>}
            </div>
          </div>}
          {available && search.reason && <p className="session-notice" role="status">{t(search.reason === 'password_changed' ? 'passwordChanged' : search.reason === 'access_changed' ? 'ownAccessChanged' : 'sessionExpired')}</p>}
          {error && <div id="login-error" className="form-error" role="alert" tabIndex={-1}><strong>{t(errorKey)}</strong>{error.status === 429 && <p>{t('retrySeconds', { count: retrySeconds })}</p>}{error.traceId && <small>{t('traceId')}: {error.traceId}</small>}</div>}
          <form id="login-form" aria-describedby={!available ? 'identity-status' : undefined} onSubmit={(event) => void submit(event)} noValidate aria-busy={busy}>
            <div className="field"><label htmlFor="username">{t('account')}</label><input id="username" name="username" type="text" autoComplete="username" autoCapitalize="none" spellCheck={false} maxLength={128} placeholder={t('accountPlaceholder')} value={username} onChange={(event) => { setUsername(event.target.value); setInvalid(null) }} disabled={busy} aria-invalid={invalid === 'username'} aria-describedby={invalid === 'username' ? 'username-error' : undefined} />{invalid === 'username' && <span className="field-error" id="username-error">{t('accountRequired')}</span>}</div>
            <div className="field"><label htmlFor="password">{t('password')}</label><div className="password-field"><input id="password" name="password" type={showPassword ? 'text' : 'password'} autoComplete="current-password" maxLength={1024} placeholder={t('passwordPlaceholder')} value={password} onChange={(event) => { setPassword(event.target.value); setInvalid(null) }} disabled={busy} aria-invalid={invalid === 'password'} aria-describedby={invalid === 'password' ? 'password-error' : undefined} /><button className="password-toggle" type="button" onClick={() => setShowPassword(!showPassword)} aria-label={t(showPassword ? 'hidePassword' : 'showPassword')} aria-pressed={showPassword} aria-controls="password"><Icon name={showPassword ? 'eyeOff' : 'eye'} /></button></div>{invalid === 'password' && <span className="field-error" id="password-error">{t('passwordRequired')}</span>}</div>
            <button className="submit-button" type="submit" disabled={!available || busy || retrySeconds > 0} aria-describedby={!available ? 'identity-status' : undefined}><span>{t(busy ? 'signingIn' : 'signIn')}</span><Icon name={busy ? 'refresh' : 'arrow'} className={busy ? 'rotating' : ''} /></button>
          </form>
          <p className="admin-help">{t('adminHelp')}</p>
          <div className="form-bottom"><Icon name="shield" /><span>{t('footer')}</span></div>
        </div>
      </section>
    </main>
    <footer className="login-footer"><span>Keel <span className="footer-dot" aria-hidden="true">·</span> Delivery &amp; Deployment</span><a href="/healthz" target="_blank" rel="noreferrer">{t('health')}<Icon name="external" /></a></footer>
  </div>
}
