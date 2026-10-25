import { Button } from '@/components/ui/button'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { components } from '../generated/api'
import type { ModelSlot } from '../lib/models'

type PanelGrid = components['schemas']['ModelPanelGrid']
type ParsedPanel = { state: 'ready'; layout: PanelGrid } | { state: 'missing' | 'invalid' }
const object = (value: unknown): value is Record<string, unknown> => !!value && typeof value === 'object' && !Array.isArray(value)
const integer = (value: unknown, max: number): value is number => typeof value === 'number' && Number.isInteger(value) && value >= 1 && value <= max
export function parseModelPanel(value: unknown, slots: ModelSlot[]): ParsedPanel {
  if (!object(value)) return { state: 'invalid' }
  if (!Object.hasOwn(value, 'layout_v1')) return { state: 'missing' }
  const grid = value.layout_v1
  if (!object(grid) || !integer(grid.rows, 16) || !integer(grid.columns, 64) || !Array.isArray(grid.ports) || grid.ports.length < 1 || grid.ports.length > 256) return { state: 'invalid' }
  const ids = new Set<string>(), cells = new Set<string>(), declared = new Set(slots.map(slot => slot.slot_no))
  for (const port of grid.ports) {
    if (!object(port) || typeof port.id !== 'string' || !port.id || port.id !== port.id.trim() || Array.from(port.id).length > 64 || typeof port.label !== 'string' || !port.label.trim() || Array.from(port.label).length > 120 || !integer(port.row, grid.rows) || !integer(port.column, grid.columns)) return { state: 'invalid' }
    const cell = `${port.row}:${port.column}`
    if (ids.has(port.id) || cells.has(cell) || (Object.hasOwn(port, 'slot_no') && (!integer(port.slot_no, 1024) || !declared.has(port.slot_no)))) return { state: 'invalid' }
    ids.add(port.id); cells.add(cell)
  }
  return { state: 'ready', layout: grid as PanelGrid }
}

export function ModelPanelPreview({ value, slots, version }: { value: unknown; slots: ModelSlot[]; version?: number }) {
  const { t } = useTranslation(), [selected, setSelected] = useState<string | null>(null)
  const parsed = parseModelPanel(value, slots)
  if (parsed.state !== 'ready') return <p className="field-hint" data-panel-state={parsed.state}>{t(parsed.state === 'missing' ? 'modelPanelMissing' : 'modelPanelInvalid')}</p>
  const grid = parsed.layout, port = grid.ports.find(port => port.id === selected), slot = slots.find(slot => slot.slot_no === port?.slot_no)
  return <section className="model-panel-preview" aria-label={t('modelPanelPreview')} data-panel-state="ready">
    <p className="field-hint">{t('modelPanelExpectation')}{version !== undefined && ` · v${version}`}</p>
    <div className="model-panel-scroll" tabIndex={0} role="region" aria-label={t('modelPanelGrid')}>
      <div className="model-panel-grid" style={{ gridTemplateColumns: `repeat(${grid.columns}, 104px)`, gridTemplateRows: `repeat(${grid.rows}, minmax(72px, auto))` }}>
        {grid.ports.map(port => {
          const role = slots.find(slot => slot.slot_no === port.slot_no)?.default_role
          return <Button key={port.id} type="button" variant="outline" className="model-panel-port ui:h-auto ui:whitespace-normal" style={{ gridRow: port.row, gridColumn: port.column }} aria-pressed={selected === port.id} onClick={() => setSelected(port.id)} title={port.label}>
            <strong>{port.label}</strong><span>{role ? t('model_role_' + role) : t('modelPanelNoSlot')}</span>
          </Button>
        })}
      </div>
    </div>
    {port && <dl className="action-target model-panel-detail" aria-live="polite"><dt>{t('modelPanelPortId')}</dt><dd>{port.id}</dd><dt>{t('modelPanelPosition')}</dt><dd>{port.row} / {port.column}</dd><dt>{t('modelSlotNo')}</dt><dd>{slot?.slot_no ?? t('modelPanelNoSlot')}</dd>{slot && <><dt>{t('modelPcieSlot')}</dt><dd>{slot.pcie_slot}</dd><dt>{t('modelNicModel')}</dt><dd>{slot.expect_nic_model}</dd><dt>{t('modelRole')}</dt><dd>{t('model_role_' + slot.default_role)}</dd></>}</dl>}
  </section>
}
