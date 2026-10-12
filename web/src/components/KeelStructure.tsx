import { useId } from 'react'

// The two gunwales and the load-bearing keel share the same stern and bow.
// Every transverse rib meets all three curves, so this remains a hull skeleton
// at every size rather than an abstract network or a pair of railway tracks.
type Point = [number, number]
const stern: Point = [78, 151]
const bow: Point = [527, 73]
const far: Point[] = [stern, [192, 34], [378, 12], bow]
const near: Point[] = [stern, [188, 215], [378, 174], bow]
const keel: Point[] = [stern, [169, 273], [382, 224], bow]
function point(points: Point[], t: number): Point {
  const a = 1 - t
  return [0, 1].map((axis) => a ** 3 * points[0][axis] + 3 * a ** 2 * t * points[1][axis]
    + 3 * a * t ** 2 * points[2][axis] + t ** 3 * points[3][axis]) as Point
}
function curve(points: Point[]) { return `M${points[0]} C${points[1]} ${points[2]} ${points[3]}` }
const ribs = Array.from({ length: 11 }, (_, index) => {
  const t = (index + 1) / 12
  const f = point(far, t), n = point(near, t), k = point(keel, t)
  return `M${f} Q${k[0] - 31},${k[1] - 8} ${k} Q${n[0] - 7},${n[1] + 12} ${n}`
})

export function KeelStructure() {
  const gradient = useId()
  return <div className="keel-diagram" aria-hidden="true">
    <div className="diagram-grid" />
    <svg className="hull-structure" viewBox="0 0 600 280" fill="none" focusable="false">
      <defs><linearGradient id={gradient} x1="78" y1="202" x2="527" y2="73" gradientUnits="userSpaceOnUse">
        <stop stopColor="var(--color-primary-500)" /><stop offset="1" stopColor="var(--color-accent-pink)" />
      </linearGradient></defs>
      <path className="hull-shadow" d={curve(keel)} />
      <path className="hull-gunwale hull-far" d={curve(far)} />
      <g className="hull-ribs">{ribs.map((d, index) => <path key={index} d={d} />)}</g>
      <path className="hull-gunwale" d={curve(near)} />
      <path className="hull-keel" d={curve(keel)} stroke={`url(#${gradient})`} />
      <g className="hull-rib-light" stroke={`url(#${gradient})`}>
        {ribs.map((d, index) => <path key={index} d={d} pathLength="100" className={`rib-phase-${index % 4}`} />)}
      </g>
      <path className="hull-keel-light" d={curve(keel)} pathLength="100" />
      <path className="hull-stem" d="M514 87 527 73 537 55" stroke={`url(#${gradient})`} />
      <path className="hull-annotation" d="M291 215v24h42" />
      <text className="hull-label" x="343" y="243">KEEL / FOUNDATION</text>
    </svg>
    <span className="diagram-caption">A STRONG SPINE. A CLEAR DIRECTION.</span>
  </div>
}
