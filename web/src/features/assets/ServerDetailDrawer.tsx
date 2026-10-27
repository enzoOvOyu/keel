import { lazy, Suspense, useCallback, useLayoutEffect, useRef, useState, type KeyboardEvent, type ReactNode } from 'react'
import { useBlocker } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { ServerDetailDrafts } from './server-detail-drafts'
import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/tabs'
import { ActionError } from '../../components/Management'
import { useProject } from '../../components/ProjectWorkspace'
import { ApiError } from '../../lib/api'
import { getProjectAsset, type ProjectAsset } from '../../lib/bom'
import { useExpireOnUnauthorized, useSession } from '../../lib/session'

const PrecollectPanel = lazy(() => import('./PrecollectDialog'))
const MappingPanel = lazy(() => import('./MappingDialog'))
const TopologyPanel = lazy(() => import('./TopologyDialog'))
const PortPanel = lazy(() => import('../monitor/PortPanel'))
const MonitorPanel = lazy(() => import('../monitor/MonitorPanel'))
export type ServerDetailTab = 'ledger' | 'mapping' | 'ports' | 'topology' | 'monitor'
export type ServerDetailTarget = { id: string; hostname: string }
const tabLabels: Record<ServerDetailTab, string> = { ledger: 'serverLedger', mapping: 'mappingTitle', ports: 'portMonitorTitle', topology: 'topologyTitle', monitor: 'monitorDetail' }

export default function ServerDetailDrawer({ target, assets, initialTab = 'ledger', initialAssessmentId = '', onSelect, onClose }: { target: ServerDetailTarget; assets: ServerDetailTarget[]; initialTab?: ServerDetailTab; initialAssessmentId?: string; onSelect: (target: ServerDetailTarget, tab: ServerDetailTab) => void; onClose: () => void }) {
 const { t } = useTranslation(), project = useProject(), session = useSession()
 const [tab, setTab] = useState<ServerDetailTab>(initialTab), [wide, setWide] = useState(false), [busy, setBusy] = useState(false)
 const [drafts, setDrafts] = useState<Record<string, boolean>>({})
 const updateDraft = useCallback((key: string, dirty: boolean) => setDrafts(previous => previous[key] === dirty ? previous : { ...previous, [key]: dirty }), [])
 const dirty = Object.values(drafts).some(Boolean)
 function mayLeave() { return !busy && (!dirty || window.confirm(t('serverDiscardDraft'))) }
 useBlocker({ shouldBlockFn: ({ current, next }) => current.pathname !== next.pathname && (busy || dirty && !window.confirm(t('serverDiscardDraft'))), enableBeforeUnload: () => dirty || busy })
 const scroll = useRef<Partial<Record<ServerDetailTab, number>>>({})
 const query = useQuery({ queryKey: ['private', session.user.id, 'project', project.id, 'asset', target.id], queryFn: ({ signal }) => getProjectAsset(project.id, target.id, signal), retry: false, refetchOnWindowFocus: false })
 useExpireOnUnauthorized(query.error)
 const denied = query.error instanceof ApiError && [401, 403, 404].includes(query.error.status), asset = denied ? undefined : query.data
 const index = assets.findIndex(item => item.id === target.id)
 function move(offset: number) { const next = assets[index + offset]; if (index >= 0 && next && mayLeave()) onSelect(next, tab) }
 function keyboard(event: KeyboardEvent) {
  const node = event.target as HTMLElement
  if (node.closest('input, textarea, select, [contenteditable="true"], [role="tablist"], [role="dialog"] [data-server-nested]') || busy || event.altKey || event.ctrlKey || event.metaKey) return
  if (event.key === 'ArrowUp') { event.preventDefault(); move(-1) }
  if (event.key === 'ArrowDown') { event.preventDefault(); move(1) }
 }
 return <ServerDetailDrafts.Provider value={updateDraft}><Dialog open onOpenChange={open => { if (!open && mayLeave()) onClose() }}><DialogContent showCloseButton={!busy} onKeyDown={keyboard} onEscapeKeyDown={event => { if (busy) event.preventDefault() }} onInteractOutside={event => { if (busy) event.preventDefault() }} className={`ui:top-0 ui:right-0 ui:left-auto ui:h-dvh ui:max-h-dvh ui:max-w-full ui:translate-x-0 ui:translate-y-0 ui:grid-rows-[auto_minmax(0,1fr)] ui:rounded-none ${wide ? 'ui:sm:max-w-[960px]' : 'ui:sm:max-w-[720px]'}`}>
  <DialogHeader><DialogTitle>{asset?.hostname || target.hostname}</DialogTitle><DialogDescription>{t('serverDrawerHelp')}</DialogDescription><div className="action-row"><Button variant="outline" size="sm" disabled={busy || index <= 0} onClick={() => move(-1)}>{t('serverPrevious')}</Button><Button variant="outline" size="sm" disabled={busy || index < 0 || index >= assets.length - 1} onClick={() => move(1)}>{t('serverNext')}</Button><Button variant="ghost" size="sm" onClick={() => setWide(value => !value)}>{t(wide ? 'serverNarrow' : 'serverWiden')}</Button></div>{asset && <p>SN: {asset.serial_number} · {asset.model_name || '—'} · {t(`bom_state_${asset.state}`)} · {t(`trust_state_${asset.ssh_trust}`)}</p>}</DialogHeader>
  <Tabs className="ui:grid ui:min-h-0 ui:grid-rows-[auto_minmax(0,1fr)]" value={tab} onValueChange={value => { if (value !== tab && value in tabLabels && mayLeave()) setTab(value as ServerDetailTab) }}><TabsList className="ui:h-auto ui:w-full ui:flex-wrap">{(Object.keys(tabLabels) as ServerDetailTab[]).map(value => <TabsTrigger key={value} value={value} disabled={busy}>{t(tabLabels[value])}</TabsTrigger>)}</TabsList>
  <TabsContent value={tab} className="ui:min-h-0 ui:overflow-hidden"><ActionError error={query.error} />{query.isPending && <p role="status">{t('loadingRecords')}</p>}{asset && <Suspense fallback={<p role="status">{t('loadingRecords')}</p>}><ScrollPanel key={tab} tab={tab} positions={scroll.current}>
   {tab === 'ledger' && <><AssetLedger asset={asset} /><PrecollectPanel asset={asset} embedded onBusy={setBusy} onClose={onClose} /></>}
   {tab === 'mapping' && <MappingPanel asset={asset} initialAssessmentId={initialAssessmentId} embedded onBusy={setBusy} onClose={onClose} />}
   {tab === 'ports' && <PortPanel key={asset.id} assetId={asset.id} />}
   {tab === 'topology' && <TopologyPanel asset={asset} embedded onBusy={setBusy} onClose={onClose} />}
   {tab === 'monitor' && <MonitorPanel key={asset.id} assetId={asset.id} />}
  </ScrollPanel></Suspense>}</TabsContent></Tabs>
 </DialogContent></Dialog></ServerDetailDrafts.Provider>
}
function ScrollPanel({ tab, positions, children }: { tab: ServerDetailTab; positions: Partial<Record<ServerDetailTab, number>>; children: ReactNode }) {
 const node = useRef<HTMLDivElement>(null)
 useLayoutEffect(() => { const element = node.current; if (element) element.scrollTop = positions[tab] || 0; return () => { if (element) positions[tab] = element.scrollTop } }, [tab, positions])
 return <div ref={node} className="ui:h-full ui:overflow-y-auto ui:pr-1" tabIndex={0}>{children}</div>
}
function AssetLedger({ asset }: { asset: ProjectAsset }) {
 const { t } = useTranslation()
 return <section className="ui:mb-4"><h3>{t('serverLedger')}</h3><dl className="ui:grid ui:grid-cols-[auto_minmax(0,1fr)] ui:gap-x-4 ui:gap-y-2"><dt>{t('bomHostname')}</dt><dd>{asset.hostname}</dd><dt>SN</dt><dd>{asset.serial_number}</dd><dt>{t('assetModelVersion')}</dt><dd>{asset.model_name || '—'} · v{asset.model_version}</dd><dt>{t('bomRack')}</dt><dd>{asset.rack || '—'} / {asset.unit_position || '—'}</dd><dt>IP</dt><dd>{asset.ip_reference || '—'}</dd><dt>{t('trustTitle')}</dt><dd>{t(`trust_state_${asset.ssh_trust}`)} / {asset.ssh_trust_source}</dd><dt>ID</dt><dd className="ui:break-all">{asset.machine_id}</dd></dl></section>
}
