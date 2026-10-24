import { Button } from "@/components/ui/button"
import { NativeSelect } from "@/components/ui/native-select"
import type {ButtonHTMLAttributes,SelectHTMLAttributes,HTMLAttributes} from 'react'
export function FabricControlButton(props:ButtonHTMLAttributes<HTMLButtonElement>){return <Button variant="outline" className="secondary-button" {...props}/>}
export function FabricControlSelect(props:SelectHTMLAttributes<HTMLSelectElement>){return <NativeSelect {...props}/>}
export function FabricControlPanel(props:HTMLAttributes<HTMLDivElement>){return <div className="fabric-diagnostic-panel" {...props}/>}
