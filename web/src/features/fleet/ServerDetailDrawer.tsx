import { useEffect, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { ActionError } from '../../components/Management'
import { useSession } from '../../lib/session'
import { getFleetStatus, getMappingRevisions, getMachineEvents, getPcieDiffs, getPortPanel, type MachineStatusPill } from '../../lib/fleet'

type Tab = 'ledger' | 'mapping' | 'ports' | 'pcie' | 'mon'

const tabs: Tab[] = ['ledger', 'mapping', 'ports', 'pcie', 'mon']

/**
 * ServerDetailDrawer per DESIGN-SYSTEM 4.12: a right-side slide-over with
 * five lazily loaded tabs. Opening from a status pill activates the
 * matching tab (mapping pill -> mapping tab, network pill -> port panel).
 * Esc or the mask closes it; the list context stays intact.
 */
export function ServerDetailDrawer({ projectId, machineId, hostname, serial, initialTab, onClose }: {
  projectId: string, machineId: string, hostname: string, serial: string, initialTab?: Tab, onClose: () => void
}) {
  const { t } = useTranslation(), session = useSession()
  const [tab, setTab] = useState<Tab>(initialTab ?? 'ledger')
  useEffect(() => {
    setTab(initialTab ?? 'ledger')
  }, [initialTab, machineId])
  useEffect(() => {
    const onKey = (event: KeyboardEvent) => { if (event.key === 'Escape') onClose() }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [onClose])
  useEffect(() => { document.title = `${hostname} · ${t('drawerTitle')}` }, [t, hostname])
  const enabled = (which: Tab) => tab === which
  const panel = useQuery({ queryKey: ['private', session.user.id, 'project', projectId, 'port-panel', machineId], queryFn: ({ signal }) => getPortPanel(projectId, machineId, signal), enabled: enabled('ports') })
  const mapping = useQuery({ queryKey: ['private', session.user.id, 'project', projectId, 'mappings', machineId], queryFn: ({ signal }) => getMappingRevisions(projectId, machineId, signal), enabled: enabled('mapping') })
  const pcie = useQuery({ queryKey: ['private', session.user.id, 'project', projectId, 'pcie', machineId], queryFn: ({ signal }) => getPcieDiffs(projectId, machineId, signal), enabled: enabled('pcie') })
  const mon = useQuery({ queryKey: ['private', session.user.id, 'project', projectId, 'events', machineId], queryFn: ({ signal }) => getMachineEvents(projectId, machineId, signal), enabled: enabled('mon') })
  return <>
    <div className="drawer-mask" onClick={onClose} aria-hidden="true" />
    <aside className="server-drawer" role="dialog" aria-label={`${t('drawerTitle')} ${hostname}`}>
      <header className="drawer-head">
        <div>
          <h2>{hostname}</h2>
          <p className="cell-subtitle mono-value">SN: {serial}</p>
        </div>
        <button type="button" className="secondary-button" onClick={onClose} aria-label={t('drawerClose')}>{t('drawerClose')}</button>
      </header>
      <nav className="drawer-tabs" aria-label={t('drawerTabs')}>
        {tabs.map((which) => <button key={which} type="button" className={tab === which ? 'drawer-tab active' : 'drawer-tab'} onClick={() => setTab(which)}>{t('tab_' + which)}</button>)}
      </nav>
      <div className="drawer-body">
        {tab === 'ledger' && <LedgerTab hostname={hostname} serial={serial} />}
        {tab === 'mapping' && <>
          <ActionError error={mapping.error} />
          {mapping.isPending ? <p className="table-loading" role="status">{t('loadingRecords')}</p> : mapping.data && (mapping.data.items.length === 0
            ? <p className="empty-hint">{t('mappingNone')}</p>
            : <table className="data-table"><thead><tr><th>{t('mappingSeq')}</th><th>{t('mappingOS')}</th><th>MAC</th><th>BDF</th><th>{t('mappingSwitch')}</th><th>GPU</th></tr></thead>
              <tbody>{mapping.data.items[0].entries.map((entry) => <tr key={entry.port_id}><td>#{mapping.data!.items[0].sequence}</td><td className="mono-value">{entry.os_interface}</td><td className="mono-value">{entry.mac_address}</td><td className="mono-value">{entry.bdf}</td><td>{entry.switch_port}</td><td>{entry.gpu_affinity}</td></tr>)}</tbody></table>)}
        </>}
        {tab === 'ports' && <>
          <ActionError error={panel.error} />
          {panel.isPending ? <p className="table-loading" role="status">{t('loadingRecords')}</p> : panel.data && <>
            {!panel.data.monitored && <p className="empty-hint">{t('panelNotMonitored')}</p>}
            <table className="data-table"><thead><tr><th>{t('panelPort')}</th><th>{t('panelOS')}</th><th>{t('panelLink')}</th><th>{t('panelPeer')}</th><th>{t('panelAlert')}</th></tr></thead>
              <tbody>{panel.data.ports.map((port) => <tr key={port.port_id}>
                <td className="mono-value">{port.port_name}</td>
                <td className="mono-value">{port.os_interface ?? '—'}</td>
                <td><span className={`status-label ${port.link_state === 'up' ? 'enabled' : port.link_state === 'down' ? 'disabled' : ''}`}>{t('link_' + port.link_state)}</span></td>
                <td>{port.lldp_peer ?? '—'}</td>
                <td>{port.alert_rule ? <span className="status-label disabled">{t('alert_' + port.alert_rule)}</span> : '—'}</td>
              </tr>)}</tbody></table>
          </>}
        </>}
        {tab === 'pcie' && <>
          <ActionError error={pcie.error} />
          {pcie.isPending ? <p className="table-loading" role="status">{t('loadingRecords')}</p> : pcie.data && (pcie.data.items.length === 0
            ? <p className="empty-hint">{t('pcieClean')}</p>
            : <table className="data-table"><thead><tr><th>{t('pcieDim')}</th><th>{t('pcieDetail')}</th><th>{t('pcieState')}</th></tr></thead>
              <tbody>{pcie.data.items.map((diff) => <tr key={diff.id}>
                <td>{t('pcie_dim_' + diff.dimension)}</td>
                <td className="mono-value">{diff.detail}</td>
                <td>{diff.resolved_at ? <span className="status-label enabled">{t('pcieResolved')}</span> : diff.escalated ? <span className="status-label disabled">{t('pcieEscalated')}</span> : <span className="status-label">{t('pcieOpen')}</span>}</td>
              </tr>)}</tbody></table>)}
        </>}
        {tab === 'mon' && <>
          <ActionError error={mon.error} />
          {mon.isPending ? <p className="table-loading" role="status">{t('loadingRecords')}</p> : mon.data && (mon.data.items.length === 0
            ? <p className="empty-hint">{t('monEmpty')}</p>
            : <table className="data-table"><thead><tr><th>{t('monPort')}</th><th>{t('monMetric')}</th><th>{t('monValue')}</th><th>{t('monTime')}</th></tr></thead>
              <tbody>{mon.data.items.map((event, index) => <tr key={index}><td className="mono-value">{event.port || '—'}</td><td>{event.metric}</td><td className="mono-value">{event.value}</td><td className="cell-subtitle">{new Date(event.ingested_at).toLocaleString()}</td></tr>)}</tbody></table>)}
        </>}
      </div>
    </aside>
  </>
}

function LedgerTab({ hostname, serial }: { hostname: string, serial: string }) {
  const { t } = useTranslation()
  return <dl className="kpi-grid">
    <div><dt>{t('bomHostname')}</dt><dd className="mono-value">{hostname}</dd></div>
    <div><dt>SN</dt><dd className="mono-value">{serial}</dd></div>
  </dl>
}

/** StatusPillPair per DESIGN-SYSTEM 4.12a: two compact badges, triple
 *  encoded (color + icon + short text); clicking opens the drawer on the
 *  matching tab. */
export function StatusPillPair({ pill, onOpen }: { pill: MachineStatusPill, onOpen: (tab: Tab) => void }) {
  const dataMachine = { 'data-machine': pill.machine_id }
  const { t } = useTranslation()
  const mappingClass = pill.mapping === 'ok' ? 'pill-ok' : pill.mapping === 'error' ? 'pill-error' : 'pill-unset'
  const mappingIcon = pill.mapping === 'ok' ? '✓' : pill.mapping === 'error' ? '✕' : '○'
  const networkClass = pill.network === 'ok' ? 'pill-ok' : pill.network === 'error' ? 'pill-error' : 'pill-unknown'
  const networkIcon = pill.network === 'ok' ? '✓' : pill.network === 'error' ? '✕' : '?'
  return <span className="pill-pair" {...dataMachine}>
    <button type="button" className={`status-pill ${mappingClass}`} onClick={() => onOpen('mapping')} title={t('pillMapping')} aria-label={`${t('pillMapping')}: ${t('pill_state_' + pill.mapping)}`}>
      <span aria-hidden="true">{mappingIcon}</span> {t('pillMapping')}
    </button>
    <button type="button" className={`status-pill ${networkClass}`} onClick={() => onOpen('ports')} title={t('pillNetwork')} aria-label={`${t('pillNetwork')}: ${t('pill_state_' + pill.network)}`}>
      <span aria-hidden="true">{networkIcon}</span> {t('pillNetwork')}
    </button>
  </span>
}

export type { Tab as DrawerTab }
export function useDrawerPills(projectId: string) {
  const session = useSession()
  const pills = useQuery({ queryKey: ['private', session.user.id, 'project', projectId, 'fleet-status'], queryFn: ({ signal }) => getFleetStatus(projectId, signal) })
  return pills.data?.items ?? []
}
