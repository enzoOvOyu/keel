import { useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { ActionDialog, ActionError } from '../../components/Management'
import { useAction } from '../../lib/management'
import { useSession } from '../../lib/session'
import { requestProvisionSSHBatch, type ProvisionInstallation, type ProvisionInstallationProgress } from '../../lib/provision'

export type SSHVerificationChoice = { item: ProvisionInstallation; progress: ProvisionInstallationProgress }

export function SSHVerificationBatch({ items, project, onClose, onDone }: { items: SSHVerificationChoice[]; project: string; onClose: () => void; onDone: () => Promise<void> }) {
 const { t } = useTranslation(), session = useSession(), action = useAction(), key = useRef(crypto.randomUUID()), [reason, setReason] = useState('')
 const valid = items.length > 0 && items.length <= 32 && items.every(({ item, progress }) => item.project_id === project && progress.installation_id === item.id && progress.state === 'awaiting_first_boot' && !!progress.ssh_repair?.failed_event_id && progress.ssh_repair.status === 'available' && progress.resource.lease_state === 'held' && !progress.resource.lease_expired)
 return <ActionDialog title={t('installSSHBatchTitle')} description={t('installSSHBatchDetail')} busy={action.busy} confirm={t('installSSHVerifySubmit')} submitDisabled={!valid || !reason.trim()} onClose={onClose} onSubmit={event => {
  event.preventDefault()
  if (!valid || !reason.trim()) return
  const selected = items.map(({ progress }) => ({ installation_id: progress.installation_id, expected_version: progress.state_version, repair_event_id: progress.ssh_repair!.event_id, failed_event_id: progress.ssh_repair!.failed_event_id! }))
  void action.run(() => requestProvisionSSHBatch(project, selected, reason.trim(), session.csrf_token, key.current)).then(async result => { if (result.ok) await onDone() })
 }}>
  <p className="impact-notice">{t('installSSHBatchImpact')}</p>
  <ActionError error={action.error} />
  <ul>{items.map(({ item, progress }) => <li key={item.id}>{item.spec?.expected.serial_number ?? item.machine_id} · v{progress.state_version} · {t('installSSHRepairEvent')} #{progress.ssh_repair?.event_id} · {t('installSSHFailedEvent')} #{progress.ssh_repair?.failed_event_id}</li>)}</ul>
  <div className="field"><Label htmlFor="install-ssh-batch-reason">{t('installReason')}</Label><Input id="install-ssh-batch-reason" maxLength={500} required disabled={action.busy} value={reason} onChange={event => { setReason(event.target.value); key.current = crypto.randomUUID() }} /></div>
 </ActionDialog>
}
