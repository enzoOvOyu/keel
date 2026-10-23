import { useEffect, useRef, useState, type ReactNode } from 'react'
import { Link, useLocation } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Brand } from './Brand'
import { Preferences } from './Preferences'
import { Icon } from './Icon'
import { ProjectSwitcher, WorkspaceNavigation } from './WorkspaceNavigation'
import { ApiError, getAuthProviders, logout } from '../lib/api'
import { SessionBoundary, useEndSession, useSession } from '../lib/session'
function WorkspaceContent({ children, projectId }: { children: ReactNode; projectId?: string }) {
  const session = useSession(), endSession = useEndSession(), { t, i18n } = useTranslation()
  const [busy, setBusy] = useState(false), [error, setError] = useState(false), [collapsed, setCollapsed] = useState(false)
  const busyRef = useRef(false)
  const { pathname } = useLocation()
  const domainPage = !!projectId && ![ `/p/${projectId}`, `/p/${projectId}/members`, `/p/${projectId}/audits` ].includes(pathname)
  useEffect(() => {
    const media = window.matchMedia('(max-width: 767px)')
    const resize = () => setCollapsed(media.matches)
    resize(); media.addEventListener('change', resize)
    return () => media.removeEventListener('change', resize)
  }, [])
  const providers = useQuery({ queryKey: ['public', 'auth', 'providers'], queryFn: ({ signal }) => getAuthProviders(signal), staleTime: 30_000 })
  useEffect(() => { document.documentElement.lang = i18n.language === 'en' ? 'en' : 'zh-CN' }, [i18n.language])
  async function signOut() {
    if (busyRef.current) return
    busyRef.current = true; setBusy(true); setError(false)
    try { await logout(session.csrf_token); await endSession(false) }
    catch (cause) { if (cause instanceof ApiError && cause.status === 401) await endSession(false); else setError(true) }
    finally { busyRef.current = false; setBusy(false) }
  }
  return <div className={`workspace-shell${collapsed ? ' sidebar-collapsed' : ''}${domainPage ? ' project-workspace' : ' basic-workspace'}`}>
    <a className="skip-link" href="#workspace-content">{t('skipContent')}</a>
    <header className="workspace-header">
      <div className="workspace-brand-area"><button className="icon-button sidebar-toggle" aria-label={t(collapsed ? 'expandNavigation' : 'collapseNavigation')} aria-expanded={!collapsed} aria-controls="workspace-navigation" onClick={() => setCollapsed(value => !value)}><Icon name="menu" /></button><Link to="/projects" className="brand-link" aria-label="Keel"><Brand /></Link></div>
      {providers.data && <span className={`environment-badge environment-${providers.data.environment}`}>{t(`environment_${providers.data.environment}`)}</span>}
      <div className="workspace-tools"><ProjectSwitcher projectId={projectId} /><Preferences /><span className="user-name">{session.user.display_name || session.user.username}</span><button className="secondary-button logout-button" aria-label={t(busy ? 'signingOut' : 'signOut')} onClick={() => void signOut()} disabled={busy} title={t(busy ? 'signingOut' : 'signOut')}><Icon name="external" /><span>{t(busy ? 'signingOut' : 'signOut')}</span></button></div>
    </header>
    <aside className="workspace-sidebar"><WorkspaceNavigation projectId={projectId} /></aside>
    <div className="workspace-body">
      {error && <div className="workspace-banner" role="alert">{t('logoutError')}</div>}
      <main id="workspace-content" className="workspace-content">{children}</main>
      <footer className="workspace-footer"><span>Keel <span aria-hidden="true">·</span> Knowledge-led Execution, Enforcement &amp; Lifecycle</span><span><Icon name="shield" />{t('authorizedWorkspace')}</span></footer>
    </div>
  </div>
}
export function Workspace({ children, projectId }: { children: ReactNode; projectId?: string }) {
  return <SessionBoundary><WorkspaceContent projectId={projectId}>{children}</WorkspaceContent></SessionBoundary>
}
