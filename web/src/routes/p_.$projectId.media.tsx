import { createFileRoute } from '@tanstack/react-router'
import { MediaPage } from '../features/media/MediaPage'
import { managementSearch } from '../lib/management'
export const Route = createFileRoute('/p_/$projectId/media')({ validateSearch: managementSearch, component: Page })
function Page() { const { projectId } = Route.useParams(); return <MediaPage projectId={projectId} /> }
