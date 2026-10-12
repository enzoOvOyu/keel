import { createFileRoute } from '@tanstack/react-router'
import { ModelsPage } from '../features/models/ModelsPage'
import { managementSearch } from '../lib/management'
export const Route = createFileRoute('/p_/$projectId/models')({ validateSearch: managementSearch, component: Page })
function Page() { const { projectId } = Route.useParams(); return <ModelsPage projectId={projectId} /> }
