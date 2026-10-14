import { createFileRoute } from '@tanstack/react-router'
import { DriverPage } from '../features/driver/DriverPage'
import { managementSearch } from '../lib/management'
export const Route = createFileRoute('/p_/$projectId/driver')({ validateSearch: managementSearch, component: Page })
function Page() { const { projectId } = Route.useParams(); return <DriverPage projectId={projectId} /> }
