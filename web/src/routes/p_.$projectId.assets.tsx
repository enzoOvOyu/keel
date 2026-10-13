import { createFileRoute } from '@tanstack/react-router'
import { AssetsPage } from '../features/assets/AssetsPage'
import { managementSearch } from '../lib/management'
export const Route = createFileRoute('/p_/$projectId/assets')({ validateSearch: managementSearch, component: Page })
function Page() { const { projectId } = Route.useParams(); return <AssetsPage projectId={projectId} /> }
