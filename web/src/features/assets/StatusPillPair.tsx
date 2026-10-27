import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { TableCell } from '@/components/ui/table'
import type { PortMonitorFleetItem } from '../../lib/port-monitor'

export default function StatusPillPair({ status, asOf, now, onMapping, onPorts }: { status?: PortMonitorFleetItem; asOf?: string; now: number; onMapping: () => void; onPorts: () => void }) {
 const { t } = useTranslation()
 const stale = !asOf || now - Date.parse(asOf) > 30000
 const mappingExpired = !!status?.mapping_valid_until && Date.parse(status.mapping_valid_until) <= now
 const mapping = !status ? 'unknown' : status.mapping_state === 'verified' && (stale || mappingExpired) ? 'blocked' : status.mapping_state
 const network = status?.network_state === 'maintained' ? 'maintained' : stale || mappingExpired ? 'unknown' : status?.network_state || 'unknown'
 const colors: Record<string, string> = { verified: 'ui:text-emerald-700 ui:dark:text-emerald-400', normal: 'ui:text-emerald-700 ui:dark:text-emerald-400', blocked: 'ui:text-red-700 ui:dark:text-red-400', abnormal: 'ui:text-red-700 ui:dark:text-red-400', maintained: 'ui:text-blue-700 ui:dark:text-blue-400', unknown: 'ui:text-muted-foreground', unbuilt: 'ui:text-muted-foreground' }
 return <><TableCell><Button variant="ghost" size="sm" onClick={onMapping} className={colors[mapping]}>{mapping === 'verified' ? '✓ ' : mapping === 'blocked' ? '✕ ' : '○ '}{t(`serverMapping_${mapping}`)}</Button></TableCell><TableCell><Button variant="ghost" size="sm" onClick={onPorts} className={colors[network]}>{network === 'normal' ? '✓ ' : network === 'abnormal' ? '✕ ' : network === 'maintained' ? '⏸ ' : '? '}{t(`serverNetwork_${network}`)}</Button></TableCell></>
}
