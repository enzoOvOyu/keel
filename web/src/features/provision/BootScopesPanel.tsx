import { useEffect, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { ActionDialog, ActionError, Pagination } from '../../components/Management'
import { useProject } from '../../components/ProjectWorkspace'
import { useSession, useExpireOnUnauthorized } from '../../lib/session'
import { useAction } from '../../lib/management'
import { actProvisionBootScope, createProvisionBootScope, listProvisionBootScopes, type ProvisionBootScope } from '../../lib/provision'

type Choice = { kind: 'create'; project: string } | { kind: 'approve' | 'revoke'; project: string; item: ProvisionBootScope }

function ScopeAction({ choice, onClose, onDone }: { choice: Choice; onClose: () => void; onDone: () => Promise<void> }) {
 const { t } = useTranslation(), session = useSession(), action = useAction()
 const [fields, setFields] = useState({ interface: '', server_ipv4: '', client_cidr: '', origin: '', networks: '', source_ref: '', customer: '', network: '', reason: '' })
 const change = (key: keyof typeof fields, value: string) => setFields(previous => ({ ...previous, [key]: value }))
 const required = choice.kind === 'create' ? ['interface', 'server_ipv4', 'client_cidr', 'origin', 'source_ref'] as const : choice.kind === 'approve' ? ['customer', 'network'] as const : ['reason'] as const
 return <ActionDialog title={t(`bootScopeAction_${choice.kind}`)} description={choice.kind === 'create' ? t('bootScopeCreateDetail') : `${choice.item.scope.clients} · ${choice.item.scope.interface}`} busy={action.busy} confirm={t('confirm')} submitDisabled={required.some(key => !fields[key].trim())} onClose={onClose} onSubmit={event => {
  event.preventDefault()
  void action.run(async () => {
   if (choice.kind === 'create') {
    await createProvisionBootScope(choice.project, { interface: fields.interface.trim(), server_ipv4: fields.server_ipv4.trim(), client_cidr: fields.client_cidr.trim(), origin: fields.origin.trim(), kea_networks: fields.networks.split(/\r?\n/).map(value => value.trim()).filter(Boolean), source_ref: fields.source_ref.trim() }, session.csrf_token)
   } else if (choice.kind === 'approve') {
    await actProvisionBootScope(choice.item, 'approve', { expected_version: choice.item.state_version, customer_approval_ref: fields.customer.trim(), network_approval_ref: fields.network.trim() }, session.csrf_token)
   } else {
    await actProvisionBootScope(choice.item, 'revoke', { expected_version: choice.item.state_version, reason: fields.reason.trim() }, session.csrf_token)
   }
  }).then(async result => { if (result.ok) await onDone() })
 }}>
  <p className="impact-notice">{t(choice.kind === 'revoke' ? 'bootScopeRevokeImpact' : 'bootScopeRegistryOnly')}</p>
  <ActionError error={action.error} />
  {required.map(key => <div className="field" key={key}><Label htmlFor={`boot-scope-${key}`}>{t(`bootScopeField_${key}`)}</Label><Input id={`boot-scope-${key}`} value={fields[key]} maxLength={key === 'reason' ? 500 : key === 'interface' ? 15 : 300} required disabled={action.busy} autoComplete="off" onChange={event => change(key, event.target.value)} /></div>)}
  {choice.kind === 'create' && <div className="field"><Label htmlFor="boot-scope-networks">{t('bootScopeExternalPools')}</Label><Textarea id="boot-scope-networks" value={fields.networks} maxLength={8192} rows={4} disabled={action.busy} onChange={event => change('networks', event.target.value)} /><p>{t('bootScopeExternalPoolsHelp')}</p></div>}
  {choice.kind === 'approve' && <dl><dt>{t('bootScopeField_source_ref')}</dt><dd>{choice.item.source_ref}</dd><dt>{t('bootScopeExternalPools')}</dt><dd className="mono-value">{choice.item.scope.kea_networks.join(', ') || t('bootScopeNoExternalPools')}</dd><dt>{t('bootScopeField_origin')}</dt><dd>{choice.item.scope.origin}</dd></dl>}
 </ActionDialog>
}

export function BootScopesPanel() {
 const { t } = useTranslation(), project = useProject(), session = useSession(), client = useQueryClient()
 const [page, setPage] = useState(1), [choice, setChoice] = useState<Choice>()
 const reviewer = session.user.is_admin || ['Admin', 'NDE'].includes(project.role)
 const writable = !['archived', 'delivered'].includes(project.state) && reviewer
 const prefix = ['private', session.user.id, 'project', project.id, 'provision-boot-scopes']
 const records = useQuery({ queryKey: [...prefix, page], queryFn: ({ signal }) => listProvisionBootScopes(project.id, page, signal), retry: false })
 useExpireOnUnauthorized(records.error)
 useEffect(() => { setPage(1); setChoice(undefined) }, [project.id])
 useEffect(() => { if (!reviewer || records.error || (!writable && choice?.kind !== 'revoke')) setChoice(undefined) }, [reviewer, writable, records.error, choice?.kind])
 const refresh = async () => { await client.invalidateQueries({ queryKey: prefix }) }
 return <section className="management-page" aria-labelledby="boot-scopes-title">
  <div className="page-heading"><div><h2 id="boot-scopes-title">{t('bootScopeTitle')}</h2><p>{t('bootScopeDetail')}</p></div><div className="table-actions"><Button variant="outline" disabled={records.isFetching} onClick={() => void refresh()}>{t('refresh')}</Button>{writable && <Button disabled={!!records.error} onClick={() => setChoice({ kind: 'create', project: project.id })}>{t('bootScopeAction_create')}</Button>}</div></div>
  <ActionError error={records.error} />
  {records.isPending && <p role="status">{t('loadingRecords')}</p>}
  {records.data && !records.error && <>
   {!records.data.items.length && <p>{t('bootScopeEmpty')}</p>}
   <div className="table-scroll"><Table><TableHeader><TableRow><TableHead>{t('bootScopeDomain')}</TableHead><TableHead>{t('bootScopeField_source_ref')}</TableHead><TableHead>{t('status')}</TableHead><TableHead>{t('actions')}</TableHead></TableRow></TableHeader><TableBody>{records.data.items.map(item => <TableRow key={item.id} data-provision-scope={item.id}>
    <TableCell><span className="mono-value">{item.scope.clients}</span><span className="cell-subtitle mono-value">{item.scope.interface} · {item.scope.server}</span><span className="cell-subtitle">{item.scope.origin}</span></TableCell>
    <TableCell><span>{item.source_ref}</span><details><summary>{t('provisionProof')}</summary><dl className="ui:max-w-96 ui:whitespace-normal ui:break-words"><dt>{t('bootScopeExternalPools')}</dt><dd>{item.scope.kea_networks.join(', ') || t('bootScopeNoExternalPools')}</dd><dt>{t('bootScopeField_customer')}</dt><dd>{item.customer_approval_ref || '—'}</dd><dt>{t('bootScopeField_network')}</dt><dd>{item.network_approval_ref || '—'}</dd><dt>{t('bootScopeField_reason')}</dt><dd>{item.revocation_reason || '—'}</dd></dl></details></TableCell>
    <TableCell>{t(`bootScopeState_${item.state}`)}<span className="cell-subtitle">v{item.state_version}</span></TableCell>
    <TableCell><div className="table-actions">{writable && item.state === 'requested' && item.created_by !== session.user.id && <Button size="sm" variant="outline" onClick={() => setChoice({ kind: 'approve', project: project.id, item })}>{t('bootScopeAction_approve')}</Button>}{reviewer && item.state !== 'revoked' && <Button size="sm" variant="outline" onClick={() => setChoice({ kind: 'revoke', project: project.id, item })}>{t('bootScopeAction_revoke')}</Button>}</div></TableCell>
   </TableRow>)}</TableBody></Table></div>
   <Pagination offset={(page - 1) * records.data.page_size} limit={records.data.page_size} total={records.data.total} busy={records.isFetching} onChange={offset => setPage(offset / records.data!.page_size + 1)} />
  </>}
  {choice && choice.project === project.id && reviewer && (writable || choice.kind === 'revoke') && !records.error && <ScopeAction key={choice.kind === 'create' ? `${project.id}:create` : `${choice.item.id}:${choice.kind}`} choice={choice} onClose={() => { setChoice(undefined); void refresh() }} onDone={async () => { setChoice(undefined); await refresh() }} />}
 </section>
}
