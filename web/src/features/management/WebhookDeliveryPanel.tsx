import { useState } from 'react'
import { Link } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Table, TableHeader, TableBody, TableHead, TableRow, TableCell } from '@/components/ui/table'
import { ActionError, Pagination } from '../../components/Management'
import { listWebhookDeliveries } from '../../lib/management'
import { useSession, useExpireOnUnauthorized } from '../../lib/session'

// Mounted only within the authorized project audit view. Project ID is also in
// the private query key; a project change remounts pagination independently.
export function WebhookDeliveryPanel({ projectId }: { projectId: string }) {
  const { t, i18n } = useTranslation(), session = useSession()
  const [page, setPage] = useState(1)
  const deliveries = useQuery({
    queryKey: ['private', session.user.id, 'project', projectId, 'webhook-deliveries', page],
    queryFn: ({ signal }) => listWebhookDeliveries(projectId, page, signal),
  })
  useExpireOnUnauthorized(deliveries.error)
  const date = (value?: string) => value ? new Intl.DateTimeFormat(i18n.language === 'en' ? 'en-US' : 'zh-CN', { dateStyle: 'short', timeStyle: 'medium' }).format(new Date(value)) : '—'
  const data = deliveries.data
  return <section className="management-panel" aria-busy={deliveries.isFetching}>
    <div className="page-heading"><div><h2>{t('notificationDeliveryTitle')}</h2><p>{t('notificationDeliveryIntro')}</p></div>
      <Button variant="outline" disabled={deliveries.isFetching} onClick={() => void deliveries.refetch()}>{t('refresh')}</Button></div>
    <ActionError error={deliveries.error} />
    {deliveries.isPending ? <p role="status">{t('loadingRecords')}</p> : data && <>
      <div className="table-scroll" tabIndex={0} role="region" aria-label={t('notificationDeliveryTitle')}>
        <Table><TableHeader><TableRow><TableHead>{t('notificationEvent')}</TableHead><TableHead>{t('notificationSink')}</TableHead><TableHead>{t('notificationState')}</TableHead><TableHead>{t('notificationAttempts')}</TableHead><TableHead>{t('notificationTiming')}</TableHead></TableRow></TableHeader>
          <TableBody>{data.items.map(item => <TableRow key={`${item.sink_id}:${item.event_id}`}>
            <TableCell className="ui:whitespace-normal"><span>{t(`notificationType_${item.event_type.replaceAll('.', '_')}`)}</span><span className="cell-subtitle mono-value">{item.event_id}</span>
              <Link to="/p/$projectId/executions" params={{ projectId }} search={{ execution: item.execution_id, tab: 'overview' }}>{item.execution_id}</Link></TableCell>
            <TableCell className="mono-value ui:whitespace-normal">{item.sink_id}</TableCell>
            <TableCell className="ui:whitespace-normal"><span className="state-badge">{t(`notificationState_${item.state}`)}</span>{item.last_error && <span className="cell-subtitle">{t(`notificationError_${item.last_error}`)}</span>}{item.http_status && <span className="cell-subtitle">HTTP {item.http_status}</span>}</TableCell>
            <TableCell>{item.attempts} / 6</TableCell>
            <TableCell className="ui:whitespace-normal">{t('updatedAt')}: {date(item.updated_at)}<span className="cell-subtitle">{item.state === 'delivered' ? `${t('notificationDeliveredAt')}: ${date(item.delivered_at)}` : item.state === 'pending' ? `${t('notificationNextAttempt')}: ${date(item.next_attempt_at)}` : item.state === 'dead' ? t('notificationDeadHint') : t('notificationDeliveringHint')}</span></TableCell>
          </TableRow>)}</TableBody>
        </Table>{!data.items.length && <p className="table-loading">{t('notificationEmpty')}</p>}
      </div>
      <Pagination offset={(page - 1) * data.page_size} limit={data.page_size} total={data.total} busy={deliveries.isFetching} onChange={offset => setPage(Math.floor(offset / data.page_size) + 1)} />
    </>}
  </section>
}
