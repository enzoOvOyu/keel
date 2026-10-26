import { useEffect, useRef, useState } from 'react'
import { useNavigate } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { ActionError } from '../../components/Management'
import { useProject } from '../../components/ProjectWorkspace'
import { createAgentExecution, createSSHExecution } from '../../lib/execution'
import { useAction } from '../../lib/management'
import { useExpireOnUnauthorized, useSession } from '../../lib/session'

function object(value: unknown): Record<string, unknown> | null { return value && typeof value === 'object' && !Array.isArray(value) ? value as Record<string, unknown> : null }

export function CacheOutput({ output }: { output: unknown }) {
  const { t } = useTranslation(), project = useProject(), session = useSession(), action = useAction(), navigate = useNavigate()
  const key = useRef(crypto.randomUUID())
  const [now, setNow] = useState(Date.now())
  useExpireOnUnauthorized(action.error)
  const receipt = object(output), observed = object(receipt?.observation)
  const artifact = receipt?.operation === 'host.artifact.inventory'
  const captured = typeof observed?.collected_at === 'string' ? Date.parse(observed.collected_at) : NaN
  const expiry = captured + 30_000
  useEffect(() => {
    if (!artifact || !Number.isFinite(expiry) || expiry <= Date.now()) return
    const timer = window.setTimeout(() => setNow(Date.now()), Math.min(Math.max(1, expiry-Date.now()+1),2_147_483_647))
    return () => window.clearTimeout(timer)
  }, [artifact, expiry])
  if (!receipt || (!artifact && receipt.operation !== 'host.cache.inventory') || !observed || observed.page_content_verified !== !artifact || typeof observed.manifest_sha256 !== 'string') return null
  const manifest = observed.manifest_sha256, cursor = typeof observed.next_cursor === 'string' ? observed.next_cursor : ''
  const machine = typeof receipt.machine_id === 'string' ? receipt.machine_id : '', mode = receipt.transport
  const canQuery = !['archived', 'delivered'].includes(project.state) && (session.user.is_admin || ['Admin', 'DE', 'DL', 'SRE'].includes(project.role))
  const items = Array.isArray(observed.items) ? observed.items.map(object).filter((item): item is Record<string, unknown> => !!item) : []
  async function nextPage() {
    if (!machine || !cursor || !canQuery || (mode !== 'agent' && mode !== 'ssh')) return
    const result = await action.run(() => (mode === 'agent' ? createAgentExecution : createSSHExecution)(project.id, { machine_ids: [machine], operation: artifact ? 'host.artifact.inventory' : 'host.cache.inventory', timeout_ms: 5000, wait_ms: 0, cache_cursor: cursor, cache_manifest_sha256: manifest }, session.csrf_token, key.current))
    if (result.ok) await navigate({ to: '/p/$projectId/executions', params: { projectId: project.id }, search: { execution: result.value.id, tab: 'outputs' } })
  }
  return <div className="cache-output">
    <p>{t('cacheInventoryRegistered')}: {typeof observed.registered_count === 'number' ? observed.registered_count : '—'}</p>
    <p>{t(artifact ? 'artifactInventoryBoundary' : 'cacheInventoryPageVerified')}</p>{artifact && <p>{t(Number.isFinite(expiry) && expiry>now && expiry>Date.now() ? 'artifactInventoryRecent' : 'artifactInventoryStale')} · {Number.isFinite(captured) ? new Date(captured).toLocaleString() : '—'}</p>}<p>{t('cacheInventoryHistorical')}</p>
    <details><summary>{t('cacheInventorySnapshot')}</summary><code className="ui:break-all">{manifest}</code></details>
    <Table><TableHeader><TableRow><TableHead>SHA256</TableHead><TableHead>{t('mediaSize')}</TableHead><TableHead>{t(artifact ? 'artifactInventoryLastVerified' : 'cacheInventoryStoredAt')}</TableHead>{artifact && <><TableHead>{t('artifactInventoryCheckpoint')}</TableHead><TableHead>{t('status')}</TableHead></>}</TableRow></TableHeader><TableBody>{items.map((item, index) => <TableRow key={typeof item.sha256 === 'string' ? item.sha256 : index}><TableCell><code className="ui:break-all">{typeof item.sha256 === 'string' ? item.sha256 : '—'}</code></TableCell><TableCell>{typeof item.size === 'number' ? item.size : '—'}</TableCell><TableCell>{typeof (artifact ? item.last_verified_at : item.stored_at) === 'string' ? new Date(String(artifact ? item.last_verified_at : item.stored_at)).toLocaleString() : '—'}</TableCell>{artifact && <><TableCell>{typeof item.checkpoint_offset === 'number' ? item.checkpoint_offset : '—'}</TableCell><TableCell>{t(item.state === 'verified' ? 'artifactInventoryVerifiedHistory' : item.state === 'paused' ? 'artifactInventoryPaused' : 'artifactInventoryPartial')}</TableCell></>}</TableRow>)}</TableBody></Table>
    <ActionError error={action.error} />
    {cursor && canQuery && <Button type="button" variant="outline" disabled={action.busy || !machine || mode !== 'agent' && mode !== 'ssh'} onClick={() => void nextPage()}>{t('cacheInventoryNext')}</Button>}
  </div>
}
