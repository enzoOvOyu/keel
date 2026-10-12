import { createFileRoute } from '@tanstack/react-router'
import { MembersPage } from '../features/management/MembersPage'
import { managementSearch } from '../lib/management'
export const Route = createFileRoute('/p_/$projectId/members')({ validateSearch: managementSearch, component: Page })
function Page() { const { projectId } = Route.useParams(); return <MembersPage projectId={projectId} /> }
