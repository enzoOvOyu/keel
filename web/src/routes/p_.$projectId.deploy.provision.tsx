import { createFileRoute } from '@tanstack/react-router'
import { ProvisionPage } from '../features/provision/ProvisionPage'
export const Route = createFileRoute('/p_/$projectId/deploy/provision')({component:Page})
function Page(){const {projectId}=Route.useParams();return <ProvisionPage projectId={projectId}/>}
