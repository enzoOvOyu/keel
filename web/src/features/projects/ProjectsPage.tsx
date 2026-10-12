import { useEffect, useRef, useState, type FormEvent } from 'react'
import { Link, useNavigate, useSearch } from '@tanstack/react-router'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Workspace } from '../../components/Workspace'
import { Pagination } from '../../components/Management'
import { Icon } from '../../components/Icon'
import { ApiError, createProject, listProjects, type CreateProject, type Project, type Session } from '../../lib/api'
import { sessionKey, useExpireOnUnauthorized, useSession } from '../../lib/session'

function ProjectForm({ onClose, onCreated }: { onClose: () => void; onCreated: (project: Project) => Promise<void> }) {
  const session = useSession()
  const { t } = useTranslation()
  const [name, setName] = useState('')
  const [slug, setSlug] = useState('')
  const [description, setDescription] = useState('')
  const [invalid, setInvalid] = useState<'name' | 'slug' | null>(null)
  const [error, setError] = useState<ApiError | null>(null)
  const [busy, setBusy] = useState(false)
  const busyRef = useRef(false)
  const attempt = useRef<{ payload: string; key: string } | null>(null)
  useExpireOnUnauthorized(error)
  useEffect(() => { document.getElementById('project-name')?.focus() }, [])
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (busyRef.current) return
    const badField = !name.trim() ? 'name' : !/^[a-z0-9](?:[a-z0-9-]{0,62}[a-z0-9])?$/.test(slug) ? 'slug' : null
    if (badField) { setInvalid(badField); document.getElementById(`project-${badField}`)?.focus(); return }
    const body: CreateProject = { name: name.trim(), slug, description: description.trim() }
    const payload = JSON.stringify(body)
    if (!attempt.current || attempt.current.payload !== payload) attempt.current = { payload, key: crypto.randomUUID() }
    busyRef.current = true; setBusy(true); setError(null); setInvalid(null)
    try { const project = await createProject(body, session.csrf_token, attempt.current.key); await onCreated(project) }
    catch (cause) { setError(cause instanceof ApiError ? cause : new ApiError(0, 'NETWORK_ERROR')); window.requestAnimationFrame(() => document.getElementById('create-error')?.focus()) }
    finally { busyRef.current = false; setBusy(false) }
  }
  const errorKey = error?.code === 'PROJECT_SLUG_EXISTS' ? 'slugExists' : error?.status === 403 ? 'createForbidden'
    : error?.status === 409 ? 'createConflict' : error?.status === 400 ? 'createInvalid' : 'createUnavailable'
  return <section className="project-create" aria-labelledby="create-heading">
    <div className="section-eyebrow">{t('newProjectLabel')}</div><h2 id="create-heading">{t('newProject')}</h2><p className="section-description">{t('newProjectDetail')}</p>
    {error && <div id="create-error" className="form-error" role="alert" tabIndex={-1}><strong>{t(errorKey)}</strong>{error.traceId && <small>{t('traceId')}: {error.traceId}</small>}</div>}
    <form onSubmit={(event) => void submit(event)} noValidate aria-busy={busy}>
      <div className="field"><label htmlFor="project-name">{t('projectName')}</label><input id="project-name" value={name} maxLength={120} disabled={busy} onChange={(event) => { setName(event.target.value); setInvalid(null) }} placeholder={t('projectNamePlaceholder')} autoComplete="off" aria-invalid={invalid === 'name'} aria-describedby={invalid === 'name' ? 'project-name-error' : undefined} />{invalid === 'name' && <span id="project-name-error" className="field-error">{t('nameRequired')}</span>}</div>
      <div className="field"><label htmlFor="project-slug">{t('projectSlug')}</label><input id="project-slug" value={slug} maxLength={64} disabled={busy} onChange={(event) => { setSlug(event.target.value); setInvalid(null) }} placeholder="cluster-delivery" autoComplete="off" autoCapitalize="none" spellCheck={false} aria-invalid={invalid === 'slug'} aria-describedby={invalid === 'slug' ? 'project-slug-error' : 'project-slug-help'} /><span id="project-slug-help" className="field-hint">{t('slugHint')}</span>{invalid === 'slug' && <span id="project-slug-error" className="field-error">{t('slugInvalid')}</span>}</div>
      <div className="field"><label htmlFor="project-description">{t('projectDescription')} <span className="optional-label">{t('optional')}</span></label><textarea id="project-description" value={description} maxLength={2000} rows={4} disabled={busy} onChange={(event) => setDescription(event.target.value)} placeholder={t('descriptionPlaceholder')} /></div>
      <div className="form-actions"><button className="secondary-button" type="button" disabled={busy} onClick={onClose}>{t('cancel')}</button><button className="primary-button" type="submit" disabled={busy}>{t(busy ? 'creating' : 'createProject')}{busy ? <Icon name="refresh" className="rotating" /> : <Icon name="arrow" />}</button></div>
    </form>
  </section>
}
function ProjectsContent() {
  const { t } = useTranslation()
  const session = useSession()
  const client = useQueryClient()
  const navigate = useNavigate()
  const { page: requestedPage } = useSearch({ from: '/projects' })
  const page = requestedPage ?? 1
  const [showForm, setShowForm] = useState(false)
  const projects = useQuery({ queryKey: ['private', session.user.id, 'projects', page], queryFn: ({ signal }) => listProjects(signal, page), staleTime: 0 })
  useExpireOnUnauthorized(projects.error)
  const canCreate = session.user.is_admin && session.permissions.includes('project:create')
  useEffect(() => { document.title = `${t('projectsTitle')} · Keel` }, [t])
  async function created(project: Project) {
    client.setQueryData<Session>(sessionKey, (old) => old ? { ...old, projects: [...old.projects.filter((item) => item.id !== project.id), project] } : old)
    await client.invalidateQueries({ queryKey: ['private', session.user.id, 'projects'] })
    await navigate({ to: '/p/$projectId', params: { projectId: project.id } })
  }
  function closeForm() { setShowForm(false); window.requestAnimationFrame(() => document.getElementById('new-project-button')?.focus()) }
  return <>
    <div className="page-heading"><div><p className="section-eyebrow">{t('workspaceLabel')}</p><h1>{t('projectsTitle')}</h1><p>{t('projectsIntro')}</p></div>{canCreate && !showForm && <button className="primary-button" id="new-project-button" onClick={() => setShowForm(true)}>{t('newProject')}<Icon name="arrow" /></button>}</div>
    <div className={`projects-layout ${showForm ? 'with-form' : ''}`}>
      <section className="projects-region" aria-label={t('projectsTitle')} aria-busy={projects.isFetching}>
        <div className="list-heading"><h2>{t('yourProjects')}{projects.data && <span className="count-label">{projects.data.total}</span>}</h2><button className="icon-button" aria-label={t('refreshProjects')} disabled={projects.isFetching} onClick={() => void projects.refetch()}><Icon name="refresh" className={projects.isFetching ? 'rotating' : ''} /></button></div>
        {projects.isPending ? <div className="empty-state" role="status">{t('loadingProjects')}</div> : projects.isError ? <div className="empty-state" role="alert"><Icon name="refresh" /><h3>{t('projectsUnavailable')}</h3><p>{t('tryAgainDetail')}</p><button className="secondary-button" onClick={() => void projects.refetch()}>{t('retry')}</button></div> : projects.data.items.length === 0 ? <div className="empty-state"><div className="empty-icon"><Icon name="shield" /></div><h3>{t('noProjects')}</h3><p>{t(canCreate ? 'noProjectsAdmin' : 'noProjectsMember')}</p></div> : <div className="project-list">{projects.data.items.map((project) => <Link className="project-card" to="/p/$projectId" params={{ projectId: project.id }} key={project.id}>
          <div className="project-card-top"><span className="project-monogram" aria-hidden="true">{project.name.slice(0, 1).toUpperCase()}</span><span className="state-badge">{t(`state_${project.state}`)}</span></div>
          <h3>{project.name}</h3><p className="project-slug">{project.slug}</p><p className="project-description">{project.description || t('noDescription')}</p>
          <div className="project-card-bottom"><span className="role-label">{project.role}</span><span>{t('openProject')}<Icon name="arrow" /></span></div>
        </Link>)}</div>}
      {projects.data && <Pagination offset={(page - 1) * projects.data.page_size} limit={projects.data.page_size} total={projects.data.total} busy={projects.isFetching} onChange={(offset) => void navigate({ to: '/projects', search: { page: offset / 20 + 1 } })} />}
      </section>
      {showForm && canCreate && <ProjectForm onClose={closeForm} onCreated={created} />}
    </div>
  </>
}
export function ProjectsPage() { return <Workspace><ProjectsContent /></Workspace> }
