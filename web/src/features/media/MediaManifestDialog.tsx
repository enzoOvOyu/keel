import { MediaCompatibilitySummary } from './MediaCompatibility'
import { useTranslation } from 'react-i18next'
import { ActionDialog, ActionError } from '../../components/Management'
import { useAction } from '../../lib/management'
import { exportMediaManifest, type MediaItemSummary } from '../../lib/media'
import { MediaDependencySummary } from './MediaDependencyPicker'

export function MediaManifestDialog({ projectId, item, onClose, onDone }: {
 projectId: string | null; item: MediaItemSummary; onClose: () => void; onDone: () => void
}) {
 const { t } = useTranslation(), action = useAction()
 return <ActionDialog title={t('mediaManifestExport')} description={t('mediaManifestExportDetail')} busy={action.busy} confirm={t('mediaManifestDownload')} onClose={onClose} onSubmit={event => {
  event.preventDefault()
  void action.run(() => exportMediaManifest(projectId, item.id)).then(result => {
   if (!result.ok) return
   const blob = new Blob([JSON.stringify(result.value, null, 2) + '\n'], { type: 'application/json' }), url = URL.createObjectURL(blob)
   const link = document.createElement('a'); link.href = url; link.download = `keel-media-${item.id}.manifest.json`; link.click()
   setTimeout(() => URL.revokeObjectURL(url), 1000); onDone()
  })
 }}>
  {action.error?.code === 'MEDIA_ARTIFACT_QUARANTINED' ? <p role="alert" className="field-error">{t('mediaQuarantined')}</p> : action.error?.code === 'MEDIA_SIGNING_UNAVAILABLE' ? <p role="alert" className="field-error">{t('mediaSigningUnavailable')}</p> : action.error?.code?.startsWith('MEDIA_DEPENDENCY_') ? <p role="alert" className="field-error">{t('mediaDependencyBlocked')}</p> : <ActionError error={action.error} />}
  <dl className="action-target"><dt>{t('mediaName')}</dt><dd>{item.name} · {item.version}</dd><dt>{t('mediaSha256')}</dt><dd>{item.sha256}</dd></dl>
  <MediaDependencySummary refs={item.deps.artifacts} />
  <MediaCompatibilitySummary matrix={item.deps.compatibility} />
  <p className="impact-notice">{t('mediaManifestTrust')}</p>
 </ActionDialog>
}
