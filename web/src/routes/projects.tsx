import { createFileRoute } from '@tanstack/react-router'
import { managementSearch } from '../lib/management'
import { ProjectsPage } from '../features/projects/ProjectsPage'
export const Route = createFileRoute('/projects')({ validateSearch: managementSearch, component: ProjectsPage })
