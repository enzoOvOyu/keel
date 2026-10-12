import { useEffect, useState, type FormEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { Workspace } from '../../components/Workspace'
import { ActionDialog, ActionError, SecretField } from '../../components/Management'
import { changeOwnPassword, useAction, validNewPassword } from '../../lib/management'
import { useEndSession, useSession } from '../../lib/session'
function PasswordDialog({ onClose }: { onClose: () => void }) {
  const { t } = useTranslation(), session = useSession(), action = useAction(), endSession = useEndSession()
  const [current, setCurrent] = useState(''), [password, setPassword] = useState(''), [confirm, setConfirm] = useState(''), [validation, setValidation] = useState('')
  const [retryAt, setRetryAt] = useState(0), [now, setNow] = useState(Date.now())
  const wait = Math.max(0, Math.ceil((retryAt - now) / 1000))
  useEffect(() => { if (!retryAt) return; const timer = window.setInterval(() => setNow(Date.now()), 500); return () => window.clearInterval(timer) }, [retryAt])
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault(); if (wait) return
    if (!validNewPassword(password)) { setValidation(t('passwordRules')); document.getElementById('new-password')?.focus(); return }
    if (password !== confirm) { setValidation(t('passwordMismatch')); document.getElementById('confirm-password')?.focus(); return }
    setValidation('')
    const result = await action.run(() => changeOwnPassword({ current_password: current, new_password: password }, session.csrf_token))
    setCurrent(''); setPassword(''); setConfirm('')
    if (result.ok) await endSession(false, 'password_changed')
  }
  useEffect(() => { if (action.error?.status === 429) { setNow(Date.now()); setRetryAt(Date.now() + Math.max(1, action.error.retryAfter) * 1000) } }, [action.error])
  return <ActionDialog title={t('changePassword')} description={t('changePasswordImpact')} busy={action.busy} submitDisabled={wait > 0} confirm={t(action.busy ? 'saving' : 'confirmChange')} danger onSubmit={(event) => void submit(event)} onClose={onClose}>
    <ActionError error={action.error} />{wait > 0 && <p className="field-hint" role="status">{t('retrySeconds', { count: wait })}</p>}{validation && <p className="field-error" role="alert">{validation}</p>}
    <SecretField id="current-password" label={t('currentPassword')} value={current} onChange={setCurrent} current disabled={action.busy} />
    <SecretField id="new-password" label={t('newPassword')} value={password} onChange={setPassword} disabled={action.busy} />
    <SecretField id="confirm-password" label={t('confirmPassword')} value={confirm} onChange={setConfirm} disabled={action.busy} />
  </ActionDialog>
}
function AccountContent() {
  const { t } = useTranslation(), session = useSession(), [open, setOpen] = useState(false)
  useEffect(() => { document.title = `${t('accountSecurity')} · Keel` }, [t])
  return <><div className="page-heading"><div><p className="section-eyebrow">{t('personalAccount')}</p><h1>{t('accountSecurity')}</h1><p>{t('accountSecurityIntro')}</p></div></div><div className="account-layout">
    <section className="management-panel"><h2>{t('accountDetails')}</h2><dl className="account-facts"><div><dt>{t('account')}</dt><dd>{session.user.username}</dd></div><div><dt>{t('displayName')}</dt><dd>{session.user.display_name}</dd></div><div><dt>{t('accountType')}</dt><dd>{t(session.user.is_admin ? 'platformAdmin' : 'regularUser')}</dd></div></dl></section>
    <section className="management-panel"><h2>{t('passwordSecurity')}</h2><p className="section-description">{t('changePasswordImpact')}</p><p className="impact-notice">{t('passwordPrivacy')}</p><button className="primary-button" disabled={!session.permissions.includes('account:password')} onClick={() => setOpen(true)}>{t('changePassword')}</button></section>
  </div>{open && <PasswordDialog onClose={() => setOpen(false)} />}</>
}
export function AccountPage() { return <Workspace><AccountContent /></Workspace> }
