import { createFileRoute } from '@tanstack/react-router'
import { MonitorPage } from '../features/monitor/MonitorPage'
export const Route = createFileRoute('/p_/$projectId/monitor')({ component: Page })
function Page() { const { projectId } = Route.useParams(); return <MonitorPage projectId={projectId} /> }
