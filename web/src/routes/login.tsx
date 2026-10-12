import { createFileRoute } from '@tanstack/react-router'
import { LoginPage } from '../features/auth/LoginPage'
export const Route = createFileRoute('/login')({
  validateSearch: (search: Record<string, unknown>): { next?: string; reason?: 'expired' | 'password_changed' | 'access_changed' } => ({
    ...(typeof search.next === 'string' ? { next: search.next } : {}),
    ...(['expired', 'password_changed', 'access_changed'].includes(String(search.reason)) ? { reason: search.reason as 'expired' | 'password_changed' | 'access_changed' } : {}),
  }),
  component: LoginPage,
})
