import { createFileRoute } from '@tanstack/react-router'
import { LoginPage } from '../features/auth/LoginPage'
export const Route = createFileRoute('/login')({
  validateSearch: (search: Record<string, unknown>): { next?: string; reason?: 'expired' } => ({
    ...(typeof search.next === 'string' ? { next: search.next } : {}),
    ...(search.reason === 'expired' ? { reason: 'expired' as const } : {}),
  }),
  component: LoginPage,
})
