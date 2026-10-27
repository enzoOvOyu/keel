import { createContext, useContext, useEffect } from 'react'

export const ServerDetailDrafts = createContext<((key: string, dirty: boolean) => void) | null>(null)

// Forms declare their actual unsaved state. Read-only filters do not block
// navigation, and unmounting a lazy tab removes only that form's registration.
export function useServerDetailDraft(key: string, dirty: boolean) {
 const update = useContext(ServerDetailDrafts)
 useEffect(() => {
  update?.(key, dirty)
  return () => update?.(key, false)
 }, [update, key, dirty])
}
