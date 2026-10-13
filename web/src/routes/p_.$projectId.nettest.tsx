import { createFileRoute } from '@tanstack/react-router'
import { NettestPage } from '../features/nettest/NettestPage'
import { managementSearch } from '../lib/management'
export const Route = createFileRoute('/p_/$projectId/nettest')({ validateSearch: managementSearch, component: Page })
function Page() { const { projectId } = Route.useParams(); return <NettestPage projectId={projectId} /> }
