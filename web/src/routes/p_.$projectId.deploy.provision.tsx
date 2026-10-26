import { createFileRoute } from '@tanstack/react-router'
import { ProvisionPage } from '../features/provision/ProvisionPage'
export const Route = createFileRoute('/p_/$projectId/deploy/provision')({validateSearch:(search:Record<string, unknown>):{request?:string}=>({request:typeof search.request==='string'&&/^[a-f0-9]{32}$/.test(search.request)?search.request:undefined}),component:Page})
function Page(){const {projectId}=Route.useParams();return <ProvisionPage projectId={projectId}/>}
