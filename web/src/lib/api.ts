// Types generated from the implemented public OpenAPI contract.
import type { paths } from '../generated/api'
export type AuthProviders = paths['/api/v1/auth/providers']['get']['responses'][200]['content']['application/json']
export class PublicApiError extends Error {
  constructor(public readonly kind: 'http' | 'contract') { super(kind) }
}
export async function getAuthProviders(signal: AbortSignal): Promise<AuthProviders> {
  const response = await fetch('/api/v1/auth/providers', {
    method: 'GET', signal, credentials: 'same-origin', headers: { Accept: 'application/json' },
  })
  if (!response.ok) throw new PublicApiError('http')
  const value: unknown = await response.json()
  if (!value || typeof value !== 'object' || !('status' in value) || !('methods' in value)
    || value.status !== 'not_configured' || !Array.isArray(value.methods) || value.methods.length !== 0) {
    throw new PublicApiError('contract')
  }
  return { status: 'not_configured', methods: [] }
}
