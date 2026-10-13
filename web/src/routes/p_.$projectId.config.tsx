import { createFileRoute } from '@tanstack/react-router'
import { ConfigPage } from '../features/config/ConfigPage'
import { managementSearch } from '../lib/management'
export const Route = createFileRoute('/p_/$projectId/config')({ validateSearch: managementSearch, component: Page })
function Page() { const { projectId } = Route.useParams(); return <ConfigPage projectId={projectId} /> }
