import { useEffect, useState, type FormEvent } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { ProjectWorkspace, canManageProject, useProject } from '../../components/ProjectWorkspace'
import { ActionError, PermissionDenied } from '../../components/Management'
import { useAction } from '../../lib/management'
import { useSession, useExpireOnUnauthorized } from '../../lib/session'
import { listProjectAssets } from '../../lib/bom'
import {
  getRoleBindings, getRolePlan, renderConfig, saveRolePlan,
  logicalRoles, slotRoles, renderKinds,
  type RolePlan, type RoleSpec, type RenderResult,
} from '../../lib/roles'

const DEFAULT_PARAMS: Record<string, string> = {
  gpu_network: JSON.stringify({ net: { management: { subnet: '10.10.0.0/24', gateway: '10.10.0.1', mtu: 1500 }, compute: { subnet: '10.30.0.0/24', mtu: 9000 } } }, null, 2),
  nic_partition: '{}',
  gpu_nic_binding: JSON.stringify({ nccl: { gid_index: 3, socket_nthreads: 4 } }, null, 2),
  roce_import: '{}',
}

function parseJSON(value: string): Record<string, unknown> | null {
  try {
    const parsed = JSON.parse(value) as unknown
    return parsed && typeof parsed === 'object' && !Array.isArray(parsed) ? parsed as Record<string, unknown> : null
  } catch { return null }
}

function RolePlanSection({ plan, manage, onSaved }: { plan: RolePlan; manage: boolean; onSaved: (message: string) => void }) {
  const { t } = useTranslation(), session = useSession(), action = useAction()
  const [roles, setRoles] = useState<Record<string, RoleSpec>>({})
  const [validation, setValidation] = useState('')
  useEffect(() => { if (plan.roles) setRoles(plan.roles) }, [plan.roles])
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setValidation('')
    const compute = roles.compute
    if (compute && (!compute.enabled || !compute.required)) { setValidation(t('configComputeLocked')); return }
    // Saving the role scheme preserves the slot overrides: the two editors
    // are independent views of one plan.
    const result = await action.run(() => saveRolePlan(plan.project_id, { roles, slot_overrides: plan.slot_overrides }, session.csrf_token))
    if (result.ok) onSaved(t('configSaved'))
  }
  return <form className="management-panel" onSubmit={(event) => void submit(event)}>
    <ActionError error={action.error} />
    {validation && <p className="field-error" role="alert">{validation}</p>}
    <table className="data-table">
      <thead><tr>
        <th scope="col">{t('configRole')}</th><th scope="col">{t('configRoleEnabled')}</th>
        <th scope="col">{t('configRoleRequired')}</th><th scope="col">{t('configCoverage')}</th>
      </tr></thead>
      <tbody>
        {logicalRoles.map((role) => {
          const spec = roles[role] ?? { enabled: true, required: role !== 'oob' }
          const missing = plan.missing.includes(role)
          return <tr key={role}>
            <th scope="row">{t('config_role_' + role)}</th>
            <td><input type="checkbox" checked={spec.enabled} disabled={!manage || action.busy || role === 'compute'}
              aria-label={t('configRoleEnabled') + ' ' + role}
              onChange={(event) => setRoles({ ...roles, [role]: { ...spec, enabled: event.target.checked } })} /></td>
            <td><input type="checkbox" checked={spec.required} disabled={!manage || action.busy || role === 'compute'}
              aria-label={t('configRoleRequired') + ' ' + role}
              onChange={(event) => setRoles({ ...roles, [role]: { ...spec, required: event.target.checked } })} /></td>
            <td>{plan.coverage[role] ?? 0}{missing && <span className="status-label disabled"> {t('configMissing')}</span>}</td>
          </tr>
        })}
      </tbody>
    </table>
    {manage && <button type="submit" className="primary-button" disabled={action.busy}>{t('configSaveRoles')}</button>}
  </form>
}

function SlotSection({ projectId, machineId, plan, manage, onSaved }: {
  projectId: string; machineId: string; plan: RolePlan; manage: boolean; onSaved: (message: string) => void
}) {
  const { t } = useTranslation(), session = useSession(), action = useAction()
  const [overrides, setOverrides] = useState<Record<string, string>>({})
  useEffect(() => { if (plan.slot_overrides) setOverrides(plan.slot_overrides) }, [plan.slot_overrides])
  const bindings = useQuery({ enabled: machineId !== '', queryKey: ['private', session.user.id, 'project', projectId, 'bindings', machineId],
    queryFn: ({ signal }) => getRoleBindings(projectId, machineId, signal) })
  useExpireOnUnauthorized(bindings.error)
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const result = await action.run(() => saveRolePlan(projectId, { roles: plan.roles, slot_overrides: overrides }, session.csrf_token))
    if (result.ok) onSaved(t('configSaved'))
  }
  return <form className="management-panel" onSubmit={(event) => void submit(event)}>
    <ActionError error={action.error} /><ActionError error={bindings.error} />
    {bindings.isPending && <p className="table-loading" role="status">{t('loadingRecords')}</p>}
    {bindings.data && <table className="data-table">
      <thead><tr>
        <th scope="col">{t('configSlot')}</th><th scope="col">{t('configPort')}</th><th scope="col">{t('configNic')}</th>
        <th scope="col">{t('configDefaultRole')}</th><th scope="col">{t('configRole')}</th>
      </tr></thead>
      <tbody>
        {bindings.data.items.map((binding) => {
          const key = binding.slot_no > 0 ? String(binding.slot_no) : binding.port_name
          return <tr key={binding.port_id} data-overridden={binding.overridden}>
            <td>{binding.slot_no || '—'}</td>
            <td>{binding.port_name}</td>
            <td>{binding.expect_nic_model || '—'}</td>
            <td>{binding.default_role}</td>
            <td><select value={overrides[key] ?? binding.role} disabled={!manage || action.busy}
              aria-label={t('configRole') + ' ' + binding.port_name}
              onChange={(event) => setOverrides({ ...overrides, [key]: event.target.value })}>
              {slotRoles.map((role) => <option key={role} value={role}>{t('config_role_' + role)}</option>)}
            </select></td>
          </tr>
        })}
      </tbody>
    </table>}
    {manage && <button type="submit" className="primary-button" disabled={action.busy}>{t('configSaveSlots')}</button>}
  </form>
}

function RenderSection({ projectId, machineId, manage }: { projectId: string; machineId: string; manage: boolean }) {
  const { t } = useTranslation(), session = useSession(), action = useAction()
  const [kind, setKind] = useState<string>('gpu_network')
  const [params, setParams] = useState(DEFAULT_PARAMS.gpu_network)
  const [configSetId, setConfigSetId] = useState(''), [readback, setReadback] = useState('')
  const [result, setResult] = useState<RenderResult | null>(null), [validation, setValidation] = useState('')
  async function run(apply: boolean) {
    setValidation(''); setResult(null)
    const parsed = parseJSON(params)
    if (!parsed) { setValidation(t('configParamInvalid')); return }
    if (apply && configSetId.trim() === '') { setValidation(t('configApplyNeedsSet')); return }
    let readbackValue: Record<string, unknown> | undefined
    if (readback.trim() !== '') {
      readbackValue = parseJSON(readback) ?? undefined
      if (!readbackValue) { setValidation(t('configParamInvalid')); return }
    }
    const outcome = await action.run(() => renderConfig(projectId, {
      kind, machine_id: machineId,
      ...(configSetId.trim() !== '' ? { config_set_id: configSetId.trim() } : {}),
      params: parsed, ...(readbackValue ? { readback: readbackValue } : {}),
    }, apply, session.csrf_token))
    if (outcome.ok) setResult(outcome.value)
  }
  return <section className="management-panel">
    <ActionError error={action.error} />
    {validation && <p className="field-error" role="alert">{validation}</p>}
    <div className="field"><label htmlFor="render-kind">{t('configKind')}</label>
      <select id="render-kind" value={kind} disabled={action.busy} onChange={(event) => { setKind(event.target.value); setParams(DEFAULT_PARAMS[event.target.value] ?? '{}') }}>
        {renderKinds.map((value) => <option key={value} value={value}>{t('config_kind_' + value)}</option>)}
      </select></div>
    <div className="field"><label htmlFor="render-params">{t('configParams')}</label>
      <textarea id="render-params" value={params} rows={6} disabled={action.busy} onChange={(event) => setParams(event.target.value)} /></div>
    <div className="field"><label htmlFor="render-set">{t('configSetId')}</label>
      <input id="render-set" value={configSetId} disabled={action.busy} onChange={(event) => setConfigSetId(event.target.value)} /></div>
    <div className="field"><label htmlFor="render-readback">{t('configReadback')}</label>
      <textarea id="render-readback" value={readback} rows={4} disabled={action.busy} onChange={(event) => setReadback(event.target.value)} /></div>
    <div className="form-actions">
      <button type="button" className="secondary-button" disabled={action.busy || machineId === ''} onClick={() => void run(false)}>{t('configPreview')}</button>
      {manage && <button type="button" className="primary-button" disabled={action.busy || machineId === ''} onClick={() => void run(true)}>{t('configApply')}</button>}
    </div>
    {result && <div className="render-result">
      <h3>{t('configRendered')}</h3><pre>{result.rendered}</pre>
      {result.diff && <><h3>{t('configDiff')}</h3><pre>{result.diff}</pre></>}
      {result.warnings.length > 0 && <><h3>{t('configWarnings')}</h3><ul>{result.warnings.map((warning) => <li key={warning}>{warning}</li>)}</ul></>}
      {result.receipt && <><h3>{t('configReceipt')}</h3>
        <p>{result.receipt} <span className={result.verified ? 'status-label enabled' : 'status-label disabled'}>{result.verified ? t('configVerified') : t('configNotVerified')}</span></p></>}
    </div>}
  </section>
}

function ConfigContent() {
  const project = useProject()
  const { t } = useTranslation(), session = useSession(), client = useQueryClient()
  const [machineId, setMachineId] = useState(''), [success, setSuccess] = useState('')
  const manage = canManageProject(project, session.user.is_admin) && session.permissions.includes('model:manage')
  const plan = useQuery({ queryKey: ['private', session.user.id, 'project', project.id, 'roles'], queryFn: ({ signal }) => getRolePlan(project.id, signal) })
  const assets = useQuery({ queryKey: ['private', session.user.id, 'project', project.id, 'assets', 1], queryFn: ({ signal }) => listProjectAssets(project.id, 1, signal) })
  useExpireOnUnauthorized(plan.error); useExpireOnUnauthorized(assets.error)
  useEffect(() => { document.title = `${t('configTitle')} · ${project.name}` }, [t, project.name])
  useEffect(() => { if (machineId === '' && assets.data?.items.length) setMachineId(assets.data.items[0].machine_id) }, [assets.data, machineId])
  async function saved(message: string) {
    setSuccess(message)
    await client.invalidateQueries({ queryKey: ['private', session.user.id, 'project', project.id] })
  }
  if (!session.permissions.includes('model:read')) return <PermissionDenied />
  return <>
    <div className="page-heading"><div><p className="section-eyebrow">{project.name}</p><h1>{t('configTitle')}</h1><p>{t('configIntro')}</p></div></div>
    {success && <p className="impact-notice" role="status">{success}</p>}
    <section className="management-panel" aria-busy={plan.isFetching}>
      <h2>{t('configRoles')}</h2>
      {plan.isPending && <p className="table-loading" role="status">{t('loadingRecords')}</p>}
      <ActionError error={plan.error} />
      {plan.data && <>
        <select aria-label={t('configTarget')} value={machineId} onChange={(event) => setMachineId(event.target.value)}>
          {(assets.data?.items ?? []).map((asset) => <option key={asset.id} value={asset.machine_id}>{asset.hostname}</option>)}
        </select>
        <RolePlanSection plan={plan.data} manage={manage} onSaved={(message) => void saved(message)} />
      </>}
    </section>
    {plan.data && machineId !== '' && <>
      <section className="management-panel"><h2>{t('configSlots')}</h2>
        <SlotSection projectId={project.id} machineId={machineId} plan={plan.data} manage={manage} onSaved={(message) => void saved(message)} /></section>
      <section className="management-panel"><h2>{t('configRender')}</h2>
        <RenderSection projectId={project.id} machineId={machineId} manage={manage} /></section>
    </>}
  </>
}
export function ConfigPage({ projectId }: { projectId: string }) {
  return <ProjectWorkspace projectId={projectId}><ConfigContent /></ProjectWorkspace>
}
