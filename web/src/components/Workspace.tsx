import { useEffect, useRef, useState, type ReactNode } from 'react'
import { Link } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { Brand } from './Brand'
import { Preferences } from './Preferences'
import { Icon } from './Icon'
import { ApiError, logout } from '../lib/api'
import { SessionBoundary, useEndSession, useSession } from '../lib/session'
function WorkspaceContent({ children }: { children: ReactNode }) {
  const session = useSession()
  const endSession = useEndSession()
  const { t, i18n } = useTranslation()
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState(false)
  const busyRef = useRef(false)
  useEffect(() => { document.documentElement.lang = i18n.language === 'en' ? 'en' : 'zh-CN' }, [i18n.language])
  async function signOut() {
    if (busyRef.current) return
    busyRef.current = true; setBusy(true); setError(false)
    try { await logout(session.csrf_token); await endSession(false) }
    catch (cause) { if (cause instanceof ApiError && cause.status === 401) await endSession(false); else setError(true) }
    finally { busyRef.current = false; setBusy(false) }
  }
  return <div className="workspace-shell">
    <a className="skip-link" href="#workspace-content">{t('skipContent')}</a>
    <header className="workspace-header"><Link to="/projects" className="brand-link" aria-label={t('allProjects')}><Brand /></Link><div className="workspace-tools"><Preferences /><span className="user-name">{session.user.display_name || session.user.username}</span><button className="secondary-button logout-button" onClick={() => void signOut()} disabled={busy}>{t(busy ? 'signingOut' : 'signOut')}</button></div></header>
    <nav className="workspace-nav" aria-label={t('workspaceNavigation')}>
      <Link to="/projects">{t('projectsTitle')}</Link>
      {session.user.is_admin && session.permissions.includes('user:read') && <Link to="/admin/users">{t('usersTitle')}</Link>}
      {session.user.is_admin && session.permissions.includes('audit:read') && <Link to="/admin/audits">{t('platformAudit')}</Link>}
      <Link to="/account/security" className="account-link">{t('accountSecurity')}</Link>
    </nav>
    {error && <div className="workspace-banner" role="alert">{t('logoutError')}</div>}
    <main id="workspace-content" className="workspace-content">{children}</main>
    <footer className="workspace-footer"><span>Keel <span aria-hidden="true">·</span> Knowledge-led Execution, Enforcement &amp; Lifecycle</span><span><Icon name="shield" />{t('authorizedWorkspace')}</span></footer>
  </div>
}
export function Workspace({ children }: { children: ReactNode }) {
  return <SessionBoundary><WorkspaceContent>{children}</WorkspaceContent></SessionBoundary>
}
