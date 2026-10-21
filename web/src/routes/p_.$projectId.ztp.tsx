import { createFileRoute } from '@tanstack/react-router'
import { ZtpOnboardBoard } from '../features/ztp/ZtpOnboardBoard'
export const Route = createFileRoute('/p_/$projectId/ztp')({ component: Page })
function Page() { const { projectId } = Route.useParams(); return <ZtpOnboardBoard projectId={projectId} /> }
