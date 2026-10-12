import type { components, paths } from '../generated/api'
export type Session = components['schemas']['Session']
export type Project = components['schemas']['ProjectSummary']
export type CreateProject = components['schemas']['CreateProjectRequest']
export type Login = components['schemas']['LoginRequest']
export type AuthProviders = paths['/api/v1/auth/providers']['get']['responses'][200]['content']['application/json']
type CSRF = paths['/api/v1/auth/csrf']['get']['responses'][200]['content']['application/json']
type ProjectList = paths['/api/v1/projects']['get']['responses'][200]['content']['application/json']

export class ApiError extends Error {
  constructor(public readonly status: number, public readonly code: string,
    public readonly traceId = '', public readonly retryAfter = 0) { super(code) }
}
async function request<T>(path: string, options: RequestInit = {}): Promise<T> {
  let response: Response
  try { response = await fetch(`/api/v1${path}`, { ...options, credentials: 'same-origin', cache: 'no-store',
    headers: { Accept: 'application/json', ...(options.body ? { 'Content-Type': 'application/json' } : {}), ...options.headers } }) }
  catch (error) { if (error instanceof DOMException && error.name === 'AbortError') throw error; throw new ApiError(0, 'NETWORK_ERROR') }
  if (!response.ok) {
    let error: Partial<components['schemas']['Error']> = {}
    try { error = await response.json() } catch { /* Non-JSON proxy failures remain safe generic errors. */ }
    const retryValue = response.headers.get('Retry-After') ?? ''
    const retryAfter = /^\d+$/.test(retryValue) ? Number(retryValue) : Math.ceil((Date.parse(retryValue) - Date.now()) / 1000)
    throw new ApiError(response.status, typeof error.code === 'string' ? error.code : 'HTTP_ERROR',
      typeof error.trace_id === 'string' ? error.trace_id : response.headers.get('X-Request-ID') ?? '',
      Number.isFinite(retryAfter) ? Math.max(0, retryAfter) : 0)
  }
  if (response.status === 204) return undefined as T
  try { return await response.json() as T } catch { throw new ApiError(502, 'INVALID_RESPONSE') }
}
export async function getAuthProviders(signal?: AbortSignal): Promise<AuthProviders> {
  const value = await request<AuthProviders>('/auth/providers', { signal })
  if (!value || !['ready', 'setup_required', 'not_configured'].includes(value.status)
    || !Array.isArray(value.methods) || value.methods.some((method) => method !== 'local')
    || (value.status === 'ready' && !value.methods.includes('local')) || typeof value.environment !== 'string') {
    throw new ApiError(502, 'INVALID_RESPONSE')
  }
  return value
}
export const getCSRF = () => request<CSRF>('/auth/csrf')
export const login = (body: Login, csrf: string) => request<Session>('/auth/login', {
  method: 'POST', headers: { 'X-CSRF-Token': csrf }, body: JSON.stringify(body),
})
export async function getSession(signal?: AbortSignal): Promise<Session | null> {
  try { return await request<Session>('/auth/session', { signal }) }
  catch (error) { if (error instanceof ApiError && error.status === 401) return null; throw error }
}
export const logout = (csrf: string) => request<void>('/auth/logout', { method: 'POST', headers: { 'X-CSRF-Token': csrf } })
export const listProjects = (signal: AbortSignal) => request<ProjectList>('/projects', { signal })
export const getProject = (id: string, signal: AbortSignal) => request<Project>(`/projects/${encodeURIComponent(id)}`, { signal })
export const createProject = (body: CreateProject, csrf: string, key: string) => request<Project>('/projects', {
  method: 'POST', headers: { 'X-CSRF-Token': csrf, 'Idempotency-Key': key }, body: JSON.stringify(body),
})

// Deliberately accept only implemented destinations. Encodings, queries,
// fragments, protocol-relative URLs and backslashes cannot pass this allowlist.
export function safeNext(value: string | undefined, session: Session): string {
  if (value === '/projects') return value
  const match = /^\/p\/([0-9a-f]{32})$/i.exec(value ?? '')
  return match && session.projects.some((project) => project.id === match[1]) ? value! : '/projects'
}
