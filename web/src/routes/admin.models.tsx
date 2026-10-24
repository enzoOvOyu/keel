import { createFileRoute } from '@tanstack/react-router'
import { GlobalModelsPage } from '../features/models/ModelsPage'
export const Route = createFileRoute('/admin/models')({ component: GlobalModelsPage })
