import { useMemo, type ReactNode } from 'react'
import { Link } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { topologyGeneration, type TopologyNode, type TopologySnapshot } from '../../lib/topology'
import '../../styles/pcie-topology.css'

export default function PcieTopologyView({ record }: { record: TopologySnapshot }) {
 const { t } = useTranslation(), capture = record.snapshot
 const graph = useMemo(() => {
  const nodes = new Map<string, TopologyNode>(), children = new Map<string, string[]>(), roots = new Set<string>(), selected = new Set<string>()
  if (!capture) return { nodes, children, roots }
  capture.nodes.forEach(node => nodes.set(node.bdf, node))
  capture.devices.forEach(device => { selected.add(device.bdf); nodes.get(device.bdf)?.upstream.forEach(id => selected.add(id)) })
  selected.forEach(id => { const node = nodes.get(id); if (!node) return; const parent = node.upstream.at(-1) ?? node.root; roots.add(node.root); children.set(parent, [...(children.get(parent) ?? []), id].sort()) })
  return { nodes, children, roots }
 }, [capture])
 if (!capture) return <p>{t('topologyEmpty')}</p>
 const byBDF = new Map(capture.devices.map(device => [device.bdf, device]))
 const affected = new Set(record.evaluation.diffs.flatMap(diff => record.scope?.bindings.filter(binding => binding.key === diff.target).map(binding => binding.bdf) ?? []))
 const matrix = new Map(capture.affinity.map(cell => [`${cell.from}:${cell.to}`, cell]))
 function branch(parent: string, depth: number): ReactNode {
  if (depth > 32) return null
  return <ul className="pcie-tree">{graph.children.get(parent)?.map(id => {
   const node = graph.nodes.get(id), device = byBDF.get(id)
   if (!node) return null
   return <li key={id}><div className={`pcie-node${affected.has(id) ? ' pcie-node-diff' : ''}`} data-numa={node.numa ?? 'unknown'}><strong>{device ? `${device.label} · ${device.kind.toUpperCase()}` : node.pcie_type || 'PCIe'}</strong><code>{id}</code><span>{t('topologyNUMA')} {node.numa ?? t('topologyUnknown')} · {t('topologySlot')} {node.physical_slot || '—'}</span></div>{branch(id, depth + 1)}</li>
  })}</ul>
 }
 return <div className="pcie-view">
  <section><h3>{t('topologyTree')}</h3>{Array.from(graph.roots).sort().map(root => <div key={root} className="pcie-root"><strong>{t('topologyRoot')} · {root}</strong>{branch(root, 0)}</div>)}</section>
  <section><h3>{t('topologyMatrix')}</h3><p className="field-hint">{t('topologyMatrixHelp')}</p><Table><TableHeader><TableRow><TableHead>GPU / NIC</TableHead>{capture.devices.map(device => <TableHead key={device.label}>{device.label}</TableHead>)}</TableRow></TableHeader><TableBody>{capture.devices.map(from => <TableRow key={from.label}><TableCell><strong>{from.label}</strong></TableCell>{capture.devices.map(to => { const cell = matrix.get(`${from.label}:${to.label}`); return <TableCell key={to.label}><span>{cell?.observed ?? t('topologyUnknown')}</span>{cell && cell.observed !== cell.computed && <span className="cell-subtitle">({cell.computed})</span>}</TableCell> })}</TableRow>)}</TableBody></Table></section>
  <section><h3>{t('topologyLinks')}</h3><Table><TableHeader><TableRow>{['PCI BDF', 'LnkCap', 'LnkSta', t('topologyDesign')].map(label => <TableHead key={label}>{label}</TableHead>)}</TableRow></TableHeader><TableBody>{capture.devices.map(device => { const node = graph.nodes.get(device.bdf), binding = record.scope?.bindings.find(value => value.bdf === device.bdf), design = record.scope?.expectation?.devices.find(value => value.key === binding?.key); return <TableRow key={device.bdf}><TableCell>{device.label}<code className="cell-subtitle">{device.bdf}</code></TableCell><TableCell>{topologyGeneration(node?.max_speed_gt)} ×{node?.max_width ?? '?'}</TableCell><TableCell>{topologyGeneration(node?.speed_gt)} ×{node?.width ?? '?'}</TableCell><TableCell>{design ? `${topologyGeneration(design.speed_gt)} ×${design.width}` : t('topologyUnknown')}</TableCell></TableRow> })}</TableBody></Table></section>
  <section><h3>{t('topologyNVLink')}</h3><div className="pcie-nvlinks">{capture.devices.filter(device => device.kind === 'gpu').map(gpu => <div key={gpu.label}><strong>{gpu.label}</strong><ul>{capture.nvlinks.filter(link => link.gpu === gpu.label).map(link => <li key={link.index} data-state={link.state}>Link {link.index} · {t(`topology_${link.state}`)}</li>)}</ul>{!capture.nvlinks.some(link => link.gpu === gpu.label) && <p>{t('topologyUnknown')}</p>}</div>)}</div></section>
  <section><h3>{t('topologyDiffs')}</h3>{!record.evaluation.diffs.length && <p>{t('topologyNoDiff')}</p>}<ul>{record.evaluation.diffs.map((diff, index) => { const issue = record.diffs.find(row => row.detail.code === diff.code && row.detail.target === diff.target)?.issue_id; return <li key={`${diff.code}:${diff.target}:${index}`} className="pcie-finding"><strong>{diff.category ? t(`topology_${diff.category}`) : t('topologyMissing')} · {diff.target}</strong><code>{diff.verdict} · {diff.code}</code><span>{t('topologyExpected')}: {JSON.stringify(diff.expected)}</span><span>{t('topologyObserved')}: {JSON.stringify(diff.actual)}</span>{issue && <Link to="/issues" search={{ project_id: record.project_id, source: 'topology', issue_id: issue }} className="text-link">{t('topologyOpenIssue')}</Link>}</li> })}</ul></section>
 </div>
}
