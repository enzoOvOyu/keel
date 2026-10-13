import { useEffect } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { ProjectWorkspace, canManageProject, useProject } from '../../components/ProjectWorkspace'
import { ActionDialog, ActionError, PermissionDenied } from '../../components/Management'
import { useState, type FormEvent } from 'react'
import { useAction } from '../../lib/management'
import { getGateDashboard, waiveGate, type GateCard, type GateLayerRate } from '../../lib/dashboard'
import { useSession } from '../../lib/session'
import { useExpireOnUnauthorized } from '../../lib/session'

const gateOrder = ['G1', 'G2', 'G3', 'G4', 'G5', 'G6'] as const
type GateKey = (typeof gateOrder)[number]

function WaiveDialog({ projectId, gate, onClose, onDone }: { projectId: string; gate: GateKey; onClose: () => void; onDone: (message: string) => Promise<void> }) {
  const { t } = useTranslation(), session = useSession(), action = useAction()
  const [reason, setReason] = useState(''), [scope, setScope] = useState(''), [hours, setHours] = useState(24), [validation, setValidation] = useState('')
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setValidation('')
    if (reason.trim().length < 4) { setValidation(t('waiveReasonRequired')); return }
    const result = await action.run(() => waiveGate(projectId, gate, [], reason.trim(), scope.trim(), hours, session.csrf_token, crypto.randomUUID()))
    if (result.ok) await onDone(t('waiveDone', { gate }))
  }
  return <ActionDialog title={t('waiveTitle', { gate })} description={t('waiveDetail')} busy={action.busy} confirm={t('waiveRun')} onSubmit={(event) => void submit(event)} onClose={onClose}>
    <ActionError error={action.error} />{validation && <p className="field-error" role="alert">{validation}</p>}
    <div className="field"><label htmlFor="waive-reason">{t('waiveReason')}</label><textarea id="waive-reason" value={reason} onChange={(event) => setReason(event.target.value)} rows={4} disabled={action.busy} /></div>
    <div className="field"><label htmlFor="waive-scope">{t('waiveScope')}</label><input id="waive-scope" value={scope} onChange={(event) => setScope(event.target.value)} maxLength={200} disabled={action.busy} /></div>
    <div className="field"><label htmlFor="waive-hours">{t('waiveHours')}</label><input id="waive-hours" type="number" min={1} max={168} value={hours} onChange={(event) => setHours(Number(event.target.value))} disabled={action.busy} /></div>
  </ActionDialog>
}

function LayerBars({ layers }: { layers: GateLayerRate[] }) {
  const { t } = useTranslation()
  return <div className="gate-layer-bars" role="img" aria-label={t('nettestG5Layers')}>
    {layers.map((layer) => <div key={layer.layer} className="gate-layer-row">
      <span className="gate-layer-name">{layer.layer}</span>
      <div className="gate-layer-track">
        <div className="gate-layer-fill" data-state={layer.failed > 0 ? 'fail' : layer.rate >= 100 ? 'pass' : 'partial'}
          style={{ width: `${Math.max(layer.rate, layer.failed > 0 ? 4 : 0)}%` }} />
      </div>
      <span className="gate-layer-rate">{layer.rate}%</span>
      <span className="cell-subtitle">{layer.passed}/{layer.planned}</span>
    </div>)}
  </div>
}

function CardRing({ rate }: { rate: number }) {
  const degrees = Math.round(rate * 3.6)
  return <div className="gate-ring" aria-hidden="true"><div className="gate-ring-fill" style={{ background: `conic-gradient(currentColor ${degrees}deg, var(--ring-track, #d8d8d8) ${degrees}deg)` }} /><span className="gate-ring-text">{rate}%</span></div>
}

function DashboardContent() {
  const project = useProject()
  const { t } = useTranslation(), session = useSession()
  const [waiving, setWaiving] = useState<GateKey | null>(null), [success, setSuccess] = useState('')
  const manage = canManageProject(project, session.user.is_admin) && session.permissions.includes('model:manage')
  const board = useQuery({ queryKey: ['private', session.user.id, 'project', project.id, 'dashboard'], queryFn: ({ signal }) => getGateDashboard(project.id, signal) })
  useExpireOnUnauthorized(board.error)
  useEffect(() => { document.title = `${t('dashboardTitle')} · ${project.name}` }, [t, project.name])
  async function waiveDone(message: string) { setWaiving(null); setSuccess(message) }
  if (!session.permissions.includes('model:read')) return <PermissionDenied />
  return <>
    <div className="page-heading"><div><p className="section-eyebrow">{project.name}</p><h1>{t('dashboardTitle')}</h1><p>{t('dashboardIntro')}</p></div></div>
    {success && <p className="impact-notice" role="status">{success}</p>}
    <section className="management-panel" aria-busy={board.isFetching}>
      <ActionError error={board.error} />
      {board.isPending ? <p className="table-loading" role="status">{t('loadingRecords')}</p> : board.data && <>
        <div className="gate-board" role="list" aria-label={t('gateBoard')}>
          {gateOrder.map((gate) => {
            const card: GateCard = board.data!.cards[gate]
            return <div key={gate} className="gate-card" role="listitem" data-state={card.state}>
              <div className="gate-card-head"><h2>{gate}</h2><span className={`status-label ${card.state === 'pass' ? 'enabled' : card.state === 'fail' ? 'disabled' : ''}`}>{t('gate_state_' + card.state)}</span></div>
              <p className="gate-card-title">{card.title}</p>
              <p className="gate-card-summary">{card.summary}</p>
              <CardRing rate={card.pass_rate} />
              {card.layers && card.layers.length > 0 && <LayerBars layers={card.layers} />}
              {manage && card.state !== 'not_implemented' && <button type="button" className="secondary-button gate-waive" onClick={() => { setSuccess(''); setWaiving(gate) }}>{t('waiveButton')}</button>}
            </div>
          })}
        </div>
        <h2 className="kpi-heading">{t('kpiTitle')}</h2>
        <dl className="kpi-grid">
          <div><dt>{t('kpiAssets')}</dt><dd>{board.data!.kpis.assets_total}</dd></div>
          <div><dt>{t('kpiTrust')}</dt><dd>{board.data!.kpis.trust_established} / {board.data!.kpis.assets_total} {t('kpiTrustUnit')} <span className="cell-subtitle">{t('kpiTrustBreakdown', { unverified: board.data!.kpis.trust_unverified, revoked: board.data!.kpis.trust_revoked })}</span></dd></div>
          <div><dt>{t('kpiIssues')}</dt><dd>{board.data!.kpis.open_issues} <span className="cell-subtitle">{t('kpiIssuesRepair', { count: board.data!.kpis.in_repair_issues })}</span></dd></div>
          <div><dt>{t('kpiDiffs')}</dt><dd>{board.data!.kpis.lldp_open_diffs}</dd></div>
        </dl>
      </>}
    </section>
    {waiving && <WaiveDialog projectId={project.id} gate={waiving} onClose={() => setWaiving(null)} onDone={(message) => waiveDone(message)} />}
  </>
}
export function DashboardPage({ projectId }: { projectId: string }) {
  return <ProjectWorkspace projectId={projectId}><DashboardContent /></ProjectWorkspace>
}
