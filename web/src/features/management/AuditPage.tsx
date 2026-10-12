import { useEffect, useState } from 'react'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Workspace } from '../../components/Workspace'
import { ProjectWorkspace, canManageProject, useProject } from '../../components/ProjectWorkspace'
import { ActionError, Pagination, PermissionDenied } from '../../components/Management'
import { listAudits } from '../../lib/management'
import { useExpireOnUnauthorized, useSession } from '../../lib/session'
import type { Project } from '../../lib/api'
function AuditContent({ project }: { project?: Project }) {
  const { t, i18n } = useTranslation(), session = useSession(), navigate = useNavigate()
  const search = useSearch({ strict: false }), page = search.page ?? 1, action = search.action ?? ''
  const [draftAction, setDraftAction] = useState(action)
  const allowed = project ? canManageProject(project, session.user.is_admin) && session.permissions.includes('project:audit:read') : session.user.is_admin && session.permissions.includes('audit:read')
  const key = project ? ['private', session.user.id, 'project', project.id, 'audits'] : ['private', session.user.id, 'audits']
  const audits = useQuery({ queryKey: [...key, page, action], queryFn: ({ signal }) => listAudits(project?.id, page, action, signal), enabled: allowed })
  useExpireOnUnauthorized(audits.error)
  useEffect(() => { document.title = `${t('auditTitle')} · ${project?.name ?? 'Keel'}` }, [t, project?.name])
  useEffect(() => setDraftAction(action), [action])
  function update(nextPage: number, nextAction = action) {
    if (project) void navigate({ to: '/p/$projectId/audits', params: { projectId: project.id }, search: { page: nextPage, action: nextAction } })
    else void navigate({ to: '/admin/audits', search: { page: nextPage, action: nextAction } })
  }
  if (!allowed) return <PermissionDenied />
  return <><div className="page-heading"><div><p className="section-eyebrow">{project?.name ?? t('platformAdministration')}</p><h1>{t(project ? 'auditTitle' : 'platformAudit')}</h1><p>{t('auditIntro')}</p></div></div>
    <section className="management-panel" aria-busy={audits.isFetching}><form className="audit-filter-form" onSubmit={(event) => { event.preventDefault(); update(1, draftAction.trim()) }}><div className="field"><label htmlFor="audit-action">{t('auditActionFilter')}</label><input id="audit-action" value={draftAction} onChange={(event) => setDraftAction(event.target.value)} maxLength={128} placeholder="project.create" /><span className="field-hint">{t('auditFilterHint')}</span></div><button className="secondary-button" type="submit">{t('applyFilter')}</button><button className="secondary-button" type="button" onClick={() => { setDraftAction(''); update(1, '') }}>{t('clearFilter')}</button><button className="secondary-button" type="button" disabled={audits.isFetching} onClick={() => void audits.refetch()}>{t('refresh')}</button></form>
      <ActionError error={audits.error} />{audits.isPending ? <p className="table-loading" role="status">{t('loadingRecords')}</p> : audits.data && <><div className="table-scroll" tabIndex={0} role="region" aria-label={t('auditTitle')}><table className="data-table"><thead><tr><th scope="col">{t('eventTime')}</th><th scope="col">{t('actor')}</th><th scope="col">{t('eventAction')}</th><th scope="col">{t('eventTarget')}</th><th scope="col">{t('auditDetails')}</th></tr></thead><tbody>{audits.data.items.map((entry) => <tr key={entry.id}><td>{new Intl.DateTimeFormat(i18n.language === 'en' ? 'en-US' : 'zh-CN', { dateStyle: 'medium', timeStyle: 'medium' }).format(new Date(entry.created_at))}</td><td>{entry.actor_name ?? t('systemActor')}<span className="cell-subtitle mono-value">{entry.actor_id ?? '—'}</span></td><td><code>{entry.action}</code></td><td className="mono-value">{entry.target_id ?? '—'}{entry.project_id && <span className="cell-subtitle">{t('projectId')}: {entry.project_id}</span>}</td><td><details className="audit-details"><summary>{t('viewDetails')}</summary><dl><dt>{t('traceId')}</dt><dd>{entry.trace_id}</dd><dt>{t('auditId')}</dt><dd>{entry.id}</dd>{Object.entries(entry.metadata).filter(([, value]) => value === null || ['string', 'number', 'boolean'].includes(typeof value)).map(([key, value]) => <div key={key}><dt>{key}</dt><dd>{value === null ? '—' : String(value)}</dd></div>)}</dl></details></td></tr>)}</tbody></table>{!audits.data.items.length && <p className="table-loading">{t('noAuditRecords')}</p>}</div><Pagination offset={(page - 1) * audits.data.page_size} limit={audits.data.page_size} total={audits.data.total} busy={audits.isFetching} onChange={(offset) => update(offset / 20 + 1)} /></>}
    </section>
  </>
}
function ProjectAudits() { const project = useProject(); return <AuditContent project={project} /> }
export function AuditPage({ projectId }: { projectId?: string }) { return projectId ? <ProjectWorkspace projectId={projectId}><ProjectAudits /></ProjectWorkspace> : <Workspace><AuditContent /></Workspace> }
