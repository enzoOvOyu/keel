import { createFileRoute } from '@tanstack/react-router'
import { UsersPage } from '../features/management/UsersPage'
import { managementSearch } from '../lib/management'
export const Route = createFileRoute('/admin/users')({ validateSearch: managementSearch, component: UsersPage })
