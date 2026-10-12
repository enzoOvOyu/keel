// Only display preferences are persisted. Never store authentication tokens here.
export function readPreference(key: 'keel.theme' | 'keel.language'): string | null {
  try { return window.localStorage.getItem(key) } catch { return null }
}
export function writePreference(key: 'keel.theme' | 'keel.language', value: string): void {
  try { window.localStorage.setItem(key, value) } catch { /* Optional preference storage. */ }
}
