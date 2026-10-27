import { useTranslation } from 'react-i18next'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import type { WiringBatch } from '../../lib/wiring'

export default function WiringTimeline({ batch, now }: { batch: WiringBatch; now: number }) {
 const { t } = useTranslation()
 return <section><h3>{t('wiringBatch')}</h3><code className="cell-subtitle">{batch.id}</code><p className="field-hint">{t('wiringAdapterPending')}</p>
  {batch.targets.map(target => <section key={target.asset_id} className="ui:my-4"><h4>{target.expectation.hostname} · {target.expectation.serial}</h4>
   <ol className="ui:grid ui:gap-3 ui:md:grid-cols-4" aria-label={t('wiringBatch')}>{[1, 2, 3, 4].map(step => {
    const reports = target.reports.filter(report => report.step === step), latest = reports[reports.length - 1]
    const effective = latest?.valid_until && Date.parse(latest.valid_until) <= now ? 'BLOCKED' : latest?.effective_verdict
    return <li key={step} className="ui:rounded-md ui:border ui:p-3"><strong>{step}. {t(`wiringStep${step}`)}</strong><p>{latest ? t(`precollect_${effective}`) : t('wiringNotStarted')}</p>{latest && <><span className="cell-subtitle">v{latest.version} · {new Date(latest.created_at).toLocaleString()}</span><code className="cell-subtitle">{latest.id}</code>{latest.valid_until && <p>{t('wiringValidUntil')}: {new Date(latest.valid_until).toLocaleString()}</p>}{latest.invalidation_reason && <code>{latest.invalidation_reason}</code>}</>}</li>
   })}</ol>
   <details><summary>{t('topologyRelationsTitle')}</summary><Table><TableHeader><TableRow><TableHead>GPU</TableHead><TableHead>NIC</TableHead><TableHead>{t('wiringPort')}</TableHead><TableHead>{t('topologyMatrix')}</TableHead></TableRow></TableHeader><TableBody>{target.relations.rows.map((row, index) => <TableRow key={`${row.gpu_key}:${row.nic_key}:${row.physical_port_id}:${index}`}><TableCell>{row.gpu_key}<code className="cell-subtitle">{row.gpu_node?.bdf}</code></TableCell><TableCell>{row.nic_key}<code className="cell-subtitle">{row.nic_node?.bdf}</code></TableCell><TableCell>{row.interface}<code className="cell-subtitle">{row.permanent_mac}</code></TableCell><TableCell>{row.expected_affinity} → {row.observed_affinity} ({row.computed_affinity})</TableCell></TableRow>)}</TableBody></Table></details>
  </section>)}
 </section>
}
