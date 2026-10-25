import { useTranslation } from 'react-i18next'
import { ActionDialog, ActionError } from '../../components/Management'
import { useAction } from '../../lib/management'
import { fetchMediaSource, type MediaItemSummary } from '../../lib/media'
import { useSession } from '../../lib/session'

export function MediaSourceDialog({ projectId, item, onClose, onDone }: {
 projectId: string | null; item: MediaItemSummary; onClose: () => void; onDone: () => Promise<void>
}) {
 const { t } = useTranslation(), action = useAction(), session = useSession()
 return <ActionDialog title={t('mediaSourceFetch')} description={t('mediaSourceDetail')} busy={action.busy} confirm={t('mediaSourceFetch')} onClose={onClose} onSubmit={event => {
  event.preventDefault()
  void action.run(() => fetchMediaSource(projectId, item.id, session.csrf_token)).then(async result => { if (result.ok) await onDone() })
 }}>
  {action.error?.code === 'MEDIA_SOURCE_REJECTED' ? <p role="alert" className="field-error">{t('mediaSourceRejected')}</p> : action.error?.code === 'MEDIA_SOURCE_UNAVAILABLE' ? <p role="alert" className="field-error">{t('mediaSourceUnavailable')}</p> : action.error?.code === 'MEDIA_DIGEST_MISMATCH' ? <p role="alert" className="field-error">{t('mediaSourceMismatch')}</p> : <ActionError error={action.error} />}
  <dl className="action-target"><dt>{t('mediaName')}</dt><dd>{item.name} · {item.version}</dd><dt>{t('mediaManifest')}</dt><dd>{item.manifest_ref}</dd><dt>{t('mediaSha256')}</dt><dd>{item.sha256}</dd><dt>{t('mediaSize')}</dt><dd>{item.size}</dd></dl>
  <p className="impact-notice">{t('mediaSourceTrust')}</p>
 </ActionDialog>
}
