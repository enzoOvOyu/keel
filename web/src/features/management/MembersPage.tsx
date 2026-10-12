import { useEffect, useState, type FormEvent } from 'react'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { ProjectWorkspace, canManageProject, useProject } from '../../components/ProjectWorkspace'
import { ActionDialog, ActionError, Pagination, PermissionDenied, SuccessNotice } from '../../components/Management'
import { addMember, listMembers, projectRoles, removeMember, updateMember, useAction, type Member, type ProjectRole } from '../../lib/management'
import { useEndSession, useExpireOnUnauthorized, useSession } from '../../lib/session'

type Selection = { kind: 'add' } | { kind: 'role' | 'remove'; member: Member }
function MemberDialog({ selection, onClose, onDone }: { selection: Selection; onClose: () => void; onDone: (id: string) => Promise<void> }) {
  const { t } = useTranslation(), session = useSession(), project = useProject(), action = useAction()
  const member = selection.kind === 'add' ? null : selection.member
  const [username, setUsername] = useState(''), [role, setRole] = useState<ProjectRole>(member?.role ?? 'Viewer')
  const title = t(selection.kind === 'add' ? 'addMember' : selection.kind === 'remove' ? 'removeMember' : 'changeRole')
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const result = await action.run(async () => {
      if (selection.kind === 'add') return (await addMember(project.id, { username: username.trim(), role }, session.csrf_token)).user.id
      if (selection.kind === 'role') await updateMember(project.id, selection.member.user.id, { role, version: selection.member.version }, session.csrf_token)
      else await removeMember(project.id, selection.member.user.id, selection.member.version, session.csrf_token)
      return selection.member.user.id
    })
    if (result.ok) await onDone(result.value)
  }
  return <ActionDialog title={title} description={t(selection.kind === 'remove' ? 'removeMemberImpact' : 'memberChangeImpact')} busy={action.busy} confirm={t(action.busy ? 'saving' : 'confirmChange')} danger onSubmit={(event) => void submit(event)} onClose={onClose}>
    <dl className="action-target"><dt>{t('projectName')}</dt><dd>{project.name}</dd>{member && <><dt>{t('account')}</dt><dd>{member.user.username}</dd><dt>{t('currentRole')}</dt><dd>{member.role}</dd><dt>{t('recordVersion')}</dt><dd>{member.version}</dd></>}</dl>
    <ActionError error={action.error} />
    {selection.kind === 'add' && <div className="field"><label htmlFor="member-username">{t('existingAccount')}</label><input id="member-username" value={username} onChange={(event) => setUsername(event.target.value)} maxLength={128} autoComplete="off" autoCapitalize="none" spellCheck={false} required disabled={action.busy} /><span className="field-hint">{t('memberAccountHint')}</span></div>}
    {selection.kind !== 'remove' && <div className="field"><label htmlFor="member-role">{t('projectRole')}</label><select id="member-role" value={role} onChange={(event) => setRole(event.target.value as ProjectRole)} disabled={action.busy}>{projectRoles.map((value) => <option key={value} value={value}>{value} — {t(`role_${value}`)}</option>)}</select><span className="field-hint">{t(`roleHelp_${role}`)}</span></div>}
    {member?.user.id === session.user.id && <p className="impact-notice">{t('ownAccessImpact')}</p>}
    {action.error?.status === 409 && <p className="field-hint">{t('conflictRecovery')}</p>}
  </ActionDialog>
}
function MembersContent() {
  const { t } = useTranslation(), session = useSession(), project = useProject(), client = useQueryClient(), navigate = useNavigate(), endSession = useEndSession()
  const search = useSearch({ from: '/p_/$projectId/members' }), page = search.page ?? 1
  const allowed = canManageProject(project, session.user.is_admin) && session.permissions.includes('project:members:read')
  const manage = allowed && session.permissions.includes('project:members:manage')
  const [selection, setSelection] = useState<Selection | null>(null), [success, setSuccess] = useState(false)
  const key = ['private', session.user.id, 'project', project.id, 'members']
  const members = useQuery({ queryKey: [...key, page], queryFn: ({ signal }) => listMembers(project.id, page, signal), enabled: allowed })
  useExpireOnUnauthorized(members.error)
  useEffect(() => { document.title = `${t('membersTitle')} · ${project.name} · Keel` }, [t, project.name])
  async function done(id: string) { setSelection(null); if (id === session.user.id) { await endSession(false, 'access_changed'); return }; setSuccess(true); await client.invalidateQueries({ queryKey: key }) }
  if (!allowed) return <PermissionDenied />
  return <><div className="page-heading"><div><p className="section-eyebrow">{project.name}</p><h1>{t('membersTitle')}</h1><p>{t('membersIntro')}</p></div>{manage && <button className="primary-button" onClick={() => { setSuccess(false); setSelection({ kind: 'add' }) }}>{t('addMember')}</button>}</div>
    {success && <SuccessNotice>{t('memberChangeSaved')}</SuccessNotice>}
    <section className="management-panel" aria-busy={members.isFetching}><div className="management-toolbar"><p className="section-description">{t('projectAdminBoundary')}</p><button className="secondary-button" onClick={() => void members.refetch()} disabled={members.isFetching}>{t('refresh')}</button></div><ActionError error={members.error} />
      {members.isPending ? <p className="table-loading" role="status">{t('loadingRecords')}</p> : members.data && <><div className="table-scroll" tabIndex={0} role="region" aria-label={t('membersTitle')}><table className="data-table"><thead><tr><th scope="col">{t('account')}</th><th scope="col">{t('projectRole')}</th><th scope="col">{t('status')}</th>{manage && <th scope="col">{t('actions')}</th>}</tr></thead><tbody>{members.data.items.map((member) => <tr key={member.id}><td><strong>{member.user.display_name}</strong><span className="cell-subtitle">{member.user.username}{member.user.id === session.user.id ? ` · ${t('you')}` : ''}</span></td><td><span className="role-label">{member.role}</span><span className="cell-subtitle">{t(`role_${member.role}`)}</span></td><td><span className={`status-label ${member.enabled ? 'enabled' : 'disabled'}`}>{t(member.enabled ? 'enabled' : 'disabled')}</span></td>{manage && <td><div className="table-actions"><button onClick={() => setSelection({ kind: 'role', member })}>{t('changeRole')}</button><button className="danger-text" onClick={() => setSelection({ kind: 'remove', member })}>{t('removeMember')}</button></div></td>}</tr>)}</tbody></table>{!members.data.items.length && <p className="table-loading">{t('noRecords')}</p>}</div><Pagination offset={(page - 1) * members.data.page_size} limit={members.data.page_size} total={members.data.total} busy={members.isFetching} onChange={(offset) => void navigate({ to: '/p/$projectId/members', params: { projectId: project.id }, search: { page: offset / 20 + 1 } })} /></>}
    </section>{selection && <MemberDialog selection={selection} onClose={() => setSelection(null)} onDone={done} />}
  </>
}
export function MembersPage({ projectId }: { projectId: string }) { return <ProjectWorkspace projectId={projectId}><MembersContent /></ProjectWorkspace> }
