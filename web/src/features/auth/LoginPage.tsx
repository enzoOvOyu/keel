import { useEffect, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Brand, KeelMark } from '../../components/Brand'
import { Icon } from '../../components/Icon'
import { getAuthProviders, PublicApiError } from '../../lib/api'
import { writePreference } from '../../lib/preferences'

export function LoginPage() {
  const { t, i18n } = useTranslation()
  const [showPassword, setShowPassword] = useState(false)
  const [theme, setTheme] = useState(document.documentElement.dataset.theme === 'dark' ? 'dark' : 'light')
  const providers = useQuery({ queryKey: ['public', 'auth', 'providers'], queryFn: ({ signal }) => getAuthProviders(signal), staleTime: 30_000 })
  const pending = providers.isPending
  const contractError = providers.error instanceof PublicApiError && providers.error.kind === 'contract'
  const statusTitle = pending ? 'loading' : contractError ? 'unsupported' : providers.isError ? 'loadError' : 'notConfigured'
  const statusDetail = pending ? 'loadingDetail' : contractError ? 'unsupportedDetail' : providers.isError ? 'loadErrorDetail' : 'notConfiguredDetail'
  const stages = t('stages', { returnObjects: true }) as string[]

  useEffect(() => {
    document.title = `${t('title')} · keel`
    document.documentElement.lang = i18n.language === 'en' ? 'en' : 'zh-CN'
  }, [i18n.language, t])

  function toggleTheme() {
    const next = theme === 'dark' ? 'light' : 'dark'
    document.documentElement.dataset.theme = next
    writePreference('keel.theme', next)
    setTheme(next)
  }
  function toggleLanguage() {
    const next = i18n.language === 'en' ? 'zh' : 'en'
    writePreference('keel.language', next)
    void i18n.changeLanguage(next)
  }

  return <div className="login-shell">
    <a className="skip-link" href="#login-form">{t('skip')}</a>
    <header className="login-header">
      <Brand />
      <div className="preferences">
        <button type="button" className="language-button" onClick={toggleLanguage} aria-label={t('language')}><Icon name="globe" /><span>{i18n.language === 'en' ? '中文' : 'EN'}</span></button>
        <span className="preference-divider" aria-hidden="true" />
        <button type="button" className="icon-button" onClick={toggleTheme} aria-label={t(theme === 'dark' ? 'lightTheme' : 'darkTheme')}><Icon name={theme === 'dark' ? 'sun' : 'moon'} /></button>
      </div>
    </header>

    <main className="login-main">
      <section className="brand-panel" aria-labelledby="brand-heading">
        <div className="brand-copy"><p className="eyebrow">DELIVERY, WITH DIRECTION.</p>
          <h2 id="brand-heading"><span>{t('brandLine1')}</span><span>{t('brandLine2')}</span></h2>
          <p className="brand-description">{t('brandBody')}</p>
        </div>
        <div className="keel-diagram" aria-hidden="true">
          <div className="diagram-grid" />
          <div className="diagram-orbit orbit-outer" /><div className="diagram-orbit orbit-inner" />
          <svg className="diagram-lines" viewBox="0 0 600 270" fill="none"><path d="M25 175h90l70-60h115l90 55h150M45 205h100l68-58h99l82 50h160M300 115V32M115 175V90M390 170V84" /></svg>
          <span className="diagram-node node-1" /><span className="diagram-node node-2" /><span className="diagram-node node-3" />
          <div className="diagram-core"><KeelMark className="diagram-mark" /></div>
          <span className="diagram-caption">BUILT ON A RELIABLE FOUNDATION</span>
        </div>
        <div className="lifecycle" aria-label={t('lifecycle')}>{stages.map((stage, index) => <span className="lifecycle-stage" key={stage}><span className="stage-number" aria-hidden="true">0{index + 1}</span>{stage}{index < stages.length - 1 && <span className="stage-connector" aria-hidden="true" />}</span>)}</div>
        <p className="brand-assurance"><Icon name="shield" />{t('tagline')}</p>
      </section>

      <section className="form-panel" aria-labelledby="login-heading">
        <div className="login-card">
          <div className="form-heading"><span className="workspace-label">KEEL WORKSPACE</span><h1 id="login-heading">{t('welcome')}</h1><p>{t('intro')}</p></div>
          <div className={`identity-notice ${providers.isError ? 'notice-error' : ''}`} id="identity-status" role="status" aria-live="polite" aria-busy={providers.isFetching}>
            <Icon name={providers.isError ? 'refresh' : 'shield'} className="notice-icon" />
            <div><strong>{t(statusTitle)}</strong><p>{t(statusDetail)}</p>
              {!pending && <button type="button" className="text-button" onClick={() => void providers.refetch()} disabled={providers.isFetching}><Icon name="refresh" className={providers.isFetching ? 'rotating' : ''} />{t(providers.isFetching ? 'retrying' : 'retry')}</button>}
            </div>
          </div>
          <form id="login-form" aria-describedby="identity-status" onSubmit={(event) => event.preventDefault()}>
            <div className="field"><label htmlFor="username">{t('account')}</label><input id="username" name="username" type="text" autoComplete="username" autoCapitalize="none" spellCheck={false} placeholder={t('accountPlaceholder')} /></div>
            <div className="field"><label htmlFor="password">{t('password')}</label><div className="password-field"><input id="password" name="password" type={showPassword ? 'text' : 'password'} autoComplete="current-password" placeholder={t('passwordPlaceholder')} /><button className="password-toggle" type="button" onClick={() => setShowPassword(!showPassword)} aria-label={t(showPassword ? 'hidePassword' : 'showPassword')} aria-pressed={showPassword} aria-controls="password"><Icon name={showPassword ? 'eyeOff' : 'eye'} /></button></div></div>
            <button className="submit-button" type="submit" disabled aria-describedby="identity-status"><span>{t('signIn')}</span><Icon name="arrow" /></button>
          </form>
          <p className="admin-help">{t('adminHelp')}</p>
          <div className="form-bottom"><Icon name="shield" /><span>{t('footer')}</span></div>
        </div>
      </section>
    </main>
    <footer className="login-footer"><span>keel <span className="footer-dot" aria-hidden="true">·</span> Delivery &amp; Deployment</span><a href="/healthz" target="_blank" rel="noreferrer">{t('health')}<Icon name="external" /></a></footer>
  </div>
}
