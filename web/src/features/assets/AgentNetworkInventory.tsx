import { useEffect, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { ActionDialog, ActionError } from '../../components/Management'
import { useProject } from '../../components/ProjectWorkspace'
import { ApiError } from '../../lib/api'
import { getAgentNetworkInventory, type AgentTarget } from '../../lib/execution'
import { useExpireOnUnauthorized, useSession } from '../../lib/session'

export function AgentNetworkInventoryDialog({ target, onClose, onDenied }: { target: AgentTarget; onClose: () => void; onDenied: (error: ApiError) => void }) {
  const { t } = useTranslation(), project = useProject(), session = useSession()
  const [now, setNow] = useState(Date.now())
  const data = useQuery({ queryKey: ['private', session.user.id, 'project', project.id, 'agent-network-inventory', target.machine_id], queryFn: ({ signal }) => getAgentNetworkInventory(project.id, target.machine_id, signal), retry: false, refetchOnWindowFocus: false })
  useExpireOnUnauthorized(data.error)
  useEffect(() => { if (data.error instanceof ApiError && [401, 403, 404].includes(data.error.status)) onDenied(data.error) }, [data.error])
  useEffect(() => { setNow(Date.now()) }, [data.dataUpdatedAt])
  const record = !data.isError ? data.data : undefined
  const expiry = Math.min(Date.parse(target.credential_expires_at), record?.collected_at ? Date.parse(record.collected_at) + 30_000 : 0, record?.last_success_at ? Date.parse(record.last_success_at) + 30_000 : 0)
  useEffect(() => {
    if (!Number.isFinite(expiry) || expiry <= now) return
    const timer = window.setTimeout(() => setNow(Date.now()), Math.max(1, Math.min(expiry - Date.now() + 1, 2_147_483_647)))
    return () => window.clearTimeout(timer)
  }, [expiry, now])
  const state = record?.state === 'ready' && (!Number.isFinite(expiry) || expiry <= now) ? 'stale' : record?.state
  return <ActionDialog title={t('agentNetworkTitle')} description={`${target.hostname} · ${target.serial_number}`} busy={false} confirm={t('close')} onClose={onClose} onSubmit={event => { event.preventDefault(); onClose() }}>
    <ActionError error={data.error} />
    <p className="impact-notice">{t('agentNetworkBoundary')}</p>
    <Button type="button" variant="outline" disabled={data.isFetching} onClick={() => { setNow(Date.now()); void data.refetch() }}>{t('refresh')}</Button>
    {data.isPending && <p role="status">{t('loadingRecords')}</p>}
    {record && <>
      <dl className="action-target"><dt>{t('status')}</dt><dd>{t(`agentNetworkState_${state}`)}</dd><dt>{t('agentNetworkCaptured')}</dt><dd>{record.collected_at ? new Date(record.collected_at).toLocaleString() : '—'}</dd><dt>{t('agentNetworkReceived')}</dt><dd>{record.received_at ? new Date(record.received_at).toLocaleString() : '—'}</dd></dl>
      {record.failure_code && <p role="status">{t(`agentNetworkFailure_${record.failure_code}`)}</p>}
      {state !== 'ready' && !!record.interfaces.length && <p>{t('agentNetworkHistorical')}</p>}
      {record.interfaces.length ? <Table><TableHeader><TableRow><TableHead>{t('agentNetworkName')}</TableHead><TableHead>MAC</TableHead><TableHead>{t('agentNetworkAdmin')}</TableHead><TableHead>{t('status')}</TableHead><TableHead>MTU</TableHead></TableRow></TableHeader><TableBody>{record.interfaces.map(item => <TableRow key={item.index}><TableCell>{item.name}<span className="cell-subtitle">#{item.index}</span></TableCell><TableCell className="mono-value">{item.mac || '—'}</TableCell><TableCell>{item.admin_up ? 'UP' : 'DOWN'}</TableCell><TableCell>{item.oper_state}</TableCell><TableCell>{item.mtu}</TableCell></TableRow>)}</TableBody></Table> : <p>{t('agentNetworkEmpty')}</p>}
    </>}
  </ActionDialog>
}
