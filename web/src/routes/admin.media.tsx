import { createFileRoute } from '@tanstack/react-router'
import { GlobalMediaPage } from '../features/media/MediaPage'
export const Route = createFileRoute('/admin/media')({ component: GlobalMediaPage })
