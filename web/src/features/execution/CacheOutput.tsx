import { useRef } from 'react'
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
  useExpireOnUnauthorized(action.error)
  const receipt = object(output), observed = object(receipt?.observation)
  if (receipt?.operation !== 'host.cache.inventory' || !observed || observed.page_content_verified !== true || typeof observed.manifest_sha256 !== 'string') return null
  const manifest = observed.manifest_sha256, cursor = typeof observed.next_cursor === 'string' ? observed.next_cursor : ''
  const machine = typeof receipt.machine_id === 'string' ? receipt.machine_id : '', mode = receipt.transport
  const canQuery = !['archived', 'delivered'].includes(project.state) && (session.user.is_admin || ['Admin', 'DE', 'DL', 'SRE'].includes(project.role))
  const items = Array.isArray(observed.items) ? observed.items.map(object).filter((item): item is Record<string, unknown> => !!item) : []
  async function nextPage() {
    if (!machine || !cursor || !canQuery || (mode !== 'agent' && mode !== 'ssh')) return
    const result = await action.run(() => (mode === 'agent' ? createAgentExecution : createSSHExecution)(project.id, { machine_ids: [machine], operation: 'host.cache.inventory', timeout_ms: 5000, wait_ms: 0, cache_cursor: cursor, cache_manifest_sha256: manifest }, session.csrf_token, key.current))
    if (result.ok) await navigate({ to: '/p/$projectId/executions', params: { projectId: project.id }, search: { execution: result.value.id, tab: 'outputs' } })
  }
  return <div className="cache-output">
    <p>{t('cacheInventoryRegistered')}: {typeof observed.registered_count === 'number' ? observed.registered_count : '—'}</p>
    <p>{t('cacheInventoryPageVerified')}</p><p>{t('cacheInventoryHistorical')}</p>
    <details><summary>{t('cacheInventorySnapshot')}</summary><code className="ui:break-all">{manifest}</code></details>
    <Table><TableHeader><TableRow><TableHead>SHA256</TableHead><TableHead>{t('mediaSize')}</TableHead><TableHead>{t('cacheInventoryStoredAt')}</TableHead></TableRow></TableHeader><TableBody>{items.map((item, index) => <TableRow key={typeof item.sha256 === 'string' ? item.sha256 : index}><TableCell><code className="ui:break-all">{typeof item.sha256 === 'string' ? item.sha256 : '—'}</code></TableCell><TableCell>{typeof item.size === 'number' ? item.size : '—'}</TableCell><TableCell>{typeof item.stored_at === 'string' ? new Date(item.stored_at).toLocaleString() : '—'}</TableCell></TableRow>)}</TableBody></Table>
    <ActionError error={action.error} />
    {cursor && canQuery && <Button type="button" variant="outline" disabled={action.busy || !machine || mode !== 'agent' && mode !== 'ssh'} onClick={() => void nextPage()}>{t('cacheInventoryNext')}</Button>}
  </div>
}
