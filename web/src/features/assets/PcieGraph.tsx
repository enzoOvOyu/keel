import { useMemo, useState } from 'react'
import { Background, Controls, Handle, Position, ReactFlow, type NodeProps, type ReactFlowInstance } from '@xyflow/react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { buildPcieGraph, type PcieGraphNode } from '../../lib/pcie-graph'
import type { TopologySnapshot } from '../../lib/topology'
import '@xyflow/react/dist/style.css'

function PcieNode({ data, selected }: NodeProps<PcieGraphNode>) {
 const { t } = useTranslation()
 return <div className={`pcie-flow-node${data.affected ? ' pcie-node-diff' : ''}${selected ? ' pcie-flow-node-selected' : ''}`} data-numa={data.numa ?? 'unknown'}>
  <Handle type="target" position={Position.Top} />
  <strong>{data.label} · {data.kind === 'root' ? 'PCIe' : data.kind.toUpperCase()}</strong>
  <code>{data.bdf}</code><span>{t('topologyNUMA')} {data.numa ?? t('topologyUnknown')}{data.missing && ` · ${t('topologyUnknown')}`}</span>
  <Handle type="source" position={Position.Bottom} />
 </div>
}
const nodeTypes = { pcie: PcieNode }

export default function PcieGraph({ record }: { record: TopologySnapshot }) {
 const { t } = useTranslation(), graph = useMemo(() => buildPcieGraph(record), [record])
 const [selected, setSelected] = useState(''), [collapsed, setCollapsed] = useState<Set<string>>(() => new Set())
 const [flow, setFlow] = useState<ReactFlowInstance<PcieGraphNode> | null>(null)
 const descendants = (parent: string) => {
  const found = new Set<string>(), pending = [parent]
  while (pending.length) { const current = pending.pop()!; graph.edges.filter(edge => edge.source === current).forEach(edge => { if (!found.has(edge.target) && edge.target !== parent) { found.add(edge.target); pending.push(edge.target) } }) }
  return found
 }
 const hidden = new Set([...collapsed].flatMap(id => [...descendants(id)]))
 const nodes = graph.nodes.map(node => ({ ...node, hidden: hidden.has(node.id), selected: selected === node.id }))
 const edges = graph.edges.map(edge => ({ ...edge, hidden: hidden.has(edge.source) || hidden.has(edge.target) }))
 const selection = graph.nodes.find(node => node.id === selected)
 const details = record.snapshot?.nodes.find(node => node.bdf === selected)
 const findings = record.evaluation.diffs.filter(diff => record.scope?.bindings.some(binding => binding.key === diff.target && binding.bdf === selected))
 const hasChildren = graph.edges.some(edge => edge.source === selected)
 return <div className="pcie-graph-view">
  <p className="field-hint">{t('topologyGraphHelp')}</p>
  <div className="pcie-graph-toolbar"><Button variant="outline" size="sm" onClick={() => void flow?.fitView({ padding: 0.15, duration: 0 })}>{t('topologyGraphFit')}</Button><Button variant="outline" size="sm" disabled={!selected || !hasChildren} onClick={() => setCollapsed(before => { const next = new Set(before); if (next.has(selected)) next.delete(selected); else next.add(selected); return next })}>{t(collapsed.has(selected) ? 'topologyGraphExpand' : 'topologyGraphCollapse')}</Button><Button variant="outline" size="sm" disabled={!collapsed.size} onClick={() => setCollapsed(new Set())}>{t('topologyGraphExpandAll')}</Button></div>
  <div className="pcie-flow" aria-label={t('topologyTree')}>
   <ReactFlow<PcieGraphNode> nodes={nodes} edges={edges} nodeTypes={nodeTypes} onInit={setFlow} onNodeClick={(_, node) => setSelected(node.id)} onSelectionChange={({ nodes: selection }) => { if (selection[0]) setSelected(selection[0].id) }} fitView fitViewOptions={{ padding: 0.15 }} minZoom={0.15} maxZoom={2} nodesDraggable={false} nodesConnectable={false} edgesReconnectable={false} deleteKeyCode={null} panOnDrag zoomOnPinch zoomOnScroll preventScrolling={false} colorMode="system">
    <Background /><Controls showInteractive={false} />
   </ReactFlow>
  </div>
  {selection && <section aria-live="polite" className="pcie-graph-selection"><h4>{selection.data.label} · {selection.data.bdf}</h4><p>{t('topologyNUMA')}: {selection.data.numa ?? t('topologyUnknown')}</p>{details && <p>{t('topologySlot')}: {details.physical_slot || '—'} · {t('topologyUpstream')}: {details.upstream.join(' → ') || details.root}</p>}{findings.map((finding, index) => <p key={`${finding.code}:${index}`} className="pcie-finding"><strong>{finding.code}</strong><span>{t('topologyExpected')}: {JSON.stringify(finding.expected)}</span><span>{t('topologyObserved')}: {JSON.stringify(finding.actual)}</span></p>)}</section>}
 </div>
}
