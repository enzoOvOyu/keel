import { useEffect, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { ActionError, Pagination } from '../../components/Management'
import { useProject } from '../../components/ProjectWorkspace'
import { listSSHDomainKeys } from '../../lib/bom'
import { useExpireOnUnauthorized, useSession } from '../../lib/session'

export function SSHDomainKeys() {
  const { t } = useTranslation(), session = useSession(), project = useProject()
  const allowed = session.user.is_admin || ['Admin', 'DL', 'SRE'].includes(project.role)
  const [page, setPage] = useState(1), [now, setNow] = useState(Date.now())
  const records = useQuery({ queryKey: ['private', session.user.id, 'project', project.id, 'ssh-domain-keys', page], queryFn: ({ signal }) => listSSHDomainKeys(project.id, page, signal), enabled: allowed })
  useExpireOnUnauthorized(records.error)
  useEffect(() => {
    const next = Math.min(...(records.data?.items ?? []).filter(item => !item.revoked_at).map(item => Date.parse(item.expires_at)).filter(expiry => expiry > now))
    if (!Number.isFinite(next)) return
    const timer = window.setTimeout(() => setNow(Date.now()), Math.max(1, Math.min(next - Date.now() + 1, 2_147_483_647)))
    return () => window.clearTimeout(timer)
  }, [records.data, now])
  if (!allowed) return null
  return <section className="management-panel ui:mt-6" aria-labelledby="ssh-domain-keys-heading" aria-busy={records.isFetching}>
    <div className="page-heading"><div><h2 id="ssh-domain-keys-heading">{t('sshDomainKeysTitle')}</h2><p>{t('sshDomainKeysDetail')}</p></div><Button type="button" variant="outline" disabled={records.isFetching} onClick={() => { setNow(Date.now()); void records.refetch() }}>{t('refresh')}</Button></div>
    <ActionError error={records.error} />
    {records.isPending && <p className="table-loading" role="status">{t('loadingRecords')}</p>}
    {!records.isError && records.data && <>
      {records.data.items.length ? <Table aria-label={t('sshDomainKeysTitle')}><TableHeader><TableRow><TableHead>{t('sshTrustCredential')}</TableHead><TableHead>{t('status')}</TableHead><TableHead>{t('sshTrustExpiry')}</TableHead><TableHead>{t('sshDomainPublicKey')}</TableHead></TableRow></TableHeader><TableBody>{records.data.items.map(item => {
        const state = item.revoked_at ? 'revoked' : Date.parse(item.expires_at) <= now ? 'expired' : 'active'
        return <TableRow key={item.id} data-ssh-domain-key={item.id}><TableCell className="mono-value">{item.credential_ref}<span className="cell-subtitle">{item.credential_version} · v{item.state_version}</span></TableCell><TableCell><span className={`status-label ${state === 'active' ? 'enabled' : 'disabled'}`}>{t(`sshDomainKey_${state}`)}</span>{item.revoked_at && <span className="cell-subtitle">{new Date(item.revoked_at).toLocaleString()}</span>}{item.revocation_reason && <span className="cell-subtitle ui:whitespace-normal ui:break-words">{item.revocation_reason}</span>}</TableCell><TableCell>{new Date(item.expires_at).toLocaleString()}</TableCell><TableCell><details><summary>{t('sshDomainPublicKey')}</summary><code className="ui:block ui:max-w-96 ui:whitespace-pre-wrap ui:break-all">{item.public_key}</code></details></TableCell></TableRow>
      })}</TableBody></Table> : <p className="table-loading">{t('sshDomainKeysEmpty')}</p>}
      <Pagination offset={(page - 1) * records.data.page_size} limit={records.data.page_size} total={records.data.total} busy={records.isFetching} onChange={offset => setPage(offset / records.data.page_size + 1)} />
    </>}
    <p className="impact-notice">{t('sshDomainKeysBoundary')}</p>
  </section>
}
