import { createFileRoute } from '@tanstack/react-router'
import { AgentPage } from '../features/agent/AgentPage'
import { managementSearch } from '../lib/management'
export const Route = createFileRoute('/p_/$projectId/agent')({ validateSearch: managementSearch, component: Page })
function Page() { const { projectId } = Route.useParams(); return <AgentPage projectId={projectId} /> }
