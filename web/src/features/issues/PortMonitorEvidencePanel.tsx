import { Link } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { ActionError } from '../../components/Management'
import { getPortMonitorIssueEvidence } from '../../lib/monitor'
import type { Issue } from '../../lib/issues'
import { useExpireOnUnauthorized, useSession } from '../../lib/session'

export default function PortMonitorEvidencePanel({ issue }: { issue: Issue }) {
 const { t } = useTranslation(), session = useSession()
 const evidence = useQuery({ queryKey: ['private', session.user.id, 'portmon-issue', issue.project_id, issue.id, issue.version], queryFn: ({ signal }) => getPortMonitorIssueEvidence(issue.id, signal), retry: false, refetchOnWindowFocus: false })
 useExpireOnUnauthorized(evidence.error)
 const record = evidence.data
 return <section className="ui:grid ui:gap-3"><h2>{t('portmonIssueEvidence')}</h2><p>{t('portmonIssueEvidenceHelp')}</p><ActionError error={evidence.error} />
  {evidence.isPending && <p role="status">{t('loadingRecords')}</p>}
  {record && !evidence.isError && <>
   <p><strong>{record.rule_id}</strong> · {record.initial.source} / {record.initial.method} / {record.initial.origin}</p>
   <p>{t('portmonIssueDiscovery')}: {new Date(record.initial.sample.observed_at).toLocaleString()} · #{record.initial.sequence}</p>
   <code className="ui:break-all">{record.physical_port_id} · {record.mapping_revision_id}</code>
   <p>{t('portmonIssueCurrent')}: {record.current_mapping_state} · {record.current?.state ?? 'unknown'}</p>
   <p>{t('wiringRetestRepair')}: v{record.repair_version} · {record.repaired_at ? new Date(record.repaired_at).toLocaleString() : '—'}</p>
   <h3>{t('portmonIssueRequiredChecks')}</h3><ul>{record.required_checks.map(check => <li key={check}>{t(`portmonCheck_${check}`, { defaultValue: check })}</li>)}</ul>
   <details><summary>{t('portmonIssueDiscovery')}</summary><pre className="ui:max-h-80 ui:overflow-auto ui:whitespace-pre-wrap ui:break-words">{JSON.stringify({ rule: record.rule_version, observation: record.initial }, null, 2)}</pre></details>
   <details><summary>{t('portmonIssueCurrent')}</summary><pre className="ui:max-h-80 ui:overflow-auto ui:whitespace-pre-wrap ui:break-words">{JSON.stringify({ mapping: record.current_mapping_revision_id, port: record.current }, null, 2)}</pre></details>
   <Link to="/p/$projectId/assets" params={{ projectId: issue.project_id }} className="text-link">{t('wiringRetestAssets')}</Link>
  </>}
  <Button variant="outline" disabled={evidence.isFetching} onClick={() => void evidence.refetch()}>{t('wiringRefreshReports')}</Button>
 </section>
}
