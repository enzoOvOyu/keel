import { Button } from '@/components/ui/button'
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter } from '@/components/ui/dialog'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { ActionError } from '../../components/Management'
import { ModelPanelPreview } from '../../components/ModelPanelPreview'
import { getAssetModelBinding, type ProjectAsset } from '../../lib/bom'
import { useExpireOnUnauthorized, useSession } from '../../lib/session'

export function AssetModelDialog({ projectId, asset, onClose }: { projectId: string; asset: ProjectAsset; onClose: () => void }) {
  const { t } = useTranslation(), session = useSession()
  const binding = useQuery({ queryKey: ['private', session.user.id, 'project', projectId, 'asset-model', asset.id, asset.model_version_id, asset.version], queryFn: ({ signal }) => getAssetModelBinding(projectId, asset.id, signal) })
  useExpireOnUnauthorized(binding.error)
  const data = binding.data, version = data?.model_version
  return <Dialog open onOpenChange={open => { if (!open) onClose() }}>
    <DialogContent showCloseButton={false} className="ui:w-[min(760px,calc(100vw-32px))] ui:max-w-none ui:sm:max-w-none ui:max-h-[calc(100svh-48px)] ui:overflow-y-auto">
      <DialogHeader><DialogTitle>{t('assetModelBinding')} · {asset.hostname}</DialogTitle><DialogDescription>{t('assetModelBindingNotice')}</DialogDescription></DialogHeader>
      <ActionError error={binding.error} />
      {binding.isPending && <p role="status">{t('loadingRecords')}</p>}
      {data && version && <>
        <dl className="ui:grid ui:grid-cols-[max-content_minmax(0,1fr)] ui:gap-x-4 ui:gap-y-2"><dt>{t('bomModel')}</dt><dd>{data.model.name}</dd><dt>{t('assetModelVersion')}</dt><dd>v{version.version}</dd><dt>{t('assetRecordVersion')}</dt><dd>v{data.asset_version}</dd><dt>{t('modelKind')}</dt><dd>{t('model_kind_' + data.model.kind.replaceAll('-', '_'))}</dd><dt>{t('status')}</dt><dd>{t('model_state_' + data.model.state)}</dd><dt>{t('assetModelVersionId')}</dt><dd className="mono-value ui:break-all">{version.id}</dd><dt>{t('modelsContentHash')}</dt><dd className="mono-value ui:break-all">{version.content_hash}</dd></dl>
        {data.model.latest_version > version.version && <p className="impact-notice">{t('assetModelNewer', { latest: data.model.latest_version, bound: version.version })}</p>}
        <ModelPanelPreview key={version.id} value={version.panel_layout} slots={version.slots} version={version.version} />
        <details><summary>{t('modelsCompletePayload')}</summary><pre className="ui:overflow-auto ui:whitespace-pre-wrap ui:break-words">{JSON.stringify({ hw_spec: version.hw_spec, expected_topology: version.expected_topology, capability_matrix: version.capability_matrix, panel_layout: version.panel_layout, bom_expect: version.bom_expect, default_role_plan: version.default_role_plan, slots: version.slots }, null, 2)}</pre></details>
      </>}
      <DialogFooter><Button type="button" variant="outline" onClick={onClose}>{t('assetModelClose')}</Button></DialogFooter>
    </DialogContent>
  </Dialog>
}
