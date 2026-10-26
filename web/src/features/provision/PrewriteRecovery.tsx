import { useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { ActionDialog, ActionError } from '../../components/Management'
import { useAction } from '../../lib/management'
import { useSession } from '../../lib/session'
import { reconcileProvisionBeforeWrite, type ProvisionInstallationProgress } from '../../lib/provision'

export function PrewriteRecovery({ item, project, current, onClose, onDone }: { item: ProvisionInstallationProgress; project: string; current: boolean; onClose: () => void; onDone: () => Promise<void> }) {
 const { t } = useTranslation(), session = useSession(), action = useAction(), key = useRef(crypto.randomUUID()), [reason, setReason] = useState('')
 return <ActionDialog title={t('installPrewriteRecoveryTitle')} description={item.installation_id} busy={action.busy} danger confirm={t('installPrewriteRecoverySubmit')} submitDisabled={!current || !reason.trim()} onClose={onClose} onSubmit={event => {
  event.preventDefault()
  if (!current || !reason.trim()) return
  void action.run(() => reconcileProvisionBeforeWrite(item, project, reason.trim(), session.csrf_token, key.current)).then(async result => { if (result.ok) await onDone() })
 }}>
  <p className="impact-notice">{t('installPrewriteRecoveryImpact')}</p>
  {!current && <p role="status">{t('installVersionChanged')}</p>}
  <ActionError error={action.error} />
  <div className="field"><Label htmlFor="install-prewrite-recovery-reason">{t('installReason')}</Label><Input id="install-prewrite-recovery-reason" required maxLength={500} disabled={action.busy} value={reason} onChange={event => { setReason(event.target.value); key.current = crypto.randomUUID() }} /></div>
 </ActionDialog>
}
