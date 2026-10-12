import { createFileRoute } from '@tanstack/react-router'
import { ExecutionsPage } from '../features/execution/ExecutionsPage'
import { executionSearch } from '../lib/execution'
export const Route = createFileRoute('/p_/$projectId/executions')({ validateSearch: executionSearch, component: Page })
function Page() { const { projectId } = Route.useParams(); return <ExecutionsPage projectId={projectId} /> }
