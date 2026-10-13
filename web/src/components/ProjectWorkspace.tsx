import { createContext, useContext, useEffect, type ReactNode } from 'react'
import { Link } from '@tanstack/react-router'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { ApiError, getProject, type Project } from '../lib/api'
import { useExpireOnUnauthorized, useSession } from '../lib/session'
import { Workspace } from './Workspace'
const projectConsumers = new Map<string, Set<symbol>>()
const ProjectContext = createContext<Project | null>(null)
export function useProject() { const project = useContext(ProjectContext); if (!project) throw new Error('Project scope is required'); return project }
export function canManageProject(project: Project, isPlatformAdmin: boolean) { return isPlatformAdmin || project.role === 'Admin' }
function ProjectScope({ projectId, children }: { projectId: string; children: ReactNode }) {
  const session = useSession(), client = useQueryClient()
  const { t } = useTranslation()
  const project = useQuery({ queryKey: ['private', session.user.id, 'project', projectId], queryFn: ({ signal }) => getProject(projectId, signal), staleTime: 0 })
  useExpireOnUnauthorized(project.error)
  useEffect(() => {
    const scope = `${session.user.id}:${projectId}`, consumer = Symbol(scope)
    const consumers = projectConsumers.get(scope) ?? new Set<symbol>()
    consumers.add(consumer); projectConsumers.set(scope, consumers)
    return () => {
      consumers.delete(consumer)
      // A tab within the same project or StrictMode may reattach immediately.
      // Clear only after the final consumer of this project has left.
      queueMicrotask(() => {
        if (consumers.size) return
        projectConsumers.delete(scope)
        const queryKey = ['private', session.user.id, 'project', projectId]
        void client.cancelQueries({ queryKey }); client.removeQueries({ queryKey })
      })
    }
  }, [projectId, session.user.id, client])
  const back = <Link className="back-link" to="/projects"><span aria-hidden="true">←</span>{t('allProjects')}</Link>
  if (project.isPending) return <>{back}<div className="empty-state" role="status">{t('loadingProjects')}</div></>
  if (project.isError) return <>{back}<div className="empty-state" role="alert"><h1>{t(project.error instanceof ApiError && project.error.status === 404 ? 'projectNotFound' : 'projectsUnavailable')}</h1><p>{t('projectNotFoundDetail')}</p><button className="secondary-button" onClick={() => void project.refetch()}>{t('retry')}</button></div></>
  const manage = canManageProject(project.data, session.user.is_admin)
  return <ProjectContext.Provider value={project.data} key={projectId}>{back}
    <nav className="project-tabs" aria-label={t('projectNavigation')}>
      <Link to="/p/$projectId" params={{ projectId }} activeOptions={{ exact: true }}>{t('overviewTab')}</Link>
      {manage && session.permissions.includes('project:members:read') && <Link to="/p/$projectId/members" params={{ projectId }}>{t('membersTitle')}</Link>}
      {manage && session.permissions.includes('project:audit:read') && <Link to="/p/$projectId/audits" params={{ projectId }}>{t('auditTitle')}</Link>}
      {session.permissions.includes("execution:read") && <Link to="/p/$projectId/executions" params={{ projectId }}>{t("executionsTab")}</Link>}
      {session.permissions.includes("model:read") && <Link to="/p/$projectId/models" params={{ projectId }}>{t("modelsTab")}</Link>}
      {session.permissions.includes("model:read") && <Link to="/p/$projectId/dashboard" params={{ projectId }}>{t("dashboardTitle")}</Link>}
      {session.permissions.includes("model:read") && <Link to="/p/$projectId/media" params={{ projectId }}>{t("mediaTab")}</Link>}
      {session.permissions.includes("model:read") && <Link to="/p/$projectId/assets" params={{ projectId }}>{t("bomTab")}</Link>}
      {session.permissions.includes("model:read") && <Link to="/p/$projectId/config" params={{ projectId }}>{t("configTab")}</Link>}
      {session.permissions.includes("model:read") && <Link to="/p/$projectId/nettest" params={{ projectId }}>{t("nettestTab")}</Link>}
    </nav>{children}
  </ProjectContext.Provider>
}
export function ProjectWorkspace({ projectId, children }: { projectId: string; children: ReactNode }) {
  return <Workspace><ProjectScope projectId={projectId}>{children}</ProjectScope></Workspace>
}
