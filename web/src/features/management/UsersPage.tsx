import { Label } from "@/components/ui/label"
import { Input } from "@/components/ui/input"
import { NativeCheckbox } from "@/components/ui/checkbox"
import { Button } from "@/components/ui/button"
import { Table, TableHeader, TableRow, TableHead, TableBody, TableCell } from "@/components/ui/table"
import { useEffect, useState, type FormEvent } from 'react'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Workspace } from '../../components/Workspace'
import { ActionDialog, ActionError, Pagination, PermissionDenied, SecretField, SuccessNotice } from '../../components/Management'
import { addUser, listUsers, resetUserPassword, unlockUser, updateUser, useAction, validNewPassword, type UserAccount } from '../../lib/management'
import { useEndSession, useExpireOnUnauthorized, useSession } from '../../lib/session'

type Selection = { kind: 'toggle' | 'reset' | 'unlock'; user: UserAccount }
function CreateUserDialog({ onClose, onDone }: { onClose: () => void; onDone: () => Promise<void> }) {
  const { t } = useTranslation(), session = useSession(), action = useAction()
  const [username, setUsername] = useState(''), [name, setName] = useState(''), [password, setPassword] = useState('')
  const [admin, setAdmin] = useState(false), [validation, setValidation] = useState('')
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!/^[a-z0-9][a-z0-9._-]{0,127}$/.test(username.trim())) { setValidation(t('usernameRules')); document.getElementById('new-username')?.focus(); return }
    if (!validNewPassword(password)) { setValidation(t('passwordRules')); document.getElementById('initial-password')?.focus(); document.getElementById('reset-password')?.focus(); return }
    setValidation('')
    const result = await action.run(() => addUser({ username: username.trim(), display_name: name.trim(), password, is_admin: admin }, session.csrf_token))
    setPassword('')
    if (result.ok) await onDone()
  }
  return <ActionDialog title={t('createUser')} description={t('createUserDetail')} busy={action.busy} confirm={t(action.busy ? 'saving' : 'createUser')} onSubmit={(event) => void submit(event)} onClose={onClose}>
    <ActionError error={action.error} />{validation && <p className="field-error" role="alert">{validation}</p>}
    <div className="field"><Label htmlFor="new-username">{t('account')}</Label><Input className="ui:h-11" id="new-username" autoComplete="off" autoCapitalize="none" spellCheck={false} value={username} onChange={(event) => setUsername(event.target.value)} maxLength={128} required disabled={action.busy} /><span className="field-hint">{t('usernameRules')}</span></div>
    <div className="field"><Label htmlFor="display-name">{t('displayName')}</Label><Input className="ui:h-11" id="display-name" value={name} onChange={(event) => setName(event.target.value)} maxLength={120} required disabled={action.busy} /></div>
    <SecretField id="initial-password" label={t('initialPassword')} value={password} onChange={setPassword} disabled={action.busy} />
    <Label className="checkbox-field ui:items-start ui:leading-7"><NativeCheckbox checked={admin} onChange={(event) => setAdmin(event.target.checked)} disabled={action.busy} /><span>{t('platformAdmin')}</span></Label>
    <p className="impact-notice">{t(admin ? 'platformAdminImpact' : 'regularUserImpact')}</p>
  </ActionDialog>
}
function UserActionDialog({ selection, onClose, onDone }: { selection: Selection; onClose: () => void; onDone: (id: string) => Promise<void> }) {
  const { t } = useTranslation(), session = useSession(), action = useAction()
  const { user, kind } = selection
  const [password, setPassword] = useState(''), [ip, setIP] = useState(''), [includeIP, setIncludeIP] = useState(false), [validation, setValidation] = useState('')
  const title = kind === 'toggle' ? t(user.enabled ? 'disableUser' : 'enableUser') : t(kind === 'reset' ? 'resetPassword' : 'unlockAccount')
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (kind === 'reset' && !validNewPassword(password)) { setValidation(t('passwordRules')); document.getElementById('initial-password')?.focus(); document.getElementById('reset-password')?.focus(); return }
    setValidation('')
    const result = await action.run(() => kind === 'toggle' ? updateUser(user.id, { enabled: !user.enabled, version: user.version }, session.csrf_token)
      : kind === 'reset' ? resetUserPassword(user.id, { password, version: user.version }, session.csrf_token)
      : unlockUser(user.id, { version: user.version, ...(includeIP ? { ip: ip.trim() } : {}) }, session.csrf_token))
    setPassword('')
    if (result.ok) await onDone(user.id)
  }
  return <ActionDialog title={title} description={t(kind === 'toggle' ? user.enabled ? 'disableUserImpact' : 'enableUserImpact' : kind === 'reset' ? 'resetPasswordImpact' : 'unlockImpact')} busy={action.busy} confirm={t(action.busy ? 'saving' : 'confirmChange')} danger onSubmit={(event) => void submit(event)} onClose={onClose}>
    <dl className="action-target"><dt>{t('account')}</dt><dd>{user.username}</dd><dt>{t('displayName')}</dt><dd>{user.display_name}</dd><dt>{t('recordVersion')}</dt><dd>{user.version}</dd></dl>
    <ActionError error={action.error} />{validation && <p className="field-error" role="alert">{validation}</p>}
    {kind === 'reset' && <SecretField id="reset-password" label={t('newPassword')} value={password} onChange={setPassword} disabled={action.busy} />}
    {kind === 'unlock' && <><Label className="checkbox-field ui:items-start ui:leading-7"><NativeCheckbox checked={includeIP} onChange={(event) => setIncludeIP(event.target.checked)} disabled={action.busy} /><span>{t('unlockSourceIP')}</span></Label>{includeIP && <div className="field"><Label htmlFor="unlock-ip">{t('sourceIP')}</Label><Input className="ui:h-11" id="unlock-ip" value={ip} onChange={(event) => setIP(event.target.value)} placeholder="192.0.2.10" required disabled={action.busy} /><span className="field-hint">{t('unlockIPImpact')}</span></div>}</>}
    {user.id === session.user.id && <p className="impact-notice">{t('ownAccessImpact')}</p>}
    {action.error?.status === 409 && <p className="field-hint">{t('conflictRecovery')}</p>}
  </ActionDialog>
}
function UsersContent() {
  const { t, i18n } = useTranslation(), session = useSession(), client = useQueryClient(), navigate = useNavigate(), endSession = useEndSession()
  const search = useSearch({ from: '/admin/users' }), page = search.page ?? 1, q = search.q ?? ''
  const [draftQ, setDraftQ] = useState(q), [create, setCreate] = useState(false), [selection, setSelection] = useState<Selection | null>(null), [success, setSuccess] = useState(false)
  const allowed = session.user.is_admin && session.permissions.includes('user:read')
  const manage = allowed && session.permissions.includes('user:manage')
  const users = useQuery({ queryKey: ['private', session.user.id, 'users', page, q], queryFn: ({ signal }) => listUsers(page, q, signal), enabled: allowed })
  useExpireOnUnauthorized(users.error)
  useEffect(() => { document.title = `${t('usersTitle')} · Keel` }, [t])
  useEffect(() => setDraftQ(q), [q])
  async function done(id?: string) {
    setCreate(false); setSelection(null)
    if (id === session.user.id) { await endSession(false, 'access_changed'); return }
    setSuccess(true); await client.invalidateQueries({ queryKey: ['private', session.user.id, 'users'] })
  }
  if (!allowed) return <PermissionDenied />
  return <><div className="page-heading"><div><p className="section-eyebrow">{t('platformAdministration')}</p><h1>{t('usersTitle')}</h1><p>{t('usersIntro')}</p></div>{manage && <Button variant="default" className="primary-button ui:min-h-11" onClick={() => { setSuccess(false); setCreate(true) }}>{t('createUser')}</Button>}</div>
    {success && <SuccessNotice>{t('changeSaved')}</SuccessNotice>}
    <section className="management-panel" aria-busy={users.isFetching}><div className="management-toolbar"><form onSubmit={(event) => { event.preventDefault(); void navigate({ to: '/admin/users', search: { q: draftQ.trim(), page: 1 } }) }}><div className="field"><Label htmlFor="user-search">{t('searchUsers')}</Label><Input className="ui:h-11" id="user-search" value={draftQ} onChange={(event) => setDraftQ(event.target.value)} maxLength={128} /></div><Button variant="outline" className="secondary-button ui:min-h-11" type="submit">{t('search')}</Button></form><Button variant="outline" className="secondary-button ui:min-h-11" onClick={() => void users.refetch()} disabled={users.isFetching}>{t('refresh')}</Button></div>
      <ActionError error={users.error} />
      {users.isPending ? <p className="table-loading" role="status">{t('loadingRecords')}</p> : users.data && <><div className="table-scroll" tabIndex={0} role="region" aria-label={t('usersTitle')}><Table scrollContainer={false} className="data-table"><TableHeader><TableRow><TableHead className="ui:align-top ui:p-4" scope="col">{t('account')}</TableHead><TableHead className="ui:align-top ui:p-4" scope="col">{t('accountType')}</TableHead><TableHead className="ui:align-top ui:p-4" scope="col">{t('status')}</TableHead><TableHead className="ui:align-top ui:p-4" scope="col">{t('createdAt')}</TableHead>{manage && <TableHead className="ui:align-top ui:p-4" scope="col">{t('actions')}</TableHead>}</TableRow></TableHeader><TableBody>{users.data.items.map((user) => <TableRow key={user.id}><TableCell className="ui:whitespace-normal ui:align-top ui:p-4"><strong>{user.display_name}</strong><span className="cell-subtitle">{user.username}{user.id === session.user.id ? ` · ${t('you')}` : ''}</span></TableCell><TableCell className="ui:whitespace-normal ui:align-top ui:p-4">{t(user.is_admin ? 'platformAdmin' : 'regularUser')}</TableCell><TableCell className="ui:whitespace-normal ui:align-top ui:p-4"><span className={`status-label ${user.enabled ? 'enabled' : 'disabled'}`}>{t(user.enabled ? 'enabled' : 'disabled')}</span></TableCell><TableCell className="ui:whitespace-normal ui:align-top ui:p-4">{new Intl.DateTimeFormat(i18n.language === 'en' ? 'en-US' : 'zh-CN', { dateStyle: 'medium' }).format(new Date(user.created_at))}</TableCell>{manage && <TableCell className="ui:whitespace-normal ui:align-top ui:p-4"><div className="table-actions"><Button variant="outline" onClick={() => setSelection({ kind: 'toggle', user })} className={[user.enabled ? 'danger-text' : '',"ui:min-h-11"].filter(Boolean).join(" ")}>{t(user.enabled ? 'disableUser' : 'enableUser')}</Button><Button className="ui:min-h-11" variant="outline" onClick={() => setSelection({ kind: 'reset', user })}>{t('resetPassword')}</Button><Button className="ui:min-h-11" variant="outline" onClick={() => setSelection({ kind: 'unlock', user })}>{t('unlockAccount')}</Button></div></TableCell>}</TableRow>)}</TableBody></Table>{!users.data.items.length && <p className="table-loading">{t('noRecords')}</p>}</div><Pagination offset={(page - 1) * users.data.page_size} limit={users.data.page_size} total={users.data.total} busy={users.isFetching} onChange={(offset) => void navigate({ to: '/admin/users', search: { page: offset / users.data.page_size + 1, q } })} /></>}
    </section>
    {create && <CreateUserDialog onClose={() => setCreate(false)} onDone={() => done()} />}
    {selection && <UserActionDialog selection={selection} onClose={() => setSelection(null)} onDone={done} />}
  </>
}
export function UsersPage() { return <Workspace><UsersContent /></Workspace> }
