import { useEffect, type ReactNode } from 'react'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'

export default function AssetDetailFrame({ title, description, busy, embedded, onBusy, onClose, children }: { title: string; description: string; busy: boolean; embedded?: boolean; onBusy?: (busy: boolean) => void; onClose: () => void; children: ReactNode }) {
 useEffect(() => { onBusy?.(busy); return () => onBusy?.(false) }, [busy, onBusy])
 if (embedded) return <section aria-label={title} className="ui:grid ui:gap-3"><p>{description}</p>{children}</section>
 return <Dialog open onOpenChange={open => { if (!open && !busy) onClose() }}><DialogContent className="ui:max-h-[90vh] ui:overflow-y-auto ui:sm:max-w-6xl" showCloseButton={!busy} onEscapeKeyDown={event => { if (busy) event.preventDefault() }} onInteractOutside={event => { if (busy) event.preventDefault() }}><DialogHeader><DialogTitle>{title}</DialogTitle><DialogDescription>{description}</DialogDescription></DialogHeader>{children}</DialogContent></Dialog>
}
