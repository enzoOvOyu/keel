import { createFileRoute } from '@tanstack/react-router'
import { DashboardPage } from '../features/dashboard/DashboardPage'
import { managementSearch } from '../lib/management'
export const Route = createFileRoute('/p_/$projectId/dashboard')({ validateSearch: managementSearch, component: Page })
function Page() { const { projectId } = Route.useParams(); return <DashboardPage projectId={projectId} /> }
