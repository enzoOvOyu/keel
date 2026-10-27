import { useTranslation } from 'react-i18next'
import type { TopologySnapshot } from '../../lib/topology'

/** Peer edges come from the retained nvidia-smi affinity matrix, never PCI BDF. */
export default function NVLinkGraph({ record }: { record: TopologySnapshot }) {
 const { t } = useTranslation(), capture = record.snapshot
 if (!capture) return null
 const gpus = capture.devices.filter(device => device.kind === 'gpu')
 if (!gpus.length) return <p>{t('topologyUnknown')}</p>
 const position = new Map(gpus.map((gpu, index) => { const angle = index * 2 * Math.PI / gpus.length - Math.PI / 2; return [gpu.label, { x: 300 + 165 * Math.cos(angle), y: 210 + 165 * Math.sin(angle) }] }))
 const labels = new Map(record.scope?.bindings.map(binding => [binding.bdf, binding.key]) ?? [])
 const edges = gpus.flatMap((from, index) => gpus.slice(index + 1).flatMap(to => {
  const observed = capture.affinity.find(cell => cell.from === from.label && cell.to === to.label)?.observed
  const actual = /^NV\d+$/.test(observed ?? '') ? Number(observed!.slice(2)) : null
  const wanted = record.scope?.expectation?.nvlinks.find(link => link.from === labels.get(from.bdf) && link.to === labels.get(to.bdf))?.links
  if (!actual && !wanted) return []
  return [{ from: from.label, to: to.label, actual, wanted, missing: wanted != null && actual !== wanted }]
 }))
 return <div className="pcie-nvlink-graph"><p className="field-hint">{t('topologyNVLinkGraphHelp')}</p><svg viewBox="0 0 600 420" role="img" aria-label={t('topologyNVLink')}>
  <title>{t('topologyNVLink')}</title>
  {edges.map(edge => { const from = position.get(edge.from)!, to = position.get(edge.to)!; return <line key={`${edge.from}:${edge.to}`} x1={from.x} y1={from.y} x2={to.x} y2={to.y} className={edge.missing ? 'nv-edge-missing' : 'nv-edge-observed'}><title>{edge.from} ↔ {edge.to}: {t('topologyObserved')} {edge.actual == null ? t('topologyUnknown') : `NV${edge.actual}`} · {t('topologyExpected')} {edge.wanted == null ? t('topologyUnknown') : `NV${edge.wanted}`}</title></line> })}
  {gpus.map(gpu => { const point = position.get(gpu.label)!, numa = capture.nodes.find(node => node.bdf === gpu.bdf)?.numa; return <g key={gpu.label} data-numa={numa ?? 'unknown'}><circle cx={point.x} cy={point.y} r="30" /><text x={point.x} y={point.y + 4} textAnchor="middle">{gpu.label}</text><title>{gpu.label} · {gpu.bdf} · {t('topologyNUMA')} {numa ?? t('topologyUnknown')}</title></g> })}
 </svg>{edges.length === 0 && <p>{t('topologyUnknown')}</p>}</div>
}
