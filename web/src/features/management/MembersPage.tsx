import { Label } from "@/components/ui/label"
import { Input } from "@/components/ui/input"
import { NativeSelect, NativeSelectOption } from "@/components/ui/native-select"
import { Button } from "@/components/ui/button"
import { Table, TableHeader, TableRow, TableHead, TableBody, TableCell } from "@/components/ui/table"
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
    {selection.kind === 'add' && <div className="field"><Label htmlFor="member-username">{t('existingAccount')}</Label><Input className="ui:h-11" id="member-username" value={username} onChange={(event) => setUsername(event.target.value)} maxLength={128} autoComplete="off" autoCapitalize="none" spellCheck={false} required disabled={action.busy} /><span className="field-hint">{t('memberAccountHint')}</span></div>}
    {selection.kind !== 'remove' && <div className="field"><Label htmlFor="member-role">{t('projectRole')}</Label><NativeSelect className="ui:min-h-11" id="member-role" value={role} onChange={(event) => setRole(event.target.value as ProjectRole)} disabled={action.busy}>{projectRoles.map((value) => <NativeSelectOption key={value} value={value}>{value} — {t(`role_${value}`)}</NativeSelectOption>)}</NativeSelect><span className="field-hint">{t(`roleHelp_${role}`)}</span></div>}
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
  return <><div className="page-heading"><div><p className="section-eyebrow">{project.name}</p><h1>{t('membersTitle')}</h1><p>{t('membersIntro')}</p></div>{manage && <Button variant="default" className="primary-button ui:min-h-11" onClick={() => { setSuccess(false); setSelection({ kind: 'add' }) }}>{t('addMember')}</Button>}</div>
    {success && <SuccessNotice>{t('memberChangeSaved')}</SuccessNotice>}
    <section className="management-panel" aria-busy={members.isFetching}><div className="management-toolbar"><p className="section-description">{t('projectAdminBoundary')}</p><Button variant="outline" className="secondary-button ui:min-h-11" onClick={() => void members.refetch()} disabled={members.isFetching}>{t('refresh')}</Button></div><ActionError error={members.error} />
      {members.isPending ? <p className="table-loading" role="status">{t('loadingRecords')}</p> : members.data && <><div className="table-scroll" tabIndex={0} role="region" aria-label={t('membersTitle')}><Table scrollContainer={false} className="data-table"><TableHeader><TableRow><TableHead className="ui:align-top ui:p-4" scope="col">{t('account')}</TableHead><TableHead className="ui:align-top ui:p-4" scope="col">{t('projectRole')}</TableHead><TableHead className="ui:align-top ui:p-4" scope="col">{t('status')}</TableHead>{manage && <TableHead className="ui:align-top ui:p-4" scope="col">{t('actions')}</TableHead>}</TableRow></TableHeader><TableBody>{members.data.items.map((member) => <TableRow key={member.id}><TableCell className="ui:whitespace-normal ui:align-top ui:p-4"><strong>{member.user.display_name}</strong><span className="cell-subtitle">{member.user.username}{member.user.id === session.user.id ? ` · ${t('you')}` : ''}</span></TableCell><TableCell className="ui:whitespace-normal ui:align-top ui:p-4"><span className="role-label">{member.role}</span><span className="cell-subtitle">{t(`role_${member.role}`)}</span></TableCell><TableCell className="ui:whitespace-normal ui:align-top ui:p-4"><span className={`status-label ${member.enabled ? 'enabled' : 'disabled'}`}>{t(member.enabled ? 'enabled' : 'disabled')}</span></TableCell>{manage && <TableCell className="ui:whitespace-normal ui:align-top ui:p-4"><div className="table-actions"><Button className="ui:min-h-11" variant="outline" onClick={() => setSelection({ kind: 'role', member })}>{t('changeRole')}</Button><Button variant="outline" className="danger-text ui:min-h-11 ui:text-destructive ui:border-destructive" onClick={() => setSelection({ kind: 'remove', member })}>{t('removeMember')}</Button></div></TableCell>}</TableRow>)}</TableBody></Table>{!members.data.items.length && <p className="table-loading">{t('noRecords')}</p>}</div><Pagination offset={(page - 1) * members.data.page_size} limit={members.data.page_size} total={members.data.total} busy={members.isFetching} onChange={(offset) => void navigate({ to: '/p/$projectId/members', params: { projectId: project.id }, search: { page: offset / 20 + 1 } })} /></>}
    </section>{selection && <MemberDialog selection={selection} onClose={() => setSelection(null)} onDone={done} />}
  </>
}
export function MembersPage({ projectId }: { projectId: string }) { return <ProjectWorkspace projectId={projectId}><MembersContent /></ProjectWorkspace> }
