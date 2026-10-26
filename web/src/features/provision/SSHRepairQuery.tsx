import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { ActionDialog, ActionError } from '../../components/Management'
import { useAction } from '../../lib/management'
import { useSession } from '../../lib/session'
import { requestProvisionSSHReceiptQuery, requestProvisionSSHVerification, type ProvisionInstallationProgress } from '../../lib/provision'

export function SSHRepairQuery({ item, project, current, mode = 'query', onClose, onDone }: { mode?: 'query' | 'verify'; item: ProvisionInstallationProgress; project: string; current: boolean; onClose: () => void; onDone: () => Promise<void> }) {
 const { t } = useTranslation(), session = useSession(), action = useAction(), [reason, setReason] = useState('')
 const repair = item.ssh_repair
 const verify = mode === 'verify'
 const allowed = !!repair && repair.status === 'available' && (!verify || !!repair.failed_event_id)
 return <ActionDialog title={t(verify ? 'installSSHVerifyTitle' : 'installSSHQueryTitle')} description={item.installation_id} busy={action.busy} confirm={t(verify ? 'installSSHVerifySubmit' : 'installSSHQuerySubmit')} submitDisabled={!current || !allowed || !reason.trim()} onClose={onClose} onSubmit={event => {
  event.preventDefault()
  if (!current || !repair || !allowed || !reason.trim()) return
  void action.run<unknown>(() => verify && repair.failed_event_id ? requestProvisionSSHVerification(item, project, repair.event_id, repair.failed_event_id, reason.trim(), session.csrf_token) : requestProvisionSSHReceiptQuery(item, project, repair.event_id, reason.trim(), session.csrf_token)).then(async result => { if (result.ok) await onDone() })
 }}>
  <p className="impact-notice">{t(verify ? 'installSSHVerifyImpact' : 'installSSHQueryImpact')}</p>
  <p>{t('installSSHQueryPrerequisites')}</p>
  {!current && <p role="status">{t('installVersionChanged')}</p>}
  <ActionError error={action.error} />
  <dl><dt>{t('installSSHRepairEvent')}</dt><dd>#{repair?.event_id} · {repair?.reason}</dd>{verify && <><dt>{t('installSSHFailedEvent')}</dt><dd>#{repair?.failed_event_id}</dd></>}</dl>
  <div className="field"><Label htmlFor={`install-ssh-${mode}-reason`}>{t('installReason')}</Label><Input id={`install-ssh-${mode}-reason`} maxLength={500} required disabled={action.busy} value={reason} onChange={event => setReason(event.target.value)} /></div>
 </ActionDialog>
}
