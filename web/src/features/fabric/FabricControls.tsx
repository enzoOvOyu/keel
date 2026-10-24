// Feature boundary: replace primitives here when the shared shadcn/ui worktree
// lands. Diagnostic calculations and chart code do not depend on the UI library.
import type {ButtonHTMLAttributes,SelectHTMLAttributes,HTMLAttributes} from 'react'
export function FabricControlButton(props:ButtonHTMLAttributes<HTMLButtonElement>){return <button className="secondary-button" {...props}/>}
export function FabricControlSelect(props:SelectHTMLAttributes<HTMLSelectElement>){return <select {...props}/>}
export function FabricControlPanel(props:HTMLAttributes<HTMLDivElement>){return <div className="fabric-diagnostic-panel" {...props}/>}
