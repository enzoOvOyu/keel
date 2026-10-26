import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { ActionError } from '../../components/Management'
import { getProvisionOSDependencies } from '../../lib/provision'
import { useExpireOnUnauthorized, useSession } from '../../lib/session'
import type { components } from '../../generated/api'

export function OSDependencies({projectId,mediaId}:{projectId:string;mediaId:string}) {
 const {t}=useTranslation(),session=useSession()
 const query=useQuery({queryKey:['private',session.user.id,'project',projectId,'provision-os-dependencies',mediaId],queryFn:({signal})=>getProvisionOSDependencies(projectId,mediaId,signal),retry:false,staleTime:10000,refetchOnWindowFocus:false})
 useExpireOnUnauthorized(query.error)
 return <section aria-label={t('osDriverDependencies')} className="ui:space-y-2"><h4>{t('osDriverDependencies')}</h4><p className="cell-subtitle">{t('osDriverDependenciesHint')}</p>{query.isPending && <p role="status">{t('loadingRecords')}</p>}<ActionError error={query.error}/>{query.data && !query.error && <>{query.data.compatibility && <Matrix value={query.data.compatibility}/>}<ul>{query.data.drivers.map(driver=><li key={driver.media_id}><strong>{driver.name} · {driver.version}</strong> · {driver.arch}<p className="mono-value ui:break-all">{driver.sha256}</p>{driver.compatibility?<Matrix value={driver.compatibility}/>:<p className="cell-subtitle">{t('osDriverMatrixMissing')}</p>}</li>)}</ul>{!query.data.drivers.length && <p role="status">{t('osDriverDependenciesEmpty')}</p>}</>}</section>
}
function Matrix({value}:{value:components['schemas']['CompatibilityMatrix']}) {
 const {t}=useTranslation()
 return <details><summary>{t('osDriverMatrixSource')}: {value.source} · {value.version}</summary><p className="mono-value ui:break-all">{value.report_ref}</p>{value.requires && <dl>{Object.entries(value.requires).map(([key,values])=><div key={key}><dt>{key}</dt><dd>{values.join(' · ')}</dd></div>)}</dl>}{value.combinations && <><p>{t('osDriverMatrixCombinations')}</p><ol>{value.combinations.map((row,index)=><li key={index}>{Object.entries(row).map(([key,value])=>`${key}=${value}`).join(' · ')}</li>)}</ol></>}</details>
}
