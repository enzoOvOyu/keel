import { Input } from "@/components/ui/input"
import { NativeSelect, NativeSelectOption } from "@/components/ui/native-select"
import { ModelPanelPreview, parseModelPanel } from '../../components/ModelPanelPreview'
import { Table, TableHeader, TableRow, TableHead, TableBody, TableCell } from "@/components/ui/table"
import { Button } from "@/components/ui/button"
import { Label } from "@/components/ui/label"
import { Textarea } from "@/components/ui/textarea"
import { useEffect, useRef, useState, type FormEvent } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Workspace } from '../../components/Workspace'
import { ProjectWorkspace, canManageProject, useProject } from '../../components/ProjectWorkspace'
import { ActionDialog, ActionError, Pagination, PermissionDenied, SuccessNotice } from '../../components/Management'
import { useAction } from '../../lib/management'
import { createDeviceModel, discardModelDraft, importModelScan, listModelDrafts, listModelVersions, listProjectModels, publishModelVersion, retireDeviceModel, type DeviceModelSummary, type DeviceModelVersionDetail, type ImportScanInput, type ModelDraft, type ModelSlot } from '../../lib/models'
import { useExpireOnUnauthorized, useSession } from '../../lib/session'

const roles: ModelSlot['default_role'][] = ['management', 'storage', 'compute', 'oob', 'unused']

function useIdempotencyKey() {
  const ref = useRef<{ payload: string; key: string } | null>(null)
  return (payload: string) => {
    if (!ref.current || ref.current.payload !== payload) ref.current = { payload, key: crypto.randomUUID() }
    return ref.current.key
  }
}
function CreateModelDialog({ projectId, onClose, onDone }: { projectId: string | null; onClose: () => void; onDone: () => Promise<void> }) {
  const { t } = useTranslation(), session = useSession(), action = useAction(), keyFor = useIdempotencyKey()
  const [name, setName] = useState(''), [kind, setKind] = useState<'rack' | 'server' | 'compute-tray' | 'switch' | 'nic' | 'gpu' | 'psu' | 'bmc' | 'optic_module'>('server'), [validation, setValidation] = useState('')
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const trimmed = name.trim()
    if (!trimmed || trimmed.length > 120) { setValidation(t('nameRequired')); return }
    setValidation('')
    const body = { name: trimmed, kind }
    const result = await action.run(() => createDeviceModel(projectId, body, session.csrf_token, keyFor(JSON.stringify(body))))
    if (result.ok) await onDone()
  }
  return <ActionDialog title={t('modelsCreate')} description={t('modelsCreateDetail')} busy={action.busy} confirm={t(action.busy ? 'saving' : 'modelsCreate')} onSubmit={(event) => void submit(event)} onClose={onClose}>
    <ActionError error={action.error} />{validation && <p className="field-error" role="alert">{validation}</p>}
    <div className="field"><Label htmlFor="model-name">{t('modelName')}</Label><Input className="ui:h-11" id="model-name" value={name} onChange={(event) => setName(event.target.value)} maxLength={120} required disabled={action.busy} /></div>
    <div className="field"><Label htmlFor="model-kind">{t('modelKind')}</Label><NativeSelect className="ui:min-h-11" id="model-kind" value={kind} onChange={(event) => setKind(event.target.value as typeof kind)} disabled={action.busy}>{(['rack', 'server', 'compute-tray', 'switch', 'nic', 'gpu', 'psu', 'bmc', 'optic_module'] as const).map((value) => <NativeSelectOption key={value} value={value}>{t('model_kind_' + value.replaceAll('-', '_'))}</NativeSelectOption>)}</NativeSelect></div>
    <p className="impact-notice">{t('modelImmutableNotice')}</p>
  </ActionDialog>
}
function ImportScanDialog({ projectId, onClose, onDone }: { projectId: string | null; onClose: () => void; onDone: (message: string) => Promise<void> }) {
  const { t } = useTranslation(), session = useSession(), action = useAction(), keyFor = useIdempotencyKey()
  const [manifest, setManifest] = useState(''), [validation, setValidation] = useState('')
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    let parsed: ImportScanInput & { _warnings?: string[] }
    try { parsed = JSON.parse(manifest) as ImportScanInput } catch { setValidation(t('modelsImportInvalid')); return }
    if (typeof parsed !== 'object' || parsed === null || !parsed.source_host || typeof parsed.source_host !== 'string') { setValidation(t('modelsImportInvalid')); return }
    setValidation('')
    // Collector warnings are metadata for the operator, not import content.
    const { _warnings, ...body } = parsed
    const result = await action.run(() => importModelScan(projectId, body, session.csrf_token, keyFor(JSON.stringify(body))))
    if (result.ok) await onDone(t('modelsImportDrafted'))
  }
  const friendly = action.error && 'code' in action.error && (action.error as { code?: string }).code === 'ALREADY_CURRENT' ? t('modelsImportCurrent') : null
  return <ActionDialog title={t('modelsImport')} description={t('modelsImportDetail')} busy={action.busy} confirm={t(action.busy ? 'saving' : 'modelsImport')} onSubmit={(event) => void submit(event)} onClose={onClose}>
    {friendly ? <p className="impact-notice" role="alert">{friendly}</p> : <ActionError error={action.error} />}{validation && <p className="field-error" role="alert">{validation}</p>}
    <div className="field"><Label htmlFor="scan-manifest">{t('modelsImport')}</Label><Textarea id="scan-manifest" value={manifest} onChange={(event) => setManifest(event.target.value)} rows={10} placeholder={t('modelsImportPlaceholder')} disabled={action.busy} className="mono-value ui:field-sizing-fixed" /></div>
    <p className="impact-notice">{t('modelsImportDetail')}</p>
  </ActionDialog>
}
type SlotDraft = { slot_no: string; pcie_slot: string; expect_nic_model: string; panel_port: string; default_role: ModelSlot['default_role'] }
const emptySlot = (): SlotDraft => ({ slot_no: '', pcie_slot: '', expect_nic_model: '', panel_port: '', default_role: 'compute' })
function PublishVersionDialog({ projectId, model, draft, baseVersion, onClose, onDone }: { projectId: string | null; model: DeviceModelSummary; draft?: ModelDraft; baseVersion?: DeviceModelVersionDetail; onClose: () => void; onDone: () => Promise<void> }) {
  const { t } = useTranslation(), session = useSession(), action = useAction(), keyFor = useIdempotencyKey()
  const seed = draft ?? baseVersion
  const seeded = seed ? { spec: JSON.stringify(seed.hw_spec ?? {}, null, 2), slots: seed.slots } : null
  const [hwSpec, setHwSpec] = useState(seeded?.spec ?? '{}'), [capabilityMatrix, setCapabilityMatrix] = useState(JSON.stringify(seed?.capability_matrix ?? {}, null, 2)), [slots, setSlots] = useState<SlotDraft[]>(seeded?.slots?.map((slot) => ({ slot_no: String(slot.slot_no), pcie_slot: slot.pcie_slot, expect_nic_model: slot.expect_nic_model, panel_port: slot.panel_port, default_role: slot.default_role })) ?? (model.kind === 'server' || model.kind === 'compute-tray' ? [emptySlot()] : [])), [validation, setValidation] = useState('')
  const [extra, setExtra] = useState(() => ({
    expected_topology: JSON.stringify(seed?.expected_topology ?? {}, null, 2),
    panel_layout: JSON.stringify(seed?.panel_layout ?? {}, null, 2),
    bom_expect: JSON.stringify(seed?.bom_expect ?? {}, null, 2),
    default_role_plan: JSON.stringify(seed?.default_role_plan ?? {}, null, 2),
  }))
  const validationRef = useRef<HTMLParagraphElement>(null)
  useEffect(() => { if (validation) validationRef.current?.focus() }, [validation])
  function update(index: number, patch: Partial<SlotDraft>) {
    setSlots((current) => current.map((slot, i) => i === index ? { ...slot, ...patch } : slot))
  }
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    let parsed: Record<string, unknown>
    try { parsed = JSON.parse(hwSpec) as Record<string, unknown>; if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed)) throw new Error('invalid') } catch { setValidation(t('modelReviewInvalid')); return }
    let capability: Record<string, unknown> = {}
    const complete: Record<string, Record<string, unknown>> = {}
    try {
      capability = JSON.parse(capabilityMatrix) as Record<string, unknown>
      if (!capability || Array.isArray(capability) || typeof capability !== 'object' || (model.kind === 'switch' && !Object.keys(capability).length)) throw new Error('invalid')
      for (const [key, value] of Object.entries(extra)) {
        const parsed: unknown = JSON.parse(value)
        if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) throw new Error('invalid')
        complete[key] = parsed as Record<string, unknown>
      }
    } catch { setValidation(t('modelReviewInvalid')); return }
    const cleaned: ModelSlot[] = []
    const seen = new Set<number>()
    const aliases = new Map<string, number>()
    for (const slot of slots) {
      const no = Number(slot.slot_no)
      if (!Number.isSafeInteger(no) || no < 1 || no > 1024 || seen.has(no)) { setValidation(t('modelSlotInvalid')); return }
      seen.add(no)
      if (!slot.pcie_slot.trim() || !slot.expect_nic_model.trim() || !slot.panel_port.trim()) { setValidation(t('modelSlotInvalid')); return }
      for (const alias of [String(no), slot.pcie_slot.trim()]) {
        if (aliases.has(alias) && aliases.get(alias) !== no) { setValidation(t('modelSlotConflict', { alias })); return }
        aliases.set(alias, no)
      }
      cleaned.push({ slot_no: no, pcie_slot: slot.pcie_slot.trim(), expect_nic_model: slot.expect_nic_model.trim(), panel_port: slot.panel_port.trim(), default_role: slot.default_role })
    }
    if (parseModelPanel(complete.panel_layout, cleaned).state === 'invalid') { setValidation(t('modelPanelInvalid')); return }
    setValidation('')
    const body = { hw_spec: parsed, capability_matrix: capability, ...complete, slots: cleaned, expected_version: model.latest_version, ...(draft ? { source_draft_id: draft.id } : {}) }
    const result = await action.run(() => publishModelVersion(projectId, model.id, body, session.csrf_token, keyFor(JSON.stringify(body))))
    if (result.ok) await onDone()
  }
  return <ActionDialog title={t('modelsPublish')} description={t('modelsPublishDetail')} busy={action.busy} confirm={t(action.busy ? 'saving' : 'modelsPublish')} onSubmit={(event) => void submit(event)} onClose={onClose}>
    {action.error && 'code' in action.error && ['MODEL_VERSION_CONFLICT', 'MODEL_DRAFT_NOT_FOUND'].includes(String(action.error.code)) ? <p role="alert" className="field-error">{t(action.error.code === 'MODEL_VERSION_CONFLICT' ? 'modelPublicationConflict' : 'modelDraftGone')}</p> : <ActionError error={action.error} />}{validation && <p ref={validationRef} tabIndex={-1} className="field-error" role="alert">{validation}</p>}
    <dl className="action-target"><dt>{t('modelName')}</dt><dd>{model.name}</dd><dt>{t('recordVersion')}</dt><dd>v{model.latest_version}</dd>{draft && <><dt>{t('modelsDraftSource')}</dt><dd>{draft.source_host}</dd></>}{baseVersion && <><dt>{t('modelsRevisionSource')}</dt><dd>v{baseVersion.version} · {baseVersion.id}</dd></>}</dl>
    {baseVersion && <p className="impact-notice">{t('modelsRevisionNotice', { version: baseVersion.version, latest: model.latest_version })}</p>}
    <div className="field"><Label htmlFor="hw-spec">{t('modelHwSpec')}</Label><Textarea id="hw-spec" value={hwSpec} onChange={(event) => setHwSpec(event.target.value)} rows={4} disabled={action.busy} className="mono-value ui:field-sizing-fixed" /><span className="field-hint">{t('modelHwSpecHint')}</span></div>
    {<div className="field"><Label htmlFor="capability-matrix">{t('modelField_capability_matrix')}</Label><Textarea id="capability-matrix" value={capabilityMatrix} onChange={(event) => setCapabilityMatrix(event.target.value)} rows={6} disabled={action.busy} className="mono-value ui:field-sizing-fixed" /><span className="field-hint">{t('modelCapabilityHint')}</span></div>}
    {Object.entries(extra).map(([key, value]) => <div className="field" key={key}>
      <Label htmlFor={key.replaceAll('_', '-')}>{t('modelField_' + key)}</Label>
      <Textarea id={key.replaceAll('_', '-')} value={value} rows={3} className="mono-value" disabled={action.busy} onChange={event => setExtra(current => ({ ...current, [key]: event.target.value }))} />
    </div>)}
    <div className="field"><strong>{t('modelPanelPreview')}</strong><ModelPanelPreview value={(() => { try { return JSON.parse(extra.panel_layout) } catch { return null } })()} slots={slots.map(slot => ({ ...slot, slot_no: Number(slot.slot_no) }))} /></div>
    <p className="impact-notice">{t('modelsReviewComplete')}</p>
    {(model.kind === 'server' || model.kind === 'compute-tray' || slots.length > 0) && <div className="field"><Label>{t('modelSlots')}</Label>
      {slots.map((slot, index) => <div key={index} className="slot-row">
        <Input className="ui:h-11" aria-label={t('modelSlotNo')} type="number" min={1} max={1024} value={slot.slot_no} onChange={(event) => update(index, { slot_no: event.target.value })} disabled={action.busy} />
        <Input className="ui:h-11" aria-label={t('modelPcieSlot')} value={slot.pcie_slot} placeholder="PCIe 3" onChange={(event) => update(index, { pcie_slot: event.target.value })} maxLength={64} disabled={action.busy} />
        <Input className="ui:h-11" aria-label={t('modelNicModel')} value={slot.expect_nic_model} placeholder="CX7" onChange={(event) => update(index, { expect_nic_model: event.target.value })} maxLength={120} disabled={action.busy} />
        <Input className="ui:h-11" aria-label={t('modelPanelPort')} value={slot.panel_port} placeholder="1-2" onChange={(event) => update(index, { panel_port: event.target.value })} maxLength={64} disabled={action.busy} />
        <NativeSelect className="ui:min-h-11" aria-label={t('modelRole')} value={slot.default_role} onChange={(event) => update(index, { default_role: event.target.value as ModelSlot['default_role'] })} disabled={action.busy}>{roles.map((role) => <NativeSelectOption key={role} value={role}>{t('model_role_' + role)}</NativeSelectOption>)}</NativeSelect>
        <Button variant="outline" type="button" className="danger-text ui:min-h-11 ui:text-destructive ui:border-destructive" disabled={action.busy} onClick={() => setSlots((current) => current.filter((_, i) => i !== index))}>{t('modelsRemoveSlot')}</Button>
      </div>)}
      <Button variant="outline" type="button" className="secondary-button ui:min-h-11" disabled={action.busy} onClick={() => setSlots((current) => [...current, emptySlot()])}>{t('modelAddSlot')}</Button>
    </div>}
    <p className="impact-notice">{t('modelImmutableNotice')}</p>
  </ActionDialog>
}
function RetireModelDialog({ projectId, model, onClose, onDone }: { projectId: string | null; model: DeviceModelSummary; onClose: () => void; onDone: () => Promise<void> }) {
  const { t } = useTranslation(), session = useSession(), action = useAction()
  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const result = await action.run(() => retireDeviceModel(projectId, model.id, session.csrf_token))
    if (result.ok) await onDone()
  }
  return <ActionDialog title={t('modelsRetire')} description={t('modelsRetireDetail')} busy={action.busy} confirm={t('modelsRetire')} danger onSubmit={(event) => void submit(event)} onClose={onClose}>
    <ActionError error={action.error} />
    <dl className="action-target"><dt>{t('modelName')}</dt><dd>{model.name}</dd><dt>{t('status')}</dt><dd>{t('model_state_' + model.state)}</dd><dt>{t('recordVersion')}</dt><dd>v{model.latest_version}</dd></dl>
    <p className="impact-notice">{t('modelsRetireImpact')}</p>
  </ActionDialog>
}
function ModelVersions({ projectId, model, canManage, onRevise }: { projectId: string | null; model: DeviceModelSummary; canManage: boolean; onRevise: (version: DeviceModelVersionDetail) => void }) {
  const { t, i18n } = useTranslation(), session = useSession()
  const [page, setPage] = useState(1)
  const versions = useQuery({ queryKey: ['private', session.user.id, 'project', projectId, 'model', model.id, 'versions', page], queryFn: ({ signal }) => listModelVersions(projectId, model.id, page, signal) })
  useExpireOnUnauthorized(versions.error)
  const format = (value: string) => new Intl.DateTimeFormat(i18n.language === 'en' ? 'en-US' : 'zh-CN', { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value))
  return <div className="model-versions">
    <h2>{t('modelsVersions')}</h2>
    <ActionError error={versions.error} />
    {versions.isPending ? <p className="table-loading" role="status">{t('loadingRecords')}</p> : versions.data && <><div className="table-scroll" tabIndex={0} role="region" aria-label={t('modelsVersions')}><Table scrollContainer={false} className="data-table"><TableHeader><TableRow><TableHead className="ui:align-top ui:p-4" scope="col">{t('recordVersion')}</TableHead><TableHead className="ui:align-top ui:p-4" scope="col">{t('modelsContentHash')}</TableHead><TableHead className="ui:align-top ui:p-4" scope="col">{t('modelSlots')}</TableHead><TableHead className="ui:align-top ui:p-4" scope="col">{t('createdAt')}</TableHead><TableHead className="ui:align-top ui:p-4">{t('actions')}</TableHead></TableRow></TableHeader><TableBody>{versions.data.items.map((version) => <TableRow key={version.id}><TableCell className="ui:whitespace-normal ui:align-top ui:p-4">v{version.version}</TableCell><TableCell className="mono-value ui:whitespace-normal ui:align-top ui:p-4">{version.content_hash.slice(0, 16)}…<details><summary>{t('modelsCompletePayload')}</summary><pre>{JSON.stringify({ hw_spec: version.hw_spec, expected_topology: version.expected_topology, capability_matrix: version.capability_matrix, panel_layout: version.panel_layout, bom_expect: version.bom_expect, default_role_plan: version.default_role_plan, slots: version.slots }, null, 2)}</pre></details><details><summary>{t('modelPanelPreview')}</summary><ModelPanelPreview value={version.panel_layout} slots={version.slots} version={version.version} /></details></TableCell><TableCell className="ui:whitespace-normal ui:align-top ui:p-4">{version.slots.length}</TableCell><TableCell className="ui:whitespace-normal ui:align-top ui:p-4">{format(version.published_at)}</TableCell><TableCell className="ui:whitespace-normal ui:align-top ui:p-4">{canManage && model.state === 'active' && <Button variant="outline" size="sm" onClick={() => onRevise(version)}>{t('modelsReviseVersion', { version: version.version })}</Button>}</TableCell></TableRow>)}</TableBody></Table>{!versions.data.items.length && <p className="table-loading">{t('noRecords')}</p>}</div><Pagination offset={(page - 1) * versions.data.page_size} limit={versions.data.page_size} total={versions.data.total} busy={versions.isFetching} onChange={(offset) => setPage(offset / versions.data.page_size + 1)} /></>}
  </div>
}
function ModelDrafts({ projectId, model, canManage, onPublishDraft }: { projectId: string | null; model: DeviceModelSummary; canManage: boolean; onPublishDraft: (draft: ModelDraft) => void }) {
  const { t, i18n } = useTranslation(), session = useSession(), client = useQueryClient()
  const drafts = useQuery({ queryKey: ['private', session.user.id, 'project', projectId, 'model', model.id, 'drafts'], queryFn: ({ signal }) => listModelDrafts(projectId, model.id, signal) })
  useExpireOnUnauthorized(drafts.error)
  const [discard, setDiscard] = useState<ModelDraft | null>(null), action = useAction()
  const format = (value: string) => new Intl.DateTimeFormat(i18n.language === 'en' ? 'en-US' : 'zh-CN', { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value))
  async function confirmDiscard(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (!discard) return
    const result = await action.run(() => discardModelDraft(projectId, model.id, discard.id, session.csrf_token))
    if (result.ok) { setDiscard(null); await client.invalidateQueries({ queryKey: ['private', session.user.id, 'project', projectId, 'model', model.id, 'drafts'] }) }
  }
  return <div className="model-versions">
    <h2>{t('modelsDrafts')}</h2>
    <ActionError error={drafts.error} />
    {drafts.isPending ? <p className="table-loading" role="status">{t('loadingRecords')}</p> : drafts.data && <>
      <div className="table-scroll" tabIndex={0} role="region" aria-label={t('modelsDrafts')}><Table scrollContainer={false} className="data-table"><TableHeader><TableRow><TableHead className="ui:align-top ui:p-4" scope="col">{t('modelsDraftSource')}</TableHead><TableHead className="ui:align-top ui:p-4" scope="col">{t('modelsContentHash')}</TableHead><TableHead className="ui:align-top ui:p-4" scope="col">{t('status')}</TableHead><TableHead className="ui:align-top ui:p-4" scope="col">{t('createdAt')}</TableHead><TableHead className="ui:align-top ui:p-4" scope="col">{t('actions')}</TableHead></TableRow></TableHeader>
        <TableBody>{(drafts.data.items ?? []).map((draft) => <TableRow key={draft.id}><TableCell className="ui:whitespace-normal ui:align-top ui:p-4"><strong>{draft.source_host}</strong><span className="cell-subtitle mono-value">{draft.model_name}</span></TableCell><TableCell className="mono-value ui:whitespace-normal ui:align-top ui:p-4">{draft.content_hash.slice(0, 16)}…</TableCell><TableCell className="ui:whitespace-normal ui:align-top ui:p-4"><span className="status-label">{t('modelsDraftBadge')}</span></TableCell><TableCell className="ui:whitespace-normal ui:align-top ui:p-4">{format(draft.created_at)}</TableCell>
          <TableCell className="ui:whitespace-normal ui:align-top ui:p-4"><div className="table-actions">{canManage && model.state === 'active' && <Button className="ui:min-h-11" variant="outline" onClick={() => onPublishDraft(draft)}>{t('modelsDraftPublish')}</Button>}{canManage && <Button variant="outline" className="danger-text ui:min-h-11 ui:text-destructive ui:border-destructive" onClick={() => setDiscard(draft)}>{t('modelsDraftDiscard')}</Button>}</div></TableCell></TableRow>)}</TableBody></Table>
        {!(drafts.data.items ?? []).length && <p className="table-loading">{t('modelsDraftEmpty')}</p>}</div>
    </>}
    {discard && <ActionDialog title={t('modelsDraftDiscard')} description={t('modelsDraftDiscardDetail')} busy={action.busy} confirm={t('modelsDraftDiscard')} danger onSubmit={(event) => void confirmDiscard(event)} onClose={() => setDiscard(null)}>
      <ActionError error={action.error} />
      <dl className="action-target"><dt>{t('modelsDraftSource')}</dt><dd>{discard.source_host}</dd><dt>{t('modelsContentHash')}</dt><dd className="mono-value">{discard.content_hash.slice(0, 16)}…</dd></dl>
    </ActionDialog>}
  </div>
}
function ModelsContent({ project }: { project: ReturnType<typeof useProject> | null }) {
  const projectId = project?.id ?? null, projectName = project?.name
  const { t } = useTranslation(), session = useSession(), client = useQueryClient()
  const [page, setPage] = useState(1), [create, setCreate] = useState(false), [importing, setImporting] = useState(false), [publish, setPublish] = useState<DeviceModelSummary | null>(null), [publishFromDraft, setPublishFromDraft] = useState<{ model: DeviceModelSummary; draft: ModelDraft } | null>(null), [publishFromVersion, setPublishFromVersion] = useState<{ model: DeviceModelSummary; version: DeviceModelVersionDetail } | null>(null), [retire, setRetire] = useState<DeviceModelSummary | null>(null), [expanded, setExpanded] = useState<string | null>(null), [success, setSuccess] = useState('')
  const allowed = (projectId !== null || session.user.is_admin) && session.permissions.includes('model:read')
  const manage = (project ? canManageProject(project, session.user.is_admin) : session.user.is_admin) && session.permissions.includes('model:manage')
  const models = useQuery({ queryKey: ['private', session.user.id, 'project', projectId, 'models', page], queryFn: ({ signal }) => listProjectModels(projectId, page, signal), enabled: allowed })
  useExpireOnUnauthorized(models.error)
  const scopeName = projectName ?? t('modelsGlobalTitle')
  useEffect(() => { document.title = `${t('modelsTitle')} · ${scopeName}` }, [t, scopeName])
  async function done(message: string) {
    setCreate(false); setImporting(false); setPublish(null); setPublishFromDraft(null); setPublishFromVersion(null); setRetire(null); setSuccess(message)
    await client.invalidateQueries({ queryKey: projectId === null ? ['private', session.user.id] : ['private', session.user.id, 'project', projectId, 'models'] })
    await client.invalidateQueries({ queryKey: ['private', session.user.id, 'project', projectId, 'model'] })
  }
  if (!allowed) return <PermissionDenied />
  return <>
    <div className="page-heading"><div><p className="section-eyebrow">{scopeName}</p><h1>{t(projectId === null ? 'modelsGlobalTitle' : 'modelsTitle')}</h1><p>{t('modelsIntro')}</p></div>
      {manage && <div className="table-actions"><Button variant="default" className="primary-button ui:min-h-11" onClick={() => { setSuccess(''); setCreate(true) }}>{t('modelsCreate')}</Button><Button variant="outline" className="secondary-button ui:min-h-11" onClick={() => { setSuccess(''); setImporting(true) }}>{t('modelsImport')}</Button></div>}</div>
    {success && <SuccessNotice>{success}</SuccessNotice>}
    <section className="management-panel" aria-busy={models.isFetching}>
      <p className="field-hint">{t(projectId === null ? 'modelsReferenceGlobalNotice' : 'modelsReferenceProjectNotice')}</p>
      <ActionError error={models.error} />
      {models.isPending ? <p className="table-loading" role="status">{t('loadingRecords')}</p> : models.data && <>
        <div className="table-scroll" tabIndex={0} role="region" aria-label={t('modelsTitle')}><Table scrollContainer={false} className="data-table"><TableHeader><TableRow><TableHead className="ui:align-top ui:p-4" scope="col">{t('modelName')}</TableHead><TableHead className="ui:align-top ui:p-4" scope="col">{t('modelKind')}</TableHead><TableHead className="ui:align-top ui:p-4" scope="col">{t('status')}</TableHead><TableHead className="ui:align-top ui:p-4" scope="col">{t('recordVersion')}</TableHead><TableHead className="ui:align-top ui:p-4">{t('modelsReferenceCount')}</TableHead><TableHead className="ui:align-top ui:p-4" scope="col">{t('actions')}</TableHead></TableRow></TableHeader>
          <TableBody>{models.data.items.map((model) => <TableRow key={model.id}><TableCell className="ui:whitespace-normal ui:align-top ui:p-4"><strong>{model.name}</strong><span className="cell-subtitle mono-value">{model.scope_key === 'global' ? t(projectId === null ? 'modelsGlobalScope' : 'modelsInheritedReadOnly') : t('modelsProjectScope')}</span></TableCell>
            <TableCell className="ui:whitespace-normal ui:align-top ui:p-4">{t('model_kind_' + model.kind.replaceAll('-', '_'))}</TableCell><TableCell className="ui:whitespace-normal ui:align-top ui:p-4"><span className={`status-label ${model.state === 'active' ? 'enabled' : 'disabled'}`}>{t('model_state_' + model.state)}</span></TableCell><TableCell className="ui:whitespace-normal ui:align-top ui:p-4">v{model.latest_version}</TableCell><TableCell className="ui:whitespace-normal ui:align-top ui:p-4">{model.reference_count ?? t('modelsReferenceUnknown')}</TableCell>
            <TableCell className="ui:whitespace-normal ui:align-top ui:p-4"><div className="table-actions"><Button className="ui:min-h-11" variant="outline" onClick={() => setExpanded(expanded === model.id ? null : model.id)}>{t('modelsVersions')}</Button>
              {manage && model.scope_key === (projectId === null ? 'global' : 'project:' + projectId) && model.state === 'active' && <Button className="ui:min-h-11" variant="outline" onClick={() => { setSuccess(''); setPublish(model) }}>{t('modelsPublish')}</Button>}
              {manage && model.scope_key === (projectId === null ? 'global' : 'project:' + projectId) && model.state === 'active' && <Button variant="outline" className="danger-text ui:min-h-11 ui:text-destructive ui:border-destructive" onClick={() => { setSuccess(''); setRetire(model) }}>{t('modelsRetire')}</Button>}</div></TableCell></TableRow>)}</TableBody></Table>
          {!models.data.items.length && <p className="table-loading">{t('noRecords')}</p>}</div>
        <Pagination offset={(page - 1) * models.data.page_size} limit={models.data.page_size} total={models.data.total} busy={models.isFetching} onChange={(offset) => setPage(offset / models.data.page_size + 1)} /></>}
      {expanded && models.data && (() => { const target = models.data.items.find((m) => m.id === expanded); return target ? <>
        <ModelDrafts canManage={manage && target.scope_key === (projectId === null ? 'global' : 'project:' + projectId)} projectId={projectId} model={target} onPublishDraft={(draft) => { const owner = models.data!.items.find((m) => m.id === draft.model_id) ?? target; setPublishFromDraft({ model: owner, draft }); setExpanded(null) }} />
        <ModelVersions projectId={projectId} model={target} canManage={manage && target.scope_key === (projectId === null ? 'global' : 'project:' + projectId)} onRevise={(version) => { setSuccess(''); setPublishFromVersion({ model: target, version }) }} />
      </> : null })()}
    </section>
    {create && <CreateModelDialog projectId={projectId} onClose={() => setCreate(false)} onDone={() => done(t('modelCreated'))} />}
    {importing && <ImportScanDialog projectId={projectId} onClose={() => setImporting(false)} onDone={(message) => done(message)} />}
    {publish && <PublishVersionDialog projectId={projectId} model={publish} onClose={() => setPublish(null)} onDone={() => done(t('modelsPublished'))} />}
    {publishFromDraft && <PublishVersionDialog projectId={projectId} model={publishFromDraft.model} draft={publishFromDraft.draft} onClose={() => setPublishFromDraft(null)} onDone={() => done(t('modelsPublished'))} />}
    {publishFromVersion && <PublishVersionDialog projectId={projectId} model={publishFromVersion.model} baseVersion={publishFromVersion.version} onClose={() => setPublishFromVersion(null)} onDone={() => done(t('modelsPublished'))} />}
    {retire && <RetireModelDialog projectId={projectId} model={retire} onClose={() => setRetire(null)} onDone={() => done(t('modelsRetired'))} />}
  </>
}
export function ModelsPage({ projectId }: { projectId: string }) {
  return <ProjectWorkspace projectId={projectId}><ProjectModelsContent /></ProjectWorkspace>
}

function ProjectModelsContent() { const project = useProject(); return <ModelsContent key={project.id} project={project} /> }
export function GlobalModelsPage() { return <Workspace><ModelsContent key="global" project={null} /></Workspace> }
