import { createFileRoute } from '@tanstack/react-router'
import { AccountPage } from '../features/management/AccountPage'
export const Route = createFileRoute('/account/security')({ component: AccountPage })
