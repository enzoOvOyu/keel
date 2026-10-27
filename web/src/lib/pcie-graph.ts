import type { Edge, Node } from '@xyflow/react'
import type { TopologySnapshot } from './topology'

export type PcieGraphData = { bdf: string; label: string; kind: string; numa: number | null; affected: boolean; missing: boolean }
export type PcieGraphNode = Node<PcieGraphData, 'pcie'>

/** Only observed sysfs paths create edges. BDF ordering sets display order. */
export function buildPcieGraph(record: TopologySnapshot): { nodes: PcieGraphNode[]; edges: Edge[] } {
 const capture = record.snapshot
 if (!capture) return { nodes: [], edges: [] }
 const actual = new Map(capture.nodes.map(node => [node.bdf, node]))
 const devices = new Map(capture.devices.map(device => [device.bdf, device]))
 const affected = new Set(record.evaluation.diffs.flatMap(diff => record.scope?.bindings.filter(binding => binding.key === diff.target).map(binding => binding.bdf) ?? []))
 const data = new Map<string, PcieGraphData>(), children = new Map<string, Set<string>>(), edges = new Map<string, Edge>(), roots = new Set<string>()
 const add = (id: string, root = false) => {
  if (data.has(id)) return
  const node = actual.get(id), device = devices.get(id)
  data.set(id, { bdf: root ? id.slice(5) : id, label: root ? 'Root Complex' : device?.label ?? node?.pcie_type ?? 'unknown', kind: root ? 'root' : device?.kind ?? (node ? 'bridge' : 'unknown'), numa: node?.numa ?? null, affected: affected.has(id), missing: !root && !node })
 }
 capture.devices.forEach(device => {
  const node = actual.get(device.bdf)
  if (!node) { add(device.bdf); roots.add(device.bdf); return }
  const root = `root:${node.root}`, chain = [root, ...node.upstream, node.bdf]
  roots.add(root); add(root, true)
  chain.forEach((id, index) => {
   add(id, index === 0)
   const parent = chain[index - 1]
   if (!parent || parent === id) return
   children.set(parent, new Set([...(children.get(parent) ?? []), id]))
   const key = `${parent}>${id}`
   edges.set(key, { id: key, source: parent, target: id, type: 'smoothstep', className: affected.has(id) ? 'pcie-edge-diff' : undefined, selectable: false })
  })
 })
 const nodes: PcieGraphNode[] = [], placed = new Set<string>()
 let column = 0
 const place = (id: string, depth: number, path: Set<string>): number => {
  if (depth > 32 || path.has(id) || placed.has(id)) return column
  placed.add(id)
  const nextPath = new Set(path); nextPath.add(id)
  const descendants = [...(children.get(id) ?? [])].sort()
  const positions = descendants.map(child => place(child, depth + 1, nextPath))
  const x = positions.length ? (positions[0]! + positions[positions.length - 1]!) / 2 : column++ * 260
  nodes.push({ id, type: 'pcie', data: data.get(id)!, position: { x, y: depth * 145 }, draggable: false, connectable: false, deletable: false, focusable: true })
  return x
 }
 ;[...roots].sort().forEach(root => { place(root, 0, new Set()); column++ })
 return { nodes, edges: [...edges.values()] }
}
