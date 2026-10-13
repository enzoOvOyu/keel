import { useEffect, useMemo, useRef, useState, type FormEvent } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import * as echarts from 'echarts'

// DS chart contract: ECharts 5. The full entry keeps one chart registry; the
// modular entries (echarts/core + echarts/charts) duplicate the internal
// registry under Vite's bundling and crash with "not a constructor".
import { ProjectWorkspace, canManageProject, useProject } from '../../components/ProjectWorkspace'
import { ActionDialog, ActionError, PermissionDenied } from '../../components/Management'
import { useAction } from '../../lib/management'
import { useSession, useExpireOnUnauthorized } from '../../lib/session'
import { listProjectAssets } from '../../lib/bom'
import {
  finishNettestRun, getNettestAdmission, getNettestMatrix, getNettestPlatform, getNettestRun,
  listNettestRuns, listOpenNettestIssues, planNettestRun, refreshNettestPlatform,
  nettestLayers, nettestProfiles,
  type NettestMatrixCell, type NettestPair, type NettestRun, type NettestTicket,
} from '../../lib/nettest'
import { waiveGate } from '../../lib/dashboard'

/** Reads a design token at runtime so the chart follows the active theme
 *  (DS chart contract: dark via CSS variables). */
function token(name: string, fallback: string): string {
  const value = getComputedStyle(document.documentElement).getPropertyValue(name).trim()
  return value || fallback
}

function layerState(pairs: NettestPair[], layer: string, planned: number): 'pass' | 'fail' | 'pending' {
  const rows = pairs.filter((pair) => pair.layer === layer)
  if (rows.length === 0) return 'pending'
  if (rows.some((pair) => pair.state === 'fail')) return 'fail'
  return rows.length >= planned ? 'pass' : 'pending'
}

function HeatMatrix({ cells, layer }: { cells: NettestMatrixCell[]; layer: string }) {
  const { t } = useTranslation()
  const holder = useRef<HTMLDivElement>(null)
  useEffect(() => {
    if (!holder.current || cells.length === 0) return
    const chart = echarts.init(holder.current)
    const machines = [...new Set(cells.map((cell) => cell.hostname))].sort()
    const peers = [...new Set(cells.map((cell) => cell.peer_hostname))].sort()
    const text = token('--color-text-secondary', '#475569')
    const border = token('--color-border', '#e2e8f0')
    const max = Math.max(1, ...cells.map((cell) => cell.bandwidth_gbps))
    const option: Record<string, unknown> = {
      animation: false,
      grid: { left: 84, right: 24, top: 8, bottom: 56 },
      tooltip: {
        formatter: (params: { data: (string | number)[] }) => {
          const [peer, machine, bandwidth, state, passed, failed] = params.data
          return `${machine} → ${peer}<br/>${t('nettestBandwidth')}: ${bandwidth}<br/>${t('nettestPassed')} ${passed} / ${t('nettestFailed')} ${failed}${state === 'fail' ? ' ✕' : ''}`
        },
      },
      xAxis: { type: 'category', data: peers, splitArea: { show: true }, axisLabel: { color: text }, axisLine: { lineStyle: { color: border } } },
      yAxis: { type: 'category', data: machines, splitArea: { show: true }, axisLabel: { color: text }, axisLine: { lineStyle: { color: border } } },
      visualMap: {
        min: 0, max, calculable: true, orient: 'horizontal', left: 'center', bottom: 0,
        textStyle: { color: text },
        inRange: { color: [token('--color-bg-muted', '#f1f5f9'), token('--color-rail-2', '#0ea5e9'), token('--color-rail-0', '#a855f7')] },
      },
      series: [{
        type: 'heatmap', data: cells.map((cell) => [peers.indexOf(cell.peer_hostname), machines.indexOf(cell.hostname),
          Math.round(cell.bandwidth_gbps * 10) / 10, cell.state, cell.passed, cell.failed]),
        label: {
          show: true, color: text,
          formatter: (params: { data: (string | number)[] }) => (params.data[3] === 'fail' ? '✕' : String(params.data[2])),
        },
        itemStyle: { borderColor: border, borderWidth: 1 },
        emphasis: { itemStyle: { shadowBlur: 6, shadowColor: 'rgba(15,23,42,.25)' } },
      }],
    }
    chart.setOption(option as unknown as echarts.EChartsOption)
    const onResize = () => chart.resize()
    window.addEventListener('resize', onResize)
    return () => { window.removeEventListener('resize', onResize); chart.dispose() }
  }, [cells, t])
  if (cells.length === 0) return <p className="empty-hint">{t('netestPairNone')}</p>
  return <div ref={holder} className="nettest-matrix" role="img" aria-label={`${t('nettestMatrix')} ${layer}`} />
}

function PlatformSection({ projectId, manage }: { projectId: string; manage: boolean }) {
  const { t } = useTranslation(), session = useSession(), action = useAction()
  const platform = useQuery({ queryKey: ['private', session.user.id, 'project', projectId, 'nettest-platform'], queryFn: ({ signal }) => getNettestPlatform(projectId, signal) })
  useExpireOnUnauthorized(platform.error)
  const [detail, setDetail] = useState('{"switch":"sw1","counters":"pfc,ecn"}')
  const [saved, setSaved] = useState('')
  let parsed: Record<string, unknown> | null = null
  try { const value = JSON.parse(detail); if (value && typeof value === 'object' && !Array.isArray(value)) parsed = value } catch { parsed = null }
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!parsed) return
    const result = await action.run(() => refreshNettestPlatform(projectId, 'ready', parsed!, session.csrf_token))
    if (result.ok) { setSaved(t('nettestPlatformRefreshed')); await platform.refetch() }
  }
  const state = platform.data?.status ?? ''
  const stale = platform.data ? Date.parse(platform.data.refreshed_at) < Date.now() - 3600_000 : false
  return <section className="management-panel" aria-busy={platform.isFetching}>
    <h2>{t('nettestPlatform')}</h2>
    <ActionError error={platform.error} />
    {saved && <p className="impact-notice" role="status">{saved}</p>}
    <p className="gate-card-summary">
      {platform.data
        ? <span className={`status-label ${state === 'ready' && !stale ? 'enabled' : 'disabled'}`}>
            {state === 'ready' ? (stale ? t('nettestPlatformStale') : t('nettestPlatformReady')) : state}</span>
        : <span className="status-label disabled">{t('nettestPlatformNone')}</span>}
      {platform.data && <span className="cell-subtitle"> · {new Date(platform.data.refreshed_at).toLocaleString()}</span>}
    </p>
    {manage && <form onSubmit={(event) => void submit(event)}>
      <div className="field"><label htmlFor="nettest-platform-detail">{t('nettestPlatformDetail')}</label>
        <textarea id="nettest-platform-detail" value={detail} rows={2} disabled={action.busy} onChange={(event) => setDetail(event.target.value)} /></div>
      <button type="submit" className="primary-button" disabled={action.busy || !parsed}>{t('nettestPlatformRefresh')}</button>
    </form>}
  </section>
}

function PlanSection({ projectId, manage, onPlanned }: { projectId: string; manage: boolean; onPlanned: (run: NettestRun) => void }) {
  const { t } = useTranslation(), session = useSession(), action = useAction()
  const assets = useQuery({ queryKey: ['private', session.user.id, 'project', projectId, 'assets', 1], queryFn: ({ signal }) => listProjectAssets(projectId, 1, signal) })
  useExpireOnUnauthorized(assets.error)
  const [targets, setTargets] = useState<string[]>([]), [profile, setProfile] = useState<string>('quick')
  const [validation, setValidation] = useState('')
  useEffect(() => {
    if (targets.length === 0 && assets.data?.items.length) setTargets(assets.data.items.map((asset) => asset.machine_id))
  }, [assets.data, targets.length])
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setValidation('')
    if (targets.length === 0) { setValidation(t('nettestTargetsRequired')); return }
    const result = await action.run(() => planNettestRun(projectId, { machine_ids: targets, profile }, session.csrf_token, crypto.randomUUID()))
    if (result.ok) onPlanned(result.value)
  }
  return <section className="management-panel">
    <h2>{t('nettestPlan')}</h2>
    <ActionError error={action.error} />
    {validation && <p className="field-error" role="alert">{validation}</p>}
    {assets.isPending && <p className="table-loading" role="status">{t('loadingRecords')}</p>}
    {assets.data && <form onSubmit={(event) => void submit(event)}>
      <fieldset className="nettest-targets" disabled={!manage || action.busy}>
        <legend>{t('nettestTargets')}</legend>
        <p className="field-hint">{t('nettestTargetsHint')}</p>
        {assets.data.items.map((asset) => <label key={asset.machine_id} className="nettest-target">
          <input type="checkbox" checked={targets.includes(asset.machine_id)}
            onChange={(event) => setTargets(event.target.checked ? [...targets, asset.machine_id] : targets.filter((id) => id !== asset.machine_id))} />
          <span>{asset.hostname}</span>
        </label>)}
      </fieldset>
      <div className="field"><label htmlFor="nettest-profile">{t('nettestProfile')}</label>
        <select id="nettest-profile" value={profile} disabled={!manage || action.busy} onChange={(event) => setProfile(event.target.value)}>
          {nettestProfiles.map((value) => <option key={value} value={value}>{t('nettestProfile' + value[0].toUpperCase() + value.slice(1))}</option>)}
        </select></div>
      {manage && <button type="submit" className="primary-button" disabled={action.busy}>{t('nettestStart')}</button>}
    </form>}
  </section>
}

function LayerProgress({ run, pairs }: { run: NettestRun; pairs: NettestPair[] }) {
  const { t } = useTranslation()
  const coverage = (run.coverage ?? {}) as Record<string, { planned?: number; measured?: number; passed?: number; failed?: number }>
  return <div className="nettest-layers" role="list">
    {nettestLayers.map((layer) => {
      const plan = run.layers.find((entry) => entry.layer === layer)
      const entry = coverage[layer] ?? {}
      const planned = entry.planned ?? plan?.pairs ?? 0
      const measured = entry.measured ?? 0
      const state = layerState(pairs, layer, planned)
      const icon = state === 'pass' ? '✓' : state === 'fail' ? '✕' : '○'
      return <div key={layer} className="nettest-layer" role="listitem" data-state={state}>
        <span className={`nettest-layer-icon ${state}`}>{icon}</span>
        <span className="nettest-layer-name">{t('nettestLayer' + layer)}</span>
        <span className="cell-subtitle">{measured}/{planned}</span>
        <div className="nettest-layer-bar" aria-hidden="true">
          <div className="nettest-layer-fill pass" style={{ width: `${planned ? Math.min(100, ((entry.passed ?? 0) * 100) / planned) : 0}%` }} />
          <div className="nettest-layer-fill fail" style={{ width: `${planned ? Math.min(100, ((entry.failed ?? 0) * 100) / planned) : 0}%` }} />
        </div>
      </div>
    })}
  </div>
}

function PrecheckChips({ run }: { run: NettestRun }) {
  const { t } = useTranslation()
  return <p className="nettest-precheck">
    <strong>{t('nettestPrecheck')}: </strong>
    {(run.precheck ?? []).map((check) => <span key={check.check} className={`status-label ${check.state === 'pass' ? 'enabled' : check.state === 'waived' ? '' : 'disabled'}`} title={check.detail}>
      {t('nettestCheck' + check.check[0].toUpperCase() + check.check.slice(1))} {t('nettestPrecheck' + check.state[0].toUpperCase() + check.state.slice(1))}
    </span>)}
  </p>
}

function WaiveDialog({ projectId, onClose, onDone }: { projectId: string; onClose: () => void; onDone: (message: string) => Promise<void> }) {
  const { t } = useTranslation(), session = useSession(), action = useAction()
  const [reason, setReason] = useState(''), [hours, setHours] = useState(24), [validation, setValidation] = useState('')
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setValidation('')
    if (reason.trim().length < 4) { setValidation(t('waiveReasonRequired')); return }
    const result = await action.run(() => waiveGate(projectId, 'G5', [], reason.trim(), '', hours, session.csrf_token, crypto.randomUUID()))
    if (result.ok) await onDone(t('waiveDone', { gate: 'G5' }))
  }
  return <ActionDialog title={t('waiveTitle', { gate: 'G5' })} description={t('waiveDetail')} busy={action.busy} confirm={t('waiveRun')} onSubmit={(event) => void submit(event)} onClose={onClose}>
    <ActionError error={action.error} />{validation && <p className="field-error" role="alert">{validation}</p>}
    <div className="field"><label htmlFor="nettest-waive-reason">{t('waiveReason')}</label>
      <textarea id="nettest-waive-reason" value={reason} rows={4} disabled={action.busy} onChange={(event) => setReason(event.target.value)} /></div>
    <div className="field"><label htmlFor="nettest-waive-hours">{t('waiveHours')}</label>
      <input id="nettest-waive-hours" type="number" min={1} max={168} value={hours} disabled={action.busy} onChange={(event) => setHours(Number(event.target.value))} /></div>
  </ActionDialog>
}

function exportCSV(run: NettestRun, pairs: NettestPair[]) {
  const header = ['layer', 'machine', 'port', 'peer_machine', 'peer_port', 'state', 'bandwidth_gbps', 'loss_ppm', 'pfc_pause', 'classification', 'measured_at']
  const rows = pairs.map((pair) => [pair.layer, pair.hostname ?? pair.machine_id, pair.port_name ?? pair.port_id,
    pair.peer_hostname ?? pair.peer_machine_id, pair.peer_port_name ?? '', pair.state,
    String(pair.bandwidth_gbps), String(pair.loss_ppm), String(pair.pfc_pause), pair.classification ?? '', pair.measured_at ?? ''])
  const csv = [header, ...rows].map((row) => row.map((cell) => `"${String(cell).replace(/"/g, '""')}"`).join(',')).join('\n')
  const url = URL.createObjectURL(new Blob([`\uFEFF${csv}`], { type: 'text/csv;charset=utf-8' }))
  const anchor = document.createElement('a')
  anchor.href = url
  anchor.download = `nettest-${run.id}.csv`
  anchor.click()
  URL.revokeObjectURL(url)
}

function RunDetail({ projectId, run, manage }: { projectId: string; run: NettestRun; manage: boolean }) {
  const { t } = useTranslation(), session = useSession(), client = useQueryClient(), action = useAction()
  const detail = useQuery({ queryKey: ['private', session.user.id, 'project', projectId, 'nettest-run', run.id], queryFn: ({ signal }) => getNettestRun(projectId, run.id, signal) })
  const tickets = useQuery({ queryKey: ['private', session.user.id, 'project', projectId, 'nettest-tickets', run.id], queryFn: ({ signal }) => listOpenNettestIssues(projectId, signal) })
  const pairs = detail.data?.pairs ?? []
  const meshLayers = run.layers.filter((entry) => entry.kind === 'mesh').map((entry) => entry.layer)
  const [layer, setLayer] = useState(meshLayers[0] ?? 'L3')
  const matrix = useQuery({ enabled: meshLayers.includes(layer), queryKey: ['private', session.user.id, 'project', projectId, 'nettest-matrix', run.id, layer], queryFn: ({ signal }) => getNettestMatrix(projectId, run.id, layer, signal) })
  const [waiving, setWaiving] = useState(false), [success, setSuccess] = useState('')
  useExpireOnUnauthorized(detail.error); useExpireOnUnauthorized(tickets.error); useExpireOnUnauthorized(matrix.error)
  const runTickets = useMemo(
    () => (tickets.data?.items ?? []).filter((ticket) => pairs.some((pair) => pair.id === ticket.difference_id)),
    [tickets.data, pairs])
  const coverage = (run.coverage ?? {}) as Record<string, unknown>
  async function finish() {
    const result = await action.run(() => finishNettestRun(projectId, run.id, session.csrf_token, crypto.randomUUID()))
    if (result.ok) {
      await Promise.all([
        detail.refetch(), tickets.refetch(),
        client.invalidateQueries({ queryKey: ['private', session.user.id, 'project', projectId, 'nettest-runs'] }),
        client.invalidateQueries({ queryKey: ['private', session.user.id, 'project', projectId, 'dashboard'] }),
      ])
    }
  }
  async function rerun(ticket: NettestTicket) {
    const result = await action.run(() => planNettestRun(projectId, { machine_ids: [run.targets[0]], profile: run.profile, issue_id: ticket.id }, session.csrf_token, crypto.randomUUID()))
    if (result.ok) {
      setSuccess(t('nettestRerunStarted'))
      await client.invalidateQueries({ queryKey: ['private', session.user.id, 'project', projectId, 'nettest-runs'] })
    }
  }
  return <section className="management-panel" aria-busy={detail.isFetching}>
    <div className="nettest-run-head">
      <h2>{t('nettestRun')} {run.id.slice(0, 8)}</h2>
      <span className={`status-label ${run.conclusion === 'PASS' ? 'enabled' : run.conclusion === 'FAIL' ? 'disabled' : ''}`}>
        {run.conclusion === 'PASS' ? t('nettestConclusionPass') : run.conclusion === 'FAIL' ? t('nettestConclusionFail') : t('nettestConclusionNone')}
      </span>
      <span className="status-label">{t('nettestState')}: {t('nettestState' + run.state[0].toUpperCase() + run.state.slice(1))}</span>
    </div>
    <PrecheckChips run={run} />
    <ActionError error={action.error} />{success && <p className="impact-notice" role="status">{success}</p>}
    <h3>{t('nettestLayers')}</h3>
    <LayerProgress run={run} pairs={pairs} />
    <p className="gate-card-summary">
      {t('nettestCoverage')}: {coverage.complete === true
        ? <span className="status-label enabled">{t('nettestComplete')}</span>
        : <span className="status-label disabled">{t('nettestIncomplete')}</span>}
      {coverage.sampled === true && <span className="status-label">{t('nettestSampled')}</span>}
      {Boolean(coverage.g5_evidence) && <span className="status-label enabled">{t('nettestG5Evidence')}</span>}
    </p>
    <div className="form-actions">
      {manage && run.state !== 'finished' && <button type="button" className="primary-button" disabled={action.busy} onClick={() => void finish()}>{t('nettestFinish')}</button>}
      <button type="button" className="secondary-button" disabled={pairs.length === 0} onClick={() => exportCSV(run, pairs)}>{t('nettestExport')}</button>
      {manage && runTickets.map((ticket) => <button key={ticket.id} type="button" className="secondary-button" disabled={action.busy} onClick={() => void rerun(ticket)}>
        {t('nettestRerunFailed')} · {ticket.category} {ticket.local_port}</button>)}
      {manage && <button type="button" className="secondary-button" onClick={() => setWaiving(true)}>{t('waiveButton')}</button>}
    </div>
    {meshLayers.length > 0 && <>
      <h3>{t('nettestMatrix')}</h3>
      <div className="field"><label htmlFor="nettest-matrix-layer">{t('nettestMatrixLayer')}</label>
        <select id="nettest-matrix-layer" value={layer} onChange={(event) => setLayer(event.target.value)}>
          {meshLayers.map((value) => <option key={value} value={value}>{t('nettestLayer' + value)}</option>)}
        </select></div>
      <p className="field-hint">{t('nettestMatrixThroughput')}</p>
      {matrix.isPending && <p className="table-loading" role="status">{t('loadingRecords')}</p>}
      {matrix.data && <HeatMatrix cells={matrix.data.items} layer={layer} />}
    </>}
    <h3>{t('nettestPairs')}</h3>
    {detail.isPending && <p className="table-loading" role="status">{t('loadingRecords')}</p>}
    {pairs.length === 0 && !detail.isPending && <p className="empty-hint">{t('netestPairNone')}</p>}
    {pairs.length > 0 && <table className="data-table">
      <thead><tr>
        <th scope="col">{t('nettestMatrixLayer')}</th><th scope="col">{t('nettestPath')}</th><th scope="col">{t('nettestState')}</th>
        <th scope="col">{t('nettestBandwidth')}</th><th scope="col">{t('nettestLoss')}</th><th scope="col">{t('nettestPfc')}</th><th scope="col">{t('nettestClassification')}</th>
      </tr></thead>
      <tbody>{pairs.map((pair) => <tr key={pair.id}>
        <td>{pair.layer}</td>
        <td className="mono-value">{(pair.hostname ?? pair.machine_id)}:{pair.port_name ?? pair.port_id} → {(pair.peer_hostname ?? pair.peer_machine_id)}:{pair.peer_port_name || '—'}</td>
        <td><span className={`status-label ${pair.state === 'pass' ? 'enabled' : 'disabled'}`}>{pair.state === 'pass' ? t('nettestPrecheckPass') : t('nettestPrecheckFail')}</span></td>
        <td className="mono-value">{pair.bandwidth_gbps || '—'}</td>
        <td className="mono-value">{pair.loss_ppm || '—'}</td>
        <td className="mono-value">{pair.pfc_pause || '—'}</td>
        <td>{pair.classification ? <span className="status-label disabled">{pair.classification}</span> : pair.state === 'fail' ? t('nettestUnclassified') : '—'}</td>
      </tr>)}</tbody>
    </table>}
    {waiving && <WaiveDialog projectId={projectId} onClose={() => setWaiving(false)} onDone={async (message) => { setWaiving(false); setSuccess(message) }} />}
  </section>
}

function AdmissionSection({ projectId, machineIds }: { projectId: string; machineIds: string[] }) {
  const { t } = useTranslation(), session = useSession()
  const admission = useQuery({ enabled: machineIds.length > 0, queryKey: ['private', session.user.id, 'project', projectId, 'nettest-admission', machineIds.join(',')], queryFn: ({ signal }) => getNettestAdmission(projectId, machineIds, signal) })
  useExpireOnUnauthorized(admission.error)
  if (machineIds.length === 0) return null
  return <section className="management-panel" aria-busy={admission.isFetching}>
    <h2>{t('nettestAdmission')}</h2>
    <ActionError error={admission.error} />
    {admission.isPending && <p className="table-loading" role="status">{t('loadingRecords')}</p>}
    {admission.data && <table className="data-table">
      <thead><tr><th scope="col">{t('nettestHost')}</th><th scope="col">{t('nettestState')}</th><th scope="col">{t('nettestConclusion')}</th></tr></thead>
      <tbody>{admission.data.items.map((item) => <tr key={item.machine_id}>
        <td>{item.hostname}</td>
        <td><span className={`status-label ${item.net_verified && !item.waived ? 'enabled' : item.net_verified ? '' : 'disabled'}`}>
          {item.net_verified ? (item.waived ? t('nettestAdmissionWaived') : t('nettestAdmissionOk')) : t('nettestAdmissionBlocked')}</span></td>
        <td className="cell-subtitle">{item.reason}</td>
      </tr>)}</tbody>
    </table>}
  </section>
}

function NettestContent() {
  const project = useProject()
  const { t } = useTranslation(), session = useSession()
  const [selected, setSelected] = useState('')
  const manage = canManageProject(project, session.user.is_admin) && session.permissions.includes('model:manage')
  const runs = useQuery({ queryKey: ['private', session.user.id, 'project', project.id, 'nettest-runs'], queryFn: ({ signal }) => listNettestRuns(project.id, signal) })
  const assets = useQuery({ queryKey: ['private', session.user.id, 'project', project.id, 'assets', 1], queryFn: ({ signal }) => listProjectAssets(project.id, 1, signal) })
  useExpireOnUnauthorized(runs.error); useExpireOnUnauthorized(assets.error)
  useEffect(() => { document.title = `${t('nettestTitle')} · ${project.name}` }, [t, project.name])
  const machineIds = (assets.data?.items ?? []).map((asset) => asset.machine_id)
  const run = (runs.data?.items ?? []).find((entry) => entry.id === selected) ?? null
  if (!session.permissions.includes('model:read')) return <PermissionDenied />
  return <>
    <div className="page-heading"><div><p className="section-eyebrow">{project.name}</p><h1>{t('nettestTitle')}</h1><p>{t('nettestIntro')}</p></div></div>
    <PlatformSection projectId={project.id} manage={manage} />
    {manage && <PlanSection projectId={project.id} manage={manage} onPlanned={(created) => { setSelected(created.id); void runs.refetch() }} />}
    <section className="management-panel" aria-busy={runs.isFetching}>
      <h2>{t('nettestRuns')}</h2>
      <ActionError error={runs.error} />
      {runs.isPending && <p className="table-loading" role="status">{t('loadingRecords')}</p>}
      {runs.data && runs.data.items.length === 0 && <p className="empty-hint">{t('nettestNoRuns')}</p>}
      {runs.data && runs.data.items.length > 0 && <table className="data-table">
        <thead><tr>
          <th scope="col">{t('nettestRun')}</th><th scope="col">{t('nettestProfile')}</th><th scope="col">{t('nettestState')}</th>
          <th scope="col">{t('nettestLayers')}</th><th scope="col">{t('nettestConclusion')}</th><th scope="col">{t('nettestCreatedAt')}</th>
        </tr></thead>
        <tbody>{runs.data.items.map((entry) => <tr key={entry.id} aria-selected={entry.id === selected}>
          <td><button type="button" className="link-button" onClick={() => setSelected(entry.id === selected ? '' : entry.id)}>{entry.id.slice(0, 8)}</button></td>
          <td>{t('nettestProfile' + entry.profile[0].toUpperCase() + entry.profile.slice(1))}</td>
          <td>{t('nettestState' + entry.state[0].toUpperCase() + entry.state.slice(1))}</td>
          <td className="nettest-run-layers">
            {nettestLayers.map((layerName) => {
              const plan = entry.layers.find((item) => item.layer === layerName)
              const covered = (entry.coverage ?? {})[layerName] as { coverage?: number } | undefined
              const ratio = covered?.coverage ?? 0
              return <span key={layerName} className={`nettest-chip ${ratio >= 1 ? 'enabled' : ratio > 0 ? 'partial' : ''}`} title={`${layerName}: ${plan?.pairs ?? 0}`}>{layerName}</span>
            })}
          </td>
          <td>{entry.conclusion === 'PASS' ? t('nettestConclusionPass') : entry.conclusion === 'FAIL' ? t('nettestConclusionFail') : entry.state === 'finished' ? t('nettestConclusionNone') : '—'}</td>
          <td className="cell-subtitle">{new Date(entry.created_at).toLocaleString()}</td>
        </tr>)}</tbody>
      </table>}
    </section>
    {run && <RunDetail projectId={project.id} run={run} manage={manage} />}
    <AdmissionSection projectId={project.id} machineIds={machineIds} />
  </>
}
export function NettestPage({ projectId }: { projectId: string }) {
  return <ProjectWorkspace projectId={projectId}><NettestContent /></ProjectWorkspace>
}
