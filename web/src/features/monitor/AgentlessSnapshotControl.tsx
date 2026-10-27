import { useEffect, useRef, useState } from 'react'
import { Link } from '@tanstack/react-router'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { ActionError } from '../../components/Management'
import { useProject } from '../../components/ProjectWorkspace'
import { useAction } from '../../lib/management'
import { createMonitorSSHSnapshot, monitorSnapshot } from '../../lib/monitor'
import { useExpireOnUnauthorized, useSession } from '../../lib/session'
import { useMonitorVisible } from './visibility'

export default function AgentlessSnapshotControl({ assetId, networkOnly = false }: { assetId: string; networkOnly?: boolean }) {
 const { t } = useTranslation(), project = useProject(), session = useSession(), visible = useMonitorVisible(), action = useAction(), queries = useQueryClient()
 const [execution, setExecution] = useState(''), attempted = useRef(false), attempt = useRef('')
 const key = ['private', session.user.id, 'project', project.id, 'monitor', assetId]
 const source = useQuery({ queryKey: key, queryFn: ({ signal }) => monitorSnapshot(project.id, assetId, 0, signal), enabled: visible, retry: false, refetchOnWindowFocus: false })
 useExpireOnUnauthorized(source.error)
 const canSample = !source.isError && !!source.data?.collection_enabled && source.data.agent_state !== 'connected' && source.data.stage !== 'maintained' && source.data.agent_state !== 'stopped' && !['archived', 'delivered'].includes(project.state) && (session.user.is_admin || ['Admin', 'DE', 'DL', 'NE', 'SRE'].includes(project.role))
 async function collect() {
  if (!canSample || action.busy || !visible) return
  if (!attempt.current) attempt.current = crypto.randomUUID()
  const result = await action.run(() => createMonitorSSHSnapshot(project.id, { asset_id: assetId, network_only: networkOnly, reason: networkOnly ? 'On-demand port panel snapshot' : 'On-demand host monitor snapshot' }, session.csrf_token, attempt.current))
  if (result.ok) { setExecution(result.value.id); attempt.current = ''; void queries.invalidateQueries({ queryKey: key }) }
 }
 useEffect(() => { if (canSample && visible && !attempted.current) { attempted.current = true; void collect() } }, [canSample, visible])
 if (!canSample && !execution && !action.error) return null
 return <section className="ui:grid ui:gap-2" aria-label={t('monitorAgentlessTitle')}><p>{t('monitorAgentlessHelp')}</p><ActionError error={action.error} /><Button variant="outline" disabled={!canSample || action.busy} onClick={() => void collect()}>{t('monitorAgentlessCollect')}</Button>{execution && <Link to="/p/$projectId/executions" params={{ projectId: project.id }} search={{ execution, tab: 'overview' }}>{t('bmcViewExecution')}</Link>}</section>
}
