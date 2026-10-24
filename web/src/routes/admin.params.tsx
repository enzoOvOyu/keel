import { createFileRoute } from '@tanstack/react-router'
import { GlobalCatalogPage } from '../features/catalog/CatalogPage'
export const Route = createFileRoute('/admin/params')({ component: GlobalCatalogPage })
