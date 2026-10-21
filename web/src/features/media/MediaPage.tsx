import { useEffect, useRef, useState, type FormEvent } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { ProjectWorkspace, canManageProject, useProject } from '../../components/ProjectWorkspace'
import { ActionDialog, ActionError, Pagination, PermissionDenied, SuccessNotice } from '../../components/Management'
import { useAction } from '../../lib/management'
import { listMedia, registerMedia, transitionMedia, type MediaItemSummary } from '../../lib/media'
import { useExpireOnUnauthorized, useSession } from '../../lib/session'

const types = ['driver', 'firmware', 'os', 'nccl', 'tool', 'config', 'ztp_script'] as const
const nextStates: Record<string, string[]> = { uploaded: ['verified', 'retired'], verified: ['approved', 'retired'], approved: ['retired'], retired: [] }

function useIdempotencyKey() {
  const ref = useRef<{ payload: string; key: string } | null>(null)
  return (payload: string) => {
    if (!ref.current || ref.current.payload !== payload) ref.current = { payload, key: crypto.randomUUID() }
    return ref.current.key
  }
}
function RegisterMediaDialog({ projectId, onClose, onDone }: { projectId: string; onClose: () => void; onDone: () => Promise<void> }) {
  const { t } = useTranslation(), session = useSession(), action = useAction(), keyFor = useIdempotencyKey()
  const [type, setType] = useState<'driver'>('driver'), [name, setName] = useState(''), [version, setVersion] = useState('')
  const [sha256, setSha256] = useState(''), [size, setSize] = useState('0'), [arch, setArch] = useState<'amd64' | 'arm64' | 'noarch'>('amd64')
  const [osScope, setOsScope] = useState(''), [hwScope, setHwScope] = useState(''), [manifest, setManifest] = useState(''), [validation, setValidation] = useState('')
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const trimmedName = name.trim(), trimmedVersion = version.trim()
    if (!trimmedName || trimmedName.length > 120 || !trimmedVersion || trimmedVersion.length > 120) { setValidation(t('invalidFields')); return }
    if (!/^[a-f0-9]{64}$/.test(sha256)) { setValidation(t('mediaSha256') + ': 64'); return }
    const bytes = Number(size)
    if (!Number.isSafeInteger(bytes) || bytes < 0) { setValidation(t('mediaSize')); return }
    setValidation('')
    const body = { type, name: trimmedName, version: trimmedVersion, sha256, size: bytes, arch, os_scope: osScope.trim(), hw_scope: hwScope.trim(), manifest_ref: manifest.trim() }
    const result = await action.run(() => registerMedia(projectId, body, session.csrf_token, keyFor(JSON.stringify(body))))
    if (result.ok) await onDone()
  }
  return <ActionDialog title={t('mediaCreate')} description={t('mediaCreateDetail')} busy={action.busy} confirm={t(action.busy ? 'saving' : 'mediaCreate')} onSubmit={(event) => void submit(event)} onClose={onClose}>
    <ActionError error={action.error} />{validation && <p className="field-error" role="alert">{validation}</p>}
    <div className="field"><label htmlFor="media-type">{t('mediaType')}</label><select id="media-type" value={type} onChange={(event) => setType(event.target.value as typeof type)} disabled={action.busy}>{types.map((value) => <option key={value} value={value}>{t('media_type_' + value)}</option>)}</select></div>
    <div className="field"><label htmlFor="media-name">{t('mediaName')}</label><input id="media-name" value={name} onChange={(event) => setName(event.target.value)} maxLength={120} required disabled={action.busy} /></div>
    <div className="field"><label htmlFor="media-version">{t('mediaVersion')}</label><input id="media-version" value={version} onChange={(event) => setVersion(event.target.value)} maxLength={120} required disabled={action.busy} /></div>
    <div className="field"><label htmlFor="media-sha">{t('mediaSha256')}</label><input id="media-sha" value={sha256} onChange={(event) => setSha256(event.target.value)} maxLength={64} required disabled={action.busy} className="mono-value" /></div>
    <div className="field"><label htmlFor="media-size">{t('mediaSize')}</label><input id="media-size" type="number" min={0} value={size} onChange={(event) => setSize(event.target.value)} required disabled={action.busy} /></div>
    <div className="field"><label htmlFor="media-arch">{t('mediaArch')}</label><select id="media-arch" value={arch} onChange={(event) => setArch(event.target.value as typeof arch)} disabled={action.busy}><option value="amd64">amd64</option><option value="arm64">arm64</option><option value="noarch">noarch</option></select><span className="field-hint">{t('mediaArchHint')}</span></div>
    <div className="field"><label htmlFor="media-os">{t('mediaOsScope')}</label><input id="media-os" value={osScope} onChange={(event) => setOsScope(event.target.value)} maxLength={120} disabled={action.busy} /></div>
    <div className="field"><label htmlFor="media-hw">{t('mediaHwScope')}</label><input id="media-hw" value={hwScope} onChange={(event) => setHwScope(event.target.value)} maxLength={120} disabled={action.busy} /></div>
    <div className="field"><label htmlFor="media-manifest">{t('mediaManifest')}</label><input id="media-manifest" value={manifest} onChange={(event) => setManifest(event.target.value)} maxLength={200} disabled={action.busy} className="mono-value" /><span className="field-hint">{t('mediaManifestHint')}</span></div>
  </ActionDialog>
}
function TransitionDialog({ projectId, item, target, onClose, onDone }: { projectId: string; item: MediaItemSummary; target: string; onClose: () => void; onDone: () => Promise<void> }) {
  const { t } = useTranslation(), session = useSession(), action = useAction()
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const result = await action.run(() => transitionMedia(projectId, item.id, target, session.csrf_token))
    if (result.ok) await onDone()
  }
  return <ActionDialog title={t('mediaNext' + target.charAt(0).toUpperCase() + target.slice(1))} description={t('mediaTransitionDetail')} busy={action.busy} confirm={t('confirmChange')} danger={target === 'retired'} onSubmit={(event) => void submit(event)} onClose={onClose}>
    <ActionError error={action.error} />
    <dl className="action-target"><dt>{t('mediaName')}</dt><dd>{item.name} · {item.version}</dd><dt>{t('mediaState')}</dt><dd>{t('media_state_' + item.state)}</dd></dl>
    {target === 'retired' && <p className="impact-notice">{t('mediaRetireImpact')}</p>}
    {target === 'approved' && <p className="impact-notice">{t('mediaSelfApproval')}</p>}
    {target === 'verified' && <p className="impact-notice">{t('mediaManifestMissing')}</p>}
  </ActionDialog>
}
function MediaContent() {
  const project = useProject()
  const { t, i18n } = useTranslation(), session = useSession(), client = useQueryClient()
  const [page, setPage] = useState(1), [create, setCreate] = useState(false), [transition, setTransition] = useState<{ item: MediaItemSummary; target: string } | null>(null), [success, setSuccess] = useState('')
  const allowed = session.permissions.includes('model:read')
  const manage = canManageProject(project, session.user.is_admin) && session.permissions.includes('model:manage')
  const media = useQuery({ queryKey: ['private', session.user.id, 'project', project.id, 'media', page], queryFn: ({ signal }) => listMedia(project.id, page, signal), enabled: allowed })
  useExpireOnUnauthorized(media.error)
  useEffect(() => { document.title = `${t('mediaTitle')} · ${project.name}` }, [t, project.name])
  async function done(message: string) {
    setCreate(false); setTransition(null); setSuccess(message)
    await client.invalidateQueries({ queryKey: ['private', session.user.id, 'project', project.id, 'media'] })
  }
  if (!allowed) return <PermissionDenied />
  const format = (value: string) => new Intl.DateTimeFormat(i18n.language === 'en' ? 'en-US' : 'zh-CN', { dateStyle: 'medium' }).format(new Date(value))
  return <>
    <div className="page-heading"><div><p className="section-eyebrow">{project.name}</p><h1>{t('mediaTitle')}</h1><p>{t('mediaIntro')}</p></div>
      {manage && <button className="primary-button" onClick={() => { setSuccess(''); setCreate(true) }}>{t('mediaCreate')}</button>}</div>
    {success && <SuccessNotice>{success}</SuccessNotice>}
    <section className="management-panel" aria-busy={media.isFetching}>
      <ActionError error={media.error} />
      {media.isPending ? <p className="table-loading" role="status">{t('loadingRecords')}</p> : media.data && <>
        <div className="table-scroll" tabIndex={0} role="region" aria-label={t('mediaTitle')}><table className="data-table"><thead><tr><th scope="col">{t('mediaName')}</th><th scope="col">{t('mediaType')}</th><th scope="col">{t('mediaArch')}</th><th scope="col">{t('mediaState')}</th><th scope="col">{t('createdAt')}</th><th scope="col">{t('actions')}</th></tr></thead>
          <tbody>{media.data.items.map((item) => <tr key={item.id}><td><strong>{item.name}</strong><span className="cell-subtitle mono-value">{item.version} · {item.sha256.slice(0, 12)}…</span></td>
            <td>{t('media_type_' + item.type)}</td><td>{item.arch}</td>
            <td><span className={`status-label ${item.state === 'approved' ? 'enabled' : item.state === 'retired' ? 'disabled' : ''}`}>{t('media_state_' + item.state)}</span></td>
            <td>{format(item.created_at)}</td>
            <td><div className="table-actions">{manage && nextStates[item.state].map((target) => <button key={target} className={target === 'retired' ? 'danger-text' : ''} onClick={() => { setSuccess(''); setTransition({ item, target }) }}>{t('mediaNext' + target.charAt(0).toUpperCase() + target.slice(1))}</button>)}</div></td></tr>)}</tbody></table>
          {!media.data.items.length && <p className="table-loading">{t('noRecords')}</p>}</div>
        <Pagination offset={(page - 1) * media.data.page_size} limit={media.data.page_size} total={media.data.total} busy={media.isFetching} onChange={(offset) => setPage(offset / media.data.page_size + 1)} /></>}
    </section>
    {create && <RegisterMediaDialog projectId={project.id} onClose={() => setCreate(false)} onDone={() => done(t('mediaCreated'))} />}
    {transition && <TransitionDialog projectId={project.id} item={transition.item} target={transition.target} onClose={() => setTransition(null)} onDone={() => done(t('mediaTransitionDone'))} />}
  </>
}
export function MediaPage({ projectId }: { projectId: string }) {
  return <ProjectWorkspace projectId={projectId}><MediaContent /></ProjectWorkspace>
}
