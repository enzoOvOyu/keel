import { useEffect } from 'react'
import { Link, useParams } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Workspace } from '../../components/Workspace'
import { Icon } from '../../components/Icon'
import { ApiError, getProject } from '../../lib/api'
import { useExpireOnUnauthorized, useSession } from '../../lib/session'
function ProjectContent() {
  const { projectId } = useParams({ from: '/p/$projectId' })
  const { t, i18n } = useTranslation()
  const session = useSession()
  const project = useQuery({ queryKey: ['private', session.user.id, 'project', projectId], queryFn: ({ signal }) => getProject(projectId, signal), staleTime: 0 })
  useExpireOnUnauthorized(project.error)
  useEffect(() => { document.title = `${project.data?.name ?? t('projectOverview')} · Keel` }, [project.data?.name, t])
  const back = <Link className="back-link" to="/projects"><span aria-hidden="true">←</span>{t('allProjects')}</Link>
  if (project.isPending) return <>{back}<div className="empty-state" role="status">{t('loadingProjects')}</div></>
  if (project.isError) return <>{back}<div className="empty-state" role="alert"><Icon name="shield" /><h1>{t(project.error instanceof ApiError && project.error.status === 404 ? 'projectNotFound' : 'projectsUnavailable')}</h1><p>{t(project.error instanceof ApiError && project.error.status === 404 ? 'projectNotFoundDetail' : 'tryAgainDetail')}</p><button className="secondary-button" onClick={() => void project.refetch()}>{t('retry')}</button></div></>
  const data = project.data
  const date = (value: string) => new Intl.DateTimeFormat(i18n.language === 'en' ? 'en-US' : 'zh-CN', { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value))
  return <>{back}<div className="page-heading project-page-heading"><div><p className="section-eyebrow">{t('projectOverview')}</p><h1>{data.name}</h1><p>{data.description || t('noDescription')}</p></div><span className="state-badge large-badge">{t(`state_${data.state}`)}</span></div>
    <div className="project-detail-layout"><section className="detail-card"><h2>{t('projectDetails')}</h2><dl className="project-facts"><div><dt>{t('projectSlug')}</dt><dd className="mono-value">{data.slug}</dd></div><div><dt>{t('yourRole')}</dt><dd><span className="role-label">{data.role}</span></dd></div><div><dt>{t('createdAt')}</dt><dd>{date(data.created_at)}</dd></div><div><dt>{t('updatedAt')}</dt><dd>{date(data.updated_at)}</dd></div><div><dt>{t('recordVersion')}</dt><dd>{data.version}</dd></div><div><dt>{t('projectId')}</dt><dd className="mono-value">{data.id}</dd></div></dl></section>
    <section className="project-readiness"><div className="empty-icon"><Icon name="shield" /></div><p className="section-eyebrow">{t('planningLabel')}</p><h2>{t('planningTitle')}</h2><p>{t('planningDetail')}</p><div className="access-note"><Icon name="shield" /><span>{t('projectAccessNote')}</span></div></section></div>
  </>
}
export function ProjectPage() { return <Workspace><ProjectContent /></Workspace> }
