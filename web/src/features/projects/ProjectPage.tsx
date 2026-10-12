import { useEffect } from 'react'
import { useParams } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { ProjectWorkspace, useProject } from '../../components/ProjectWorkspace'
import { Icon } from '../../components/Icon'
function ProjectContent() {
  const { t, i18n } = useTranslation()
  const data = useProject()
  useEffect(() => { document.title = `${data.name} · Keel` }, [data.name])
  const date = (value: string) => new Intl.DateTimeFormat(i18n.language === 'en' ? 'en-US' : 'zh-CN', { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value))
  return <><div className="page-heading project-page-heading"><div><p className="section-eyebrow">{t('projectOverview')}</p><h1>{data.name}</h1><p>{data.description || t('noDescription')}</p></div><span className="state-badge large-badge">{t(`state_${data.state}`)}</span></div>
    <div className="project-detail-layout"><section className="detail-card"><h2>{t('projectDetails')}</h2><dl className="project-facts"><div><dt>{t('projectSlug')}</dt><dd className="mono-value">{data.slug}</dd></div><div><dt>{t('yourRole')}</dt><dd><span className="role-label">{data.role}</span></dd></div><div><dt>{t('createdAt')}</dt><dd>{date(data.created_at)}</dd></div><div><dt>{t('updatedAt')}</dt><dd>{date(data.updated_at)}</dd></div><div><dt>{t('recordVersion')}</dt><dd>{data.version}</dd></div><div><dt>{t('projectId')}</dt><dd className="mono-value">{data.id}</dd></div></dl></section>
    <section className="project-readiness"><div className="empty-icon"><Icon name="shield" /></div><p className="section-eyebrow">{t('planningLabel')}</p><h2>{t('planningTitle')}</h2><p>{t('planningDetail')}</p><div className="access-note"><Icon name="shield" /><span>{t('projectAccessNote')}</span></div></section></div>
  </>
}
export function ProjectPage() { const { projectId } = useParams({ from: '/p/$projectId' }); return <ProjectWorkspace projectId={projectId}><ProjectContent /></ProjectWorkspace> }
