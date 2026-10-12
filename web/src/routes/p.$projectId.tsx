import { createFileRoute } from '@tanstack/react-router'
import { ProjectPage } from '../features/projects/ProjectPage'
export const Route = createFileRoute('/p/$projectId')({ component: ProjectPage })
