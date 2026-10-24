import { Link, useLocation, useNavigate } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import type { Project, Session } from '../lib/api'
import { useSession } from '../lib/session'
import { Icon, type IconName } from './Icon'

type ProjectPage = '/p/$projectId/delivery' | '/p/$projectId/flows' | '/p/$projectId/health' | '/p/$projectId/configuration' | '/p/$projectId/fabric' | '/p/$projectId' | '/p/$projectId/members' | '/p/$projectId/audits' | '/p/$projectId/assets' | '/p/$projectId/ztp' | '/p/$projectId/executions' | '/p/$projectId/models' | '/p/$projectId/media' | '/p/$projectId/params'
type Item = { to: ProjectPage; label: string; icon: IconName; permission?: string; admin?: boolean; roles?: string[] }
const deployment: Item[] = [
  { to: '/p/$projectId', label: 'overviewTab', icon: 'overview' },
  { to: '/p/$projectId/assets', label: 'bomTab', icon: 'box', permission: 'model:read' },
]
const operations: Item[] = [
  { to: '/p/$projectId/delivery', label: 'networkDeliveryTitle', icon: 'archive', permission: 'fabric:read' },
 {to:'/p/$projectId/flows',label:'flowWorkbenchTitle',icon:'play',permission:'execution:read'},
 {to:'/p/$projectId/health',label:'networkHealthTitle',icon:'network',permission:'fabric:read'},
  { to: '/p/$projectId/configuration', label: 'networkConfiguration', icon: 'layers', permission: 'fabric:read' },
  { to: '/p/$projectId/fabric', label: 'fabricTab', icon: 'network', permission: 'fabric:read' },
  { to: '/p/$projectId/ztp', label: 'ztpTab', icon: 'network', permission: 'model:read' },
  { to: '/p/$projectId/executions', label: 'executionsTab', icon: 'play', permission: 'execution:read' },
  { to: '/p/$projectId/models', label: 'modelsTab', icon: 'layers', permission: 'model:read' },
  { to: '/p/$projectId/media', label: 'mediaTab', icon: 'archive', permission: 'model:read' },
]
const administration: Item[] = [
  { to: '/p/$projectId/params', label: 'catalogTitle', icon: 'layers', permission: 'model:read', roles: ['Admin', 'DL'] },
  { to: '/p/$projectId/members', label: 'membersTitle', icon: 'users', permission: 'project:members:read', admin: true },
  { to: '/p/$projectId/audits', label: 'auditTitle', icon: 'audit', permission: 'project:audit:read', admin: true },
]
function allowed(item: Item, project: Project, session: Session) {
  return (!item.permission || session.permissions.includes(item.permission)) && (!item.admin || session.user.is_admin || project.role === 'Admin') && (!item.roles || session.user.is_admin || item.roles.includes(project.role))
}
function switchDestination(pathname: string, project: Project, session: Session): ProjectPage {
  const leaf = pathname.replace(/^\/p\/[^/]+/, '/p/$projectId')
  return [...deployment, ...operations, ...administration].find(item => item.to === leaf && allowed(item, project, session))?.to ?? '/p/$projectId'
}
// Both project-switch entry points navigate this tab's URL. No global active-project
// state or storage can retarget a different tab, open confirmation or request.
export function ProjectSwitcher({ projectId }: { projectId?: string }) {
  const session = useSession(), navigate = useNavigate(), { pathname } = useLocation(), { t } = useTranslation()
  return <label className="workspace-project-switcher"><span className="sr-only">{t('switchProject')}</span><Icon name="folder" />
    <select aria-label={t('switchProject')} value={session.projects.some(project => project.id === projectId) ? projectId : ''} onChange={event => {
      const project = session.projects.find(item => item.id === event.target.value)
      if (project) void navigate({ to: switchDestination(pathname, project, session), params: { projectId: project.id } })
    }}>
      <option value="" disabled>{t(session.projects.length ? 'selectProject' : 'noProjects')}</option>
      {session.projects.map(project => <option value={project.id} key={project.id}>{project.name}</option>)}
    </select>
  </label>
}
export function WorkspaceNavigation({ projectId }: { projectId?: string }) {
  const session = useSession(), { pathname } = useLocation(), { t } = useTranslation()
  const project = session.projects.find(item => item.id === projectId)
  function links(items: Item[]) {
    return project && items.filter(item => allowed(item, project, session)).map(item => <Link key={item.to} to={item.to} params={{ projectId: project!.id }} activeOptions={{ exact: true }} title={t(item.label)} aria-label={t(item.label)}>
      <Icon name={item.icon} /><span className="nav-label">{t(item.label)}</span>
    </Link>)
  }
  return <nav id="workspace-navigation" className="workspace-sidebar-nav" aria-label={t('workspaceNavigation')}>
    <section aria-label={t('deploymentPlane')}><h2 className="nav-section-heading"><span>{t('deploymentPlane')}</span></h2>
      <Link to="/projects" activeOptions={{ exact: true }} title={t('allProjects')} aria-label={t('allProjects')}><Icon name="folder" /><span className="nav-label">{t('allProjects')}</span></Link>
      <div className="sidebar-project-list" aria-label={t('projectListNavigation')}>
        {session.projects.map(item => <Link key={item.id} to={switchDestination(pathname, item, session)} params={{ projectId: item.id }} className={projectId === item.id ? 'current-project' : ''} title={item.name} aria-label={`${t('openProject')}: ${item.name}`}>
          <span className="project-nav-mark" aria-hidden="true">{item.name.slice(0, 1).toUpperCase()}</span><span className="nav-label">{item.name}</span>
        </Link>)}
      </div>
      {links(deployment)}
    </section>
    <section aria-label={t('operationsPlane')}><h2 className="nav-section-heading"><span>{t('operationsPlane')}</span></h2>
      {project ? <><p className="nav-scope-label">{t('projectFilter')}: {project.name}</p>{links(operations)}</> : <p className="nav-scope-label">{t('selectProjectForTools')}</p>}
    </section>
    <section aria-label={t('systemNavigation')}><h2 className="nav-section-heading"><span>{t('systemNavigation')}</span></h2>
      {links(administration)}
      {session.user.is_admin && session.permissions.includes('user:read') && <Link to="/admin/users" title={t('usersTitle')} aria-label={t('usersTitle')}><Icon name="users" /><span className="nav-label">{t('usersTitle')}</span></Link>}
      {session.user.is_admin && session.permissions.includes('audit:read') && <Link to="/admin/audits" title={t('platformAudit')} aria-label={t('platformAudit')}><Icon name="audit" /><span className="nav-label">{t('platformAudit')}</span></Link>}
      <Link to="/account/security" title={t('accountSecurity')} aria-label={t('accountSecurity')}><Icon name="shield" /><span className="nav-label">{t('accountSecurity')}</span></Link>
    </section>
  </nav>
}
