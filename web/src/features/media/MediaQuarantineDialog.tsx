import { useTranslation } from 'react-i18next'
import { useQuery } from '@tanstack/react-query'
import { Button } from '@/components/ui/button'
import { ActionDialog, ActionError } from '../../components/Management'
import { getMediaQuarantineEvidence, type MediaItemSummary } from '../../lib/media'
import { useExpireOnUnauthorized, useSession } from '../../lib/session'

export function MediaQuarantineDialog({ projectId, item, onClose }: { projectId: string | null; item: MediaItemSummary; onClose: () => void }) {
 const { t, i18n } = useTranslation(), session = useSession()
 const query = useQuery({ queryKey: ['private', session.user.id, projectId === null ? 'global' : 'project', projectId, 'media-quarantine', item.id], queryFn: ({ signal }) => getMediaQuarantineEvidence(projectId, item.id, signal), staleTime: 0, retry: false })
 useExpireOnUnauthorized(query.error)
 const evidence = query.isError ? undefined : query.data
 return <ActionDialog title={t('mediaQuarantineView')} description={t('mediaQuarantineDetail')} busy={false} confirm={t('mediaMatrixClose')} onClose={onClose} onSubmit={event => { event.preventDefault(); onClose() }}>
  <p className="ui:break-all">{item.name} · {item.version}</p>
  <ActionError error={query.error} />
  {query.isPending && <p role="status">{t('loadingRecords')}</p>}
  {query.isError && <Button type="button" variant="outline" onClick={() => void query.refetch()} disabled={query.isFetching}>{t('retry')}</Button>}
  {evidence && <dl className="action-target">
   <dt>{t('mediaState')}</dt><dd>{t('media_state_' + evidence.state)}</dd>
   <dt>{t('mediaQuarantineWhen')}</dt><dd>{new Intl.DateTimeFormat(i18n.language === 'en' ? 'en-US' : 'zh-CN', { dateStyle: 'medium', timeStyle: 'medium' }).format(new Date(evidence.quarantined_at))}</dd>
   <dt>{t('mediaQuarantineReason')}</dt><dd>{t('mediaQuarantineMismatch')}</dd>
   <dt>{t('mediaQuarantineCount')}</dt><dd>{evidence.mismatch_count}</dd>
   <dt>{t('mediaQuarantineVersion')}</dt><dd className="mono-value ui:break-all">{evidence.object_version}</dd>
   <dt>{t('mediaQuarantineExpected')}</dt><dd className="mono-value ui:break-all"><span>{evidence.expected_sha256}</span><span className="cell-subtitle">{evidence.expected_size} B</span></dd>
   <dt>{t('mediaQuarantineObserved')}</dt><dd className="mono-value ui:break-all"><span>{evidence.observed_sha256}</span><span className="cell-subtitle">{evidence.observed_size} B</span></dd>
  </dl>}
  <p className="impact-notice">{t('mediaQuarantineTrust')}</p>
 </ActionDialog>
}
