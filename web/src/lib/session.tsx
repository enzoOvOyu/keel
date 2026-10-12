import { createContext, useContext, useEffect, useState, type ReactNode } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { ApiError, getSession, type Session } from './api'

const SessionContext = createContext<Session | null>(null)
export const sessionKey = ['private', 'session'] as const
export function useAuthSession(enabled = true) {
  return useQuery({ queryKey: sessionKey, queryFn: ({ signal }) => getSession(signal), enabled,
    staleTime: 0, retry: false, refetchOnWindowFocus: true, refetchOnMount: 'always' })
}
export function useSession() {
  const session = useContext(SessionContext)
  if (!session) throw new Error('An authenticated workspace is required')
  return session
}
export function useEndSession() {
  const client = useQueryClient()
  const navigate = useNavigate()
  return async (expired = true, notice?: 'password_changed' | 'access_changed') => {
    await client.cancelQueries()
    client.clear()
    client.setQueryData(sessionKey, null)
    const next = window.location.pathname
    await navigate({ to: '/login', search: { ...(notice ? { reason: notice } : expired ? { reason: 'expired' as const, next } : {}) }, replace: true })
  }
}
export function SessionBoundary({ children }: { children: ReactNode }) {
  const auth = useAuthSession()
  const endSession = useEndSession()
  const { t } = useTranslation()
  const [verifiedUserId, setVerifiedUserId] = useState<string | null>(null)
  // A mounted workspace must verify its identity once before revealing data.
  // Later checks for the same identity update context without destroying drafts.
  useEffect(() => {
    if (auth.isSuccess && !auth.isFetching && auth.data) setVerifiedUserId(auth.data.user.id)
  }, [auth.isSuccess, auth.isFetching, auth.data?.user.id])
  const verifiedIdentity = !!auth.data && verifiedUserId === auth.data.user.id
  useEffect(() => { if (auth.isSuccess && !auth.data) void endSession() }, [auth.isSuccess, auth.data])
  useEffect(() => {
    if (!auth.data) return
    const remaining = Date.parse(auth.data.expires_at) - Date.now()
    const timer = window.setTimeout(() => void auth.refetch(), Math.max(1000, Math.min(remaining, 2_147_000_000)))
    return () => window.clearTimeout(timer)
  }, [auth.data?.expires_at])
  if (auth.isError && !verifiedIdentity) return <main className="workspace-loading" role="alert"><h1>{t('sessionUnavailable')}</h1><p>{t('tryAgainDetail')}</p><button className="primary-button" onClick={() => void auth.refetch()}>{t('retry')}</button></main>
  if (!verifiedIdentity || !auth.data) return <main className="workspace-loading" role="status">{t('checkingSession')}</main>
  return <>
    {auth.isError && <div className="workspace-banner" role="status">{t('sessionUnavailable')} <button className="text-button" onClick={() => void auth.refetch()}>{t('retry')}</button></div>}
    <SessionContext.Provider value={auth.data}>{children}</SessionContext.Provider>
  </>
}
export function useExpireOnUnauthorized(error: unknown) {
  const endSession = useEndSession()
  useEffect(() => { if (error instanceof ApiError && error.status === 401) void endSession() }, [error])
}
