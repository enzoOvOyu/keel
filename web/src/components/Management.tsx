import { useEffect, useId, useRef, useState, type FormEvent, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { ApiError } from '../lib/api'
import { Icon } from './Icon'

export function PermissionDenied() {
  const { t } = useTranslation()
  return <section className="empty-state" role="alert"><Icon name="shield" /><h1>{t('accessDenied')}</h1><p>{t('accessDeniedDetail')}</p></section>
}
export function ActionError({ error }: { error: unknown }) {
  const { t } = useTranslation()
  if (!error) return null
  const known: Record<string, string> = {
    VERSION_CONFLICT: 'versionConflict', CURRENT_PASSWORD_INVALID: 'currentPasswordInvalid', LAST_PLATFORM_ADMIN: 'lastAdminProtected', LAST_PROJECT_ADMIN: 'lastProjectAdminProtected', MEMBER_EXISTS: 'memberExists', USER_NOT_FOUND: 'userNotFound', CONFLICT: 'versionConflict', USERNAME_EXISTS: 'usernameExists',
    USER_EXISTS: 'usernameExists', LAST_ADMIN: 'lastAdminProtected', LAST_ADMIN_REQUIRED: 'lastAdminProtected',
    INVALID_CREDENTIALS: 'currentPasswordInvalid', VALIDATION_ERROR: 'invalidFields',
    CSRF_INVALID: 'loginContextExpired', ORIGIN_FORBIDDEN: 'loginContextExpired',
  }
  const problem = error instanceof ApiError ? error : new ApiError(0, 'NETWORK_ERROR')
  const key = known[problem.code] ?? (problem.status === 403 ? 'accessDenied' : problem.status === 404 ? 'resourceNotFound' : problem.status === 409 ? 'versionConflict' : problem.status === 429 ? 'rateLimited' : 'actionUnavailable')
  return <div className="form-error" role="alert"><strong>{t(key)}</strong>{problem.traceId && <small>{t('traceId')}: {problem.traceId}</small>}</div>
}
export function SuccessNotice({ children }: { children: ReactNode }) {
  return <div className="success-notice" role="status"><Icon name="shield" /><span>{children}</span></div>
}
export function Pagination({ offset, limit, total, busy, onChange }: { offset: number; limit: number; total: number; busy: boolean; onChange: (offset: number) => void }) {
  const { t } = useTranslation()
  return <nav className="pagination" aria-label={t('pagination')}><span>{t('pageRange', { from: total > offset ? offset + 1 : 0, to: total > offset ? Math.min(offset + limit, total) : 0, total })}</span><div>{offset >= total && offset > 0 && <button type="button" className="secondary-button" disabled={busy} onClick={() => onChange(0)}>{t('firstPage')}</button>}<button type="button" className="secondary-button" disabled={busy || offset === 0} onClick={() => onChange(Math.max(0, offset - limit))}>{t('previousPage')}</button><button type="button" className="secondary-button" disabled={busy || offset + limit >= total} onClick={() => onChange(offset + limit)}>{t('nextPage')}</button></div></nav>
}
export function SecretField({ id, label, value, onChange, current = false, disabled = false, required = true }: {
  id: string; label: string; value: string; onChange: (value: string) => void; current?: boolean; disabled?: boolean; required?: boolean
}) {
  const { t } = useTranslation()
  const [visible, setVisible] = useState(false)
  return <div className="field"><label htmlFor={id}>{label}</label><div className="password-field"><input id={id} name={id} type={visible ? 'text' : 'password'} autoComplete={current ? 'current-password' : 'new-password'} value={value} onChange={(event) => onChange(event.target.value)} disabled={disabled} required={required} maxLength={1024} aria-describedby={!current ? `${id}-hint` : undefined} /><button type="button" className="password-toggle" onClick={() => setVisible(!visible)} aria-label={t(visible ? 'hidePassword' : 'showPassword')} aria-controls={id} aria-pressed={visible} disabled={disabled}><Icon name={visible ? 'eyeOff' : 'eye'} /></button></div>{!current && <span className="field-hint" id={`${id}-hint`}>{t('passwordRules')}</span>}</div>
}
export function ActionDialog({ title, description, children, busy, confirm, danger = false, submitDisabled = false, onSubmit, onClose }: {
  title: string; description: string; children: ReactNode; busy: boolean; confirm: string; danger?: boolean; submitDisabled?: boolean;
  onSubmit: (event: FormEvent<HTMLFormElement>) => void; onClose: () => void
}) {
  const { t } = useTranslation()
  const ref = useRef<HTMLDialogElement>(null)
  const heading = useId(), descriptionId = useId()
  useEffect(() => {
    const previous = document.activeElement instanceof HTMLElement ? document.activeElement : null
    const dialog = ref.current
    dialog?.showModal()
    return () => { dialog?.close(); previous?.focus() }
  }, [])
  return <dialog ref={ref} className="action-dialog" aria-labelledby={heading} aria-describedby={descriptionId}
    onCancel={(event) => { event.preventDefault(); if (!busy) onClose() }}>
    <form onSubmit={onSubmit} aria-busy={busy}>
      <p className="section-eyebrow">KEEL / {danger ? 'ACCESS CONTROL' : 'WORKSPACE'}</p>
      <h2 id={heading}>{title}</h2><p id={descriptionId} className="dialog-description">{description}</p>
      {children}
      <div className="form-actions"><button type="button" className="secondary-button" onClick={onClose} disabled={busy}>{t('cancel')}</button><button type="submit" className={danger ? 'danger-button' : 'primary-button'} disabled={busy || submitDisabled}>{busy && <Icon name="refresh" className="rotating" />}{confirm}</button></div>
    </form>
  </dialog>
}
