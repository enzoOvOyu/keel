import { createFileRoute } from '@tanstack/react-router'
import { AuditPage } from '../features/management/AuditPage'
import { managementSearch } from '../lib/management'
export const Route = createFileRoute('/admin/audits')({ validateSearch: managementSearch, component: () => <AuditPage /> })
