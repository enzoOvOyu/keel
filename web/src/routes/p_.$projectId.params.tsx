import { createFileRoute } from '@tanstack/react-router'
import { CatalogPage } from '../features/catalog/CatalogPage'
export const Route = createFileRoute('/p_/$projectId/params')({ component: Page })
function Page() { const { projectId } = Route.useParams(); return <CatalogPage projectId={projectId} /> }
