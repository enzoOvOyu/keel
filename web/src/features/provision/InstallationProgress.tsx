import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { SSHRepairQuery } from './SSHRepairQuery'
import { PrewriteRecovery } from './PrewriteRecovery'
import { useProject } from '../../components/ProjectWorkspace'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { ActionError } from '../../components/Management'
import { useSession, useExpireOnUnauthorized } from '../../lib/session'
import { getProvisionInstallationProgress, type ProvisionInstallation, type ProvisionInstallationProgress } from '../../lib/provision'

// Expanded rows share the board's refresh cadence, without fetching raw reports.
export function InstallationProgress({ item, selectedForVerification = false, canSelectVerification = true, onSelectVerification }: { item: ProvisionInstallation; selectedForVerification?: boolean; canSelectVerification?: boolean; onSelectVerification?: (progress: ProvisionInstallationProgress) => void }) {
 const { t } = useTranslation(), session = useSession(), project = useProject(), client = useQueryClient(), [open, setOpen] = useState(false)
 const [repairMode, setRepairMode] = useState<'query' | 'verify'>('query')
 const [repairChoice, setRepairChoice] = useState<ProvisionInstallationProgress>()
 const [prewriteChoice, setPrewriteChoice] = useState<ProvisionInstallationProgress>()
 const canRepair = (session.user.is_admin || ['Admin', 'DL', 'SRE'].includes(project.role)) && !['archived', 'delivered'].includes(project.state)
 const progress = useQuery({
  queryKey: ['private', session.user.id, 'project', item.project_id, 'provision-installation-progress', item.id],
  queryFn: ({ signal }) => getProvisionInstallationProgress(item.project_id, item.id, signal),
  enabled: open, retry: false,
  refetchInterval: query => open && !query.state.error && !['registered', 'failed', 'canceled', 'unknown'].includes(query.state.data?.state ?? item.state) ? 5000 : false,
 })
 useExpireOnUnauthorized(progress.error)
 const data = progress.data
 return <div className="ui:mt-2 ui:whitespace-normal ui:max-w-xl">
  <Button size="sm" variant="outline" aria-expanded={open} aria-controls={`installation-progress-${item.id}`} onClick={() => setOpen(value => !value)}>{t('installProgressTitle')}</Button>
  {open && <section id={`installation-progress-${item.id}`} aria-label={t('installProgressTitle')} className="ui:mt-3 ui:space-y-3">
   <ActionError error={progress.error} />
   {progress.isPending && <p role="status">{t('loadingRecords')}</p>}
   {data && !progress.error && <>
    <p className="cell-subtitle">{t('installProgressEvidenceHint')}</p>
    <ol className="ui:space-y-3">{data.stages.map((stage, index) => <li key={stage.id} className="ui:rounded-md ui:border ui:border-border ui:p-3" data-stage={stage.id} data-stage-status={stage.status}>
     <div className="ui:flex ui:flex-wrap ui:items-center ui:gap-2"><strong>{index + 1}. {t(`installProgressStage_${stage.id}`)}</strong><Badge variant={stage.status === 'failed' || stage.status === 'unknown' ? 'destructive' : stage.status === 'verified' ? 'default' : 'secondary'}>{t(`installProgressStatus_${stage.status}`)}</Badge></div>
     <p className="ui:mt-1">{t(`installProgressReason_${stage.reason}`)}</p>
     {!!stage.evidence.length && <details className="ui:mt-2"><summary>{t('provisionProof')}</summary><ul>{stage.evidence.map(fact => <li key={fact.id} className="mono-value ui:break-all">#{fact.id} · {fact.kind} · {new Date(fact.at).toLocaleString()}</li>)}</ul></details>}
    </li>)}</ol>
    <dl className="ui:grid ui:grid-cols-2 ui:gap-2"><dt>{t('installProgressReservation')}</dt><dd>{t(data.resource.machine_reserved ? 'installProgressReserved' : 'installProgressUnreserved')}</dd><dt>{t('installProgressLease')}</dt><dd>{t(`installProgressLease_${data.resource.lease_state}`)}{data.resource.lease_expired && <> · {t('provisionExpired')}</>}</dd>{data.resource.lease_expires_at && <><dt>{t('provisionExpiry')}</dt><dd>{new Date(data.resource.lease_expires_at).toLocaleString()}</dd></>}</dl>
    {data.rollout && <div><p>{t('installRolloutMode')}: {t(data.rollout.mode === 'canary' ? 'installRolloutCanary' : 'installRolloutValidated')}</p><p className="cell-subtitle mono-value ui:break-all">{data.rollout.identity_sha256}</p>{data.rollout.canary_ids.length > 0 && <ul>{data.rollout.canary_ids.map(id => <li key={id} className="mono-value">{id}</li>)}</ul>}</div>}
    {data.ssh_configuration && <div role="status"><p>{t('installSSHConfiguration')}: {t(`installSSHConfiguration_${data.ssh_configuration}`)}</p><p className="cell-subtitle">{t('installSSHConfigurationHint')}</p></div>}
    {data.resource.reconciliation_required && <p role="status" className="impact-notice">{t('installProgressReconcile')}</p>}
    {data.ssh_repair && data.state !== 'registered' && <div className="ui:space-y-2"><p>{t('installSSHRepairEvent')} #{data.ssh_repair.event_id} · {t(`${data.ssh_repair.verification_id ? 'installSSHVerifyStatus' : 'installSSHQueryStatus'}_${data.ssh_repair.status}`)}</p><p>{t('installSSHQueryPrerequisites')}</p>{canRepair && data.state === 'awaiting_first_boot' && data.ssh_repair.status === 'available' && data.resource.lease_state === 'held' && !data.resource.lease_expired && <Button size="sm" variant="outline" disabled={progress.isFetching} onClick={() => { setRepairMode('query'); setRepairChoice(data) }}>{t('installSSHQueryTitle')}</Button>}{data.ssh_repair.failed_event_id && <p role="status" className="ui:text-destructive">{t('installSSHFailedEvent')} #{data.ssh_repair.failed_event_id} · {t('installSSHKnownFailed')}</p>}{canRepair && data.state === 'awaiting_first_boot' && data.ssh_repair.status === 'available' && !!data.ssh_repair.failed_event_id && data.resource.lease_state === 'held' && !data.resource.lease_expired && <Button size="sm" variant="outline" disabled={progress.isFetching} onClick={() => { setRepairMode('verify'); setRepairChoice(data) }}>{t('installSSHVerifyTitle')}</Button>}</div>}
    {onSelectVerification && canRepair && data.state === 'awaiting_first_boot' && data.ssh_repair?.status === 'available' && !!data.ssh_repair.failed_event_id && data.resource.lease_state === 'held' && !data.resource.lease_expired && <Button size="sm" variant={selectedForVerification ? 'default' : 'outline'} aria-pressed={selectedForVerification} disabled={progress.isFetching || (!selectedForVerification && !canSelectVerification)} onClick={() => onSelectVerification(data)}>{t(selectedForVerification ? 'installSSHBatchUnselect' : 'installSSHBatchSelect')}</Button>}
    {canRepair && data.state === 'unknown' && !item.permit_issued_at && data.resource.lease_state === 'reconciling' && <Button size="sm" variant="outline" disabled={progress.isFetching || item.state_version !== data.state_version} onClick={() => setPrewriteChoice(data)}>{t('installPrewriteRecoveryTitle')}</Button>}
    {data.failure_reason && <p role="status" className="ui:break-all">{data.failure_reason}</p>}
    <p className="cell-subtitle">{t('installProgressAsOf')} {new Date(data.observed_at).toLocaleString()}</p>
   </>}
   <Button size="sm" variant="outline" disabled={progress.isFetching} onClick={() => void progress.refetch()}>{t('refresh')}</Button>
   {prewriteChoice && canRepair && !progress.error && <PrewriteRecovery item={prewriteChoice} project={item.project_id} current={!!data && data.state === 'unknown' && data.state_version === prewriteChoice.state_version && item.state_version === data.state_version && !item.permit_issued_at && data.resource.lease_state === 'reconciling'} onClose={() => setPrewriteChoice(undefined)} onDone={async () => { setPrewriteChoice(undefined); await progress.refetch(); await client.invalidateQueries({ queryKey: ['private', session.user.id, 'project', item.project_id, 'provision-installations'] }) }} />}
   {repairChoice && canRepair && !progress.error && <SSHRepairQuery mode={repairMode} item={repairChoice} project={item.project_id} current={!!data && data.state === 'awaiting_first_boot' && data.state_version === repairChoice.state_version && data.ssh_repair?.event_id === repairChoice.ssh_repair?.event_id && data.ssh_repair?.status === 'available' && (repairMode === 'query' || (!!data.ssh_repair?.failed_event_id && data.ssh_repair.failed_event_id === repairChoice.ssh_repair?.failed_event_id)) && data.resource.lease_state === 'held' && !data.resource.lease_expired} onClose={() => setRepairChoice(undefined)} onDone={async () => { setRepairChoice(undefined); await progress.refetch(); await client.invalidateQueries({ queryKey: ['private', session.user.id, 'project', item.project_id, 'provision-installations'] }) }} />}
  </section>}
 </div>
}
