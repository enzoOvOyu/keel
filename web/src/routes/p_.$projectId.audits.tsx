import { createFileRoute } from '@tanstack/react-router'
import { AuditPage } from '../features/management/AuditPage'
import { managementSearch } from '../lib/management'
export const Route = createFileRoute('/p_/$projectId/audits')({ validateSearch: managementSearch, component: Page })
function Page() { const { projectId } = Route.useParams(); return <AuditPage projectId={projectId} /> }
