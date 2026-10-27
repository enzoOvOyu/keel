import { createFileRoute } from '@tanstack/react-router'
import { GatesPage } from '../features/gates/GatesPage'
export const Route = createFileRoute('/p_/$projectId/gates')({ component: Page })
function Page() { const { projectId } = Route.useParams(); return <GatesPage projectId={projectId} /> }
