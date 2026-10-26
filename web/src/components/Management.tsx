import { Label } from "@/components/ui/label"
import { Input } from "@/components/ui/input"
import { Button } from "@/components/ui/button"
import {Dialog,DialogContent,DialogHeader,DialogTitle,DialogDescription,DialogFooter} from '@/components/ui/dialog'
import { useRef, useState, type FormEvent, type ReactNode } from 'react'
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
    PROVISION_PREFLIGHT_REFRESH_PENDING: 'installRefreshPending', PROVISION_INSTALL_DISABLED: 'installRuntimeUnavailable', PROVISION_STORAGE_FACTS_REQUIRED: 'installFactsRequired', PROVISION_STORAGE_STATE_CHANGED: 'installStateChanged', PROVISION_NETWORK_CONFLICT: 'provisionNetworkConflict', PROVISION_INDEPENDENT_APPROVAL_REQUIRED: 'provisionIndependentReview', VERSION_CONFLICT: 'versionConflict', CURRENT_PASSWORD_INVALID: 'currentPasswordInvalid', LAST_PLATFORM_ADMIN: 'lastAdminProtected', LAST_PROJECT_ADMIN: 'lastProjectAdminProtected', MEMBER_EXISTS: 'memberExists', USER_NOT_FOUND: 'userNotFound', CONFLICT: 'versionConflict', USERNAME_EXISTS: 'usernameExists',
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
  return <nav className="pagination" aria-label={t('pagination')}><span>{t('pageRange', { from: total > offset ? offset + 1 : 0, to: total > offset ? Math.min(offset + limit, total) : 0, total })}</span><div>{offset >= total && offset > 0 && <Button variant="outline" type="button" className="secondary-button ui:min-h-11" disabled={busy} onClick={() => onChange(0)}>{t('firstPage')}</Button>}<Button variant="outline" type="button" className="secondary-button ui:min-h-11" disabled={busy || offset === 0} onClick={() => onChange(Math.max(0, offset - limit))}>{t('previousPage')}</Button><Button variant="outline" type="button" className="secondary-button ui:min-h-11" disabled={busy || offset + limit >= total} onClick={() => onChange(offset + limit)}>{t('nextPage')}</Button></div></nav>
}
export function SecretField({ id, label, value, onChange, current = false, disabled = false, required = true }: {
  id: string; label: string; value: string; onChange: (value: string) => void; current?: boolean; disabled?: boolean; required?: boolean
}) {
  const { t } = useTranslation()
  const [visible, setVisible] = useState(false)
  return <div className="field"><Label htmlFor={id}>{label}</Label><div className="password-field"><Input className="ui:h-11 ui:pr-14" id={id} name={id} type={visible ? 'text' : 'password'} autoComplete={current ? 'current-password' : 'new-password'} value={value} onChange={(event) => onChange(event.target.value)} disabled={disabled} required={required} maxLength={1024} aria-describedby={!current ? `${id}-hint` : undefined} /><Button variant="ghost" type="button" className="password-toggle ui:min-h-11 ui:absolute ui:inset-y-0 ui:right-0 ui:h-full ui:w-11 ui:p-0" onClick={() => setVisible(!visible)} aria-label={t(visible ? 'hidePassword' : 'showPassword')} aria-controls={id} aria-pressed={visible} disabled={disabled}><Icon name={visible ? 'eyeOff' : 'eye'} /></Button></div>{!current && <span className="field-hint" id={`${id}-hint`}>{t('passwordRules')}</span>}</div>
}
export function ActionDialog({ title, description, children, busy, confirm, danger = false, submitDisabled = false, onSubmit, onClose }: {
  title: string; description: string; children: ReactNode; busy: boolean; confirm: string; danger?: boolean; submitDisabled?: boolean;
  onSubmit: (event: FormEvent<HTMLFormElement>) => void; onClose: () => void
}) {
  const { t } = useTranslation()
  const previous = useRef(document.activeElement instanceof HTMLElement ? document.activeElement : null)
  return <Dialog open onOpenChange={open=>{if(!open&&!busy)onClose()}}>
    <DialogContent className="action-dialog ui:w-[min(560px,calc(100vw-32px))] ui:max-w-none ui:sm:max-w-none ui:max-h-[calc(100svh-48px)] ui:overflow-y-auto ui:p-[var(--space-8)] ui:text-foreground" showCloseButton={false}
      onEscapeKeyDown={event=>{if(busy)event.preventDefault()}}
      onInteractOutside={event=>event.preventDefault()}
      onCloseAutoFocus={event=>{event.preventDefault();previous.current?.focus()}}>
      <form onSubmit={onSubmit} aria-busy={busy}>
        <DialogHeader className="ui:gap-2 ui:text-left">
          <p className="section-eyebrow">KEEL / {danger ? 'ACCESS CONTROL' : 'WORKSPACE'}</p>
          <DialogTitle className="ui:text-2xl">{title}</DialogTitle>
          <DialogDescription className="dialog-description">{description}</DialogDescription>
        </DialogHeader>
        {children}
        <DialogFooter className="form-actions ui:flex-row">
          <Button type="button" variant="outline" className="ui:min-h-11" onClick={onClose} disabled={busy}>{t('cancel')}</Button>
          <Button type="submit" variant={danger?'outline':'default'} className={danger?'ui:min-h-11 ui:border-destructive ui:text-destructive ui:hover:bg-destructive/10':'ui:min-h-11'} disabled={busy || submitDisabled}>{busy && <Icon name="refresh" className="rotating" />}{confirm}</Button>
        </DialogFooter>
      </form>
    </DialogContent>
  </Dialog>
}
